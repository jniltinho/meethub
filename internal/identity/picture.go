package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // accepted upload formats
	_ "image/jpeg" //
	"image/png"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
)

// Profile picture limits (profile-images spec).
const (
	PictureMaxBytes = 5 << 20
	PictureMaxSide  = 4096
	PictureSide     = 256
)

// Picture errors.
var (
	ErrInvalidImage  = errors.New("the file is not a PNG, JPEG or GIF image of at most 4096 pixels a side")
	ErrImageTooLarge = errors.New("the image is larger than 5 MiB")
	ErrNoPicture     = errors.New("no profile picture")
)

// NormalizePicture turns an upload into the stored form: a 256 px square PNG
// cropped around the center. The format comes from the bytes (never a name or
// a declared type), the size from the header before anything is decoded, and
// re-encoding drops all metadata and whatever was appended to the file.
func NormalizePicture(data []byte) ([]byte, error) {
	if len(data) > PictureMaxBytes {
		return nil, ErrImageTooLarge
	}
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg", "image/gif":
	default:
		return nil, ErrInvalidImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > PictureMaxSide || cfg.Height > PictureMaxSide {
		return nil, ErrInvalidImage
	}
	src, _, err := image.Decode(bytes.NewReader(data)) // a GIF gives its first frame
	if err != nil {
		return nil, ErrInvalidImage
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, PictureSide, PictureSide))
	// box filter: each output pixel is the average of the source pixels it covers (at least one)
	for y := range PictureSide {
		sy0, sy1 := y0+y*side/PictureSide, y0+(y+1)*side/PictureSide
		sy1 = max(sy1, sy0+1)
		for x := range PictureSide {
			sx0, sx1 := x0+x*side/PictureSide, x0+(x+1)*side/PictureSide
			sx1 = max(sx1, sx0+1)
			var r, g, bl, a, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, pa := src.At(sx, sy).RGBA() // premultiplied, 16 bits
					r, g, bl, a, n = r+uint64(pr), g+uint64(pg), bl+uint64(pb), a+uint64(pa), n+1
				}
			}
			i := dst.PixOffset(x, y)
			//nolint:gosec // an average of 16-bit channels, shifted to 8 bits, fits a byte
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), uint8(a/n>>8)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, fmt.Errorf("encode picture: %w", err)
	}
	return out.Bytes(), nil
}

// userImage is a stored profile picture (always a normalized PNG).
type userImage struct {
	UserID    string `gorm:"primaryKey;size:26"`
	Data      []byte
	UpdatedAt int64 `gorm:"autoUpdateTime:false"`
}

func (userImage) TableName() string { return "user_images" }

// SetPicture stores a normalized picture for the user and stamps their
// last_picture_update, in tx; it returns the new stamp.
func SetPicture(ctx context.Context, tx *gorm.DB, userID string, normalized []byte, now time.Time) (int64, error) {
	at := now.UTC().UnixMilli()
	if err := tx.WithContext(ctx).Where("user_id = ?", userID).Delete(&userImage{}).Error; err != nil {
		return 0, fmt.Errorf("replace picture: %w", err)
	}
	if err := tx.WithContext(ctx).Create(&userImage{UserID: userID, Data: normalized, UpdatedAt: at}).Error; err != nil {
		return 0, fmt.Errorf("store picture: %w", err)
	}
	return at, stampPicture(ctx, tx, userID, at)
}

// RemovePicture deletes the user's picture, in tx; their stamp returns to 0.
func RemovePicture(ctx context.Context, tx *gorm.DB, userID string) error {
	if err := tx.WithContext(ctx).Where("user_id = ?", userID).Delete(&userImage{}).Error; err != nil {
		return fmt.Errorf("remove picture: %w", err)
	}
	return stampPicture(ctx, tx, userID, 0)
}

func stampPicture(ctx context.Context, tx *gorm.DB, userID string, at int64) error {
	if err := tx.WithContext(ctx).Model(&User{}).Where("id = ?", userID).UpdateColumn("last_picture_update", at).Error; err != nil {
		return fmt.Errorf("stamp picture: %w", err)
	}
	return nil
}

// Picture returns the user's stored PNG and its stamp, or ErrNoPicture.
func Picture(ctx context.Context, db *gorm.DB, userID string) ([]byte, int64, error) {
	var img userImage
	if err := db.WithContext(ctx).Where("user_id = ?", userID).Take(&img).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, 0, ErrNoPicture
		}
		return nil, 0, fmt.Errorf("read picture: %w", err)
	}
	return img.Data, img.UpdatedAt, nil
}
