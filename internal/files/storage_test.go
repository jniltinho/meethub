package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/testenv"
)

var ctx = context.Background()

// s3Config is the test bucket's configuration: a fresh bucket per test.
func s3Config(t *testing.T) config.FilesS3T {
	t.Helper()
	srv := testenv.S3(t)
	return config.FilesS3T{
		Endpoint: srv.Endpoint, Bucket: srv.NewBucket(t), Region: "us-east-1", AccessKey: srv.AccessKey, SecretKey: srv.SecretKey,
	}
}

// eachStorage runs fn against the local driver and, with the test stack, the S3 one.
func eachStorage(t *testing.T, fn func(t *testing.T, st files.Storage)) {
	t.Run("local", func(t *testing.T) {
		st, err := files.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		fn(t, st)
	})
	t.Run("s3", func(t *testing.T) {
		st, err := files.NewS3(s3Config(t))
		if err != nil {
			t.Fatal(err)
		}
		fn(t, st)
	})
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestStorage(t *testing.T) {
	eachStorage(t, func(t *testing.T, st files.Storage) {
		if err := st.Check(ctx); err != nil {
			t.Fatalf("check: %v", err)
		}
		const key = "20260101/chan/file/file"
		data := bytes.Repeat([]byte("meethub "), 4096)
		if err := st.Put(ctx, key, bytes.NewReader(data), -1); err != nil {
			t.Fatalf("put: %v", err)
		}
		if err := st.Put(ctx, key+"_thumb.jpg", strings.NewReader("thumb"), 5); err != nil {
			t.Fatalf("put with a known size: %v", err)
		}
		rc, err := st.Open(ctx, key)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		got, err := io.ReadAll(rc)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("read back %d bytes, %v", len(got), err)
		}
		// seekable: what range requests need
		if size, err := rc.Seek(0, io.SeekEnd); err != nil || size != int64(len(data)) {
			t.Fatalf("size by seek: %d %v", size, err)
		}
		if _, err := rc.Seek(8, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		part := make([]byte, 7)
		if _, err := io.ReadFull(rc, part); err != nil || string(part) != "meethub" {
			t.Fatalf("read at offset: %q %v", part, err)
		}
		_ = rc.Close()

		// a reader that fails leaves nothing and its own error comes back
		limit := errors.New("limit reached")
		err = st.Put(ctx, "20260101/chan/big/big", io.MultiReader(bytes.NewReader(data), failingReader{limit}), -1)
		if !errors.Is(err, limit) {
			t.Fatalf("failed put: %v", err)
		}
		if _, err := st.Open(ctx, "20260101/chan/big/big"); !errors.Is(err, files.ErrNoObject) {
			t.Fatalf("object left by a failed put: %v", err)
		}

		if err := st.Delete(ctx, key); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if err := st.Delete(ctx, key); err != nil {
			t.Fatalf("delete twice: %v", err)
		}
		if _, err := st.Open(ctx, key); !errors.Is(err, files.ErrNoObject) {
			t.Fatalf("open after delete: %v", err)
		}
	})
}

// TestLocalKeysStayInside: a key is never client text, and even a hostile one
// cannot leave the directory.
func TestLocalKeysStayInside(t *testing.T) {
	dir := t.TempDir()
	st, err := files.NewLocal(dir + "/files")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../escape", "/etc/passwd", "a/../../escape", ""} {
		if err := st.Put(ctx, key, strings.NewReader("x"), 1); err == nil {
			t.Errorf("put %q accepted", key)
		}
		if _, err := st.Open(ctx, key); err == nil {
			t.Errorf("open %q accepted", key)
		}
	}
	if _, err := os.Stat(dir + "/escape"); err == nil {
		t.Fatal("a file escaped the storage directory")
	}
}

func TestStorageUnavailable(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		dir := t.TempDir() + "/files"
		st, err := files.NewLocal(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := st.Check(ctx); !errors.Is(err, files.ErrUnavailable) {
			t.Fatalf("check: %v", err)
		}
		if err := st.Put(ctx, "a/b", strings.NewReader("x"), 1); !errors.Is(err, files.ErrUnavailable) {
			t.Fatalf("put: %v", err)
		}
	})
	t.Run("s3", func(t *testing.T) {
		cfg := s3Config(t)
		cfg.Endpoint = "127.0.0.1:1" // nothing listens there
		st, err := files.NewS3(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Check(ctx); !errors.Is(err, files.ErrUnavailable) {
			t.Fatalf("check: %v", err)
		}
		if err := st.Put(ctx, "a/b", strings.NewReader("x"), 1); !errors.Is(err, files.ErrUnavailable) {
			t.Fatalf("put: %v", err)
		}
		if _, err := st.Open(ctx, "a/b"); !errors.Is(err, files.ErrUnavailable) {
			t.Fatalf("open: %v", err)
		}
	})
}
