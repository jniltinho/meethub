package database_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// gateDB opens an empty SQLite database with a table for the gate tests.
func gateDB(t *testing.T, wait time.Duration) *database.DB {
	t.Helper()
	s := dbtest.NewSettings(t, config.DriverSQLite)
	s.WriteWait = wait
	db := dbtest.OpenEmpty(t, s)
	if err := db.Gorm.Exec("CREATE TABLE gate_t (id INTEGER PRIMARY KEY, n INTEGER NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// hold opens a write transaction and keeps it open until the returned func is called.
func hold(t *testing.T, db *database.DB) func() {
	t.Helper()
	opened, done := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		err := db.Gorm.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("INSERT INTO gate_t (n) VALUES (-1)").Error; err != nil {
				return err
			}
			close(opened)
			<-done
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	})
	<-opened
	var once sync.Once
	return func() { once.Do(func() { close(done); wg.Wait() }) }
}

func count(t *testing.T, db *database.DB, where string) int64 {
	t.Helper()
	var n int64
	if err := db.Gorm.Raw("SELECT count(*) FROM gate_t WHERE " + where).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWriteGateServesInArrivalOrder(t *testing.T) {
	db := gateDB(t, 0)
	release := hold(t, db)
	var (
		mu    sync.Mutex
		order []int
		wg    sync.WaitGroup
	)
	for i := range 6 {
		wg.Go(func() {
			err := db.Gorm.Transaction(func(tx *gorm.DB) error {
				mu.Lock()
				order = append(order, i)
				mu.Unlock()
				return tx.Exec("INSERT INTO gate_t (n) VALUES (?)", i).Error
			})
			if err != nil {
				t.Error(err)
			}
		})
		time.Sleep(30 * time.Millisecond) // writer i is queued before writer i+1 asks
	}
	release()
	wg.Wait()
	if !slices.Equal(order, []int{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("served in order %v", order)
	}
}

func TestWriteGateCancelledWhileWaiting(t *testing.T) {
	db := gateDB(t, 0)
	release := hold(t, db)
	defer release()
	for _, outside := range []bool{false, true} { // a transaction, and a write outside one
		c, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		var err error
		if outside {
			err = db.Gorm.WithContext(c).Exec("INSERT INTO gate_t (n) VALUES (7)").Error
		} else {
			err = db.Gorm.WithContext(c).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("INSERT INTO gate_t (n) VALUES (7)").Error
			})
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("outside=%v: %v", outside, err)
		}
		if waited := time.Since(start); waited > time.Second {
			t.Fatalf("outside=%v: returned after %s", outside, waited)
		}
	}
	release()
	if count(t, db, "n = 7") != 0 {
		t.Fatal("a cancelled write was stored")
	}
}

func TestWriteGateReleasedWhenTheContextEnds(t *testing.T) {
	db := gateDB(t, 0)
	c, cancel := context.WithCancel(context.Background())
	tx := db.Gorm.WithContext(c).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	cancel() // never committed nor rolled back by the caller: database/sql rolls back, the gate opens
	w, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := db.Gorm.WithContext(w).Exec("INSERT INTO gate_t (n) VALUES (1)").Error; err != nil {
		t.Fatal(err)
	}
}

func TestReadsDoNotWaitForTheWriter(t *testing.T) {
	db := gateDB(t, 0)
	release := hold(t, db)
	defer release()
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	var n int64
	if err := db.Gorm.WithContext(c).Raw("SELECT count(*) FROM gate_t").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 || time.Since(start) > 500*time.Millisecond { // the open transaction's row is not visible yet
		t.Fatalf("read %d rows after %s", n, time.Since(start))
	}
}

// TestTransactionDB: tx.DB() inside a transaction still reaches the pool (GORM's GetDBConnector).
func TestTransactionDB(t *testing.T) {
	db := gateDB(t, 0)
	err := db.Gorm.Transaction(func(tx *gorm.DB) error {
		sqlDB, err := tx.DB()
		if err == nil && sqlDB != db.SQL {
			err = errors.New("another pool")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCancelledTransactionWithOpenRows: a transaction whose context ends while a query of it has rows open is
// rolled back, and the gate opens for the next writer (after the transaction's connection closed: Conn.Close waits
// for database/sql's own rollback, so the next writer does not meet SQLite's lock).
func TestCancelledTransactionWithOpenRows(t *testing.T) {
	db := gateDB(t, 0)
	for i := range 50 {
		if err := db.Gorm.Exec("INSERT INTO gate_t (n) VALUES (?)", i).Error; err != nil {
			t.Fatal(err)
		}
	}
	c, cancel := context.WithCancel(context.Background())
	tx := db.Gorm.WithContext(c).Begin()
	if err := tx.Exec("INSERT INTO gate_t (n) VALUES (-5)").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Raw("SELECT n FROM gate_t").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cancel()
	w, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := db.Gorm.WithContext(w).Transaction(func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO gate_t (n) VALUES (-6)").Error
	}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "n = -5") != 0 || count(t, db, "n = -6") != 1 {
		t.Fatal("the cancelled transaction was not rolled back, or the next one not stored")
	}
}

func TestTransactionInsideAnotherIsAClearError(t *testing.T) {
	db := gateDB(t, 200*time.Millisecond)
	err := db.Gorm.Transaction(func(tx *gorm.DB) error {
		return db.Gorm.Transaction(func(inner *gorm.DB) error { return nil }) // the pool, not tx: a bug
	})
	if !errors.Is(err, database.ErrWriteWait) {
		t.Fatalf("got %v", err)
	}
}

// TestBurstOfWritersIsFair: eight writers each commit 40 transactions that hold the lock for about 2 ms; first
// come first served, no transaction waits longer than the writers ahead of it take (seven of them, plus slack).
// Without the gate SQLite's busy handler polls with growing sleeps and the worst wait is far above this bound.
func TestBurstOfWritersIsFair(t *testing.T) {
	db := gateDB(t, 0)
	const writers, each = 8, 40
	var (
		mu              sync.Mutex
		worstWait, slow time.Duration
		wg              sync.WaitGroup
	)
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				asked := time.Now()
				var began time.Time
				err := db.Gorm.Transaction(func(tx *gorm.DB) error {
					began = time.Now()
					if err := tx.Exec("INSERT INTO gate_t (n) VALUES (?)", w*each+i).Error; err != nil {
						return err
					}
					time.Sleep(2 * time.Millisecond)
					return nil
				})
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				worstWait = max(worstWait, began.Sub(asked))
				slow = max(slow, time.Since(began))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if n := count(t, db, "n >= 0"); n != writers*each {
		t.Fatalf("stored %d of %d", n, writers*each)
	}
	bound := (writers-1)*slow + 50*time.Millisecond
	t.Logf("worst wait %s, slowest transaction %s, bound %s", worstWait, slow, bound)
	if worstWait > bound {
		t.Fatalf("a writer waited %s, more than the %s the writers ahead of it can take", worstWait, bound)
	}
}
