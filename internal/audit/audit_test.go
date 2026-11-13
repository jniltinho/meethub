package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

var ctx = context.Background()

func ptr(s string) *string { return &s }

func TestRecordDropsSecrets(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		err := audit.Record(ctx, db.Gorm, audit.Event{
			Action: audit.ActionPasswordChanged, ActorID: ptr("01J00000000000000000000001"), TargetType: "user",
			Details: map[string]any{"password": "hunter2", "password_hash": "$argon2id$", "session_token": "t", "reason": "self"},
		})
		if err != nil {
			t.Fatal(err)
		}
		page, err := audit.List(ctx, db.Gorm, audit.Filter{})
		if err != nil || len(page.Events) != 1 {
			t.Fatalf("list: %v %+v", err, page)
		}
		raw := page.Events[0].RawDetails
		for _, secret := range []string{"hunter2", "argon2id", `"t"`} {
			if strings.Contains(raw, secret) {
				t.Fatalf("details leak %q: %s", secret, raw)
			}
		}
		if page.Events[0].Details["reason"] != "self" {
			t.Fatalf("details = %v", page.Events[0].Details)
		}
	})
}

func TestListFilterAndStablePages(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		for i := range 5 {
			action := audit.ActionLoginSucceeded
			if i%2 == 0 {
				action = audit.ActionLoginFailed
			}
			if err := audit.Record(ctx, db.Gorm, audit.Event{Action: action, ClientIP: "10.0.0.1"}); err != nil {
				t.Fatal(err)
			}
		}
		// force identical timestamps so only the id breaks ties
		same := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
		if err := db.Gorm.Exec(`UPDATE audit_events SET created_at = ?`, same).Error; err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		cursor := ""
		for range 10 {
			page, err := audit.List(ctx, db.Gorm, audit.Filter{Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range page.Events {
				if seen[e.ID] {
					t.Fatalf("event %s returned twice", e.ID)
				}
				seen[e.ID] = true
			}
			if cursor = page.NextCursor; cursor == "" {
				break
			}
		}
		if len(seen) != 5 {
			t.Fatalf("paged %d events, want 5", len(seen))
		}
		failed, err := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionLoginFailed})
		if err != nil || len(failed.Events) != 3 {
			t.Fatalf("filter: %v %d", err, len(failed.Events))
		}
		if _, err := audit.List(ctx, db.Gorm, audit.Filter{Cursor: "garbage!"}); !errors.Is(err, audit.ErrBadCursor) {
			t.Fatalf("bad cursor: %v", err)
		}
	})
}

// TestAuditFailureRollsBackTheChange injects an audit insert failure with
// go-sqlmock: the privileged change in the same transaction must roll back.
func TestAuditFailureRollsBackTheChange(t *testing.T) {
	t.Parallel()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE memberships SET role`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "audit_events"`).WillReturnError(errors.New("disk full"))
	mock.ExpectRollback()

	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE memberships SET role = ? WHERE user_id = ?`, "admin", "u1").Error; err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Action: audit.ActionRoleChanged, TargetType: "user", TargetID: "u1"})
	})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("transaction error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the change was not rolled back: %v", err)
	}
}
