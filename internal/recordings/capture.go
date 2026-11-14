package recordings

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// What a track's capture is made of (design D4): media files and one manifest of append-only JSON lines, synced
// with the media, that places each file on the recording's clock.

// Kinds and sources of a captured track.
const (
	KindAudio = "audio"
	KindVideo = "video"

	SourceMicrophone = "microphone"
	SourceCamera     = "camera"
	SourceScreen     = "screen_share"
)

// Reasons a recording is partial (they reach the API and the message).
const (
	PartialTimingApproximate = "timing_approximate" // a track sent no sender report: placed by arrival time
	PartialCodec             = "codec_not_captured" // a camera in a codec the recorder does not capture
	PartialTrackLimit        = "track_limit"        // more tracks than a recording takes
	PartialCut               = "capture_cut"        // the capture ended without its recorder (a crash)
	PartialVideoGap          = "video_gap"          // no keyframe came within the bounded wait
)

// Header is a manifest's first line.
type Header struct {
	Kind        string `json:"kind"`
	Codec       string `json:"codec"`
	Clock       uint32 `json:"clock"`
	Participant string `json:"participant"` // user id
	Name        string `json:"name"`        // display name, for the name tile
	Source      string `json:"source"`
	Track       string `json:"track"`
}

// Line is any later manifest line: a file's start (File, PosMS), a better position for it (Pos), its progress
// (Dur: the media synced so far, DurMS) and a partial reason.
type Line struct {
	Seg     int    `json:"seg"`
	File    string `json:"file,omitempty"`
	PosMS   int64  `json:"pos_ms,omitempty"`
	Approx  bool   `json:"approx,omitempty"`
	Pos     bool   `json:"pos,omitempty"`
	Dur     bool   `json:"dur,omitempty"`
	DurMS   int64  `json:"dur_ms,omitempty"`
	Partial string `json:"partial,omitempty"`
}

// manifest appends lines to a track's manifest, syncing each.
type manifest struct {
	f *os.File
}

func newManifest(path string, h Header) (*manifest, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o640) //nolint:gosec // our own path
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	m := &manifest{f: f}
	if err := m.write(h); err != nil {
		_ = f.Close()
		return nil, err
	}
	return m, nil
}

func (m *manifest) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := m.f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	return m.f.Sync()
}

func (m *manifest) close() error { return m.f.Close() }

// Segment is one media file of a track as the composition sees it.
type Segment struct {
	File   string
	PosMS  int64 // its start on the recording's clock (ms from the recording's start)
	DurMS  int64 // the media known to be durable
	Approx bool
}

// Track is a manifest read back: its header, its files in order and the partial reasons it recorded.
type Track struct {
	Header
	Segments []Segment
	Partial  []string
}

// ReadManifest reads a manifest; a torn last line (a crash while writing it) is ignored, and so is every file
// without a durable length.
func ReadManifest(path string) (*Track, error) {
	b, err := os.ReadFile(path) //nolint:gosec // our own path
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var t Track
	first := true
	for sc.Scan() {
		raw := sc.Bytes()
		if first {
			if err := json.Unmarshal(raw, &t.Header); err != nil {
				return nil, fmt.Errorf("manifest header: %w", err)
			}
			first = false
			continue
		}
		var l Line
		if json.Unmarshal(raw, &l) != nil {
			break // a torn line: only the last one can be
		}
		switch {
		case l.File != "":
			t.Segments = append(t.Segments, Segment{File: l.File, PosMS: l.PosMS, Approx: l.Approx})
		case l.Partial != "":
			t.Partial = append(t.Partial, l.Partial)
		case l.Seg >= 1 && l.Seg <= len(t.Segments):
			s := &t.Segments[l.Seg-1]
			if l.Pos {
				s.PosMS, s.Approx = l.PosMS, false
			}
			if l.Dur {
				s.DurMS = l.DurMS
			}
		}
	}
	if first {
		return nil, errors.New("manifest: empty")
	}
	kept := t.Segments[:0]
	for _, s := range t.Segments {
		if s.DurMS > 0 {
			kept = append(kept, s)
		}
	}
	t.Segments = kept
	return &t, nil
}

