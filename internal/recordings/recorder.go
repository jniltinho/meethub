package recordings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

// The recorder (design D2, D4): a hidden participant that subscribes to the room's tracks and writes them as they
// arrive, until it is told to stop or a stop condition of D4 is met.

// Why a capture ended.
const (
	EndStopped  = "stopped"     // someone allowed asked
	EndEmpty    = "empty"       // nobody but the recorder for EmptyAfter
	EndMaxTime  = "max_minutes" // recordings.max_minutes
	EndMaxBytes = "max_bytes"   // recordings.max_bytes
	EndLease    = "lease_lost"  // the lease could not be renewed: stop at once (fail closed)
	EndShutdown = "shutdown"    // the server is stopping
	EndArchived = "archived"    // the channel was archived
)

// Recorder limits and timings.
const (
	EmptyAfter     = 30 * time.Second
	MaxTracks      = 25
	audioReorder   = 200 * time.Millisecond
	videoReorder   = time.Second
	keyframeEvery  = 500 * time.Millisecond
	keyframeBound  = 5 * time.Second
	idleDrain      = time.Second
	connectTimeout = 15 * time.Second
)

// RecorderIdentity is the LiveKit identity of a recording's recorder; meetings' presence ignores it.
func RecorderIdentity(recordingID string) string { return "recorder:" + recordingID }

// RecorderToken is the recorder's own token (design D2): this room only, hidden, recorder, subscribe only.
func RecorderToken(key, secret, room, recordingID string, ttl time.Duration) (string, error) {
	grant := &auth.VideoGrant{RoomJoin: true, Room: room, Hidden: true, Recorder: true}
	grant.SetCanSubscribe(true)
	grant.SetCanPublish(false)
	grant.SetCanPublishData(false)
	tok, err := auth.NewAccessToken(key, secret).SetVideoGrant(grant).SetIdentity(RecorderIdentity(recordingID)).
		SetValidFor(ttl).ToJWT()
	if err != nil {
		return "", fmt.Errorf("recorder token: %w", err)
	}
	return tok, nil
}

// Capture is one recording's capture in progress.
type Capture struct {
	Dir      string // the recording's capture directory (created by the caller)
	URL      string // LiveKit's signalling URL as the server reaches it (ws or http)
	Token    string
	Mode     string // audio or video
	Start    time.Time
	MaxBytes int64
	MaxTime  time.Duration
	Logger   *slog.Logger

	bytes    atomic.Int64
	mu       sync.Mutex
	tracks   map[string]bool // publication SIDs taken
	partial  map[string]bool
	n        int
	files    int
	wg       sync.WaitGroup
	full     chan struct{}
	fullOnce sync.Once
	room     *lksdk.Room
}

// CaptureResult is how a capture ended.
type CaptureResult struct {
	End     string
	Ended   time.Time
	Partial []string
}

// Run joins the room and captures until stop is closed (EndStopped), ctx ends (the caller's reason) or a stop
// condition is met; it always leaves the room and closes every file before returning.
func (c *Capture) Run(ctx context.Context, stop <-chan string) (CaptureResult, error) {
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
	c.tracks, c.partial, c.full = map[string]bool{}, map[string]bool{}, make(chan struct{})
	cb := lksdk.NewRoomCallback()
	cb.OnTrackPublished = func(p *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) { c.take(p, rp) }
	cb.OnTrackSubscribed = c.subscribed
	cb.OnTrackUnmuted = func(p lksdk.TrackPublication, _ lksdk.Participant) { c.unmuted(p.SID()) }
	url := "ws" + strings.TrimPrefix(strings.TrimPrefix(c.URL, "ws"), "http")
	room := lksdk.NewRoom(cb)
	jctx, cancel := context.WithTimeout(ctx, connectTimeout)
	err := room.JoinWithContextAndToken(jctx, url, c.Token, lksdk.WithAutoSubscribe(false))
	cancel()
	if err != nil {
		return CaptureResult{}, fmt.Errorf("recorder join: %w", err)
	}
	c.mu.Lock()
	c.room = room
	c.mu.Unlock()
	for _, rp := range room.GetRemoteParticipants() {
		for _, tp := range rp.TrackPublications() {
			if p, ok := tp.(*lksdk.RemoteTrackPublication); ok {
				c.take(p, rp)
			}
		}
	}
	end := c.wait(ctx, room, stop)
	room.Disconnect() // ends every track's reads
	c.wg.Wait()
	res := CaptureResult{End: end, Ended: time.Now()}
	c.mu.Lock()
	for r := range c.partial {
		res.Partial = append(res.Partial, r)
	}
	c.mu.Unlock()
	return res, nil
}

