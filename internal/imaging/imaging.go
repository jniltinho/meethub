// Package imaging is what MeetHub does to uploaded images: read the format
// from the bytes, check the size from the header before decoding anything,
// and scale. Its outputs are always the server's own encodings, so metadata
// and anything appended to a file never reach other users.
package imaging

import (
	"bufio"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif" // accepted formats
	"image/jpeg"
	_ "image/png" //
	"io"
	"net/http"
)

// ErrNotImage is returned for bytes that are not a decodable PNG, JPEG or GIF
// within the given pixel limits.
var ErrNotImage = errors.New("not a supported image")

// Accepted reports whether the bytes start as a PNG, JPEG or GIF (by content,
// never by a name or a declared type).
func Accepted(head []byte) bool {
	switch http.DetectContentType(head) {
	case "image/png", "image/jpeg", "image/gif":
		return true
	}
	return false
}

// Size reads the image's dimensions from its header (cheap, whatever the
// file declares) and rejects sides above maxSide or more than maxPixels.
func Size(r io.Reader, maxSide, maxPixels int) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide ||
		cfg.Width*cfg.Height > maxPixels {
		return 0, 0, ErrNotImage
	}
	return cfg.Width, cfg.Height, nil
}

// Decode decodes a PNG, JPEG or the first frame of a GIF.
func Decode(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, ErrNotImage
	}
	return img, nil
}

// Scale draws the region of src into a w×h picture with a box filter: each
// output pixel is the average of the source pixels it covers (at least one).
func Scale(src image.Image, region image.Rectangle, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		sy0, sy1 := region.Min.Y+y*region.Dy()/h, region.Min.Y+(y+1)*region.Dy()/h
		sy1 = max(sy1, sy0+1)
		for x := range w {
			sx0, sx1 := region.Min.X+x*region.Dx()/w, region.Min.X+(x+1)*region.Dx()/w
			sx1 = max(sx1, sx0+1)
			var r, g, b, a, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, pa := src.At(sx, sy).RGBA() // premultiplied, 16 bits
					r, g, b, a, n = r+uint64(pr), g+uint64(pg), b+uint64(pb), a+uint64(pa), n+1
				}
			}
			i := dst.PixOffset(x, y)
			//nolint:gosec // an average of 16-bit channels, shifted to 8 bits, fits a byte
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(b/n>>8), uint8(a/n>>8)
		}
	}
	return dst
}

// Fit returns the size of a w×h image scaled down (never up) to fit maxW×maxH.
func Fit(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	if w*maxH > h*maxW { // the width is the tighter bound
		return maxW, max(1, h*maxW/w)
	}
	return max(1, w*maxH/h), maxH
}

// EncodeJPEG writes img as a JPEG (quality 85) over a white background, since
// JPEG has no transparency. It changes img.
func EncodeJPEG(w io.Writer, img *image.RGBA) error {
	for i := 0; i < len(img.Pix); i += 4 { // premultiplied: color + white × (1 - alpha)
		rest := 255 - img.Pix[i+3]
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = img.Pix[i]+rest, img.Pix[i+1]+rest, img.Pix[i+2]+rest, 255
	}
	return jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
}

// Orientation reads the EXIF orientation (1 to 8) of a JPEG: how a viewer
// must turn the stored pixels. It answers 1 (as stored) for anything else.
func Orientation(r io.Reader) int {
	br := bufio.NewReader(r)
	var h [4]byte
	if _, err := io.ReadFull(br, h[:2]); err != nil || h[0] != 0xff || h[1] != 0xd8 {
		return 1
	}
	for { // the segments before the image data: marker, length, payload
		if _, err := io.ReadFull(br, h[:]); err != nil || h[0] != 0xff || h[1] == 0xda {
			return 1
		}
		n := int(binary.BigEndian.Uint16(h[2:])) - 2
		if n < 0 {
			return 1
		}
		if h[1] != 0xe1 { // not APP1
			if _, err := br.Discard(n); err != nil {
				return 1
			}
			continue
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(br, seg); err != nil {
			return 1
		}
		if o := exifOrientation(seg); o != 0 {
			return o
		}
	}
}

// exifOrientation finds tag 0x0112 in the first directory of an EXIF segment (0: none).
func exifOrientation(seg []byte) int {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := seg[6:]
	var order binary.ByteOrder = binary.BigEndian
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
	default:
		return 0
	}
	dir := int(order.Uint32(tiff[4:8]))
	if dir < 8 || dir+2 > len(tiff) {
		return 0
	}
	for i := range int(order.Uint16(tiff[dir:])) {
		entry := dir + 2 + i*12
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			if v := int(order.Uint16(tiff[entry+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}

// Turned reports whether an orientation swaps width and height.
func Turned(orientation int) bool { return orientation >= 5 && orientation <= 8 }

// Orient returns img as a viewer shows it for an EXIF orientation.
func Orient(img *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if Turned(orientation) {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := range h {
		for x := range w {
			dx, dy := x, y
			switch orientation {
			case 2: // mirrored
				dx = w - 1 - x
			case 3: // upside down
				dx, dy = w-1-x, h-1-y
			case 4: // flipped
				dy = h - 1 - y
			case 5: // transposed
				dx, dy = y, x
			case 6: // a quarter turn clockwise
				dx, dy = h-1-y, x
			case 7: // transversed
				dx, dy = h-1-y, w-1-x
			case 8: // a quarter turn counterclockwise
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):][:4], img.Pix[img.PixOffset(x, y):][:4])
		}
	}
	return dst
}
