package imaging_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/jniltinho/meethub/internal/imaging"
)

func TestFit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ w, h, maxW, maxH, wantW, wantH int }{
		{4000, 3000, 1920, 1 << 30, 1920, 1440}, // preview: width bound
		{4000, 3000, 1 << 30, 240, 320, 240},    // thumbnail: height bound
		{100, 50, 1920, 240, 100, 50},           // never enlarged
		{10000, 10, 1920, 240, 1920, 1},         // extreme ratio keeps at least a pixel
	} {
		if w, h := imaging.Fit(tc.w, tc.h, tc.maxW, tc.maxH); w != tc.wantW || h != tc.wantH {
			t.Errorf("Fit(%d,%d,%d,%d) = %d,%d, want %d,%d", tc.w, tc.h, tc.maxW, tc.maxH, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestScaleAverages(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := range 4 { // left half black, right half white
		c := color.RGBA{A: 255}
		if x >= 2 {
			c = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		}
		src.SetRGBA(x, 0, c)
		src.SetRGBA(x, 1, c)
	}
	got := imaging.Scale(src, src.Bounds(), 2, 1)
	if l, r := got.RGBAAt(0, 0), got.RGBAAt(1, 0); l.R != 0 || r.R != 255 {
		t.Fatalf("scaled halves: %v %v", l, r)
	}
	if mid := imaging.Scale(src, src.Bounds(), 1, 1).RGBAAt(0, 0); mid.R < 120 || mid.R > 135 {
		t.Fatalf("average of black and white: %v", mid)
	}
}

// withOrientation inserts an EXIF segment with the orientation tag after a JPEG's start marker.
func withOrientation(t *testing.T, order binary.AppendByteOrder, o uint16) []byte {
	t.Helper()
	var img bytes.Buffer
	if err := jpeg.Encode(&img, image.NewRGBA(image.Rect(0, 0, 4, 2)), nil); err != nil {
		t.Fatal(err)
	}
	tiff := []byte("MM\x00\x2a")
	if order == binary.LittleEndian {
		tiff = []byte("II\x2a\x00")
	}
	tiff = order.AppendUint32(tiff, 8)      // the first directory follows the header
	tiff = order.AppendUint16(tiff, 1)      // one entry
	tiff = order.AppendUint16(tiff, 0x0112) // orientation
	tiff = order.AppendUint16(tiff, 3)      // SHORT
	tiff = order.AppendUint32(tiff, 1)
	tiff = order.AppendUint16(tiff, o)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0) // value padding, no next directory
	exif := append([]byte("Exif\x00\x00"), tiff...)
	seg := binary.BigEndian.AppendUint16([]byte{0xff, 0xe0, 0, 4, 'x', 'y', 0xff, 0xe1}, uint16(len(exif)+2)) //nolint:gosec // short
	out := append([]byte{0xff, 0xd8}, append(seg, exif...)...)
	return append(out, img.Bytes()[2:]...)
}

func TestOrientation(t *testing.T) {
	t.Parallel()
	for _, order := range []binary.AppendByteOrder{binary.BigEndian, binary.LittleEndian} {
		data := withOrientation(t, order, 6)
		if o := imaging.Orientation(bytes.NewReader(data)); o != 6 {
			t.Fatalf("%v orientation = %d, want 6", order, o)
		}
		if _, err := imaging.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("the fixture is not a JPEG: %v", err)
		}
	}
	for name, data := range map[string][]byte{
		"no exif":   withOrientation(t, binary.BigEndian, 6)[:2],
		"png":       []byte("\x89PNG\r\n\x1a\n"),
		"bad value": withOrientation(t, binary.BigEndian, 42),
		"truncated": withOrientation(t, binary.BigEndian, 6)[:20],
		"empty":     nil,
	} {
		if o := imaging.Orientation(bytes.NewReader(data)); o != 1 {
			t.Errorf("%s: orientation = %d, want 1", name, o)
		}
	}
}

func TestOrient(t *testing.T) {
	t.Parallel()
	// a 2×1 picture: red on the left, blue on the right
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	src.SetRGBA(0, 0, red)
	src.SetRGBA(1, 0, blue)
	for o, want := range map[int]struct {
		w, h   int
		redAt  image.Point
		blueAt image.Point
	}{
		1: {2, 1, image.Pt(0, 0), image.Pt(1, 0)},
		2: {2, 1, image.Pt(1, 0), image.Pt(0, 0)},
		3: {2, 1, image.Pt(1, 0), image.Pt(0, 0)},
		4: {2, 1, image.Pt(0, 0), image.Pt(1, 0)},
		5: {1, 2, image.Pt(0, 0), image.Pt(0, 1)},
		6: {1, 2, image.Pt(0, 0), image.Pt(0, 1)}, // clockwise: the left edge goes to the top
		7: {1, 2, image.Pt(0, 1), image.Pt(0, 0)},
		8: {1, 2, image.Pt(0, 1), image.Pt(0, 0)}, // counterclockwise: the left edge goes to the bottom
	} {
		got := imaging.Orient(src, o)
		if got.Bounds().Dx() != want.w || got.Bounds().Dy() != want.h || got.RGBAAt(want.redAt.X, want.redAt.Y) != red ||
			got.RGBAAt(want.blueAt.X, want.blueAt.Y) != blue {
			t.Errorf("orientation %d: %v %v", o, got.Bounds(), got.Pix)
		}
		if imaging.Turned(o) != (want.w == 1) {
			t.Errorf("Turned(%d)", o)
		}
	}
}
