package recordings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Error classes of an ffmpeg run: the only thing about a failure that reaches logs and messages (design D5); its
// error output stays in a file beside the captures.
const (
	ClassMissing = "ffmpeg_missing" // the executable cannot be started
	ClassTimeout = "ffmpeg_timeout" // it ran past its time limit and was killed
	ClassFailed  = "ffmpeg_failed"  // it exited with an error
)

// FFmpegError is a failed ffmpeg run, by class; Log is the file holding its error output.
type FFmpegError struct {
	Class string
	Log   string
	err   error
}

func (e *FFmpegError) Error() string { return "ffmpeg: " + e.Class }
func (e *FFmpegError) Unwrap() error { return e.err }

// ffmpegThreads and ffmpegNice keep a composition from taking the server (design D5).
const (
	ffmpegThreads = "2"
	ffmpegNice    = 10
)

// FFmpeg runs the ffmpeg executable with argument lists built by this package (never a shell).
type FFmpeg struct {
	Path string // a name looked up in PATH, or a path
}

// baseArgs come first in every run: no prompts, no banner or progress, errors only, two threads for filters.
func baseArgs() []string {
	return []string{"-nostdin", "-hide_banner", "-nostats", "-loglevel", "error", "-y", "-filter_threads", ffmpegThreads,
		"-filter_complex_threads", ffmpegThreads}
}

// Run runs ffmpeg with args (after baseArgs) in dir, at a lowered priority, killed after limit. Its error output
// goes to dir/<step>.log, kept on failure and removed on success.
func (f *FFmpeg) Run(ctx context.Context, dir, step string, limit time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	logPath := filepath.Join(dir, step+".log")
	logf, err := os.Create(logPath) //nolint:gosec // a path of this package's own capture directory
	if err != nil {
		return fmt.Errorf("ffmpeg log: %w", err)
	}
	cmd := exec.CommandContext(ctx, f.Path, append(baseArgs(), args...)...) //nolint:gosec // argument lists built here
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, logf, logf
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return &FFmpegError{Class: ClassMissing, Log: logPath, err: err}
	}
	// lowered priority; a failure (an unprivileged process may always lower it) only costs fairness
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, ffmpegNice)
	err = cmd.Wait()
	_ = logf.Close()
	switch {
	case err == nil:
		_ = os.Remove(logPath)
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &FFmpegError{Class: ClassTimeout, Log: logPath, err: err}
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		return &FFmpegError{Class: ClassFailed, Log: logPath, err: err}
	}
}

// Version returns the first line of `ffmpeg -version`, or a ClassMissing error.
func (f *FFmpeg) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, f.Path, "-hide_banner", "-version").Output() //nolint:gosec // fixed arguments
	if err != nil {
		return "", &FFmpegError{Class: ClassMissing, err: err}
	}
	line, _, _ := cutLine(string(out))
	return line, nil
}

// Encoders returns ffmpeg's encoder list (`ffmpeg -encoders`).
func (f *FFmpeg) Encoders(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, f.Path, "-hide_banner", "-encoders").Output() //nolint:gosec // fixed arguments
	if err != nil {
		return "", &FFmpegError{Class: ClassMissing, err: err}
	}
	return string(out), nil
}

func cutLine(s string) (string, string, bool) {
	for i := range len(s) {
		if s[i] == '\n' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// timeLimit is how long a composition step of a recording of d may run: four times its length, at least 10 minutes.
func timeLimit(d time.Duration) time.Duration {
	return max(4*d, 10*time.Minute)
}
