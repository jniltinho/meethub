// Package testenv tells tests which external services (Redis, MariaDB,
// PostgreSQL) the test stack provides. make test and make test-db set the
// MEETHUB_TEST_* variables; when one is unset, tests that need that service
// are skipped, so "go test ./..." also runs on a bare machine.
package testenv

import (
	"os"
	"testing"
)

// Server is a database server reachable by tests.
type Server struct {
	Addr          string // host:port
	User          string
	Password      string
	Name          string // database name
	AdminUser     string // may create and drop databases
	AdminPassword string
}

// RedisAddr returns the test Redis address or skips the test.
func RedisAddr(t testing.TB) string {
	t.Helper()
	addr := os.Getenv("MEETHUB_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("MEETHUB_TEST_REDIS_ADDR not set (run make test)")
	}
	return addr
}

// MariaDB returns the test MariaDB server or skips the test.
func MariaDB(t testing.TB) Server {
	s := server(t, "MARIADB")
	s.AdminUser, s.AdminPassword = "root", "meethub-test-root"
	return s
}

// Postgres returns the test PostgreSQL server or skips the test.
func Postgres(t testing.TB) Server {
	s := server(t, "POSTGRES")
	s.AdminUser, s.AdminPassword = s.User, s.Password // POSTGRES_USER is a superuser
	return s
}

func server(t testing.TB, kind string) Server {
	t.Helper()
	addr := os.Getenv("MEETHUB_TEST_" + kind + "_ADDR")
	if addr == "" {
		t.Skipf("MEETHUB_TEST_%s_ADDR not set (run make test-db)", kind)
	}
	return Server{Addr: addr, User: "meethub", Password: "meethub-test", Name: "meethub"}
}
