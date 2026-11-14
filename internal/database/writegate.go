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
// than Settings.WriteWait (the tests set it to turn a transaction begun inside
// another one into a clear error instead of a hang).
var ErrWriteWait = errors.New("database: waited too long for the SQLite write gate")

// writeGate serializes the write transactions of this process on SQLite, first
// come first served (database spec "Fair writes on SQLite", design D1): SQLite's
// busy handler polls for the lock, so under a burst one writer can keep missing
// it; a channel of capacity one queues blocked senders in arrival order instead.
type writeGate struct {
	slot    chan struct{}
	maxWait time.Duration // 0: only the context bounds the wait
}

func newWriteGate(maxWait time.Duration) *writeGate {
	return &writeGate{slot: make(chan struct{}, 1), maxWait: maxWait}
}

// acquire waits for the gate in arrival order and returns its release (safe to
// call more than once).
func (g *writeGate) acquire(ctx context.Context) (func(), error) {
	var expired <-chan time.Time
	if g.maxWait > 0 {
		t := time.NewTimer(g.maxWait)
		defer t.Stop()
		expired = t.C
	}
	select {
	case g.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-expired:
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

// BeginTx takes the gate before BEGIN IMMEDIATE and keeps it until Commit,
// Rollback or the end of ctx (database/sql rolls the transaction back then).
func (p *gatedPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	release, err := p.gate.acquire(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := p.db.BeginTx(ctx, opts)
	if err != nil {
		release()
		return nil, err
	}
	stop := context.AfterFunc(ctx, release)
	return &gatedTx{Tx: tx, release: func() { stop(); release() }}, nil
}

// gatedTx is a transaction holding the gate; GORM sees it as its ConnPool.
type gatedTx struct {
	*sql.Tx
	release func()
}

var (
	_ gorm.ConnPool    = (*gatedTx)(nil)
	_ gorm.TxCommitter = (*gatedTx)(nil)
)

func (t *gatedTx) Commit() error {
	defer t.release()
	return t.Tx.Commit()
}

func (t *gatedTx) Rollback() error {
	defer t.release()
	return t.Tx.Rollback()
}
