//go:build unix

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// fileLocker serializes SQLite migrations between processes on one host with
// an exclusive flock on <db>.migrate.lock (SQLite is single-node only).
type fileLocker struct {
	path    string
	timeout time.Duration
	f       *os.File
}

func (l *fileLocker) SessionLock(ctx context.Context, _ *sql.Conn) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("migration lock: %w", err)
	}
	deadline := time.Now().Add(l.timeout)
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			l.f = f
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			if errors.Is(err, unix.EWOULDBLOCK) {
				return ErrLockTimeout
			}
			return fmt.Errorf("migration lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (l *fileLocker) SessionUnlock(_ context.Context, _ *sql.Conn) error {
	if l.f == nil {
		return nil
	}
	defer func() { l.f = nil }()
	if err := unix.Flock(int(l.f.Fd()), unix.LOCK_UN); err != nil {
		_ = l.f.Close()
		return err
	}
	return l.f.Close()
}
