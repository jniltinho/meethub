package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/imaging"
	"github.com/jniltinho/meethub/internal/jobs"
)

// Limits of uploads and their derived images.
const (
	NameMaxRunes        = 255
	PreviewMaxPixels    = 24_000_000 // larger images are stored without derived images
	PreviewMaxWidth     = 1920
	ThumbnailMaxHeight  = 240
	UploadsPerMinute    = 60
	UnattachedRetention = 24 * time.Hour
	concurrentDecodes   = 2 // each may hold a 24 MP image in memory
)

// TaskCleanup is the hourly file cleanup task type.
const TaskCleanup = "files:cleanup"

// ErrTooLarge is returned for a file above the size limit.
var ErrTooLarge = errors.New("the file is too large")

// Variants of a stored file.
const (
	Original  = ""
	Thumbnail = "thumbnail"
	Preview   = "preview"
)

// Service uploads, serves and cleans up the chat's files.
type Service struct {
	DB         *database.DB
	Chat       *chat.Service
	Storage    Storage
	MaxSize    int64        // bytes per file
	MaxPerPost int          // files per upload request and per message
	Limiter    chat.Limiter // uploads per user; nil: unlimited
	Logger     *slog.Logger
	Now        func() time.Time
	decodes    chan struct{}
}

// New builds the service.
func New(db *database.DB, chatSvc *chat.Service, st Storage, maxSize int64, maxPerPost int, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		DB: db, Chat: chatSvc, Storage: st, MaxSize: maxSize, MaxPerPost: maxPerPost, Logger: logger,
		Now: time.Now, decodes: make(chan struct{}, concurrentDecodes),
	}
}

// limitReader fails with ErrTooLarge once more than limit bytes were read.
type limitReader struct {
	r     io.Reader
	limit int64
	n     int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if l.n += int64(n); l.n > l.limit {
		return n, ErrTooLarge
	}
	return n, err
}

// cleanName keeps a file's name as metadata: its last path element, without
// control characters, at most NameMaxRunes.
func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if name = clip(name); name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

// clip cuts s to NameMaxRunes (lowercasing may lengthen a name).
func clip(s string) string {
	if r := []rune(s); len(r) > NameMaxRunes {
		return string(r[:NameMaxRunes])
	}
	return s
}

// Upload stores one file of r for the channel and returns its info. The
// caller's right to post there is checked before any byte is stored; the
// media type comes from the bytes and the storage key from ids, never from
// the client.
func (s *Service) Upload(ctx context.Context, sub chat.Subject, channelID, name string, r io.Reader) (*chat.FileView, error) {
	if err := s.Chat.CheckUpload(ctx, sub, channelID); err != nil {
		return nil, err
	}
	if s.Limiter != nil {
		if ok, wait := s.Limiter.Allow(ctx, sub.UserID); !ok {
			return nil, &chat.RateLimitedError{RetryAfter: wait, Action: "uploads"}
		}
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, uploadError(err)
	}
	if head = head[:n]; n == 0 {
		return nil, chat.ErrInvalidInput // an empty file
	}
	now, id := s.Now().UTC(), ulid.Make().String()
	key := fmt.Sprintf("%s/%s/%s/%s", now.Format("20060102"), channelID, id, id)
	body := &limitReader{r: io.MultiReader(bytes.NewReader(head), r), limit: s.MaxSize}
	if err := s.Storage.Put(ctx, key, body, -1); err != nil {
		s.remove(ctx, key) // the drivers leave nothing; this is for one that might
		return nil, uploadError(err)
	}
	mimeType, _, _ := mime.ParseMediaType(http.DetectContentType(head))
	name = cleanName(name)
	f := &chat.FileInfo{
		ID: id, CreatorID: sub.UserID, ChannelID: channelID, Name: name, SearchName: clip(strings.ToLower(name)),
		Extension: strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")), Size: body.n, MimeType: mimeType,
		Path: key, CreateAt: now.UnixMilli(), UpdateAt: now.UnixMilli(),
	}
	if len(f.Extension) > 32 {
		f.Extension = ""
	}
	if imaging.Accepted(head) {
		s.derive(ctx, f)
	}
	if err := s.DB.Gorm.WithContext(ctx).Create(f).Error; err != nil {
		s.remove(ctx, f.Path, f.ThumbnailPath, f.PreviewPath)
		return nil, fmt.Errorf("store file info: %w", err)
	}
	v := f.View()
	return &v, nil
}

// uploadError names what stopped an upload: the size limits (the file's or
// the request's) or the storage.
func uploadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return ErrTooLarge
	}
	return err
}

// remove deletes objects after a failed upload; what stays is harmless and
// unreachable (no row points at it).
func (s *Service) remove(ctx context.Context, keys ...string) {
	for _, key := range keys {
		if key != "" {
			if err := s.Storage.Delete(context.WithoutCancel(ctx), key); err != nil {
				s.Logger.WarnContext(ctx, "files: object not removed", "key", key, "error", err)
			}
		}
	}
}

