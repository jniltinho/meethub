package database

import (
	"time"

	"github.com/pressly/goose/v3/lock"
)

// NewLockerForTest exposes the dialect's migration locker to external tests.
func NewLockerForTest(db *DB, timeout time.Duration) (lock.SessionLocker, error) {
	_, l, err := newLocker(db, timeout)
	return l, err
}
