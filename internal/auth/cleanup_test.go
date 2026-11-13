package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

func TestPurgeSessions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
		idle := 7 * 24 * time.Hour
		day := 24 * time.Hour
		if err := db.Gorm.Exec(`INSERT INTO users (id, email, email_normalized, display_name, password_hash, status, created_at, updated_at)
			VALUES ('u1', 'a@x.io', 'a@x.io', 'A', 'h', 'active', ?, ?)`, now, now).Error; err != nil {
			t.Fatal(err)
		}
		type row struct {
			id                string
			lastSeen, expires time.Time
			revoked           *time.Time
		}
		at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
		rows := []row{
			{"revoked-8d", now.Add(-8 * day), now.Add(20 * day), at(-8 * day)}, // purged
			{"revoked-1d", now.Add(-1 * day), now.Add(20 * day), at(-1 * day)}, // kept
			{"expired-9d", now.Add(-9 * day), now.Add(-9 * day), nil},          // absolute expiry 9 days ago: purged
			{"idle-15d", now.Add(-15 * day), now.Add(10 * day), nil},           // idle-expired 8 days ago: purged
			{"active", now.Add(-time.Hour), now.Add(20 * day), nil},            // kept
		}
		for i, r := range rows {
			if err := db.Gorm.Exec(`INSERT INTO sessions (id, user_id, token_hash, created_at, last_seen_at, expires_at, revoked_at)
				VALUES (?, 'u1', ?, ?, ?, ?, ?)`, r.id, string(rune('a'+i)), r.lastSeen, r.lastSeen, r.expires, r.revoked).Error; err != nil {
				t.Fatal(err)
			}
		}
		n, err := auth.PurgeSessions(ctx, db, now, idle)
		if err != nil {
			t.Fatal(err)
		}
		var left []string
		db.Gorm.Raw(`SELECT id FROM sessions ORDER BY id`).Scan(&left)
		for i := range left {
			left[i] = strings.TrimSpace(left[i]) // CHAR(26) pads these short test ids on PostgreSQL
		}
		if n != 3 || len(left) != 2 || left[0] != "active" || left[1] != "revoked-1d" {
			t.Fatalf("purged %d, left %v", n, left)
		}
	})
}
