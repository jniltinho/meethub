// Package files keeps the bytes of the chat's attachments: the storage
// drivers (a directory or an S3-compatible bucket), uploads with their derived
// images, downloads and the cleanup. The metadata and who may read a file
// belong to internal/chat.
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrUnavailable is returned while the storage cannot be reached or written.
var ErrUnavailable = errors.New("file storage unavailable")

// ErrNoObject is returned by Open for a key that is not stored.
var ErrNoObject = errors.New("no such object")

// Storage keeps objects under server-generated keys ("a/b/c", never client text).
type Storage interface {
	// Put stores r under key; size is -1 when unknown. A failed Put leaves no object.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Open returns the object for reading from any offset.
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	// Delete removes the object; a missing one is not an error.
	Delete(ctx context.Context, key string) error
	// Check reports whether the storage can be used now (readiness).
	Check(ctx context.Context) error
}

// Local stores objects as files under a directory (shared by every replica).
type Local struct{ root *os.Root }

// NewLocal opens (creating it when missing) the directory of a local storage.
func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("files directory: %w", err)
	}
	root, err := os.OpenRoot(dir) // every path below is resolved inside dir
	if err != nil {
		return nil, fmt.Errorf("files directory: %w", err)
	}
	return &Local{root: root}, nil
}

func localKey(key string) (string, error) {
	if !filepath.IsLocal(key) {
		return "", fmt.Errorf("storage key %q: %w", key, fs.ErrInvalid)
	}
	return filepath.FromSlash(key), nil
}

// Put writes to a temporary name and renames, so readers never see a partial file.
func (l *Local) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	name, err := localKey(key)
	if err != nil {
		return err
	}
	if err := l.root.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	tmp := name + ".part"
	f, err := l.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	lr := &lastErrReader{r: r}
	_, err = io.Copy(f, lr)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = l.root.Rename(tmp, name)
	}
	if err != nil {
		_ = l.root.Remove(tmp)
		if lr.err != nil && !errors.Is(lr.err, io.EOF) {
			return lr.err // the reader's own error (a size limit), not the storage's
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

// lastErrReader remembers the error its reader ended with.
type lastErrReader struct {
	r   io.Reader
	err error
}

func (l *lastErrReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if err != nil {
		l.err = err
	}
	return n, err
}

// Open opens the stored file.
func (l *Local) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	name, err := localKey(key)
	if err != nil {
		return nil, err
	}
	f, err := l.root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoObject
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return f, nil
}

// Delete removes the stored file.
func (l *Local) Delete(_ context.Context, key string) error {
	name, err := localKey(key)
	if err != nil {
		return err
	}
	if err := l.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	_ = l.root.Remove(filepath.Dir(name)) // the file's own directory, when now empty
	return nil
}

// Check proves the directory is writable.
func (l *Local) Check(_ context.Context) error {
	f, err := l.root.OpenFile(".ready", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return ErrUnavailable
	}
	_ = f.Close()
	return nil
}
