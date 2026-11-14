package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/jobs"
)

// Activity kinds (activity spec).
const (
	ActivityMention = "mention" // the message mentions the person by name
	ActivityReply   = "reply"   // a reply in a thread the person started or wrote in
)

// ActivityRetention is how long activity items are kept.
const ActivityRetention = 90 * 24 * time.Hour

// TaskActivityCleanup is the daily activity purge task type.
const TaskActivityCleanup = "activity:cleanup"

// ActivityItem is one message addressed to one person (design D1).
type ActivityItem struct {
	UserID    string `gorm:"primaryKey;size:26"`
	PostID    string `gorm:"primaryKey;size:26"`
	ChannelID string `gorm:"size:26"`
	Kind      string `gorm:"size:16"`
	CreateAt  int64  `gorm:"autoCreateTime:false"`
}

// TableName maps ActivityItem to activity_items.
func (ActivityItem) TableName() string { return "activity_items" }

// ActivityView is an item of the caller's list: the message and its channel.
type ActivityView struct {
	Kind     string        `json:"kind"`
	CreateAt int64         `json:"create_at"`
	Post     PostView      `json:"post"`
	Channel  ActivityPlace `json:"channel"`
}

// ActivityPlace names the channel of an item (the SPA fills the rest from its store).
type ActivityPlace struct {
	ID          string `json:"id"`
	TeamID      string `json:"team_id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// recordActivity writes the items of a new message inside the posting
// transaction: one per member mentioned by name and, for a reply, one per
// earlier author of the thread who is still a member; never for the author,
// one per person (a mention wins). Conversations have their own unread badges
// and get none. It returns the people who got an item.
func recordActivity(ctx context.Context, tx *gorm.DB, p *Post, ch *Channel, named []string) ([]string, error) {
	if ch.Type == ChannelDirect || ch.Type == ChannelGroup {
		return nil, nil
	}
	kinds := map[string]string{}
	if p.RootID != "" {
		var authors []string
		if err := tx.WithContext(ctx).Table("posts p").
			Joins("JOIN channel_members m ON m.channel_id = p.channel_id AND m.user_id = p.user_id").
			Where("(p.id = ? OR p.root_id = ?) AND p.delete_at = 0 AND p.id <> ?", p.RootID, p.RootID, p.ID).
			Distinct("p.user_id").Pluck("p.user_id", &authors).Error; err != nil {
			return nil, fmt.Errorf("thread authors: %w", err)
		}
		for _, id := range trimAll(authors) {
			kinds[id] = ActivityReply
		}
	}
	for _, id := range named {
		kinds[id] = ActivityMention
	}
	delete(kinds, p.UserID)
	if len(kinds) == 0 {
		return nil, nil
	}
	rows := make([]ActivityItem, 0, len(kinds))
	for id, kind := range kinds {
		rows = append(rows, ActivityItem{UserID: id, PostID: p.ID, ChannelID: ch.ID, Kind: kind, CreateAt: p.CreateAt})
	}
	slices.SortFunc(rows, func(a, b ActivityItem) int { return strings.Compare(a.UserID, b.UserID) })
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200).Error; err != nil {
		return nil, fmt.Errorf("record activity: %w", err)
	}
	users := make([]string, len(rows))
	for i, r := range rows {
		users[i] = r.UserID
	}
	return users, nil
}

// readableItems selects the items a person can still read: of a channel they
// are a member of, of a message that is not deleted.
func readableItems(tx *gorm.DB) *gorm.DB {
	return tx.Table("activity_items a").
		Joins("JOIN channel_members m ON m.channel_id = a.channel_id AND m.user_id = a.user_id").
		Joins("JOIN posts p ON p.id = a.post_id AND p.delete_at = 0")
}

// unseenCounts counts each person's readable items newer than their last visit.
func unseenCounts(ctx context.Context, tx *gorm.DB, users []string) (map[string]int64, error) {
	out := make(map[string]int64, len(users))
	for _, id := range users {
		out[id] = 0
	}
	for chunk := range slices.Chunk(users, 500) {
		var rows []struct {
			UserID string
			N      int64
		}
		if err := readableItems(tx.WithContext(ctx)).Joins("JOIN users u ON u.id = a.user_id").
			Where("a.user_id IN ? AND a.create_at > u.activity_seen_at", chunk).
			Select("a.user_id AS user_id, COUNT(*) AS n").Group("a.user_id").Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("count activity: %w", err)
		}
		for _, r := range rows {
			out[strings.TrimSpace(r.UserID)] = r.N
		}
	}
	return out, nil
}

// emitActivity adds activity_updated with each person's count, to that person only.
func emitActivity(ctx context.Context, tx *gorm.DB, em *emitter, users []string) error {
	if len(users) == 0 {
		return nil
	}
	counts, err := unseenCounts(ctx, tx, users)
	if err != nil {
		return err
	}
	for _, id := range users {
		em.add("activity_updated", map[string]any{"unseen": counts[id]}, []string{id})
	}
	return nil
}

// removeActivity deletes the items of deleted messages and returns whose they were.
func removeActivity(ctx context.Context, tx *gorm.DB, postIDs []string) ([]string, error) {
	var users []string
	if err := tx.WithContext(ctx).Model(&ActivityItem{}).Where("post_id IN ?", postIDs).
		Distinct("user_id").Pluck("user_id", &users).Error; err != nil {
		return nil, fmt.Errorf("read activity: %w", err)
	}
	if err := tx.WithContext(ctx).Where("post_id IN ?", postIDs).Delete(&ActivityItem{}).Error; err != nil {
		return nil, fmt.Errorf("delete activity: %w", err)
	}
	return trimAll(users), nil
}

// Activity pages the caller's readable items, newest first, by an opaque
// cursor ("" on the last page), with their unseen count.
func (s *Service) Activity(ctx context.Context, sub Subject, cursor string, limit int) ([]ActivityView, string, int64, error) {
	limit = perPage(limit)
	tx := s.DB.Gorm.WithContext(ctx)
	sel := readableItems(tx).Where("a.user_id = ?", sub.UserID)
	if cursor != "" {
		at, id, err := parseCursor(cursor)
		if err != nil {
			return nil, "", 0, err
		}
		sel = sel.Where("(a.create_at < ? OR (a.create_at = ? AND a.post_id < ?))", at, at, id)
	}
	var items []ActivityItem
	if err := sel.Select("a.*").Order("a.create_at DESC, a.post_id DESC").Limit(limit + 1).Scan(&items).Error; err != nil {
		return nil, "", 0, fmt.Errorf("list activity: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = fmt.Sprintf("%d:%s", items[limit-1].CreateAt, strings.TrimSpace(items[limit-1].PostID))
	}
	postIDs, channelIDs := make([]string, len(items)), make([]string, 0, len(items))
	for i := range items {
		items[i].PostID, items[i].ChannelID = strings.TrimSpace(items[i].PostID), strings.TrimSpace(items[i].ChannelID)
		postIDs[i] = items[i].PostID
		if !slices.Contains(channelIDs, items[i].ChannelID) {
			channelIDs = append(channelIDs, items[i].ChannelID)
		}
	}
	var (
		posts    []Post
		channels []Channel
	)
	if len(items) > 0 {
		if err := tx.Where("id IN ?", postIDs).Find(&posts).Error; err != nil {
			return nil, "", 0, fmt.Errorf("read posts: %w", err)
		}
		if err := tx.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
			return nil, "", 0, fmt.Errorf("read channels: %w", err)
		}
	}
	pv, err := views(ctx, tx, posts)
	if err != nil {
		return nil, "", 0, err
	}
	byPost := make(map[string]PostView, len(pv))
	for _, v := range pv {
		byPost[v.ID] = v
	}
	byChannel := make(map[string]ActivityPlace, len(channels))
	for i := range channels {
		c := &channels[i]
		c.trimIDs()
		byChannel[c.ID] = ActivityPlace{ID: c.ID, TeamID: c.TeamID, Type: c.Type, Name: c.Name, DisplayName: c.DisplayName}
	}
	out := make([]ActivityView, len(items))
	for i, it := range items {
		out[i] = ActivityView{Kind: it.Kind, CreateAt: it.CreateAt, Post: byPost[it.PostID], Channel: byChannel[it.ChannelID]}
	}
	counts, err := unseenCounts(ctx, tx, []string{sub.UserID})
	if err != nil {
		return nil, "", 0, err
	}
	return out, next, counts[sub.UserID], nil
}

// SeeActivity records the caller's visit to Activity: nothing before now is
// unseen. Their connections receive activity_updated (other tabs clear).
func (s *Service) SeeActivity(ctx context.Context, sub Subject) error {
	var em emitter
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE users SET activity_seen_at = ? WHERE id = ? AND activity_seen_at < ?`,
			s.now(), sub.UserID, s.now()).Error; err != nil {
			return fmt.Errorf("see activity: %w", err)
		}
		return emitActivity(ctx, tx, &em, []string{sub.UserID})
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}

// PurgeActivity deletes the items older than ActivityRetention and returns how many.
func PurgeActivity(ctx context.Context, db *gorm.DB, now time.Time) (int64, error) {
	res := db.WithContext(ctx).Where("create_at < ?", nowMillis(now.Add(-ActivityRetention))).Delete(&ActivityItem{})
	if res.Error != nil {
		return 0, fmt.Errorf("purge activity: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ActivityCleanupTask is the periodic task purging old activity items every day.
func (s *Service) ActivityCleanupTask() jobs.TaskType {
	return jobs.TaskType{
		Name:        TaskActivityCleanup,
		MaxAttempts: 3,
		Period:      24 * time.Hour,
		Timeout:     15 * time.Minute,
		Handler: func(ctx context.Context, _ *asynq.Task) error {
			now := time.Now()
			if s.Now != nil {
				now = s.Now()
			}
			_, err := PurgeActivity(ctx, s.DB.Gorm, now)
			return err
		},
	}
}
