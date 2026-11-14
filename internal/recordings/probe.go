package recordings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Probe (design D10): a two-second generated capture — two voices, and in video mode a camera — composed exactly as
// a recording is, as the server's user, in the capture directory. It proves the ffmpeg, its encoders and the
// directory before any meeting is recorded.

// ProbeError names what the probe found missing; its message names the setting to fix.
type ProbeError struct {
	Setting string // the setting that names the culprit, e.g. recordings.ffmpeg_path
	What    string
	err     error
}

func (e *ProbeError) Error() string { return e.Setting + ": " + e.What }
func (e *ProbeError) Unwrap() error { return e.err }

// Probe composes a short generated recording in mode (audio or video) and format (webm or mp4) under dir.
func Probe(ctx context.Context, ff *FFmpeg, dir, mode, format string) error {
	if _, err := ff.Version(ctx); err != nil {
		return &ProbeError{Setting: "recordings.ffmpeg_path", What: fmt.Sprintf("cannot run %q", ff.Path), err: err}
	}
	encoders, err := ff.Encoders(ctx)
	if err != nil {
		return &ProbeError{Setting: "recordings.ffmpeg_path", What: fmt.Sprintf("cannot run %q", ff.Path), err: err}
	}
	need := []string{"libopus", "aac"}
	if mode == "video" {
		need = append(need, "png")
		if format == "mp4" {
			need = append(need, "libx264")
		} else {
			need = append(need, "libvpx")
		}
	}
	for _, e := range need {
		if !hasEncoder(encoders, e) {
			setting := "recordings.ffmpeg_path"
			if e == "libx264" {
				setting = "recordings.video_format"
			}
			return &ProbeError{Setting: setting, What: fmt.Sprintf("ffmpeg has no %s encoder", e), err: ErrNoEncoder}
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs // ffmpeg runs inside the probe's directory
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return &ProbeError{Setting: "recordings.dir", What: "cannot create " + dir, err: err}
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	work := filepath.Join(dir, "probe-"+hex.EncodeToString(b))
	if err := os.Mkdir(work, 0o750); err != nil {
		return &ProbeError{Setting: "recordings.dir", What: "cannot write in " + dir, err: err}
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := probeCapture(ctx, ff, work, mode == "video"); err != nil {
		return &ProbeError{Setting: "recordings.ffmpeg_path", What: "could not generate a test capture", err: err}
	}
	c := &Composer{FFmpeg: ff, Mode: mode, Format: format}
	out, err := c.Compose(ctx, work, 2000, nil)
	if err != nil {
		var fe *FFmpegError
		what := "could not compose a test recording"
		if errors.As(err, &fe) {
			what += " (" + fe.Class + ")"
		}
		return &ProbeError{Setting: "recordings.ffmpeg_path", What: what, err: err}
	}
	if err := ff.Run(ctx, work, "probe-decode", time.Minute, "-i", out.Path, "-f", "null", "-"); err != nil {
		return &ProbeError{Setting: "recordings.ffmpeg_path", What: "the test recording does not decode", err: err}
	}
	return nil
}

func hasEncoder(list, name string) bool {
	for line := range strings.Lines(list) {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == name {
			return true
		}
	}
	return false
}

// probeCapture writes a capture as the recorder would: two voices of one and two seconds, and a camera.
func probeCapture(ctx context.Context, ff *FFmpeg, dir string, video bool) error {
	write := func(name string, h Header, lines ...Line) error {
		f, err := os.Create(filepath.Join(dir, name)) //nolint:gosec // our own directory
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		err = enc.Encode(h)
		for _, l := range lines {
			err = errors.Join(err, enc.Encode(l))
		}
		return errors.Join(err, f.Close())
	}
	voices := []struct {
		freq, pos, dur int
	}{{440, 0, 1000}, {660, 1000, 1000}}
	for i, v := range voices {
		file := fmt.Sprintf("t%03d-001.ogg", i+1)
		if err := ff.Run(ctx, dir, "probe-voice", time.Minute, "-filter_complex",
			fmt.Sprintf("sine=f=%d:sample_rate=48000:d=%s[o]", v.freq, sec(int64(v.dur))), "-map", "[o]",
			"-c:a", "libopus", "-b:a", "64k", file); err != nil {
			return err
		}
		h := Header{Kind: KindAudio, Codec: "opus", Clock: 48000, Participant: fmt.Sprintf("probe%d", i+1),
			Name: fmt.Sprintf("Probe %d", i+1), Source: SourceMicrophone}
		if err := write(fmt.Sprintf("t%03d.jsonl", i+1), h, Line{Seg: 1, File: file, PosMS: int64(v.pos)},
			Line{Seg: 1, Dur: true, DurMS: int64(v.dur)}); err != nil {
			return err
		}
	}
	if !video {
		return nil
	}
	file := "t003-001.ivf"
	if err := ff.Run(ctx, dir, "probe-camera", time.Minute, "-filter_complex",
		"color=c=0x3060a0:s=640x360:r=25:d=2[o]", "-map", "[o]", "-c:v", "libvpx", "-b:v", "500k",
		"-deadline", "realtime", "-f", "ivf", file); err != nil {
		return err
	}
	h := Header{Kind: KindVideo, Codec: "vp8", Clock: 90000, Participant: "probe1", Name: "Probe 1", Source: SourceCamera}
	return write("t003.jsonl", h, Line{Seg: 1, File: file}, Line{Seg: 1, Dur: true, DurMS: 2000})
}