// wait returns why the capture ends.
func (c *Capture) wait(ctx context.Context, room *lksdk.Room, stop <-chan string) string {
	limit := time.NewTimer(c.MaxTime - time.Since(c.Start))
	defer limit.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	empty := time.Time{}
	for {
		select {
		case <-ctx.Done():
			if r := endCause(""); errors.As(context.Cause(ctx), &r) {
				return string(r)
			}
			return EndShutdown
		case r := <-stop:
			return r
		case <-limit.C:
			return EndMaxTime
		case <-c.full:
			return EndMaxBytes
		case now := <-tick.C:
			if len(room.GetRemoteParticipants()) > 0 {
				empty = time.Time{}
			} else if empty.IsZero() {
				empty = now
			} else if now.Sub(empty) >= EmptyAfter {
				return EndEmpty
			}
		}
	}
}

// endCause is a context cause naming why a capture ends (EndLease, EndShutdown).
type endCause string

func (e endCause) Error() string { return string(e) }

// count adds n written bytes to the budget; reaching it ends the capture.
func (c *Capture) count(n int64) {
	if c.bytes.Add(n) >= c.MaxBytes {
		c.fullOnce.Do(func() { close(c.full) })
	}
}

// Bytes is what the capture wrote so far.
func (c *Capture) Bytes() int64 { return c.bytes.Load() }

func (c *Capture) markPartial(reason string) {
	c.mu.Lock()
	c.partial[reason] = true
	c.mu.Unlock()
}

// take subscribes to a publication the recording captures: audio always, cameras and screen shares in video mode,
// at most MaxTracks; a camera in a codec that is not captured is left out and makes the recording partial.
func (c *Capture) take(p *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	if strings.HasPrefix(rp.Identity(), "recorder:") {
		return
	}
	video := p.Kind() == lksdk.TrackKindVideo
	if video && c.Mode != "video" {
		return
	}
	c.mu.Lock()
	if c.tracks[p.SID()] {
		c.mu.Unlock()
		return
	}
	if c.n >= MaxTracks {
		c.partial[PartialTrackLimit] = true
		c.mu.Unlock()
		return
	}
	if video {
		mime := strings.ToLower(p.MimeType())
		if mime != "" && mime != strings.ToLower(webrtc.MimeTypeVP8) && mime != strings.ToLower(webrtc.MimeTypeVP9) {
			c.partial[PartialCodec] = true
			c.mu.Unlock()
			return
		}
	}
	c.tracks[p.SID()] = true
	c.n++
	c.mu.Unlock()
	if video {
		q := livekit.VideoQuality_MEDIUM
		if p.Source() == livekit.TrackSource_SCREEN_SHARE {
			q = livekit.VideoQuality_HIGH
		}
		_ = p.SetVideoQuality(q)
	}
	if err := p.SetSubscribed(true); err != nil {
		c.Logger.Warn("recordings: subscribe", "track", p.SID(), "error", err)
	}
}

// handlers of the tracks being captured, by publication SID (for mute events)
var audioHandlers sync.Map

func (c *Capture) unmuted(sid string) {
	if h, ok := audioHandlers.Load(c.Dir + "/" + sid); ok {
		h.(*audioHandler).markUnmuted()
	}
}

func sourceOf(s livekit.TrackSource) string {
	switch s {
	case livekit.TrackSource_CAMERA:
		return SourceCamera
	case livekit.TrackSource_SCREEN_SHARE:
		return SourceScreen
	default:
		return SourceMicrophone
	}
}

