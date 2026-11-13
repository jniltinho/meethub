package testenv_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/testenv"
)

// TestServicesReachable proves the test stack is wired: each configured service answers.
func TestServicesReachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("redis", func(t *testing.T) {
		rdb := redis.NewClient(&redis.Options{Addr: testenv.RedisAddr(t)})
		defer func() { _ = rdb.Close() }()
		if err := rdb.Ping(ctx).Err(); err != nil {
			t.Fatalf("ping redis: %v", err)
		}
	})
	t.Run("mariadb", func(t *testing.T) {
		s := testenv.MariaDB(t)
		ping(ctx, t, "mysql", fmt.Sprintf("%s:%s@tcp(%s)/%s", s.User, s.Password, s.Addr, s.Name))
	})
	t.Run("postgres", func(t *testing.T) {
		s := testenv.Postgres(t)
		ping(ctx, t, "pgx", fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", s.User, s.Password, s.Addr, s.Name))
	})
}

func ping(ctx context.Context, t *testing.T, driver, dsn string) {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", driver, err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping %s: %v", driver, err)
	}
}