// derive stores the thumbnail and the preview of an image and fills the
// file's dimensions. It reads the stored object (never the upload again),
// checks the size from the header before decoding, and never fails the
// upload: a file it cannot handle is just a file without derived images.
func (s *Service) derive(ctx context.Context, f *chat.FileInfo) {
	rc, err := s.Storage.Open(ctx, f.Path)
	if err != nil {
		return
	}
	defer func() { _ = rc.Close() }()
	w, h, err := imaging.Size(rc, PreviewMaxPixels, PreviewMaxPixels)
	if err != nil {
		return
	}
	select { // bounded: concurrent uploads cannot multiply the memory of a decode
	case s.decodes <- struct{}{}:
		defer func() { <-s.decodes }()
	case <-ctx.Done():
		return
	}
	if _, err := rc.Seek(0, io.SeekStart); err != nil {
		return
	}
	turn := imaging.Orientation(rc) // a camera may store the photo turned and say so in its EXIF data
	if _, err := rc.Seek(0, io.SeekStart); err != nil {
		return
	}
	src, err := imaging.Decode(rc)
	if err != nil {
		return
	}
	if imaging.Turned(turn) {
		w, h = h, w // as shown
	}
	pw, ph := imaging.Fit(w, h, PreviewMaxWidth, PreviewMaxPixels)
	sw, sh := pw, ph // the stored pixels are scaled before they are turned
	if imaging.Turned(turn) {
		sw, sh = ph, pw
	}
	preview := imaging.Orient(imaging.Scale(src, src.Bounds(), sw, sh), turn)
	tw, th := imaging.Fit(pw, ph, PreviewMaxPixels, ThumbnailMaxHeight)
	thumb := imaging.Scale(preview, preview.Bounds(), tw, th) // from the preview: far fewer pixels to average
	keys := [2]string{f.Path + "_preview.jpg", f.Path + "_thumb.jpg"}
	for i, img := range []*image.RGBA{preview, thumb} {
		var buf bytes.Buffer
		if err = imaging.EncodeJPEG(&buf, img); err == nil {
			err = s.Storage.Put(ctx, keys[i], &buf, int64(buf.Len()))
		}
		if err != nil {
			s.Logger.WarnContext(ctx, "files: derived image not stored", "file", f.ID, "error", err)
			s.remove(ctx, keys[:]...)
			return
		}
	}
	f.Width, f.Height, f.HasPreviewImage, f.PreviewPath, f.ThumbnailPath = w, h, true, keys[0], keys[1]
}

// Open returns a file the caller may read and one of its variants' bytes.
func (s *Service) Open(ctx context.Context, sub chat.Subject, id, variant string) (*chat.FileInfo, io.ReadSeekCloser, error) {
	f, err := s.Chat.ReadableFile(ctx, sub, id)
	if err != nil {
		return nil, nil, err
	}
	key := f.Path
	switch variant {
	case Thumbnail:
		key = f.ThumbnailPath
	case Preview:
		key = f.PreviewPath
	}
	if key == "" {
		return nil, nil, chat.ErrNotFound
	}
	rc, err := s.Storage.Open(ctx, key)
	if errors.Is(err, ErrNoObject) {
		return nil, nil, chat.ErrNotFound
	}
	return f, rc, err
}

// Cleanup removes the uploads never attached for UnattachedRetention and the
// bytes of deleted files. An object that cannot be removed keeps its row for
// the next run and does not stop the others; it is reported in the error.
func (s *Service) Cleanup(ctx context.Context) (removed int, err error) {
	now := s.Now().UTC()
	db := s.DB.Gorm.WithContext(ctx)
	// conditional, like the claim of a post: an upload is either attached or abandoned, never both
	if err := db.Model(&chat.FileInfo{}).
		Where("post_id IS NULL AND delete_at = 0 AND create_at < ?", now.Add(-UnattachedRetention).UnixMilli()).
		Updates(map[string]any{"delete_at": now.UnixMilli(), "update_at": now.UnixMilli()}).Error; err != nil {
		return 0, fmt.Errorf("mark abandoned uploads: %w", err)
	}
	failed, last := 0, ""
	for {
		var rows []chat.FileInfo
		if err := db.Where("delete_at > 0 AND id > ?", last).Order("id").Limit(200).Find(&rows).Error; err != nil {
			return removed, fmt.Errorf("read deleted files: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for i := range rows {
			f := &rows[i]
			last = f.ID
			var derr error
			for _, key := range []string{f.Path, f.ThumbnailPath, f.PreviewPath} {
				if key != "" {
					derr = errors.Join(derr, s.Storage.Delete(ctx, key))
				}
			}
			if derr == nil {
				derr = db.Where("id = ?", f.ID).Delete(&chat.FileInfo{}).Error
			}
			if derr != nil {
				failed++
				s.Logger.WarnContext(ctx, "files: cleanup left a file for the next run", "file", strings.TrimSpace(f.ID), "error", derr)
				continue
			}
			removed++
		}
	}
	if failed > 0 {
		return removed, fmt.Errorf("files cleanup: %d files left for the next run", failed)
	}
	return removed, nil
}

// CleanupTask is the periodic task running Cleanup every hour.
func (s *Service) CleanupTask() jobs.TaskType {
	return jobs.TaskType{
		Name:        TaskCleanup,
		MaxAttempts: 3,
		Period:      time.Hour,
		Timeout:     15 * time.Minute,
		Handler: func(ctx context.Context, _ *asynq.Task) error {
			n, err := s.Cleanup(ctx)
			if n > 0 {
				s.Logger.InfoContext(ctx, "files: cleanup", "removed", n)
			}
			return err
		},
	}
}
