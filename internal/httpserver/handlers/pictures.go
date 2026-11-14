package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/identity"
)

// pictureFormOverhead is the room for the multipart framing around the file.
const pictureFormOverhead = 64 << 10

// BodyLimits lists the routes with a request body limit of their own.
func BodyLimits() map[string]int64 {
	return map[string]int64{"POST /api/v1/users/me/image": identity.PictureMaxBytes + pictureFormOverhead}
}

// uploadPicture serves POST /users/me/image (multipart field "image"): the
// caller's profile picture, normalized before it is stored.
func (a *API) uploadPicture(c *echo.Context) error {
	p := principal(c)
	fh, err := c.FormFile("image")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return identity.ErrImageTooLarge
		}
		return identity.ErrInvalidImage
	}
	if fh.Size > identity.PictureMaxBytes {
		return identity.ErrImageTooLarge
	}
	f, err := fh.Open()
	if err != nil {
		return identity.ErrInvalidImage
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, identity.PictureMaxBytes+1))
	if err != nil {
		return identity.ErrInvalidImage
	}
	normalized, err := identity.NormalizePicture(data)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var at int64
	err = a.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		at, err = identity.SetPicture(ctx, tx, p.User.ID, normalized, time.Now())
		return err
	})
	if err != nil {
		return err
	}
	a.userUpdated(c, p.User.ID)
	return c.JSON(http.StatusOK, map[string]int64{"last_picture_update": at})
}

// picture serves GET /users/:id/image to callers who may see that user; a
// user without a picture and a user the caller may not see answer alike.
func (a *API) picture(c *echo.Context) error {
	if err := a.chatOrUnavailable(); err != nil {
		return err
	}
	ctx, id := c.Request().Context(), c.Param("id")
	if id == "me" {
		id = principal(c).User.ID
	}
	if visible, err := a.Chat.UsersByIDs(ctx, subject(c), []string{id}); err != nil {
		return err
	} else if len(visible) == 0 {
		return identity.ErrNoPicture
	}
	data, at, err := identity.Picture(ctx, a.DB.Gorm, id)
	if err != nil {
		return err
	}
	h := c.Response().Header()
	etag := `"` + strconv.FormatInt(at, 10) + `"`
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	if c.Request().Header.Get("If-None-Match") == etag {
		return c.NoContent(http.StatusNotModified)
	}
	return c.Blob(http.StatusOK, "image/png", data)
}

// removeMyPicture serves DELETE /users/me/image.
func (a *API) removeMyPicture(c *echo.Context) error {
	p, ctx := principal(c), c.Request().Context()
	err := a.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return identity.RemovePicture(ctx, tx, p.User.ID)
	})
	if err != nil {
		return err
	}
	a.userUpdated(c, p.User.ID)
	return c.NoContent(http.StatusNoContent)
}

// removeUserPicture serves DELETE /admin/users/:id/image (moderation).
func (a *API) removeUserPicture(c *echo.Context) error {
	id := c.Param("id")
	if err := a.Users.RemovePicture(c.Request().Context(), principal(c).Actor(c.RealIP()), id); err != nil {
		return err
	}
	a.userUpdated(c, id)
	return c.NoContent(http.StatusNoContent)
}

// userUpdated tells the users who may see id that their profile changed; the
// picture is already stored, so a failure here only delays what they see.
func (a *API) userUpdated(c *echo.Context, id string) {
	if a.Chat == nil {
		return
	}
	if err := a.Chat.UserUpdated(c.Request().Context(), id); err != nil {
		c.Logger().Warn("user_updated not announced", "user", id, "error", err)
	}
}
