//go:build searchbench

package chat_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// TestSearchAtScale measures search over 500 000 messages with the scan (as before the index) and with the index,
// on each database (add-native-message-search task 2.3). Run with:
//
//	go test -tags searchbench -run TestSearchAtScale -v -timeout 60m ./internal/chat
func TestSearchAtScale(t *testing.T) {
	const total = 500_000
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w, ch := postWorld(t, db)
		rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // reproducible data, not security
		vocab := make([]string, 5000)
		for i := range vocab {
			vocab[i] = fmt.Sprintf("w%04dx", i)
		}
		start := time.Now()
		base := time.Now().Add(-365 * 24 * time.Hour).UnixMilli()
		const batch = 1000
		for b := 0; b < total/batch; b++ {
			posts := make([]chat.Post, batch)
			idx := make([]string, 0, 2*batch)
			rows := make([]string, 0, batch)
			for i := range posts {
				n := b*batch + i
				words := make([]string, 12)
				for j := range words {
					words[j] = vocab[rng.IntN(len(vocab))]
				}
				if n%50_000 == 0 {
					words[3] = "needlequartz" // 10 messages
				}
				if n%10 == 0 {
					words[5] = "deployment" // 50 000 messages
				}
				msg := strings.Join(words, " ")
				at := base + int64(n)*60
				posts[i] = chat.Post{
					ID: fmt.Sprintf("01BENCH%019d", n), ChannelID: ch.ID, UserID: w.ana.UserID, Message: msg, Type: "",
					CreateAt: at, UpdateAt: at,
				}
				rows = append(rows, "(?, ?)")
				idx = append(idx, posts[i].ID, " "+chat.SearchText(msg)+" ")
			}
			args := make([]any, len(idx))
			for i, v := range idx {
				args[i] = v
			}
			tx := db.Gorm.Begin()
			if err := tx.CreateInBatches(posts, 250).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec("INSERT INTO post_search (post_id, search_text) VALUES "+strings.Join(rows, ", "), args...).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: seeded %d messages in %s", db.Dialect, total, time.Since(start).Round(time.Second))
		// as a running database would have them: statistics up to date
		analyze := map[string]string{"sqlite": "ANALYZE", "mariadb": "ANALYZE TABLE posts, post_search, channel_members, channels", "postgres": "ANALYZE"}[db.Dialect]
		if err := db.Gorm.Exec(analyze).Error; err != nil {
			t.Fatal(err)
		}
		measure := func(label string, svc *chat.Service, q string) {
			t.Helper()
			best := time.Duration(1<<63 - 1)
			var n int
			for range 5 {
				s := time.Now()
				res, err := svc.Search(ctx, w.ana, q, 0, 20)
				if err != nil {
					t.Fatal(err)
				}
				best, n = min(best, time.Since(s)), len(res.Posts)
			}
			t.Logf("%s %s %q: best of 5 %s (%d results on the first page)", db.Dialect, label, q, best.Round(time.Microsecond), n)
		}
		indexed := &chat.Service{DB: db}
		for _, q := range []string{"needlequartz", "deployment", "w0042x deployment", `"w0042x w0043x"`} {
			measure("index", indexed, q)
		}
		if err := db.Gorm.Exec("UPDATE search_backfill SET done = 0 WHERE name = 'posts'").Error; err != nil {
			t.Fatal(err)
		}
		scan := &chat.Service{DB: db}
		for _, q := range []string{"needlequartz", "deployment", "w0042x deployment", `"w0042x w0043x"`} {
			measure("scan", scan, q)
		}
	})
}
