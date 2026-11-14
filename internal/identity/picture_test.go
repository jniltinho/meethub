package identity_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/jniltinho/meethub/internal/identity"
)

// stripes is a w×h picture: red on the left third, green in the middle, blue on the right.
func stripes(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/3 {
				c = color.RGBA{G: 255, A: 255}
			}
			if x >= 2*w/3 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encode(t *testing.T, format string, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&b, img)
	case "jpeg":
		err = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	case "gif":
		err = gif.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withEXIF inserts an APP1 segment (as a camera does, here with a GPS marker) after the JPEG's SOI.
func withEXIF(jpg []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), []byte("GPSLatitude=-23.5505;GPSLongitude=-46.6333")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(append(append([]byte{}, jpg[:2]...), append(seg, payload...)...), jpg[2:]...)
}

// bombPNG is a tiny file whose header declares side×side pixels.
func bombPNG(side uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], side)
	binary.BigEndian.PutUint32(ihdr[4:], side)
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

func TestNormalizePicture(t *testing.T) {
	t.Parallel()
	wide := stripes(3000, 2000)
	for _, tc := range []struct {
		name string
		in   []byte
		err  error
	}{
		{name: "png", in: encode(t, "png", wide)},
		{name: "jpeg with EXIF", in: withEXIF(encode(t, "jpeg", wide))},
		{name: "gif", in: encode(t, "gif", stripes(300, 200))},
		{name: "small picture is enlarged", in: encode(t, "png", stripes(30, 20))},
		{name: "png with a script appended", in: append(encode(t, "png", stripes(300, 200)), []byte("<script>alert(1)</script>")...)},
		{name: "html named photo.png", in: []byte("<!doctype html><html><body><script>alert(1)</script></body></html>"), err: identity.ErrInvalidImage},
		{name: "empty", in: nil, err: identity.ErrInvalidImage},
		{name: "truncated png", in: encode(t, "png", wide)[:200], err: identity.ErrInvalidImage},
		{name: "decompression bomb", in: bombPNG(30000), err: identity.ErrInvalidImage},
		{name: "one pixel over the side limit", in: bombPNG(identity.PictureMaxSide + 1), err: identity.ErrInvalidImage},
		{name: "over 5 MiB", in: make([]byte, identity.PictureMaxBytes+1), err: identity.ErrImageTooLarge},
	} {
		out, err := identity.NormalizePicture(tc.in)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("%s: %v, want %v", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		img, format, err := image.Decode(bytes.NewReader(out))
		if err != nil || format != "png" || img.Bounds().Dx() != identity.PictureSide || img.Bounds().Dy() != identity.PictureSide {
			t.Errorf("%s: stored as %s %v (%v)", tc.name, format, img.Bounds(), err)
			continue
		}
		// the centered square of a 3:2 picture is its middle: green in the center, never the outer edges' full width
		if r, g, b, _ := img.At(identity.PictureSide/2, identity.PictureSide/2).RGBA(); g < 0x8000 || r > 0x4000 || b > 0x4000 {
			t.Errorf("%s: center is not the middle stripe: %04x %04x %04x", tc.name, r, g, b)
		}
		for _, leak := range []string{"Exif", "GPS", "<script>"} {
			if bytes.Contains(out, []byte(leak)) {
				t.Errorf("%s: %q survived the re-encoding", tc.name, leak)
			}
		}
	}
}
