package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/jniltinho/meethub/internal/config"
)

// Server is the service the applier restarts.
type Server interface {
	// Restart restarts the server and returns once the restart was carried out (not once it is ready).
	Restart(ctx context.Context) error
	// BootID asks the running server for its boot id (GET /api/v1/ready); an error while it is not ready.
	BootID(ctx context.Context) (string, error)
}

// Applier carries out the requests of one host (design D4): one transaction per Run.
type Applier struct {
	Config   string // config.toml
	Requests string // the mailbox, <data_dir>/settings
	State    string // its own directory, <config dir>/settings-state
	Server   Server
	Version  string
	Logger   *slog.Logger
	Now      func() time.Time
	// Timeouts; zero: the design's values
	ReadyTimeout  time.Duration // 60 s
	ConfirmWindow time.Duration // 2 min
	Poll          time.Duration // 1 s
	MaxPerHour    int           // 3
}

func (a *Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Applier) logger() *slog.Logger {
	if a.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return a.Logger
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Run holds the applier's lock for one transaction: it announces itself, finishes or undoes a transaction its
// state shows unfinished, then carries out the request in the mailbox, if any. Cancelling ctx while it waits rolls
// the change back (a package operation stops the unit).
func (a *Applier) Run(ctx context.Context) error {
	lock, err := a.lock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := writeJSON(filepath.Join(a.State, PresenceFile), Presence{
		Config: a.Config, Requests: a.Requests, Version: a.Version, StartedAt: a.now().UTC(),
	}, 0o640); err != nil {
		return err
	}
	if err := a.recover(ctx); err != nil {
		return err
	}
	return a.next(ctx)
}

// Recover holds the lock and only finishes or undoes an interrupted transaction (the package scripts run it before
// the binary is replaced or removed); it fails while a transaction is still unfinished.
func (a *Applier) Recover(ctx context.Context) error {
	lock, err := a.lock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := a.recover(ctx); err != nil {
		return err
	}
	if st, err := ReadState(a.State); err != nil || st != nil {
		return fmt.Errorf("a change is still unfinished in %s", a.State)
	}
	return nil
}

func (a *Applier) lock() (*os.File, error) {
	if err := os.MkdirAll(a.State, 0o750); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(a.State, lockFile), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // fixed name in root's directory
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock: %w", err)
	}
	return lock, nil
}

// recover ends a transaction cut short (a reboot, a kill): back to the previous file unless the owner had kept it.
func (a *Applier) recover(ctx context.Context) error {
	var st State
	if err := readJSON(a.State, StateFile, &st); err != nil {
		return err
	}
	if st.ID == "" {
		return nil
	}
	a.logger().Warn("settings: finishing an interrupted transaction", "id", st.ID, "phase", st.Phase)
	if r := a.resultOf(st.ID); r != nil { // the result was written: only the state is left
		return a.clearState()
	}
	switch st.Phase {
	case PhaseConfirmed:
		return a.finish(&st, Applied, "")
	case PhaseBackup: // config.toml was not touched yet
		return a.finish(&st, RolledBack, "interrupted before the file was replaced")
	case PhaseReplacing:
		if sum, _, err := a.readConfig(); err == nil && sum == st.OldSHA256 {
			return a.finish(&st, RolledBack, "interrupted before the file was replaced")
		}
	}
	return a.rollback(context.WithoutCancel(ctx), &st, "interrupted (the host or the applier stopped)")
}

