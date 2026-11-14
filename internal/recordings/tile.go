package recordings

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Name tiles (design D5): the background of a layout span, drawn in Go with an embedded font so ffmpeg needs none —
// a dark frame with, in every slot, a coloured tile with the person's name. Cameras are laid over their slots.

// Slot is a rectangle of the frame and who it shows.
type Slot struct {
	X, Y, W, H int
	Name       string
	Key        string // stable colour
}

var (
	fontOnce sync.Once
	fontFace *opentype.Font
	fontErr  error
)

func face(size float64) (font.Face, error) {
	fontOnce.Do(func() { fontFace, fontErr = opentype.Parse(goregular.TTF) })
	if fontErr != nil {
		return nil, fontErr
	}
	return opentype.NewFace(fontFace, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

// tileColour is a muted colour derived from key (enough contrast for white text).
func tileColour(key string) color.RGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	palette := []color.RGBA{
		{0x2f, 0x4f, 0x7f, 0xff}, {0x5a, 0x3e, 0x7a, 0xff}, {0x1f, 0x5e, 0x4f, 0xff}, {0x7a, 0x3b, 0x3b, 0xff},
		{0x4a, 0x55, 0x2a, 0xff}, {0x6b, 0x47, 0x1f, 0xff}, {0x26, 0x4d, 0x63, 0xff}, {0x55, 0x33, 0x55, 0xff},
	}
	return palette[h.Sum32()%uint32(len(palette))]
}

// fit shortens name with an ellipsis until it fits width at face f.
func fit(f font.Face, name string, width int) string {
	if font.MeasureString(f, name).Ceil() <= width {
		return name
	}
	for r := []rune(name); len(r) > 1; r = r[:len(r)-1] {
		s := strings.TrimSpace(string(r[:len(r)-1])) + "…"
		if font.MeasureString(f, s).Ceil() <= width {
			return s
		}
	}
	return "…"
}

// DrawBackground writes a PNG of w×h with a tile per slot.
func DrawBackground(path string, w, h int, slots []Slot) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{0x12, 0x14, 0x18, 0xff}}, image.Point{}, draw.Src)
	for _, s := range slots {
		r := image.Rect(s.X+2, s.Y+2, s.X+s.W-2, s.Y+s.H-2)
		draw.Draw(img, r, &image.Uniform{C: tileColour(s.Key)}, image.Point{}, draw.Src)
		name := strings.TrimSpace(s.Name)
		if name == "" || !utf8.ValidString(name) {
			continue
		}
		size := max(min(float64(s.H)/7, 48), 12)
		f, err := face(size)
		if err != nil {
			return fmt.Errorf("name tile font: %w", err)
		}
		text := fit(f, name, s.W-24)
		tw := font.MeasureString(f, text).Ceil()
		d := font.Drawer{Dst: img, Src: image.White, Face: f,
			Dot: fixed.P(s.X+(s.W-tw)/2, s.Y+s.H/2+int(size)/3)}
		d.DrawString(text)
		_ = f.Close()
	}
	out, err := os.Create(path) //nolint:gosec // our own capture directory
	if err != nil {
		return fmt.Errorf("name tile: %w", err)
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close()
		return fmt.Errorf("name tile: %w", err)
	}
	return out.Close()
}