// clockMap turns a track's RTP time into the recorder's wall time (design D4): the latest sender report maps RTP
// time to the sender's clock (LiveKit's own, for a down track), and the smallest arrival-minus-report time seen
// converts that clock to ours, whatever the offset between the hosts.
type clockMap struct {
	mu     sync.Mutex
	ok     bool
	ntpNS  int64  // the report's wall clock, Unix ns
	rtp    uint32 // the report's RTP time
	offset int64  // arrival − report time, the smallest seen
	clock  int64
}

// set records a sender report (NTP 32.32 time and RTP time) that arrived at arrival.
func (c *clockMap) set(ntp uint64, rtpTime uint32, arrival time.Time) {
	sec := int64(ntp>>32) - 2208988800 //nolint:gosec // NTP seconds fit
	frac := int64(ntp & 0xffffffff)    //nolint:gosec // 32 bits
	ns := sec*1e9 + (frac*1e9)>>32
	c.mu.Lock()
	if off := arrival.UnixNano() - ns; !c.ok || off < c.offset {
		c.offset = off
	}
	c.ok, c.ntpNS, c.rtp = true, ns, rtpTime
	c.mu.Unlock()
}

// wall is ts on the recorder's wall clock (Unix ns); false before any report.
func (c *clockMap) wall(ts uint32) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ok {
		return 0, false
	}
	return c.ntpNS + c.offset + int64(int32(ts-c.rtp))*1e9/c.clock, true //nolint:gosec // a signed RTP difference
}

// Audio capture rules, measured in the spike (design D4).
const (
	opusFrame      = 960 // 20 ms at 48 kHz
	opusPreSkip    = 4   // silence frames covering the 3840 samples of pre-skip Pion's Ogg writer declares
	maxFilledGap   = 2 * time.Second
	reanchorAfter  = 40 * time.Millisecond
	misalignedSlop = 2 * time.Millisecond
	progressEvery  = 2 * time.Second
)

// silenceFrame is a 20 ms Opus frame of silence (CELT, TOC 0xf8).
var silenceFrame = []byte{0xf8, 0xff, 0xfe}

// audioTrack writes one Opus track to Ogg files with silence where RTP time has gaps, a new file where the
// timeline breaks, and their positions in the manifest (design D4).
type audioTrack struct {
	dir, prefix string
	man         *manifest
	clk         *clockMap
	startNS     int64         // the recording's start, Unix ns
	countBytes  func(n int64) // the capture's byte budget

	seg     int
	f       *os.File
	w       *oggwriter.OggWriter
	outTS   uint32
	outSeq  uint16
	samples int64 // media and silence written to the current file after the pre-skip frames
	last    int64 // extended RTP time of the last packet written
	lastDur int64
	ext     extTS
	unmuted bool
	synced  time.Time

	// the file's position: by arrival until a sender report, then the median of the reports' placements
	posNS   int64
	posMS   int64 // what the manifest says
	approx  bool
	offs    []int64
	sampled time.Time

	approxAtEnd bool
}

// extTS extends 32-bit RTP timestamps to 64 bits, so a wrap is never a jump.
type extTS struct {
	ok   bool
	last int64
}

func (e *extTS) next(ts uint32) int64 {
	if !e.ok {
		e.ok, e.last = true, int64(ts)
		return e.last
	}
	v := e.last + int64(int32(ts-uint32(e.last))) //nolint:gosec // a signed RTP difference
	if v > e.last {
		e.last = v
	}
	return v
}

// opusSamples is the length of an Opus packet at 48 kHz, from its TOC byte (RFC 6716 section 3.1).
func opusSamples(b []byte) int64 {
	if len(b) == 0 {
		return opusFrame
	}
	cfg := b[0] >> 3
	var frame int64
	switch {
	case cfg < 12: // SILK: 10, 20, 40, 60 ms
		frame = []int64{480, 960, 1920, 2880}[cfg%4]
	case cfg < 16: // hybrid: 10, 20 ms
		frame = []int64{480, 960}[cfg%2]
	default: // CELT: 2.5, 5, 10, 20 ms
		frame = []int64{120, 240, 480, 960}[cfg%4]
	}
	n := int64(1)
	switch b[0] & 3 {
	case 1, 2:
		n = 2
	case 3:
		if len(b) > 1 {
			n = max(int64(b[1]&0x3f), 1)
		}
	}
	return frame * n
}