// subscribed starts writing a track the recorder subscribed to.
func (c *Capture) subscribed(track *webrtc.TrackRemote, p *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	c.mu.Lock()
	c.files++
	prefix := fmt.Sprintf("t%03d", c.files) // in the order of subscription
	c.mu.Unlock()
	mime := strings.ToLower(track.Codec().MimeType)
	h := Header{
		Kind: KindAudio, Codec: strings.TrimPrefix(mime, "audio/"), Clock: track.Codec().ClockRate,
		Participant: rp.Identity(), Name: rp.Name(), Source: sourceOf(p.Source()), Track: p.SID(),
	}
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		h.Kind, h.Codec = KindVideo, strings.TrimPrefix(mime, "video/")
	}
	man, err := newManifest(filepath.Join(c.Dir, prefix+".jsonl"), h)
	if err != nil {
		c.Logger.Error("recordings: manifest", "error", err)
		return
	}
	pli := func() { rp.WritePLI(track.SSRC()) }
	c.wg.Add(1)
	if h.Kind == KindAudio {
		if mime != strings.ToLower(webrtc.MimeTypeOpus) {
			_ = man.close()
			c.wg.Done()
			return
		}
		a := &audioHandler{t: &audioTrack{
			dir: c.Dir, prefix: prefix, man: man, clk: &clockMap{clock: int64(track.Codec().ClockRate)},
			startNS: c.Start.UnixNano(), countBytes: c.count,
		}}
		key := c.Dir + "/" + p.SID()
		audioHandlers.Store(key, a)
		p.OnRTCP(func(pk rtcp.Packet) {
			if sr, ok := pk.(*rtcp.SenderReport); ok && sr.SSRC == uint32(track.SSRC()) {
				a.senderReport(sr.NTPTime, sr.RTPTime)
			}
		})
		go func() {
			defer c.wg.Done()
			defer audioHandlers.Delete(key)
			if err := a.run(track); err != nil {
				c.Logger.Error("recordings: audio track", "track", p.SID(), "error", err)
			}
			if a.partial() {
				c.markPartial(PartialTimingApproximate)
			}
		}()
		return
	}
	v := &videoTrack{
		dir: c.Dir, prefix: prefix, man: man, startNS: c.Start.UnixNano(), countBytes: c.count, pli: pli,
		vp9: mime == strings.ToLower(webrtc.MimeTypeVP9), clk: &clockMap{clock: int64(track.Codec().ClockRate)},
	}
	p.OnRTCP(func(pk rtcp.Packet) {
		if sr, ok := pk.(*rtcp.SenderReport); ok && sr.SSRC == uint32(track.SSRC()) {
			v.senderReport(sr.NTPTime, sr.RTPTime)
		}
	})
	go func() {
		defer c.wg.Done()
		if err := v.run(track); err != nil {
			c.Logger.Error("recordings: video track", "track", p.SID(), "error", err)
		}
		if v.gap {
			c.markPartial(PartialVideoGap)
		}
	}()
}

// audioHandler feeds an audioTrack from a track and its reports, one at a time.
type audioHandler struct {
	mu sync.Mutex
	t  *audioTrack
}

func (a *audioHandler) senderReport(ntp uint64, rtpTime uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.t.senderReport(ntp, rtpTime, time.Now())
}

func (a *audioHandler) markUnmuted() {
	a.mu.Lock()
	a.t.unmuted = true
	a.mu.Unlock()
}

func (a *audioHandler) partial() bool { return a.t.approxAtEnd }

func (a *audioHandler) run(track *webrtc.TrackRemote) error {
	sb := samplebuilder.New(512, &codecs.OpusPacket{}, 48000, samplebuilder.WithMaxTimeDelay(audioReorder))
	var werr error
	pop := func() {
		for s := sb.Pop(); s != nil; s = sb.Pop() {
			a.mu.Lock()
			err := a.t.push(s.PacketTimestamp, s.Data, time.Now())
			a.mu.Unlock()
			if err != nil && werr == nil {
				werr = err
			}
		}
	}
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			break
		}
		if len(pkt.Payload) == 0 {
			continue // padding
		}
		sb.Push(pkt)
		pop()
		if werr != nil {
			break
		}
	}
	sb.Flush()
	pop()
	a.mu.Lock()
	defer a.mu.Unlock()
	return errors.Join(werr, a.t.close())
}

// videoTrack writes a VP8 or VP9 track to IVF files (design D4): it waits for a keyframe at the start and after a
// loss, asking for one every keyframeEvery; a wait beyond keyframeBound is a gap.
type videoTrack struct {
	dir, prefix string
	man         *manifest
	startNS     int64
	countBytes  func(int64)
	pli         func()
	vp9         bool
	gap         bool
	clk         *clockMap

	mu     sync.Mutex // the file, also placed by sender reports
	approx bool

	seg      int
	w        *ivfWriter
	first    int64
	ext      extTS
	lastPTS  int64
	synced   time.Time
	waitKey  bool
	since    time.Time
	askedAt  time.Time
	lastSeen time.Time
}

func (v *videoTrack) run(track *webrtc.TrackRemote) error {
	var dep rtp.Depacketizer = &codecs.VP8Packet{}
	if v.vp9 {
		dep = &codecs.VP9Packet{}
	}
	sb := samplebuilder.New(1024, dep, 90000, samplebuilder.WithMaxTimeDelay(videoReorder))
	v.waitKey, v.since = true, time.Now()
	v.ask(time.Now())
	pkts := make(chan *rtp.Packet, 256)
	go func() {
		defer close(pkts)
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			pkts <- pkt
		}
	}()
	idle := time.NewTicker(idleDrain)
	defer idle.Stop()
	var werr error
	pop := func() {
		for s := sb.Pop(); s != nil; s = sb.Pop() {
			if err := v.sample(s, time.Now()); err != nil && werr == nil {
				werr = err
			}
		}
	}
