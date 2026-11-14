// Package notify sends notification e-mails (notifications spec): direct
// messages and direct mentions still unread after 10 minutes, to people who
// are not online, at most one e-mail per person every 15 minutes, gathering
// what happened since the previous one; and the unsubscribe links they carry.
package notify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/presence"
)

// Timing and size of the notification e-mails (notifications spec).
const (
	UnreadFor   = 10 * time.Minute // a message waits this long, unread, before it is e-mailed
	EmailEvery  = 15 * time.Minute // at most one e-mail per person in this time
	MaxListed   = 20               // messages listed in one e-mail; the rest are counted
	excerptMax  = 160              // runes of a message's start
	tokenMaxAge = 90 * 24 * time.Hour
)

// TaskEmail is the periodic e-mail task type.
const TaskEmail = "notify:email"

// ErrInvalidToken is an unknown or expired unsubscribe token.
var ErrInvalidToken = errors.New("invalid unsubscribe token")

// Presence reads statuses (online, and whether they are stale while Redis is down).
type Presence interface {
	Get(ctx context.Context, userIDs []string) ([]presence.Status, error)
}

// Service sends the notification e-mails.
type Service struct {
	DB        *gorm.DB
	Sender    Sender
	From      string // the From header (smtp.from)
	Presence  Presence
	PublicURL string // http.public_url, for the links
	Excerpt   bool   // notifications.email_excerpt
	Catalog   Catalog
	Logger    *slog.Logger
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Task is the periodic task sending the e-mails every minute (at most one
// replica per minute: the period claim).
func (s *Service) Task() jobs.TaskType {
	return jobs.TaskType{
		Name: TaskEmail, MaxAttempts: 1, Period: time.Minute, Timeout: 5 * time.Minute,
		Handler: func(ctx context.Context, _ *asynq.Task) error { return s.Run(ctx) },
	}
}

// Run sends what is due now. It sends nothing while presence is unavailable
// (people who may be online are never e-mailed); a failed send leaves its
// messages for a later run.
func (s *Service) Run(ctx context.Context) error {
	now := s.now()
	due := now.Add(-UnreadFor).UnixMilli()
	var users []string
	if err := s.DB.WithContext(ctx).Raw(`SELECT DISTINCT user_id FROM notification_queue WHERE create_at <= ?`, due).
		Scan(&users).Error; err != nil {
		return fmt.Errorf("notify: pending: %w", err)
	}
	if len(users) == 0 {
		return nil
	}
	away := map[string]bool{}
	for start := 0; start < len(users); start += 200 {
		chunk := users[start:min(start+200, len(users))]
		for i := range chunk {
			chunk[i] = strings.TrimSpace(chunk[i])
		}
		sts, err := s.Presence.Get(ctx, chunk)
		if err != nil {
			return fmt.Errorf("notify: presence: %w", err)
		}
		for _, st := range sts {
			if st.Stale {
				s.logger().WarnContext(ctx, "notify: presence unavailable, no e-mail this run")
				return nil
			}
			if st.Status != presence.Online {
				away[st.UserID] = true
			}
		}
	}
	var errs []error
	for _, id := range users {
		if away[id] {
			errs = append(errs, s.mailUser(ctx, id, now, due))
		}
	}
	return errors.Join(errs...)
}

// pending is a queued message as the person may read it now.
type pending struct {
	PostID      string
	ChannelID   string
	ChannelType string
	ChannelName string // display name
	AuthorName  string
	Message     string
	CreateAt    int64
	Readable    bool // a member, message not deleted, channel not archived
	Unread      bool
}

// mailUser sends one person their e-mail, or drops what no longer needs one.
func (s *Service) mailUser(ctx context.Context, userID string, now time.Time, due int64) error {
	db := s.DB.WithContext(ctx)
	var who struct {
		Email    string
		Status   string
		Language string
		Pref     string
	}
	if err := db.Raw(`SELECT u.email, u.status, COALESCE(p.language, '') AS language, COALESCE(p.email_notifications, '') AS pref
		FROM users u LEFT JOIN user_preferences p ON p.user_id = u.id WHERE u.id = ?`, userID).Scan(&who).Error; err != nil {
		return fmt.Errorf("notify: user: %w", err)
	}
	if who.Email == "" || who.Status != identity.StatusActive || who.Pref == identity.NotifyNone {
		return s.dropAll(ctx, userID)
	}
	var rows []pending
	if err := db.Raw(`SELECT q.post_id, p.channel_id, c.type AS channel_type, c.display_name AS channel_name,
			a.display_name AS author_name, p.message, p.create_at,
			CASE WHEN m.user_id IS NOT NULL AND p.delete_at = 0 AND c.archived_at = 0 THEN 1 ELSE 0 END AS readable,
			CASE WHEN m.user_id IS NOT NULL AND p.create_at > m.last_viewed_at THEN 1 ELSE 0 END AS unread
		FROM notification_queue q
		JOIN posts p ON p.id = q.post_id
		JOIN channels c ON c.id = p.channel_id
		JOIN users a ON a.id = p.user_id
		LEFT JOIN channel_members m ON m.channel_id = p.channel_id AND m.user_id = q.user_id
		WHERE q.user_id = ? AND q.create_at <= ?
		ORDER BY q.create_at, q.post_id`, userID, due).Scan(&rows).Error; err != nil {
		return fmt.Errorf("notify: messages: %w", err)
	}
	var send, drop []string
	var items []pending
	for _, r := range rows {
		r.PostID, r.ChannelID = strings.TrimSpace(r.PostID), strings.TrimSpace(r.ChannelID)
		if r.Readable && r.Unread {
			items = append(items, r)
			send = append(send, r.PostID)
		} else {
			drop = append(drop, r.PostID) // read in time, or no longer readable: never e-mailed
		}
	}
	if err := s.drop(ctx, userID, drop); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	// claim the person's e-mail slot: one e-mail per 15 minutes, even if two runs overlap
	last, claimed, err := s.claim(ctx, userID, now)
	if err != nil || !claimed {
		return err
	}
	token, err := s.newUnsubscribe(ctx, userID, now)
	if err != nil {
		_ = s.release(ctx, userID, last)
		return err
	}
	msg, err := s.compose(ctx, who.Email, who.Language, items, token, now)
	if err == nil {
		err = s.Sender.Send(ctx, who.Email, msg)
	}
	if err != nil {
		// nothing lost: the messages stay queued and the slot is given back (no content in the log)
		s.logger().WarnContext(ctx, "notify: e-mail not sent, retrying on a later run", "user_id", userID, "messages", len(items), "error", err.Error())
		_ = s.release(ctx, userID, last)
		return fmt.Errorf("notify: send: %w", err)
	}
	return s.drop(ctx, userID, send)
}

// claim takes the person's e-mail slot when the last e-mail is older than
// EmailEvery; it returns the previous time to give the slot back.
func (s *Service) claim(ctx context.Context, userID string, now time.Time) (int64, bool, error) {
	db := s.DB.WithContext(ctx)
	var last []int64
	if err := db.Raw(`SELECT last_email_at FROM notification_state WHERE user_id = ?`, userID).Scan(&last).Error; err != nil {
		return 0, false, fmt.Errorf("notify: state: %w", err)
	}
	at, limit := now.UnixMilli(), now.Add(-EmailEvery).UnixMilli()
	if len(last) == 0 {
		err := database.Translate(db.Exec(`INSERT INTO notification_state (user_id, last_email_at) VALUES (?, ?)`, userID, at).Error)
		if errors.Is(err, database.ErrDuplicate) {
			return s.claim(ctx, userID, now) // inserted concurrently: compare with it
		}
		if err != nil {
			return 0, false, fmt.Errorf("notify: state: %w", err)
		}
		return 0, true, nil
	}
	res := db.Exec(`UPDATE notification_state SET last_email_at = ? WHERE user_id = ? AND last_email_at = ? AND last_email_at <= ?`,
		at, userID, last[0], limit)
	if res.Error != nil {
		return 0, false, fmt.Errorf("notify: state: %w", res.Error)
	}
	return last[0], res.RowsAffected == 1, nil
}

func (s *Service) release(ctx context.Context, userID string, last int64) error {
	return s.DB.WithContext(context.WithoutCancel(ctx)).Exec(`UPDATE notification_state SET last_email_at = ? WHERE user_id = ?`,
		last, userID).Error
}

// drop removes queued messages of a person.
func (s *Service) drop(ctx context.Context, userID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.DB.WithContext(ctx).Exec(`DELETE FROM notification_queue WHERE user_id = ? AND post_id IN ?`, userID, ids).Error; err != nil {
		return fmt.Errorf("notify: dequeue: %w", err)
	}
	return nil
}

// dropAll removes every queued message of a person.
func (s *Service) dropAll(ctx context.Context, userID string) error {
	if err := s.DB.WithContext(ctx).Exec(`DELETE FROM notification_queue WHERE user_id = ?`, userID).Error; err != nil {
		return fmt.Errorf("notify: dequeue: %w", err)
	}
	return nil
}

// newUnsubscribe makes the random token of one e-mail's unsubscribe link; only
// its SHA-256 is stored (design D4). Tokens older than tokenMaxAge are removed.
func (s *Service) newUnsubscribe(ctx context.Context, userID string, now time.Time) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	db := s.DB.WithContext(ctx)
	if err := db.Exec(`DELETE FROM notification_unsubscribe WHERE create_at < ?`, now.Add(-tokenMaxAge).UnixMilli()).Error; err != nil {
		return "", fmt.Errorf("notify: tokens: %w", err)
	}
	if err := db.Exec(`INSERT INTO notification_unsubscribe (token_hash, user_id, create_at) VALUES (?, ?, ?)`,
		tokenHash(token), userID, now.UnixMilli()).Error; err != nil {
		return "", fmt.Errorf("notify: tokens: %w", err)
	}
	return token, nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Unsubscribe turns notification e-mails off for the owner of token, without a
// session; the queued messages are dropped.
func (s *Service) Unsubscribe(ctx context.Context, token string) error {
	if len(token) != 64 {
		return ErrInvalidToken
	}
	var ids []string
	if err := s.DB.WithContext(ctx).Raw(`SELECT user_id FROM notification_unsubscribe WHERE token_hash = ?`, tokenHash(token)).
		Scan(&ids).Error; err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	if len(ids) != 1 {
		return ErrInvalidToken
	}
	userID := strings.TrimSpace(ids[0])
	none := identity.NotifyNone
	if err := (identity.Store{}).UpdatePreferences(ctx, s.DB, userID, identity.PreferencesPatch{Email: &none}); err != nil {
		return err
	}
	return s.dropAll(ctx, userID)
}

// ResetUnsubscribe ends the unsubscribe links of earlier e-mails (the person
// turned e-mails on again).
func (s *Service) ResetUnsubscribe(ctx context.Context, userID string) error {
	return s.DB.WithContext(ctx).Exec(`DELETE FROM notification_unsubscribe WHERE user_id = ?`, userID).Error
}
