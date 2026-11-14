package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/database"
)

// Notification preference values (notifications spec); "" means the default, NotifyMentions.
const (
	NotifyAll      = "all"      // desktop only: every message of the person's conversations
	NotifyMentions = "mentions" // direct messages and mentions
	NotifyNone     = "none"
)

// Preferences are a user's personal settings; empty values mean "default".
type Preferences struct {
	UserID    string    `gorm:"primaryKey;size:26" json:"-"`
	Theme     string    `json:"theme"`
	Language  string    `json:"language"`
	Desktop   string    `gorm:"column:desktop_notifications" json:"desktop_notifications"`
	Email     string    `gorm:"column:email_notifications" json:"email_notifications"`
	UpdatedAt time.Time `json:"-"`
}

// PreferencesPatch names the preferences to change; nil leaves one as it is.
type PreferencesPatch struct {
	Theme, Language, Desktop, Email *string
}

// TableName maps Preferences to user_preferences.
func (Preferences) TableName() string { return "user_preferences" }

// Preferences returns userID's preferences (zero values when never saved).
func (Store) Preferences(ctx context.Context, tx *gorm.DB, userID string) (Preferences, error) {
	var p Preferences
	err := tx.WithContext(ctx).Where("user_id = ?", userID).Take(&p).Error
	if database.IsNotFound(err) {
		return Preferences{UserID: userID}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("get preferences: %w", err)
	}
	p.UserID = strings.TrimSpace(p.UserID)
	return p, nil
}

// ValidDesktopNotifications and ValidEmailNotifications check a preference value ("" is the default).
func ValidDesktopNotifications(v string) bool {
	return v == "" || v == NotifyAll || v == NotifyMentions || v == NotifyNone
}

// ValidEmailNotifications reports whether v is an e-mail notification preference.
func ValidEmailNotifications(v string) bool { return v == "" || v == NotifyMentions || v == NotifyNone }

// UpdatePreferences sets only the given fields (nil = unchanged) in one atomic
// upsert, so concurrent changes of different fields never undo each other.
func (Store) UpdatePreferences(ctx context.Context, tx *gorm.DB, userID string, patch PreferencesPatch) error {
	p := Preferences{UserID: userID, UpdatedAt: time.Now().UTC()}
	cols := []string{"updated_at"}
	for _, f := range []struct {
		v   *string
		dst *string
		col string
	}{
		{patch.Theme, &p.Theme, "theme"}, {patch.Language, &p.Language, "language"},
		{patch.Desktop, &p.Desktop, "desktop_notifications"}, {patch.Email, &p.Email, "email_notifications"},
	} {
		if f.v != nil {
			*f.dst = *f.v
			cols = append(cols, f.col)
		}
	}
	err := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns(cols),
	}).Create(&p).Error
	if err != nil {
		return fmt.Errorf("update preferences: %w", err)
	}
	return nil
}
