package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

type pending bool

func (p pending) HasPending(context.Context) (bool, error) { return bool(p), nil }

func TestReadyCheckPingFailure(t *testing.T) {
	t.Parallel()
	sqlDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	mock.ExpectPing().WillReturnError(errors.New("connection refused: host=db password=secret"))

	check := database.ReadyCheck(&database.DB{SQL: sqlDB}, pending(false))
	if err := check(ctx); !errors.Is(err, database.ErrUnreachable) || err.Error() != "database unreachable" {
		t.Fatalf("got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadyCheckMigrations(t *testing.T) {
	t.Parallel()
	db := dbtest.OpenEmpty(t, dbtest.NewSettings(t, config.DriverSQLite))
	m, err := database.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	check := database.ReadyCheck(db, m)
	if err := check(ctx); !errors.Is(err, database.ErrMigrationsPending) {
		t.Fatalf("before migrate: %v", err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := check(ctx); err != nil {
		t.Fatalf("after migrate: %v", err)
	}
}
