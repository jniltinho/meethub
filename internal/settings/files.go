// Package settings carries the changes of Administration → Settings to config.toml (add-admin-settings): the server
// validates a change and leaves a request in its mailbox; the applier, a separate process running as root, checks
// the request again, replaces the file durably, restarts the server, waits for the owner's confirmation and rolls
// back without it.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

// Fixed names: nothing the server writes is ever part of a path (design D4a).
const (
	// in the mailbox, <data_dir>/settings: the service's own directory, hostile to the applier
	RequestFile = "request.json"
	ConfirmFile = "confirm.json"
	PendingFile = "pending.json" // the server's own record of its request; the applier never reads it
	// in the applier's directory (<config dir>/settings-state): root's, readable by the service
	PresenceFile = "applier.json"
	StateFile    = "state.json"
	ResultsFile  = "results.json"
	ConsumedFile = "consumed.json"
	BackupFile   = "config.toml.bak"
	lockFile     = "lock"
	// StateDirName is the applier's directory, beside config.toml.
	StateDirName = "settings-state"
)

// Bounds of what is read from the mailbox and of config.toml.
const (
	maxRequestBytes = 16 << 10
	maxConfirmBytes = 1 << 10
	maxConfigBytes  = 1 << 20
	maxStateBytes   = 256 << 10
)

// Outcomes of a request.
const (
	Applied    = "applied"
	RolledBack = "rolled_back"
	Refused    = "refused"
)

// Phases of a transaction, in the applier's state file.
const (
	PhaseBackup      = "backup"       // the backup is being written; config.toml is the old one
	PhaseReplacing   = "replacing"    // config.toml is being replaced: old or new, whole
	PhaseRestarting  = "restarting"   // the new file is in place; the server restarts
	PhaseConfirming  = "confirming"   // the server is ready with it; waiting for the owner
	PhaseConfirmed   = "confirmed"    // the owner kept it
	PhaseRollingBack = "rolling_back" // the backup is being put back
)

// Request is what the server leaves for the applier.
type Request struct {
	ID           string                     `json:"id"`
	UserID       string                     `json:"user_id"`
	CreatedAt    time.Time                  `json:"created_at"`
	ConfigSHA256 string                     `json:"config_sha256"`
	Keys         map[string]json.RawMessage `json:"keys"`
}

// Confirm is the owner's "Keep these settings": the request and the server process the applier restarted.
type Confirm struct {
	ID     string `json:"id"`
	BootID string `json:"boot_id"`
}

// State is the applier's record of the transaction in progress.
type State struct {
	ID        string    `json:"id"`
	Phase     string    `json:"phase"`
	Keys      []string  `json:"keys"`
	OldSHA256 string    `json:"old_sha256"`
	NewSHA256 string    `json:"new_sha256"`
	StartedAt time.Time `json:"started_at"`
	PrevBoot  string    `json:"prev_boot_id,omitempty"`
	BootID    string    `json:"boot_id,omitempty"`
	Deadline  time.Time `json:"deadline,omitzero"`
}

// Result ends a request.
type Result struct {
	ID      string    `json:"id"`
	Outcome string    `json:"outcome"`
	Reason  string    `json:"reason,omitempty"`
	Keys    []string  `json:"keys,omitempty"`
	At      time.Time `json:"at"`
}

// Presence tells the server an applier serves this host, and which files.
type Presence struct {
	Config    string    `json:"config"`
	Requests  string    `json:"requests"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
}

type consumed struct {
	ID    string    `json:"id"`
	At    time.Time `json:"at"`
	Apply bool      `json:"apply"`
}

var (
	idPattern  = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`) // a ULID
	sumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// errNotRegular is a mailbox name that is a link, a directory, a device or too large.
var errNotRegular = errors.New("not a regular file of a bounded size")

// readNoFollow reads name in dir once into memory: neither dir's last component nor name may be a link, and name
// must be a regular file of at most limit bytes. A missing file is fs.ErrNotExist.
func readNoFollow(dir, name string, limit int64) ([]byte, error) {
	dfd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, fs.ErrNotExist
		}
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	defer func() { _ = syscall.Close(dfd) }()
	return readAt(dfd, name, limit)
}

func readAt(dfd int, name string, limit int64) ([]byte, error) {
	fd, err := syscall.Openat(dfd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	switch {
	case errors.Is(err, syscall.ENOENT):
		return nil, fs.ErrNotExist
	case errors.Is(err, syscall.ELOOP):
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	case err != nil:
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > limit {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	return b, nil
}

// takeNoFollow reads name in dir like readNoFollow and removes that name; it fails with fs.ErrNotExist when the
// name was removed in between (the server withdrew its request), so a file is taken only once.
func takeNoFollow(dir, name string, limit int64) ([]byte, error) {
	dfd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, fs.ErrNotExist
		}
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	defer func() { _ = syscall.Close(dfd) }()
	b, readErr := readAt(dfd, name, limit)
	if errors.Is(readErr, fs.ErrNotExist) {
		return nil, readErr
	}
	// whatever it was (a link, a FIFO, a file too large): the name goes, so the path unit does not fire again
	if err := syscall.Unlinkat(dfd, name); err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, fs.ErrNotExist
		}
		return nil, fmt.Errorf("remove %s: %w", name, err)
	}
	return b, readErr
}

// removeNoFollow removes name in dir (the name itself, never what a link points to).
func removeNoFollow(dir, name string) {
	dfd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer func() { _ = syscall.Close(dfd) }()
	_ = syscall.Unlinkat(dfd, name)
}

// writeDurable replaces path with b: a temporary file beside it, synced, renamed over it, the directory synced. A
// stop at any point leaves the old file or the new one, whole. The mode is perm; the owner, when ref exists, is
// ref's (else the directory's group, through its set-group-id bit).
func writeDurable(path string, b []byte, perm fs.FileMode, ref string) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if fi, err := os.Lstat(ref); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && (int(st.Uid) != os.Geteuid() || int(st.Gid) != os.Getegid()) {
			if err := f.Chown(int(st.Uid), int(st.Gid)); err != nil {
				_ = f.Close()
				return err
			}
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // our own directory
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// writeJSON writes v durably as JSON.
func writeJSON(path string, v any, perm fs.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeDurable(path, append(b, '\n'), perm, "")
}

// decodeStrict decodes one JSON value, unknown fields refused.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// readJSON reads one of the applier's own files (a missing one leaves v as it is).
func readJSON(dir, name string, v any) error {
	b, err := readNoFollow(dir, name, maxStateBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