loop:
	for {
		select {
		case pkt, ok := <-pkts:
			if !ok {
				break loop
			}
			v.lastSeen = time.Now()
			sb.Push(pkt)
			pop()
		case now := <-idle.C:
			if v.waitKey {
				v.ask(now)
			}
			if !v.lastSeen.IsZero() && now.Sub(v.lastSeen) >= idleDrain { // a sparse screen share: write what came
				sb.Flush()
				pop()
			}
		}
		if werr != nil {
			break
		}
	}
	sb.Flush()
	pop()
	return errors.Join(werr, v.close())
}

func (v *videoTrack) ask(now time.Time) {
	if now.Sub(v.askedAt) >= keyframeEvery {
		v.askedAt = now
		v.pli()
	}
}

// keyframe reports whether a VP8 or VP9 frame is a keyframe.
func keyframe(b []byte, vp9 bool) bool {
	if len(b) == 0 {
		return false
	}
	if !vp9 {
		return b[0]&1 == 0 // VP8: the P bit of the frame tag (RFC 6386 section 9.1)
	}
	// VP9 uncompressed header: frame_marker(2), profile(2, +1 reserved for profile 3), show_existing_frame,
	// frame_type (0: key)
	bit := func(i int) int { return int(b[i/8]>>(7-i%8)) & 1 }
	if b[0]>>6 != 2 {
		return false
	}
	i := 4
	if bit(2) == 1 && bit(3) == 1 {
		i++
	}
	if len(b)*8 <= i+1 || bit(i) == 1 {
		return false
	}
	return bit(i+1) == 0
}

func (v *videoTrack) sample(s *media.Sample, now time.Time) error {
	key := keyframe(s.Data, v.vp9)
	if s.PrevDroppedPackets > 0 && !v.waitKey {
		v.waitKey, v.since = true, now
		v.ask(now)
	}
	if v.waitKey {
		if !key {
			v.ask(now)
			return nil
		}
		if now.Sub(v.since) > keyframeBound {
			v.gap = true
		}
		v.waitKey = false
	}
	x := v.ext.next(s.PacketTimestamp)
	if v.w == nil {
		if err := v.startFile(x, now); err != nil {
			return err
		}
	}
	pts := x - v.first
	if pts < v.lastPTS {
		return nil
	}
	v.lastPTS = pts
	v.mu.Lock()
	defer v.mu.Unlock()
	n, err := v.w.frame(s.Data, uint64(pts)) //nolint:gosec // non-negative
	if err != nil {
		return fmt.Errorf("video file: %w", err)
	}
	v.countBytes(n)
	if now.Sub(v.synced) >= progressEvery {
		v.synced = now
		if err := v.w.f.Sync(); err != nil {
			return fmt.Errorf("video file: %w", err)
		}
		return v.man.write(Line{Seg: v.seg, Dur: true, DurMS: pts / 90})
	}
	return nil
}

func (v *videoTrack) startFile(x int64, now time.Time) error {
	v.seg++
	name := fmt.Sprintf("%s-%03d.ivf", v.prefix, v.seg)
	fourcc := "VP80"
	if v.vp9 {
		fourcc = "VP90"
	}
	w, err := newIVF(filepath.Join(v.dir, name), fourcc)
	if err != nil {
		return err
	}
	wall, ok := v.clk.wall(uint32(x)) //nolint:gosec // the low 32 bits
	if !ok {
		wall = now.UnixNano()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.w, v.first, v.lastPTS, v.synced, v.approx = w, x, 0, now, !ok
	return v.man.write(Line{Seg: v.seg, File: name, PosMS: (wall - v.startNS) / 1e6, Approx: !ok})
}

// senderReport records a report; the current file, placed by arrival, is placed by it from now on.
func (v *videoTrack) senderReport(ntp uint64, rtpTime uint32) {
	v.clk.set(ntp, rtpTime, time.Now())
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.approx || v.w == nil {
		return
	}
	v.approx = false
	if wall, ok := v.clk.wall(uint32(v.first)); ok { //nolint:gosec // the low 32 bits
		_ = v.man.write(Line{Seg: v.seg, Pos: true, PosMS: (wall - v.startNS) / 1e6})
	}
}

func (v *videoTrack) close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	var err error
	if v.w != nil {
		err = errors.Join(v.w.close(), v.man.write(Line{Seg: v.seg, Dur: true, DurMS: v.lastPTS / 90}))
	}
	return errors.Join(err, v.man.close())
}
