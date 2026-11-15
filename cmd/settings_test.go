package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareConfig(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed.toml")
	file := filepath.Join(dir, "data", "config", "config.toml")
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	read := func() string {
		t.Helper()
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		name   string
		before func()
		want   string
		warn   bool
	}{
		{"the first start copies the seed", func() { write(seed, "[log]\nlevel = \"warn\"\n") }, "[log]\nlevel = \"warn\"\n", false},
		{"the same seed again", func() {}, "[log]\nlevel = \"warn\"\n", false},
		{"a change made since is kept", func() { write(file, "[log]\nlevel = \"debug\"\n") }, "[log]\nlevel = \"debug\"\n", false},
		{"a seed that changed is reported, not copied", func() { write(seed, "[log]\nlevel = \"error\"\n") }, "[log]\nlevel = \"debug\"\n", true},
		{"a deleted file starts again from the seed", func() { _ = os.Remove(file) }, "[log]\nlevel = \"error\"\n", false},
	}
	for _, s := range steps {
		log.Reset()
		s.before()
		if err := prepareConfig(seed, file, logger); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got := read(); got != s.want {
			t.Errorf("%s: file %q, want %q", s.name, got, s.want)
		}
		if warned := strings.Contains(log.String(), "seed changed"); warned != s.warn {
			t.Errorf("%s: warned %v, want %v (%s)", s.name, warned, s.warn, log.String())
		}
	}
	// no seed at all, or one that is not there: an empty file, the defaults and the environment
	for _, seed := range []string{"", filepath.Join(dir, "missing.toml")} {
		empty := filepath.Join(t.TempDir(), "config", "config.toml")
		if err := prepareConfig(seed, empty, logger); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(empty); err != nil || len(b) != 0 {
			t.Errorf("seed %q: %q %v, want an empty file", seed, b, err)
		}
	}
}

// A change undone while the container stops puts the file back and leaves the server down.
func TestChildServerStopping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &childServer{stopping: ctx, args: []string{"sleep", "60"}}
	if err := s.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	first := s.exited
	cancel()
	if err := s.Restart(ctx); err == nil {
		t.Fatal("the server was started again while the applier stops")
	}
	select {
	case <-first:
	default:
		t.Fatal("the server still runs")
	}
	if s.exited != first {
		t.Fatal("another server was started")
	}
}
