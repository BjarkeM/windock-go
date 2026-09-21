package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/lxn/walk"
)

// The program icon: coloured segments around a screen edge, broken where one
// trigger ends and the next begins. Drawn in code so the disabled state is the
// same shape in grey. 16x16 is the size that matters, so every dimension is a
// multiple of size/16 and snapped to whole pixels.
var (
	cBlue   = color.NRGBA{0x2A, 0x78, 0xD6, 0xFF}
	cOrange = color.NRGBA{0xEB, 0x68, 0x34, 0xFF}
	cAqua   = color.NRGBA{0x1B, 0xAF, 0x7A, 0xFF}
	cYellow = color.NRGBA{0xED, 0xA1, 0x00, 0xFF}
	cGrey   = color.NRGBA{0x80, 0x80, 0x80, 0xFF}
)

type iconKey struct {
	enabled bool
	nominal int
}

// iconImages memoises one walk.Image per state and size: walk's own icon cache
// is keyed on the Image value and never evicts, so a fresh object per call
// would leak a GDI icon. UI thread only, hence no mutex.
var iconImages = map[iconKey]walk.Image{}

// appIcon returns the program icon for a title bar, taskbar button, Alt-Tab
// entry or the notification area.
func appIcon(enabled bool) walk.Image {
	return appIconSized(enabled, 16)
}

// appIconSized is appIcon at a given nominal size. A paint function rather
// than a fixed bitmap, so each size is drawn at its own size and stays crisp.
func appIconSized(enabled bool, nominal int) walk.Image {
	key := iconKey{enabled, nominal}
	if img, ok := iconImages[key]; ok {
		return img
	}

	img := walk.NewPaintFuncImagePixels(
		walk.Size{Width: nominal, Height: nominal},
		func(canvas *walk.Canvas, bounds walk.Rectangle) error {
			size := bounds.Height
			if bounds.Width < size {
				size = bounds.Width
			}
			if size < 1 {
				return nil
			}
			bmp, err := walk.NewBitmapFromImageForDPI(drawIcon(size, enabled), 96)
			if err != nil {
				return err
			}
			defer bmp.Dispose()
			return canvas.DrawImageStretchedPixels(bmp, bounds)
		})

	iconImages[key] = img
	return img
}

func drawIcon(size int, enabled bool) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	u := unit(size)
	r := plateRect(size)
	a, b, c, ylw := iconPalette(enabled)

	t := 3 * u // two units leaves too much hole to read as a ring at 16px
	gap := u   // the break between adjacent segments on the same edge
	mid := r.Min.Y + r.Dy()/2

	fillRect(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, mid-gap), a)
	fillRect(img, image.Rect(r.Min.X, mid+gap, r.Min.X+t, r.Max.Y), c)
	fillRect(img, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, mid-gap), c)
	fillRect(img, image.Rect(r.Max.X-t, mid+gap, r.Max.X, r.Max.Y), a)

	// Butted against the vertical bars; inset, they leave a hole at each corner.
	fillRect(img, image.Rect(r.Min.X+t, r.Min.Y, r.Max.X-t, r.Min.Y+t), b)
	fillRect(img, image.Rect(r.Min.X+t, r.Max.Y-t, r.Max.X-t, r.Max.Y), ylw)
	return img
}

func unit(size int) int {
	u := int(math.Round(float64(size) / 16))
	if u < 1 {
		return 1
	}
	return u
}

// plateRect is the screen the icon sits inside, one unit in on every side.
func plateRect(size int) image.Rectangle {
	u := unit(size)
	return image.Rect(u, u, size-u, size-u)
}

func iconPalette(enabled bool) (a, b, c, dsh color.NRGBA) {
	if !enabled {
		return cGrey, cGrey, cGrey, cGrey
	}
	return cBlue, cOrange, cAqua, cYellow
}

func fillRect(dst draw.Image, r image.Rectangle, c color.NRGBA) {
	draw.Draw(dst, r, &image.Uniform{c}, image.Point{}, draw.Over)
}
