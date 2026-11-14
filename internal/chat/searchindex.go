package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/jobs"
)

// TaskSearchBackfill indexes the messages stored before the search index existed.
const TaskSearchBackfill = "search:backfill"

// searchBackfillBatch is how many messages one step of the backfill indexes.
const searchBackfillBatch = 1000

// searchState caches what search learns once per process: that the backfill
// is done (it never goes back while the server runs) and MariaDB's FULLTEXT
// limits.
type searchState struct {
	mu        sync.Mutex
	indexed   bool
	ftLoaded  bool
	ftMinLen  int
	stopwords map[string]bool
}

// indexPost writes a message's search text in the transaction that stores or
// edits it (design D2); a message without text leaves the index.
func indexPost(ctx context.Context, tx *gorm.DB, dialect, postID, message string) error {
	text := SearchText(message)
	if text == "" {
		return unindexPosts(ctx, tx, []string{postID})
	}
	// padded with spaces, so a LIKE '% word%' finds the beginning of any word (the MariaDB fallback)
	padded := " " + text + " "
	upsert := `INSERT INTO post_search (post_id, search_text) VALUES (?, ?)
		ON CONFLICT (post_id) DO UPDATE SET search_text = excluded.search_text`
	if dialect == config.DriverMariaDB {
		upsert = `INSERT INTO post_search (post_id, search_text) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE search_text = VALUES(search_text)`
	}
	if err := tx.WithContext(ctx).Exec(upsert, postID, padded).Error; err != nil {
		return fmt.Errorf("index post: %w", err)
	}
	return nil
}

// unindexPosts removes messages from the index (deleted messages are never found).
func unindexPosts(ctx context.Context, tx *gorm.DB, postIDs []string) error {
	for chunk := range slices.Chunk(postIDs, 500) {
		if err := tx.WithContext(ctx).Exec(`DELETE FROM post_search WHERE post_id IN ?`, chunk).Error; err != nil {
			return fmt.Errorf("unindex posts: %w", err)
		}
	}
	return nil
}

// searchIndexed reports whether every message is in the index; until then
// search scans the messages (design D3).
func (s *Service) searchIndexed(ctx context.Context) (bool, error) {
	s.search.mu.Lock()
	defer s.search.mu.Unlock()
	if s.search.indexed {
		return true, nil
	}
	var done []int
	if err := s.DB.Gorm.WithContext(ctx).Raw(`SELECT done FROM search_backfill WHERE name = 'posts'`).Scan(&done).Error; err != nil {
		return false, fmt.Errorf("search index state: %w", err)
	}
	s.search.indexed = len(done) == 1 && done[0] == 1
	return s.search.indexed, nil
}

// BackfillSearch indexes the messages stored before the index existed, in
// batches by id, recording its progress so a restart continues where it
// stopped; it returns whether everything is indexed. Messages written since
// the migration are indexed by their own transaction and left as they are.
func (s *Service) BackfillSearch(ctx context.Context, deadline time.Time) (bool, error) {
	for time.Now().Before(deadline) {
		done, err := s.backfillStep(ctx)
		if err != nil || done {
			return done, err
		}
	}
	return false, nil
}

func (s *Service) backfillStep(ctx context.Context) (bool, error) {
	done := false
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state struct {
			LastID string
			Done   int
		}
		lock := " FOR UPDATE" // two replicas take turns (SQLite: the write gate already does)
		if s.DB.Dialect == config.DriverSQLite {
			lock = ""
		}
		if err := tx.Raw(`SELECT last_id, done FROM search_backfill WHERE name = 'posts'` + lock).Scan(&state).Error; err != nil {
			return fmt.Errorf("search backfill state: %w", err)
		}
		if state.Done == 1 {
			done = true
			return nil
		}
		var posts []Post
		if err := tx.Select("id", "message").Where("id > ? AND delete_at = 0", strings.TrimSpace(state.LastID)).
			Order("id").Limit(searchBackfillBatch).Find(&posts).Error; err != nil {
			return fmt.Errorf("search backfill read: %w", err)
		}
		rows, args := make([]string, 0, len(posts)), make([]any, 0, 2*len(posts))
		for _, p := range posts {
			if text := SearchText(p.Message); text != "" {
				rows = append(rows, "(?, ?)")
				args = append(args, strings.TrimSpace(p.ID), " "+text+" ")
			}
		}
		if len(rows) > 0 {
			insert := `INSERT INTO post_search (post_id, search_text) VALUES ` + strings.Join(rows, ", ") + ` ON CONFLICT (post_id) DO NOTHING`
			if s.DB.Dialect == config.DriverMariaDB {
				insert = `INSERT IGNORE INTO post_search (post_id, search_text) VALUES ` + strings.Join(rows, ", ")
			}
			if err := tx.Exec(insert, args...).Error; err != nil {
				return fmt.Errorf("search backfill write: %w", err)
			}
		}
		last := strings.TrimSpace(state.LastID)
		if len(posts) > 0 {
			last = strings.TrimSpace(posts[len(posts)-1].ID)
		}
		done = len(posts) < searchBackfillBatch
		flag := 0
		if done {
			flag = 1
		}
		return tx.Exec(`UPDATE search_backfill SET last_id = ?, done = ? WHERE name = 'posts'`, last, flag).Error
	})
	return done, err
}

