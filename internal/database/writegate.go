package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ErrWriteWait is returned when a write waited for the SQLite write gate longer
// than Settings.WriteWait: a transaction begun on the pool from inside another
// one fails with it instead of hanging.
var ErrWriteWait = errors.New("database: waited too long for the SQLite write gate")

// DefaultWriteWait bounds the wait for the gate when Settings.WriteWait is 0:
// twice SQLite's busy timeout, the most a writer could wait before the gate.
const DefaultWriteWait = 60 * time.Second

// writeGate serializes the write transactions of this process on SQLite, first
// come first served (database spec "Fair writes on SQLite", design D1): SQLite's
// busy handler polls for the lock, so under a burst one writer can keep missing
// it; a channel of capacity one queues blocked senders in arrival order instead.
type writeGate struct {
	slot    chan struct{}
	maxWait time.Duration
}

func newWriteGate(maxWait time.Duration) *writeGate {
	if maxWait <= 0 {
		maxWait = DefaultWriteWait
	}
	return &writeGate{slot: make(chan struct{}, 1), maxWait: maxWait}
}

// acquire waits for the gate in arrival order and returns its release (safe to
// call more than once).
func (g *writeGate) acquire(ctx context.Context) (func(), error) {
	t := time.NewTimer(g.maxWait)
	defer t.Stop()
	select {
	case g.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.C:
		return nil, fmt.Errorf("%w (%s; a transaction begun inside another?)", ErrWriteWait, g.maxWait)
	}
	var once sync.Once
	return func() { once.Do(func() { <-g.slot }) }, nil
}

// gatedPool is the connection pool GORM gets on SQLite: beginning a transaction,
// and a write outside one, pass the gate; reads go straight to the pool (WAL
// keeps them concurrent with the one writer).
type gatedPool struct {
	db   *sql.DB
	gate *writeGate
}

var (
	_ gorm.ConnPool         = (*gatedPool)(nil)
	_ gorm.ConnPoolBeginner = (*gatedPool)(nil)
	_ gorm.GetDBConnector   = (*gatedPool)(nil)
)

func (p *gatedPool) GetDBConn() (*sql.DB, error) { return p.db, nil }

func (p *gatedPool) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return p.db.PrepareContext(ctx, query)
}

func (p *gatedPool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	release, err := p.gate.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return p.db.ExecContext(ctx, query, args...)
}

// QueryContext and QueryRowContext pass through. shortcut: a write that returns
// rows (INSERT … RETURNING outside a transaction, which MeetHub's string ids do
// not produce) holds SQLite's lock until its rows close and meets the busy
// timeout instead of the gate (none in the test suite, checked when the gate was
// added); gate it if such a write ever appears.
func (p *gatedPool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return p.db.QueryContext(ctx, query, args...)
}

func (p *gatedPool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return p.db.QueryRowContext(ctx, query, args...)
}

// BeginTx takes the gate before BEGIN IMMEDIATE and keeps it until the
// transaction has ended: at Commit, at Rollback, or when ctx ends (database/sql
// rolls the transaction back then). The transaction runs on a connection of its
// own, whose Close waits for that rollback to finish, so the next writer never
// meets SQLite's lock still held by this one.
func (p *gatedPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	release, err := p.gate.acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := p.db.Conn(ctx)
	if err != nil {
		release()
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, opts)
	if err != nil {
		_ = conn.Close()
		release()
		return nil, err
	}
	var once sync.Once
	end := func() {
		once.Do(func() {
			_ = conn.Close() // blocks until the transaction's own rollback, if any, is done
			release()
		})
	}
	stop := context.AfterFunc(ctx, end)
	return &gatedTx{Tx: tx, db: p.db, end: func() { stop(); end() }}, nil
}

// gatedTx is a transaction holding the gate; GORM sees it as its ConnPool.
type gatedTx struct {
	*sql.Tx
	db  *sql.DB
	end func()
}

var (
	_ gorm.ConnPool       = (*gatedTx)(nil)
	_ gorm.TxCommitter    = (*gatedTx)(nil)
	_ gorm.GetDBConnector = (*gatedTx)(nil)
)

func (t *gatedTx) GetDBConn() (*sql.DB, error) { return t.db, nil }

func (t *gatedTx) Commit() error {
	defer t.end()
	return t.Tx.Commit()
}

func (t *gatedTx) Rollback() error {
	defer t.end()
	return t.Tx.Rollback()
}