func (a *audioTrack) durMS() int64 { return a.samples * 1000 / 48000 }

func (a *audioTrack) write(b []byte, n int64) error {
	err := a.w.WriteRTP(&rtp.Packet{Header: rtp.Header{SequenceNumber: a.outSeq, Timestamp: a.outTS}, Payload: b})
	a.outSeq++
	a.outTS += uint32(n) //nolint:gosec // a packet's length
	a.countBytes(int64(len(b)) + 27)
	return err
}

// startFile closes the current file and opens the next one, starting with the packet of RTP time ts.
func (a *audioTrack) startFile(ts uint32, arrival time.Time) error {
	if err := a.closeFile(); err != nil {
		return err
	}
	a.seg++
	name := fmt.Sprintf("%s-%03d.ogg", a.prefix, a.seg)
	f, err := os.OpenFile(filepath.Join(a.dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640) //nolint:gosec // ours
	if err != nil {
		return fmt.Errorf("audio file: %w", err)
	}
	w, err := oggwriter.NewWith(f, 48000, 2)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("audio file: %w", err)
	}
	a.f, a.w, a.outTS, a.outSeq, a.samples, a.offs = f, w, 0, 0, 0, a.offs[:0]
	wall, ok := a.clk.wall(ts)
	if !ok {
		wall = arrival.UnixNano()
	}
	a.posNS, a.approx, a.posMS = wall, !ok, (wall-a.startNS)/1e6
	if ok {
		a.offs = append(a.offs, wall)
	}
	// the decoder drops the declared pre-skip: these frames are what it drops, so the file starts at its position
	for range opusPreSkip {
		if err := a.write(silenceFrame, opusFrame); err != nil {
			return err
		}
	}
	a.synced, a.sampled = arrival, arrival
	return a.man.write(Line{Seg: a.seg, File: name, PosMS: a.posMS, Approx: !ok})
}

// closeFile syncs and closes the current file and records its position and length.
func (a *audioTrack) closeFile() error {
	if a.f == nil {
		return nil
	}
	err := errors.Join(a.w.Close(), a.recordPosition(), a.man.write(Line{Seg: a.seg, Dur: true, DurMS: a.durMS()}))
	a.f, a.w = nil, nil
	return err
}

// recordPosition writes the file's position when the median of its placements moved it.
func (a *audioTrack) recordPosition() error {
	if len(a.offs) == 0 {
		return nil
	}
	sorted := slices.Clone(a.offs)
	slices.Sort(sorted)
	wasApprox := a.approx
	a.posNS, a.approx = sorted[len(sorted)/2], false
	ms := (a.posNS - a.startNS) / 1e6
	if ms == a.posMS && !wasApprox {
		return nil
	}
	a.posMS = ms
	return a.man.write(Line{Seg: a.seg, Pos: true, PosMS: ms})
}

