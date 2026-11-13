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

// Preferences are a user's personal settings; empty values mean "default".
type Preferences struct {
	UserID    string    `gorm:"primaryKey;size:26" json:"-"`
	Theme     string    `json:"theme"`
	Language  string    `json:"language"`
	UpdatedAt time.Time `json:"-"`
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

// SavePreferences inserts or replaces userID's preferences (portable upsert).
func (Store) SavePreferences(ctx context.Context, tx *gorm.DB, p Preferences) error {
	p.UpdatedAt = time.Now().UTC()
	err := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"theme", "language", "updated_at"}),
	}).Create(&p).Error
	if err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}
	return nil
}
