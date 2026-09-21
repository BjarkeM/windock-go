package engine

import (
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/palette"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

// Colours, perimeter ordering and keylines live in internal/palette, shared
// with the settings editor. What stays here is the GDI side: the paint path
// runs from the mouse hook, so brushes are created once and reused.

var paletteColors = palette.Colors

const (
	keylineDarkColor  = palette.KeylineDark
	keylineLightColor = palette.KeylineLight
)

func assignSlots(mons *monitor.Set, layout *zones.Layout) []int {
	return palette.AssignSlots(mons, layout)
}

// slotBrushes holds the GDI brushes for one palette slot.
type slotBrushes struct {
	fill   uintptr
	border uintptr // a lightened version, for the preview outline
}

var (
	paletteBrushes []slotBrushes
	keylineDark    uintptr
	keylineLight   uintptr
)

// initPalette creates the brushes. Passing a non-zero override collapses the
// palette to a single colour, for users who preferred the uniform look.
func initPalette(override uint32, categorical bool) {
	freePalette()

	colors := paletteColors
	if !categorical {
		colors = []uint32{override}
	}
	paletteBrushes = make([]slotBrushes, len(colors))
	for i, c := range colors {
		paletteBrushes[i] = slotBrushes{
			fill:   win.CreateSolidBrush(colorRef(c)),
			border: win.CreateSolidBrush(colorRef(lighten(c, 0.45))),
		}
	}
	keylineDark = win.CreateSolidBrush(colorRef(keylineDarkColor))
	keylineLight = win.CreateSolidBrush(colorRef(keylineLightColor))
}

func freePalette() {
	for _, b := range paletteBrushes {
		win.DeleteObject(b.fill)
		win.DeleteObject(b.border)
	}
	paletteBrushes = nil
	win.DeleteObject(keylineDark)
	win.DeleteObject(keylineLight)
	keylineDark, keylineLight = 0, 0
}

// brushesFor returns the brushes for a slot, wrapping if a profile has more
// triggers than the palette has colours.
func brushesFor(slot int) slotBrushes {
	if len(paletteBrushes) == 0 {
		return slotBrushes{}
	}
	return paletteBrushes[((slot%len(paletteBrushes))+len(paletteBrushes))%len(paletteBrushes)]
}
