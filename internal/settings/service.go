package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pelletier/go-toml/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
)

// Errors of the server's side.
var (
	ErrReadOnly        = errors.New("settings cannot be changed on this install: no settings applier serves it")
	ErrPending         = errors.New("a change is already being applied: wait for its result")
	ErrNotConfirmable  = errors.New("this change cannot be confirmed: it is not waiting for you, or the time is over")
	ErrTooMany         = errors.New("too many setting changes: try again later")
	ErrNothingToChange = errors.New("nothing to change")
)

// LockedError is a key set by its environment variable: the variable wins at load (design D6).
type LockedError struct{ Key, Env string }

func (e *LockedError) Error() string {
	return e.Key + " is set by " + e.Env + " and cannot be changed here"
}

// InvalidConfigError is a change the whole configuration refuses, as at startup.
type InvalidConfigError struct{ Msg string }

func (e *InvalidConfigError) Error() string { return e.Msg }

// Service is the server's side: the view, the request and the confirmation, and their audit.
type Service struct {
	DB       *gorm.DB
	Config   string // the file the server loaded; "": none (read-only)
	Requests string // the mailbox, <data_dir>/settings
	State    string // the applier's directory, <config dir>/settings-state
	Lookup   func(string) (string, bool)
	Running  *config.Config // what the server runs with
	BootID   string
	Host     string
	Limit    *ratelimit.Window // per user; nil: unlimited
	// Check refuses a change the running server knows it cannot take now (values as ParseAdminValue returns them)
	Check  func(ctx context.Context, values map[string]any) error
	Redis  *redis.Client // the replicas' heartbeat; nil: one replica assumed
	Logger *slog.Logger
	Now    func() time.Time
	// StallAfter: a request the applier has not taken by then is withdrawn ("the applier is not running")
	StallAfter time.Duration

	mu sync.Mutex // one request and one reconciliation at a time in this process (one replica, design D8)
}

// New fills the paths from the server's configuration: the mailbox in the data directory, the applier's
// directory beside config.toml.
func New(db *gorm.DB, cfg *config.Config, configFile, bootID string) *Service {
	host, _ := os.Hostname()
	s := &Service{DB: db, Config: configFile, Running: cfg, BootID: bootID, Host: host, Lookup: os.LookupEnv,
		Requests: filepath.Join(cfg.App.DataDir, "settings")}
	if abs, err := filepath.Abs(s.Requests); err == nil {
		s.Requests = abs
	}
	if configFile != "" {
		s.State = filepath.Join(filepath.Dir(configFile), StateDirName)
	}
	return s
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Logger
}

// change is one key's old and new value, as recorded.
type change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// pending is the server's record of its request (in its own mailbox).
type pending struct {
	ID        string            `json:"id"`
	UserID    string            `json:"user_id"`
	SessionID string            `json:"session_id"`
	ClientIP  string            `json:"client_ip"`
	Keys      map[string]change `json:"keys"`
	CreatedAt time.Time         `json:"created_at"`
	// Withdrawn is the reason once the server withdrew the request: closing it is retried until it succeeds
	Withdrawn string `json:"withdrawn,omitempty"`
}

// KeyView is one editable key on the page.
type KeyView struct {
	config.AdminKey
	Value   any    `json:"value"` // in effect now
	Default any    `json:"default"`
	Source  string `json:"source"` // environment, file or default
	Env     string `json:"env"`
	Locked  bool   `json:"locked"`
}

