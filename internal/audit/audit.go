// Package audit records security-relevant actions as a transactional
// application history (not a tamper-proof log) and lists them for
// administrators. Events never carry passwords, hashes, tokens or message
// bodies.
package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Actions recorded by this release.
const (
	ActionSystemInitialized = "system.initialized"
	ActionLoginSucceeded    = "auth.login_succeeded"
	ActionLoginFailed       = "auth.login_failed"
	ActionLogout            = "auth.logout"
	ActionPasswordChanged   = "user.password_changed"
	ActionPasswordReset     = "user.password_reset"
	ActionUserCreated       = "user.created"
	ActionRoleChanged       = "user.role_changed"
	ActionUserDisabled      = "user.disabled"
	ActionUserEnabled       = "user.enabled"
	ActionUserUpdated       = "user.updated"
	ActionSettingsChanged   = "organization.settings_changed"
	ActionTeamCreated       = "team.created"
	ActionTeamUpdated       = "team.updated"
	ActionTeamArchived      = "team.archived"
	ActionTeamRestored      = "team.restored"
	ActionTeamMemberAdded   = "team.member_added"
	ActionTeamMemberRemoved = "team.member_removed"
	ActionTeamRoleChanged   = "team.role_changed"
	ActionChannelCreated    = "channel.created"
	ActionChannelUpdated    = "channel.updated"
	ActionChannelArchived   = "channel.archived"
	ActionChannelRestored   = "channel.restored"
	ActionChannelMemberAdd  = "channel.member_added"
	ActionChannelMemberRem  = "channel.member_removed"
	ActionChannelRoleChange = "channel.role_changed"
	ActionPostModerated     = "post.deleted_by_moderator"
)

// ErrBadCursor is returned for a cursor that cannot be decoded.
var ErrBadCursor = errors.New("invalid cursor")

// Event is one audit record.
type Event struct {
	ID         string         `gorm:"primaryKey;size:26" json:"id"`
	OrgID      *string        `json:"org_id,omitempty"`
	ActorID    *string        `json:"actor_id,omitempty"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	ClientIP   string         `json:"client_ip"`
	Details    map[string]any `gorm:"-" json:"details"`
	RawDetails string         `gorm:"column:details" json:"-"`
	CreatedAt  time.Time      `json:"created_at"`
}

// TableName maps Event to audit_events.
func (Event) TableName() string { return "audit_events" }

// sensitiveKeys are dropped from details, whatever the caller passes.
var sensitiveKeys = []string{"password", "hash", "token", "secret", "csrf", "cookie", "body"}

// Record inserts e using tx — the caller's transaction — so the event and the
// change it describes commit or roll back together.
func Record(ctx context.Context, tx *gorm.DB, e Event) error {
	if e.Action == "" {
		return errors.New("audit: action is required")
	}
	e.ID = ulid.Make().String()
	e.CreatedAt = time.Now().UTC()
	clean := make(map[string]any, len(e.Details))
	for k, v := range e.Details {
		if !isSensitive(k) {
			clean[k] = v
		}
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	e.RawDetails = string(b)
	if err := tx.WithContext(ctx).Create(&e).Error; err != nil {
		return fmt.Errorf("record audit event %s: %w", e.Action, err)
	}
	return nil
}

func isSensitive(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Filter selects events; zero values mean "any".
type Filter struct {
	ActorID string
	Action  string
	From    time.Time
	To      time.Time
	Limit   int    // default 50, max 200
	Cursor  string // from Page.NextCursor
}

// Page is one page of events, newest first.
type Page struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// List returns events ordered by (created_at, id) descending.
func List(ctx context.Context, db *gorm.DB, f Filter) (*Page, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	q := db.WithContext(ctx).Model(&Event{})
	if f.ActorID != "" {
		q = q.Where("actor_id = ?", f.ActorID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if !f.From.IsZero() {
		q = q.Where("created_at >= ?", f.From.UTC())
	}
	if !f.To.IsZero() {
		q = q.Where("created_at < ?", f.To.UTC())
	}
	if f.Cursor != "" {
		at, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, err
		}
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))", at, at, id)
	}
	var events []Event
	if err := q.Order("created_at DESC").Order("id DESC").Limit(limit + 1).Find(&events).Error; err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	page := &Page{Events: events}
	if len(events) > limit {
		page.Events = events[:limit]
		last := page.Events[limit-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	for i := range page.Events {
		e := &page.Events[i]
		e.ID = strings.TrimSpace(e.ID)
		e.CreatedAt = e.CreatedAt.UTC()
		_ = json.Unmarshal([]byte(e.RawDetails), &e.Details)
	}
	return page, nil
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + strings.TrimSpace(id)))
}

func decodeCursor(c string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return time.Time{}, "", ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil || len(id) != 26 {
		return time.Time{}, "", ErrBadCursor
	}
	return t, id, nil
}