// SearchBackfillTask runs the backfill every minute until it is done (at most
// one replica at a time: the period claim), then does nothing.
func (s *Service) SearchBackfillTask() jobs.TaskType {
	return jobs.TaskType{
		Name:        TaskSearchBackfill,
		MaxAttempts: 3,
		Period:      time.Minute,
		Timeout:     10 * time.Minute,
		Handler: func(ctx context.Context, _ *asynq.Task) error {
			if ok, err := s.searchIndexed(ctx); ok || err != nil {
				return err
			}
			_, err := s.BackfillSearch(ctx, time.Now().Add(9*time.Minute))
			return err
		},
	}
}

// mariadbFulltext reads, once, the FULLTEXT index's minimum word length and
// stop words: such words are not in the index and are matched with LIKE.
func (s *Service) mariadbFulltext(ctx context.Context) (int, map[string]bool, error) {
	s.search.mu.Lock()
	defer s.search.mu.Unlock()
	if s.search.ftLoaded {
		return s.search.ftMinLen, s.search.stopwords, nil
	}
	tx := s.DB.Gorm.WithContext(ctx)
	var vars struct {
		MinLen    int
		Stopwords int
		Table     *string
	}
	if err := tx.Raw(`SELECT @@innodb_ft_min_token_size AS min_len, @@innodb_ft_enable_stopword AS stopwords,
		@@innodb_ft_server_stopword_table AS ` + "`table`").Scan(&vars).Error; err != nil {
		return 0, nil, fmt.Errorf("fulltext settings: %w", err)
	}
	stop := map[string]bool{}
	if vars.Stopwords != 0 {
		from := "INFORMATION_SCHEMA.INNODB_FT_DEFAULT_STOPWORD"
		if vars.Table != nil && *vars.Table != "" {
			// "db/table", as the server names it
			db, table, _ := strings.Cut(*vars.Table, "/")
			from = "`" + strings.ReplaceAll(db, "`", "``") + "`.`" + strings.ReplaceAll(table, "`", "``") + "`"
		}
		var words []string
		if err := tx.Raw(`SELECT value FROM ` + from).Scan(&words).Error; err != nil {
			return 0, nil, fmt.Errorf("fulltext stop words: %w", err)
		}
		for _, w := range words {
			stop[SearchText(w)] = true
		}
	}
	s.search.ftLoaded, s.search.ftMinLen, s.search.stopwords = true, max(vars.MinLen, 1), stop
	return s.search.ftMinLen, stop, nil
}

// matchIndex adds the index conditions for the terms to a query that joins
// post_search as s (design D1): every term a word or the beginning of a word,
// a term of several words those words in sequence.
func (s *Service) matchIndex(ctx context.Context, sel *gorm.DB, terms [][]string) (*gorm.DB, error) {
	switch s.DB.Dialect {
	case config.DriverPostgres:
		parts := make([]string, len(terms))
		for i, words := range terms {
			if len(words) == 1 {
				parts[i] = words[0] + ":*"
			} else {
				parts[i] = "(" + strings.Join(words, " <-> ") + ")"
			}
		}
		return sel.Where("s.tsv @@ to_tsquery('simple', ?)", strings.Join(parts, " & ")), nil
	case config.DriverMariaDB:
		minLen, stop, err := s.mariadbFulltext(ctx)
		if err != nil {
			return nil, err
		}
		indexable := func(w string) bool { return len([]rune(w)) >= minLen && !stop[w] }
		var parts []string
		for _, words := range terms {
			if !slices.ContainsFunc(words, func(w string) bool { return !indexable(w) }) {
				if len(words) == 1 {
					parts = append(parts, "+"+words[0]+"*")
				} else {
					parts = append(parts, `+"`+strings.Join(words, " ")+`"`)
				}
				continue
			}
			// a word the index leaves out: LIKE on the same padded text
			pattern := "% " + strings.Join(words, " ") + "%"
			if len(words) > 1 {
				pattern = "% " + strings.Join(words, " ") + " %"
			}
			sel = sel.Where("s.search_text LIKE ?", pattern)
		}
		if len(parts) > 0 {
			sel = sel.Where("MATCH (s.search_text) AGAINST (? IN BOOLEAN MODE)", strings.Join(parts, " "))
		}
		return sel, nil
	default:
		parts := make([]string, len(terms))
		for i, words := range terms {
			if len(words) == 1 {
				parts[i] = `"` + words[0] + `"*`
			} else {
				parts[i] = `"` + strings.Join(words, " ") + `"`
			}
		}
		return sel.Joins("JOIN post_search_fts f ON f.rowid = s.rid").Where("post_search_fts MATCH ?", strings.Join(parts, " ")), nil
	}
}
