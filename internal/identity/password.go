package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Password length limits, in bytes.
const (
	MinPasswordBytes = 12
	MaxPasswordBytes = 256
)

// Password errors.
var (
	ErrWeakPassword     = errors.New("password must be at least 12 bytes")
	ErrPasswordTooLong  = errors.New("password must be at most 256 bytes")
	ErrBusy             = errors.New("password hashing is saturated, try again")
	ErrMalformedHash    = errors.New("malformed password hash")
	ErrPasswordMismatch = errors.New("password does not match")
)

// CheckPassword enforces the length policy.
func CheckPassword(pw string) error {
	switch {
	case len(pw) > MaxPasswordBytes:
		return ErrPasswordTooLong
	case len(pw) < MinPasswordBytes:
		return ErrWeakPassword
	}
	return nil
}

// Argon2Params are the Argon2id cost parameters.
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// DefaultArgon2 is the production cost: 64 MiB, 3 passes, 2 lanes.
var DefaultArgon2 = Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 2}

// Hasher hashes and verifies passwords with Argon2id, bounding concurrent
// hash computations so login floods cannot exhaust memory.
type Hasher struct {
	params Argon2Params
	slots  chan struct{}
	wait   time.Duration
	dummy  string
}

// NewHasher builds a hasher with slots concurrent computations (GOMAXPROCS
// when <= 0) and the given wait for a free slot (5 s when <= 0).
func NewHasher(p Argon2Params, slots int, wait time.Duration) *Hasher {
	if slots <= 0 {
		slots = runtime.GOMAXPROCS(0)
	}
	if wait <= 0 {
		wait = 5 * time.Second
	}
	h := &Hasher{params: p, slots: make(chan struct{}, slots), wait: wait}
	h.dummy = h.compute("meethub-dummy-password", []byte("meethub-dummy-sa"))
	return h
}

func (h *Hasher) acquire(ctx context.Context) error {
	t := time.NewTimer(h.wait)
	defer t.Stop()
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-t.C:
		return ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.slots }

// Hash returns the PHC string of pw ($argon2id$v=19$m=…,t=…,p=…$salt$hash).
func (h *Hasher) Hash(ctx context.Context, pw string) (string, error) {
	if err := CheckPassword(pw); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	return h.compute(pw, salt), nil
}

func (h *Hasher) compute(pw string, salt []byte) string {
	p := h.params
	key := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key))
}

// Verify checks pw against a PHC hash in constant time. It returns
// ErrPasswordMismatch on a wrong password.
func (h *Hasher) Verify(ctx context.Context, encoded, pw string) error {
	if len(pw) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	p, salt, want, err := parsePHC(encoded)
	if err != nil {
		return err
	}
	if err := h.acquire(ctx); err != nil {
		return err
	}
	defer h.release()
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want))) //nolint:gosec // key length is 32
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

// VerifyDummy spends the same work as Verify against a fixed hash, so unknown
// accounts cannot be told apart by timing. It always returns ErrPasswordMismatch
// (or ErrBusy / a context error).
func (h *Hasher) VerifyDummy(ctx context.Context, pw string) error {
	if err := h.Verify(ctx, h.dummy, pw); err != nil && !errors.Is(err, ErrPasswordMismatch) {
		return err
	}
	return ErrPasswordMismatch
}

func parsePHC(s string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, ErrMalformedHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return Argon2Params{}, nil, nil, ErrMalformedHash
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(key) == 0 {
		return Argon2Params{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}