// next takes the request in the mailbox and carries it out.
func (a *Applier) next(ctx context.Context) error {
	raw, err := takeNoFollow(a.Requests, RequestFile, takenFile, maxRequestBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	removeNoFollow(a.Requests, ConfirmFile) // a confirmation left from before means nothing for this request
	if err != nil {
		return a.refuse("", nil, "the request is not a regular file of at most 16 KiB")
	}
	var req Request
	if err := decodeStrict(raw, &req); err != nil || !idPattern.MatchString(req.ID) {
		return a.refuse("", nil, "the request is malformed")
	}
	keys := slices.Sorted(maps.Keys(req.Keys))
	// a request is good for an hour: within it, every id carried out is still in the consumed list (three an hour
	// at most are recorded), so none is carried out twice
	if age := a.now().Sub(req.CreatedAt); age > maxRequestAge || age < -5*time.Minute {
		return a.refuse(req.ID, keys, "the request expired: make the change again")
	}
	seen, applies, err := a.consumed(req.ID)
	if err != nil {
		return err
	}
	if seen {
		return a.refuse(req.ID, keys, "this request was carried out before (replayed)")
	}
	old, candidate, reason := a.check(&req)
	if reason == "" && applies >= orInt(a.MaxPerHour, 3) {
		reason = "too many changes in the last hour: try again later"
	}
	if reason != "" { // a refusal is not recorded: refusals cannot push carried-out ids out of the list
		return a.refuse(req.ID, keys, reason)
	}
	// recorded before anything changes, so this request is never carried out twice
	if err := a.consume(req.ID, true); err != nil {
		return err
	}
	return a.apply(ctx, &req, keys, old, candidate)
}

func orInt(n, def int) int {
	if n > 0 {
		return n
	}
	return def
}

// check decides alone, trusting nothing the server wrote: allowlisted keys within bounds, the file the request
// was made from, a candidate that parses.
func (a *Applier) check(req *Request) ([]byte, []byte, string) {
	if len(req.Keys) == 0 || len(req.Keys) > len(config.AdminKeys) {
		return nil, nil, "the request names no setting, or too many"
	}
	values := make(map[string]any, len(req.Keys))
	for k, raw := range req.Keys {
		v, err := config.ParseAdminValue(k, raw)
		if err != nil {
			return nil, nil, err.Error()
		}
		values[k] = v
	}
	if !sumPattern.MatchString(req.ConfigSHA256) {
		return nil, nil, "the request is malformed"
	}
	sum, old, err := a.readConfig()
	if err != nil {
		return nil, nil, "config.toml cannot be read: " + err.Error()
	}
	if sum != req.ConfigSHA256 {
		return nil, nil, "config.toml changed after the request was made (stale): reload the page"
	}
	candidate, err := config.EditAdmin(old, values)
	if err != nil {
		return nil, nil, err.Error()
	}
	return old, candidate, ""
}

func (a *Applier) readConfig() (string, []byte, error) {
	b, err := readNoFollow(filepath.Dir(a.Config), filepath.Base(a.Config), maxConfigBytes)
	if err != nil {
		return "", nil, err
	}
	return sha(b), b, nil
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// apply is steps 3 to 6 of design D4.
func (a *Applier) apply(ctx context.Context, req *Request, keys []string, old, candidate []byte) error {
	st := &State{ID: req.ID, Phase: PhaseBackup, Keys: keys, OldSHA256: sha(old), NewSHA256: sha(candidate),
		StartedAt: a.now().UTC()}
	if err := a.saveState(st); err != nil {
		return err
	}
	fi, err := os.Stat(a.Config)
	if err != nil {
		return a.finish(st, RolledBack, "config.toml: "+err.Error())
	}
	if err := writeDurable(filepath.Join(a.State, BackupFile), old, 0o640, ""); err != nil {
		return a.finish(st, RolledBack, "the backup could not be written: "+err.Error())
	}
	st.PrevBoot, _ = a.bootID(ctx, 3*time.Second)
	st.Phase = PhaseReplacing
	if err := a.saveState(st); err != nil {
		return err
	}
	if err := writeDurable(a.Config, candidate, fi.Mode().Perm(), a.Config); err != nil {
		if sum, _, rerr := a.readConfig(); rerr == nil && sum == st.OldSHA256 {
			return a.finish(st, RolledBack, "config.toml could not be replaced: "+err.Error())
		}
		return a.rollback(context.WithoutCancel(ctx), st, "config.toml could not be replaced: "+err.Error())
	}
	st.Phase = PhaseRestarting
	if err := a.saveState(st); err != nil {
		return a.rollback(context.WithoutCancel(ctx), st, err.Error())
	}
	a.logger().Info("settings: applying", "id", st.ID, "keys", keys)
	boot, err := a.restart(ctx, st.PrevBoot)
	if err != nil {
		return a.rollback(context.WithoutCancel(ctx), st, err.Error())
	}
	st.Phase, st.BootID = PhaseConfirming, boot
	st.Deadline = a.now().UTC().Add(orDefault(a.ConfirmWindow, 2*time.Minute))
	if err := a.saveState(st); err != nil {
		return a.rollback(context.WithoutCancel(ctx), st, err.Error())
	}
	if err := a.waitConfirm(ctx, st); err != nil {
		return a.rollback(context.WithoutCancel(ctx), st, err.Error())
	}
	st.Phase = PhaseConfirmed
	if err := a.saveState(st); err != nil {
		return err
	}
	return a.finish(st, Applied, "")
}

// restart restarts the server and waits until a process other than prev answers ready.
func (a *Applier) restart(ctx context.Context, prev string) (string, error) {
	wait, cancel := context.WithTimeout(ctx, orDefault(a.ReadyTimeout, 60*time.Second))
	defer cancel()
	if err := a.Server.Restart(wait); err != nil {
		return "", fmt.Errorf("the restart failed: %w", err)
	}
	last := errors.New("no answer")
	for {
		boot, err := a.Server.BootID(wait)
		if err == nil && boot != "" && boot != prev {
			return boot, nil
		}
		if err != nil {
			last = err
		}
		select {
		case <-wait.Done():
			if ctx.Err() != nil {
				return "", errors.New("interrupted while the server restarted")
			}
			return "", fmt.Errorf("the server did not become ready within %s: %w", orDefault(a.ReadyTimeout, 60*time.Second), last)
		case <-time.After(orDefault(a.Poll, time.Second)):
		}
	}
}

func (a *Applier) bootID(ctx context.Context, d time.Duration) (string, error) {
	c, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return a.Server.BootID(c)
}

// waitConfirm waits for the owner's confirmation of this request and this server process, until the deadline.
func (a *Applier) waitConfirm(ctx context.Context, st *State) error {
	for {
		b, err := readNoFollow(a.Requests, ConfirmFile, maxConfirmBytes)
		if err == nil {
			var c Confirm
			if decodeStrict(b, &c) == nil && c.ID == st.ID && c.BootID == st.BootID {
				removeNoFollow(a.Requests, ConfirmFile)
				return nil
			}
		}
		if !a.now().Before(st.Deadline) {
			return fmt.Errorf("not confirmed within %s", orDefault(a.ConfirmWindow, 2*time.Minute))
		}
		select {
		case <-ctx.Done():
			return errors.New("interrupted while waiting for the confirmation")
		case <-time.After(orDefault(a.Poll, time.Second)):
		}
	}
}

// rollback puts the backup back durably, restarts the server and waits for it; the result says why.
func (a *Applier) rollback(ctx context.Context, st *State, reason string) error {
	a.logger().Warn("settings: rolling back", "id", st.ID, "reason", reason)
	st.Phase = PhaseRollingBack
	if err := a.saveState(st); err != nil {
		return err
	}
	b, err := readNoFollow(a.State, BackupFile, maxConfigBytes)
	if err != nil || sha(b) != st.OldSHA256 {
		return fmt.Errorf("the backup %s is missing or damaged: restore config.toml by hand", filepath.Join(a.State, BackupFile))
	}
	fi, err := os.Stat(a.Config)
	perm := fs.FileMode(0o644)
	if err == nil {
		perm = fi.Mode().Perm()
	}
	if err := writeDurable(a.Config, b, perm, a.Config); err != nil {
		return fmt.Errorf("restore config.toml: %w", err) // the state stays: the next start tries again
	}
	removeNoFollow(a.Requests, ConfirmFile)
	if _, err := a.restart(ctx, st.BootID); err != nil {
		reason += "; the server is not ready with the previous file either: " + err.Error()
	}
	return a.finish(st, RolledBack, reason)
}

// finish writes the result and clears the state.
func (a *Applier) finish(st *State, outcome, reason string) error {
	if err := a.addResult(Result{ID: st.ID, Outcome: outcome, Reason: reason, Keys: st.Keys, At: a.now().UTC()}); err != nil {
		return err
	}
	a.logger().Info("settings: done", "id", st.ID, "outcome", outcome, "reason", reason)
	return a.clearState()
}

func (a *Applier) refuse(id string, keys []string, reason string) error {
	a.logger().Warn("settings: request refused", "id", id, "reason", reason)
	return a.addResult(Result{ID: id, Outcome: Refused, Reason: reason, Keys: keys, At: a.now().UTC()})
}

func (a *Applier) saveState(st *State) error {
	return writeJSON(filepath.Join(a.State, StateFile), st, 0o640)
}

func (a *Applier) clearState() error {
	if err := os.Remove(filepath.Join(a.State, StateFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(a.State)
}

// maxResults are kept, newest last.
const maxResults = 20

func (a *Applier) addResult(r Result) error {
	var rs []Result
	if err := readJSON(a.State, ResultsFile, &rs); err != nil {
		rs = nil // a damaged list is replaced
	}
	rs = append(rs, r)
	if len(rs) > maxResults {
		rs = rs[len(rs)-maxResults:]
	}
	return writeJSON(filepath.Join(a.State, ResultsFile), rs, 0o640)
}

func (a *Applier) resultOf(id string) *Result {
	rs, _ := ReadResults(a.State)
	return lastResult(rs, id)
}

func lastResult(rs []Result, id string) *Result {
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i].ID == id {
			return &rs[i]
		}
	}
	return nil
}

// consumed reports whether id was seen, and how many changes were applied in the last hour.
func (a *Applier) consumed(id string) (bool, int, error) {
	var cs []consumed
	if err := readJSON(a.State, ConsumedFile, &cs); err != nil {
		return false, 0, fmt.Errorf("%s: %w", ConsumedFile, err) // never forget what was consumed: fail closed
	}
	seen, n := false, 0
	for _, c := range cs {
		if c.ID == id {
			seen = true
		}
		if c.Apply && a.now().Sub(c.At) < time.Hour {
			n++
		}
	}
	return seen, n, nil
}

// maxConsumed ids are kept: only requests carried out are recorded, three an hour at most, so the list spans far
// more than maxRequestAge.
const maxConsumed = 1000

// maxRequestAge is how long a request may wait for the applier.
const maxRequestAge = time.Hour

func (a *Applier) consume(id string, apply bool) error {
	var cs []consumed
	if err := readJSON(a.State, ConsumedFile, &cs); err != nil {
		return err
	}
	cs = append(cs, consumed{ID: id, At: a.now().UTC(), Apply: apply})
	if len(cs) > maxConsumed {
		cs = cs[len(cs)-maxConsumed:]
	}
	return writeJSON(filepath.Join(a.State, ConsumedFile), cs, 0o640)
}

// ReadResults reads the applier's results (newest last).
func ReadResults(stateDir string) ([]Result, error) {
	var rs []Result
	err := readJSON(stateDir, ResultsFile, &rs)
	return rs, err
}

// ReadState reads the transaction in progress; nil when there is none.
func ReadState(stateDir string) (*State, error) {
	var st State
	if err := readJSON(stateDir, StateFile, &st); err != nil || st.ID == "" {
		return nil, err
	}
	return &st, nil
}

// ReadPresence reads what the applier announced; nil when no applier ran here.
func ReadPresence(stateDir string) (*Presence, error) {
	var p Presence
	if err := readJSON(stateDir, PresenceFile, &p); err != nil || p.Config == "" {
		return nil, err
	}
	return &p, nil
}
