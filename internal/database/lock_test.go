package database_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// TestLockTimeout holds the migration lock on one connection and checks that a
// second migrator gives up with ErrLockTimeout once its timeout passes.
func TestLockTimeout(t *testing.T) {
	dbtest.EachEmpty(t, func(t *testing.T, s database.Settings) {
		holder := dbtest.OpenEmpty(t, s)
		held, err := database.NewLockerForTest(holder, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := holder.SQL.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := held.SessionLock(ctx, conn); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = held.SessionUnlock(ctx, conn) }()

		m, err := database.NewMigrator(dbtest.OpenEmpty(t, s), database.WithLockTimeout(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err := m.Up(ctx); !errors.Is(err, database.ErrLockTimeout) {
			t.Fatalf("want ErrLockTimeout, got %v", err)
		}
		if d := time.Since(start); d > 30*time.Second {
			t.Fatalf("timeout not honored: %s", d)
		}
	})
}