// ReadOnlyView is one other setting: its value (a secret: only whether it is set) and why it is read-only.
type ReadOnlyView struct {
	Key    string `json:"key"`
	Value  any    `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	Set    bool   `json:"set,omitempty"`
	Env    string `json:"env"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// PendingView is the request in progress.
type PendingView struct {
	ID          string            `json:"id"`
	Phase       string            `json:"phase"` // waiting, applying, confirming, rolling_back
	Keys        map[string]change `json:"keys"`
	Mine        bool              `json:"mine"`
	CanConfirm  bool              `json:"can_confirm"`
	SecondsLeft int               `json:"seconds_left,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// ApplierView says whether changes can be made here, and if not why: no_file, no_applier, other_file.
type ApplierView struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// View is GET /api/v1/admin/settings.
type View struct {
	Keys       []KeyView      `json:"keys"`
	ReadOnly   []ReadOnlyView `json:"read_only"`
	Applier    ApplierView    `json:"applier"`
	Pending    *PendingView   `json:"pending,omitempty"`
	LastResult *Result        `json:"last_result,omitempty"`
	Host       string         `json:"host"`
	Replicas   []string       `json:"replicas,omitempty"`
}

// Get returns the view to someone with manage_system.
func (s *Service) Get(ctx context.Context, actor org.Actor, sessionID string) (*View, error) {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return nil, err
	}
	s.Reconcile(ctx)
	inFile := s.fileKeys()
	def := config.Default()
	v := &View{Applier: s.applier(), Host: s.Host, Replicas: s.replicas(ctx)}
	for _, k := range config.AdminKeys {
		kv := KeyView{AdminKey: k, Value: s.Running.AdminValue(k.Key), Default: def.AdminValue(k.Key), Env: k.Env(),
			Source: s.source(k.Env(), k.Key, inFile)}
		kv.Locked = kv.Source == "environment"
		v.Keys = append(v.Keys, kv)
	}
	for _, f := range s.Running.Fields() {
		if _, err := config.FindAdminKey(f.Key); err == nil {
			continue
		}
		if f.Key == "ai.api_key" || f.Key == "ai.transcription.api_key" {
			continue // entered in the keys form of the Meeting summaries tab
		}
		ro := ReadOnlyView{Key: f.Key, Env: f.Env, Source: s.source(f.Env, f.Key, inFile), Reason: readOnlyReason(f.Key, f.Secret)}
		if f.Secret {
			ro.Secret, ro.Set = true, f.Value == true
			if _, ok := s.lookup(f.Env + "_FILE"); ok {
				ro.Source = "environment"
			}
		} else {
			ro.Value = f.Value
		}
		v.ReadOnly = append(v.ReadOnly, ro)
	}
	if p := s.readPending(); p != nil {
		v.Pending = s.pendingView(p, actor, sessionID)
	}
	if rs, _ := ReadResults(s.State); len(rs) > 0 && s.State != "" {
		v.LastResult = &rs[len(rs)-1]
	}
	if b, err := os.ReadFile(filepath.Join(s.Requests, ServerResult)); err == nil {
		var r Result
		if json.Unmarshal(b, &r) == nil && (v.LastResult == nil || !r.At.Before(v.LastResult.At)) {
			v.LastResult = &r
		}
	}
	return v, nil
}

func (s *Service) lookup(env string) (string, bool) {
	if s.Lookup == nil {
		return os.LookupEnv(env)
	}
	return s.Lookup(env)
}

func (s *Service) source(env, key string, inFile map[string]bool) string {
	if _, ok := s.lookup(env); ok {
		return "environment"
	}
	if inFile[key] {
		return "file"
	}
	return "default"
}

// fileKeys are the keys config.toml sets.
func (s *Service) fileKeys() map[string]bool {
	out := map[string]bool{}
	if s.Config == "" {
		return out
	}
	b, err := os.ReadFile(s.Config)
	if err != nil {
		return out
	}
	var doc map[string]any
	if toml.Unmarshal(b, &doc) != nil {
		return out
	}
	var visit func(prefix string, m map[string]any)
	visit = func(prefix string, m map[string]any) {
		for k, v := range m {
			if sub, ok := v.(map[string]any); ok {
				visit(prefix+k+".", sub)
				continue
			}
			out[prefix+k] = true
		}
	}
	visit("", doc)
	return out
}

// readOnlyReason says why a setting is not on the page's allowlist (design D2).
func readOnlyReason(key string, secret bool) string {
	has := func(prefixes ...string) bool {
		return slices.ContainsFunc(prefixes, func(p string) bool { return key == p || strings.HasPrefix(key, p+".") })
	}
	switch {
	case secret:
		return "secret"
	case has("smtp", "oidc.issuer", "oidc.client_id", "oidc.scopes", "livekit.url", "livekit.api_url"):
		return "destination"
	case has("http.trusted_proxies"):
		return "trust"
	case has("http.public_url", "auth.password_login", "http.body_limit_bytes", "http.addr", "http.cert_file", "http.key_file", "http.self_signed"):
		return "lockout"
	case has("app", "database", "redis", "files.driver", "files.local", "files.s3"):
		return "data"
	case has("oidc.auto_create", "oidc.allowed_domains", "oidc.default_role", "notifications.email_excerpt"):
		return "irreversible"
	}
	return "other"
}

// applier tells whether an applier serves this server's file and mailbox.
func (s *Service) applier() ApplierView {
	if s.Config == "" {
		return ApplierView{Reason: "no_file"}
	}
	p, err := ReadPresence(s.State)
	if err != nil || p == nil {
		return ApplierView{Reason: "no_applier"}
	}
	if p.Config != s.Config || p.Requests != s.Requests {
		return ApplierView{Reason: "other_file"}
	}
	return ApplierView{Available: true}
}

func (s *Service) pendingView(p *pending, actor org.Actor, sessionID string) *PendingView {
	pv := &PendingView{ID: p.ID, Keys: p.Keys, Mine: p.UserID == actor.UserID, CreatedAt: p.CreatedAt, Phase: "applying"}
	if _, err := os.Lstat(filepath.Join(s.Requests, RequestFile)); err == nil {
		pv.Phase = "waiting"
	}
	if st, _ := ReadState(s.State); st != nil && st.ID == p.ID {
		switch st.Phase {
		case PhaseConfirming:
			pv.Phase = "confirming"
			if left := st.Deadline.Sub(s.now()); left > 0 {
				pv.SecondsLeft = int(left.Seconds())
				pv.CanConfirm = pv.Mine && p.SessionID == sessionID && st.BootID == s.BootID
			}
		case PhaseRollingBack:
			pv.Phase = "rolling_back"
		}
	}
	return pv
}

// Request validates a change as at startup and leaves it for the applier (design D4). changes maps keys to new
// values; restore names keys to put back to their built-in default.
func (s *Service) Request(ctx context.Context, actor org.Actor, sessionID string, changes map[string]json.RawMessage, restore []string) (*View, error) {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return nil, err
	}
	if !s.applier().Available {
		return nil, ErrReadOnly
	}
	def := config.Default()
	values := map[string]any{}
	for _, k := range restore {
		if _, err := config.FindAdminKey(k); err != nil {
			return nil, err
		}
		values[k] = def.AdminValue(k)
	}
	for k, raw := range changes {
		if _, dup := values[k]; dup {
			return nil, &config.AdminValueError{Key: k, Msg: "is both changed and restored"}
		}
		v, err := config.ParseAdminValue(k, raw)
		if err != nil {
			return nil, err
		}
		values[k] = v
	}
	if len(values) == 0 {
		return nil, ErrNothingToChange
	}
	for _, k := range slices.Sorted(maps.Keys(values)) {
		key, _ := config.FindAdminKey(k)
		if _, ok := s.lookup(key.Env()); ok {
			return nil, &LockedError{Key: k, Env: key.Env()}
		}
	}
	if s.Check != nil {
		if err := s.Check(ctx, values); err != nil {
			return nil, err
		}
	}
	if err := s.publish(ctx, actor, sessionID, values); err != nil {
		return nil, err
	}
	return s.Get(ctx, actor, sessionID)
}

// publish leaves the request for the applier, one at a time: the whole configuration validated as at startup, the
// request audited, then the server's record and the request written.
func (s *Service) publish(ctx context.Context, actor org.Actor, sessionID string, values map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcile(ctx)
	if s.readPending() != nil {
		return ErrPending
	}
	if s.Limit != nil {
		if ok, _ := s.Limit.Allow(ctx, actor.UserID); !ok {
			return ErrTooMany
		}
	}
	old, err := readLimited(s.Config)
	if err != nil {
		return err
	}
	candidate, err := config.EditAdmin(old, values)
	if err != nil {
		return err
	}
	if _, err := config.LoadWith(s.Config, s.lookup, func(p string) ([]byte, error) {
		if p == s.Config {
			return candidate, nil
		}
		return os.ReadFile(p) //nolint:gosec // a _FILE secret named by the environment, as at startup
	}); err != nil {
		return &InvalidConfigError{Msg: strings.ReplaceAll(strings.TrimPrefix(err.Error(), "invalid configuration:\n"), "\n", "; ")}
	}
	before, err := config.FileValues(old)
	if err != nil {
		return err
	}
	p := &pending{ID: ulid.Make().String(), UserID: actor.UserID, SessionID: sessionID, ClientIP: actor.ClientIP,
		Keys: map[string]change{}, CreatedAt: s.now().UTC()}
	req := Request{ID: p.ID, UserID: actor.UserID, CreatedAt: p.CreatedAt, ConfigSHA256: sha(old), Keys: map[string]json.RawMessage{}}
	for k, v := range values {
		p.Keys[k] = change{Old: before.AdminValue(k), New: v}
		req.Keys[k], _ = json.Marshal(v)
	}
	if err := os.MkdirAll(s.Requests, 0o750); err != nil {
		return err
	}
	// audited before the applier can see it: no change is applied without its record
	details := map[string]any{"request_id": p.ID, "host": s.Host}
	for k, c := range p.Keys {
		details[k] = map[string]any{"old": c.Old, "new": c.New}
	}
	if err := audit.Record(ctx, s.DB, s.event(actor.UserID, actor.ClientIP, audit.ActionSettingsRequested, details)); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.Requests, PendingFile), p, 0o640); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.Requests, RequestFile), req, 0o640); err != nil {
		_ = os.Remove(filepath.Join(s.Requests, PendingFile))
		return err
	}
	return nil
}

func readLimited(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxConfigBytes {
		return nil, fmt.Errorf("%s is larger than 1 MiB", path)
	}
	return os.ReadFile(path) //nolint:gosec // the server's own config file
}

// Confirm is "Keep these settings": only the owner who made the request, in the same session, while the applier
// waits for this very process (design D4a).
func (s *Service) Confirm(ctx context.Context, actor org.Actor, sessionID, id string) error {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return err
	}
	p := s.readPending()
	if p == nil || p.ID != id || p.UserID != actor.UserID || p.SessionID != sessionID {
		return ErrNotConfirmable
	}
	st, _ := ReadState(s.State)
	if st == nil || st.ID != id || st.Phase != PhaseConfirming || st.BootID != s.BootID || !s.now().Before(st.Deadline) {
		return ErrNotConfirmable
	}
	return writeJSON(filepath.Join(s.Requests, ConfirmFile), Confirm{ID: id, BootID: s.BootID}, 0o640)
}

// CanApply reports whether an applier serves this server: changes made on the page are applied here.
func (s *Service) CanApply() bool { return s.applier().Available }

// HasPending reports whether a change is on its way or awaits "Keep these settings": what a rollback could not
// take back (the recordings' expiry) waits meanwhile.
func (s *Service) HasPending() bool { return s.readPending() != nil }

// PendingKey reports whether the change on its way, or awaiting its confirmation, names key.
func (s *Service) PendingKey(key string) bool {
	p := s.readPending()
	if p == nil {
		return false
	}
	_, ok := p.Keys[key]
	return ok
}

func (s *Service) readPending() *pending {
	b, err := os.ReadFile(filepath.Join(s.Requests, PendingFile))
	if err != nil {
		return nil
	}
	var p pending
	if json.Unmarshal(b, &p) != nil || p.ID == "" {
		return nil
	}
	return &p
}

// lostAfter: a request taken but with neither a transaction nor a result for this long is given up.
const lostAfter = 10 * time.Minute

// Reconcile records the result of the pending request once it has one, and withdraws a request no applier took.
func (s *Service) Reconcile(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcile(ctx)
}

func (s *Service) reconcile(ctx context.Context) {
	p := s.readPending()
	if p == nil {
		return
	}
	if p.Withdrawn != "" {
		s.close(ctx, p, Refused, p.Withdrawn)
		return
	}
	if rs, _ := ReadResults(s.State); s.State != "" {
		if r := lastResult(rs, p.ID); r != nil {
			s.close(ctx, p, r.Outcome, r.Reason)
			return
		}
	}
	if st, _ := ReadState(s.State); st != nil && st.ID == p.ID {
		return
	}
	age := s.now().Sub(p.CreatedAt)
	req := filepath.Join(s.Requests, RequestFile)
	if _, err := os.Lstat(req); err == nil {
		stall := s.StallAfter
		if stall <= 0 {
			stall = time.Minute
		}
		if age < stall {
			return
		}
		// a rename is atomic against the applier taking the same name: whoever gets it, gets it
		if err := os.Rename(req, req+".withdrawn"); err == nil {
			_ = os.Remove(req + ".withdrawn")
			p.Withdrawn = "the settings applier did not take the request: it is not running"
			if err := writeJSON(filepath.Join(s.Requests, PendingFile), p, 0o640); err != nil {
				s.logger().ErrorContext(ctx, "settings: pending request", "error", err)
			}
			s.close(ctx, p, Refused, p.Withdrawn)
		}
		return
	}
	if age > lostAfter {
		s.close(ctx, p, Refused, "the settings applier gave no result")
	}
}

func (s *Service) close(ctx context.Context, p *pending, outcome, reason string) {
	action := map[string]string{Applied: audit.ActionSettingsApplied, RolledBack: audit.ActionSettingsRolledBack}[outcome]
	if action == "" {
		action = audit.ActionSettingsRefused
	}
	details := map[string]any{"request_id": p.ID, "keys": slices.Sorted(maps.Keys(p.Keys)), "host": s.Host}
	if reason != "" {
		details["reason"] = reason
	}
	// the page shows it, also when no applier gave it (withdrawn, lost); the pending record stays until both the
	// result and its audit event are written, and the next reconciliation tries again
	r := Result{ID: p.ID, Outcome: outcome, Reason: reason, Keys: slices.Sorted(maps.Keys(p.Keys)), At: s.now().UTC()}
	if err := writeJSON(filepath.Join(s.Requests, ServerResult), r, 0o640); err != nil {
		s.logger().ErrorContext(ctx, "settings: result", "error", err)
		return
	}
	if err := audit.Record(ctx, s.DB, s.event(p.UserID, p.ClientIP, action, details)); err != nil {
		s.logger().ErrorContext(ctx, "settings: audit", "error", err)
		return
	}
	if err := os.Remove(filepath.Join(s.Requests, PendingFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.logger().ErrorContext(ctx, "settings: pending request", "error", err)
	}
	s.logger().InfoContext(ctx, "settings: request ended", "id", p.ID, "outcome", outcome, "reason", reason)
}

func (s *Service) event(userID, ip, action string, details map[string]any) audit.Event {
	e := audit.Event{Action: action, TargetType: "settings", TargetID: s.Host, ClientIP: ip, Details: details}
	if userID != "" {
		e.ActorID = &userID
	}
	return e
}

const hostsKey = "meethub:settings:hosts"

// Run reconciles the pending request and announces this replica, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		s.Reconcile(ctx)
		if s.Redis != nil && s.Host != "" {
			_ = s.Redis.HSet(ctx, hostsKey, s.Host, s.now().Unix()).Err()
			_ = s.Redis.Expire(ctx, hostsKey, time.Minute).Err()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// replicas lists the hosts that announced themselves in the last 30 seconds, when there is more than this one.
func (s *Service) replicas(ctx context.Context) []string {
	if s.Redis == nil {
		return nil
	}
	m, err := s.Redis.HGetAll(ctx, hostsKey).Result()
	if err != nil {
		return nil
	}
	var out []string
	for h, at := range m {
		var t int64
		if _, err := fmt.Sscan(at, &t); err == nil && s.now().Unix()-t < 30 {
			out = append(out, h)
		}
	}
	if len(out) < 2 {
		return nil
	}
	slices.Sort(out)
	return out
}