// push writes one Opus packet, in RTP order, that arrived at arrival.
func (a *audioTrack) push(ts uint32, payload []byte, arrival time.Time) error {
	x := a.ext.next(ts)
	if a.f != nil && x <= a.last {
		return nil // media already written (a duplicate or a late copy)
	}
	newFile := a.f == nil || a.unmuted
	var gap int64
	wall, reported := a.clk.wall(ts)
	if !newFile {
		gap = x - (a.last + a.lastDur)
		slop := int64(misalignedSlop) * 48000 / int64(time.Second)
		r := ((gap % opusFrame) + opusFrame) % opusFrame
		switch {
		case gap > int64(maxFilledGap)*48000/int64(time.Second), gap < -opusFrame:
			newFile = true
		case r > slop && r < opusFrame-slop: // silence frames cannot fill a jump that is not whole frames
			newFile = true
		case reported && !a.approx:
			placed := a.posNS + (a.samples+max(gap, 0))*1e9/48000
			if d := wall - placed; d > int64(reanchorAfter) || d < -int64(reanchorAfter) {
				newFile = true // the file's own clock and the reports' clock parted
			}
		}
	}
	if newFile {
		a.unmuted = false
		if err := a.startFile(ts, arrival); err != nil {
			return err
		}
	} else if gap > 0 {
		for range (gap + opusFrame/2) / opusFrame {
			if err := a.write(silenceFrame, opusFrame); err != nil {
				return err
			}
			a.samples += opusFrame
		}
	}
	if reported && !newFile && arrival.Sub(a.sampled) >= time.Second {
		a.sampled = arrival
		a.offs = append(a.offs, wall-a.samples*1e9/48000)
	}
	n := opusSamples(payload)
	if err := a.write(payload, n); err != nil {
		return err
	}
	a.samples += n
	a.last, a.lastDur = x, n
	if arrival.Sub(a.synced) >= progressEvery {
		return a.sync(arrival)
	}
	return nil
}

// sync makes the media written so far durable and records it with the file's latest position.
func (a *audioTrack) sync(now time.Time) error {
	if a.f == nil {
		return nil
	}
	a.synced = now
	if err := a.f.Sync(); err != nil {
		return fmt.Errorf("audio file: %w", err)
	}
	return errors.Join(a.recordPosition(), a.man.write(Line{Seg: a.seg, Dur: true, DurMS: a.durMS()}))
}

// senderReport records a report; a file placed by arrival is placed by it from now on.
func (a *audioTrack) senderReport(ntp uint64, rtpTime uint32, arrival time.Time) error {
	a.clk.set(ntp, rtpTime, arrival)
	if a.f == nil || !a.approx {
		return nil
	}
	wall, _ := a.clk.wall(uint32(a.last)) //nolint:gosec // the low 32 bits
	a.offs = append(a.offs[:0], wall-(a.samples-a.lastDur)*1e9/48000)
	return a.recordPosition()
}

// close ends the track: its last file and, when a file never got a sender report, the partial reason.
func (a *audioTrack) close() error {
	approx := a.f != nil && a.approx
	a.approxAtEnd = approx
	err := a.closeFile()
	if approx {
		err = errors.Join(err, a.man.write(Line{Partial: PartialTimingApproximate}))
	}
	return errors.Join(err, a.man.close())
}

// ivfWriter writes VP8 or VP9 frames to IVF with their RTP time (90 kHz) as timestamps.
type ivfWriter struct {
	f      *os.File
	frames uint32
}

func newIVF(path, fourcc string) (*ivfWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640) //nolint:gosec // our own path
	if err != nil {
		return nil, fmt.Errorf("video file: %w", err)
	}
	h := make([]byte, 32)
	copy(h, "DKIF")
	binary.LittleEndian.PutUint16(h[6:], 32)
	copy(h[8:], fourcc)
	binary.LittleEndian.PutUint16(h[12:], 1280) // a hint only: every keyframe carries its size
	binary.LittleEndian.PutUint16(h[14:], 720)
	binary.LittleEndian.PutUint32(h[16:], 90000)
	binary.LittleEndian.PutUint32(h[20:], 1)
	if _, err := f.Write(h); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("video file: %w", err)
	}
	return &ivfWriter{f: f}, nil
}

func (w *ivfWriter) frame(b []byte, pts uint64) (int64, error) {
	h := make([]byte, 12)
	binary.LittleEndian.PutUint32(h, uint32(len(b))) //nolint:gosec // a frame's size
	binary.LittleEndian.PutUint64(h[4:], pts)
	if _, err := w.f.Write(h); err != nil {
		return 0, err
	}
	if _, err := w.f.Write(b); err != nil {
		return 0, err
	}
	w.frames++
	return int64(len(b)) + 12, nil
}

func (w *ivfWriter) close() error {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, w.frames)
	_, err := w.f.WriteAt(b, 24)
	return errors.Join(err, w.f.Sync(), w.f.Close())
}

var _ io.Closer = (*os.File)(nil)
