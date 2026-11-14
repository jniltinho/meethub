package handlers

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/httpserver"
)

// ErrTooManyFiles answers an upload with more files than a message carries.
var ErrTooManyFiles = httpserver.NewError(http.StatusBadRequest, "too_many_files", "Too many files for one message.")

// filesFormOverhead is the room for the multipart framing around the files.
const filesFormOverhead = 1 << 20

func (a *API) filesOrUnavailable() error {
	if a.Files == nil {
		return ErrUnavailable
	}
	return nil
}

// uploadFiles serves POST /files: a multipart stream with channel_id first
// and then one or more "files" parts, each piped to the storage as it
// arrives (never buffered whole).
func (a *API) uploadFiles(c *echo.Context) error {
	if err := a.filesOrUnavailable(); err != nil {
		return err
	}
	mr, err := c.Request().MultipartReader()
	if err != nil {
		return httpserver.ErrBadRequest
	}
	ctx, sub := c.Request().Context(), subject(c)
	channelID := ""
	infos := []chat.FileView{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return files.ErrTooLarge
		}
		if err != nil {
			return httpserver.ErrBadRequest
		}
		switch part.FormName() {
		case "channel_id":
			id, _ := io.ReadAll(io.LimitReader(part, 64))
			channelID = strings.TrimSpace(string(id))
		case "files":
			if channelID == "" { // the channel decides who may upload: it comes before any file
				return httpserver.ErrInvalidInput
			}
			// files stored before a refused one stay unattached and are removed by the cleanup
			if len(infos) >= a.Files.MaxPerPost {
				return ErrTooManyFiles
			}
			info, err := a.Files.Upload(ctx, sub, channelID, part.FileName(), part)
			if err != nil {
				return err
			}
			infos = append(infos, *info)
		default:
			return httpserver.ErrInvalidInput
		}
	}
	if len(infos) == 0 {
		return httpserver.ErrInvalidInput
	}
	return c.JSON(http.StatusCreated, map[string]any{"file_infos": infos})
}

// fileContent serves GET /files/:id and its thumbnail and preview. The file
// itself is always a download, whatever its type, in a sandbox: it never
// renders from this origin. The derived images are the server's own JPEG
// encodings and may show inline.
func (a *API) fileContent(variant string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if err := a.filesOrUnavailable(); err != nil {
			return err
		}
		f, rc, err := a.Files.Open(c.Request().Context(), subject(c), c.Param("id"), variant)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		h := c.Response().Header()
		h.Set("X-Content-Type-Options", "nosniff")
		if variant == files.Original {
			h.Set("Content-Type", f.MimeType)
			h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
			h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
			h.Set("Cache-Control", "private, no-cache")
		} else {
			h.Set("Content-Type", "image/jpeg")
			h.Set("Cache-Control", "private, max-age=86400") // a file's bytes never change
		}
		http.ServeContent(c.Response(), c.Request(), "", time.UnixMilli(f.CreateAt), rc) // ranges and conditionals
		return nil
	}
}

// fileInfo serves GET /files/:id/info.
func (a *API) fileInfo(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	f, err := a.Chat.ReadableFile(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, f.View())
}

// channelFiles serves GET /channels/:id/files.
func (a *API) channelFiles(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	list, next, err := a.Chat.ChannelFiles(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("cursor"), perPage(c))
	return paged(c, list, next, err)
}

// myFiles serves GET /users/me/files (q filters by name).
func (a *API) myFiles(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	list, next, err := a.Chat.MyFiles(c.Request().Context(), subject(c), c.QueryParam("q"), c.QueryParam("cursor"), perPage(c))
	return paged(c, list, next, err)
}
