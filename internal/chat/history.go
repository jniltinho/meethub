package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
)

// Page sizes for history, catch-up and threads.
const (
	DefaultPerPage = 60
	MaxPerPage     = 200
)

func perPage(n int) int {
	if n <= 0 {
		return DefaultPerPage
	}
	return min(n, MaxPerPage)
}

// readableChannel loads a channel the caller may read.
func (s *Service) readableChannel(ctx context.Context, tx *gorm.DB, sub Subject, channelID string) (*Channel, error) {
	var ch Channel
	if err := tx.WithContext(ctx).Where("id = ?", channelID).Take(&ch).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read channel: %w", err)
	}
	ch.trimIDs()
	a, _, err := accessTx(ctx, tx, sub, &ch)
	if err != nil {
		return nil, err
	}
	if !CanRead(sub, a) {
		return nil, ReadError(sub, a)
	}
	return &ch, nil
}

// views projects posts with their reactions (tombstones have none).
func views(ctx context.Context, tx *gorm.DB, posts []Post) ([]PostView, error) {
	ids := make([]string, 0, len(posts))
	for i := range posts {
		posts[i].trimIDs()
		if posts[i].DeleteAt == 0 {
			ids = append(ids, posts[i].ID)
		}
	}
	reactions, err := reactionsForPosts(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]PostView, len(posts))
	for i := range posts {
		out[i] = posts[i].View()
		if posts[i].DeleteAt == 0 {
			out[i].Reactions = reactions[posts[i].ID]
		}
	}
	return out, nil
}

func parseCursor(cursor string) (int64, string, error) {
	at, id, ok := strings.Cut(cursor, ":")
	n, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || id == "" {
		return 0, "", ErrInvalidInput
	}
	return n, id, nil
}

// History pages a channel's messages backwards (newest first) by an opaque
// cursor over (create_at, id); next is "" on the last page.
func (s *Service) History(ctx context.Context, sub Subject, channelID, cursor string, limit int) ([]PostView, string, error) {
	limit = perPage(limit)
	tx := s.DB.Gorm.WithContext(ctx)
	if _, err := s.readableChannel(ctx, tx, sub, channelID); err != nil {
		return nil, "", err
	}
	q := tx.Where("channel_id = ?", channelID)
	if cursor != "" {
		at, id, err := parseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		q = q.Where("(create_at < ? OR (create_at = ? AND id < ?))", at, at, id)
	}
	var posts []Post
	if err := q.Order("create_at DESC, id DESC").Limit(limit + 1).Find(&posts).Error; err != nil {
		return nil, "", fmt.Errorf("history: %w", err)
	}
	next := ""
	if len(posts) > limit {
		posts = posts[:limit]
		last := posts[limit-1]
		next = fmt.Sprintf("%d:%s", last.CreateAt, strings.TrimSpace(last.ID))
	}
	out, err := views(ctx, tx, posts)
	return out, next, err
}

// Changes is a catch-up page.
type Changes struct {
	Posts        []PostView `json:"posts"`
	NextRevision int64      `json:"next_revision"`
	HasMore      bool       `json:"has_more"`
}

// ChangesSince returns, in revision order, every message whose revision is
// greater than since (tombstones and re-stamped roots included). Pages never
// split a revision (a thread deletion stamps all its posts with one), so
// paging with next_revision is lossless; commit order equals revision order,
// so concurrent writes only add later revisions.
func (s *Service) ChangesSince(ctx context.Context, sub Subject, channelID string, since int64, limit int) (*Changes, error) {
	limit = perPage(limit)
	tx := s.DB.Gorm.WithContext(ctx)
	if _, err := s.readableChannel(ctx, tx, sub, channelID); err != nil {
		return nil, err
	}
	var posts []Post
	if err := tx.Where("channel_id = ? AND revision > ?", channelID, since).
		Order("revision, id").Limit(limit + 1).Find(&posts).Error; err != nil {
		return nil, fmt.Errorf("changes: %w", err)
	}
	out := &Changes{NextRevision: since}
	if len(posts) > limit {
		out.HasMore = true
		last := posts[limit-1].Revision
		if posts[limit].Revision == last { // complete the last revision
			var rest []Post
			if err := tx.Where("channel_id = ? AND revision = ?", channelID, last).Order("id").Find(&rest).Error; err != nil {
				return nil, fmt.Errorf("changes: %w", err)
			}
			kept := posts[:0]
			for _, p := range posts {
				if p.Revision < last {
					kept = append(kept, p)
				}
			}
			posts = append(kept, rest...)
		} else {
			posts = posts[:limit]
		}
	}
	if len(posts) > 0 {
		out.NextRevision = posts[len(posts)-1].Revision
	}
	var err error
	out.Posts, err = views(ctx, tx, posts)
	return out, err
}

// Thread returns a root message and its replies in creation order, paged
// forward by cursor (the root comes with the first page).
func (s *Service) Thread(ctx context.Context, sub Subject, postID, cursor string, limit int) ([]PostView, string, error) {
	limit = perPage(limit)
	tx := s.DB.Gorm.WithContext(ctx)
	var root Post
	if err := tx.Where("id = ?", postID).Take(&root).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("thread: %w", err)
	}
	root.trimIDs()
	if root.RootID != "" {
		return s.Thread(ctx, sub, root.RootID, cursor, limit)
	}
	if _, err := s.readableChannel(ctx, tx, sub, root.ChannelID); err != nil {
		return nil, "", err
	}
	q := tx.Where("root_id = ?", root.ID)
	var posts []Post
	if cursor != "" {
		at, id, err := parseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		q = q.Where("(create_at > ? OR (create_at = ? AND id > ?))", at, at, id)
	} else {
		posts = append(posts, root)
	}
	var replies []Post
	if err := q.Order("create_at, id").Limit(limit + 1).Find(&replies).Error; err != nil {
		return nil, "", fmt.Errorf("thread: %w", err)
	}
	next := ""
	if len(replies) > limit {
		replies = replies[:limit]
		last := replies[limit-1]
		next = fmt.Sprintf("%d:%s", last.CreateAt, strings.TrimSpace(last.ID))
	}
	out, err := views(ctx, tx, append(posts, replies...))
	return out, next, err
}
