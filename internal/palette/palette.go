// The palette package holds the categorical colours the guides, the drag preview
// and the settings editor draw in.
package palette

import (
	"math"
	"sort"

	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/zones"
)

// Colors is a categorical palette, used in this fixed order. The order is the
// colourblind-safety mechanism: it is validated for separation between
// adjacent slots, which is why AssignSlots hands them out around the monitor
// perimeter.
var Colors = []uint32{
	0x2A78D6, // blue
	0xEB6834, // orange
	0x1BAF7A, // aqua
	0xEDA100, // yellow
	0xE87BA4, // magenta
	0x008300, // green
	0x4A3AA7, // violet
	0xE34948, // red
}

// Keylines. The surface here is whatever wallpaper the user has, so every shape
// carries a dark keyline to stay legible; the active shape swaps to the light
// one.
const (
	KeylineDark  = 0x101010
	KeylineLight = 0xFFFFFF
)

// Color returns the colour for a slot, or the override if not categorical.
func Color(slot int, categorical bool, override uint32) uint32 {
	if !categorical {
		return override
	}
	n := len(Colors)
	return Colors[((slot%n)+n)%n]
}

// Lighten moves a colour towards white by a fraction.
func Lighten(c uint32, f float64) uint32 {
	r := float64((c >> 16) & 0xff)
	g := float64((c >> 8) & 0xff)
	b := float64(c & 0xff)
	mix := func(v float64) uint32 {
		v = v + (255-v)*f
		if v > 255 {
			v = 255
		}
		if v < 0 {
			v = 0
		}
		return uint32(v + 0.5)
	}
	return mix(r)<<16 | mix(g)<<8 | mix(b)
}

// Blend mixes a colour over an opaque background at the given opacity, 0 to 1.
// GDI brushes have no alpha, so anything translucent is flattened first.
func Blend(fg, bg uint32, alpha float64) uint32 {
	if alpha <= 0 {
		return bg
	}
	if alpha >= 1 {
		return fg
	}
	ch := func(shift uint) uint32 {
		f := float64((fg >> shift) & 0xff)
		b := float64((bg >> shift) & 0xff)
		return uint32(f*alpha + b*(1-alpha) + 0.5)
	}
	return ch(16)<<16 | ch(8)<<8 | ch(0)
}

// AssignSlots gives every zone a palette slot, in order around each monitor's
// perimeter so that neighbouring triggers get neighbouring - and therefore
// validated-distinct - colours. Indexed by zone index.
func AssignSlots(mons *monitor.Set, layout *zones.Layout) []int {
	slots := make([]int, layout.Len())

	for _, m := range mons.All() {
		type entry struct {
			zoneIndex int
			angle     float64
		}
		var ring []entry

		cx := float64(m.Bounds.Left+m.Bounds.Right) / 2
		cy := float64(m.Bounds.Top+m.Bounds.Bottom) / 2

		for i, z := range layout.Zones() {
			if z.TriggerMonitor != m.Index {
				continue
			}
			px := float64(z.Hit.Left+z.Hit.Right) / 2
			py := float64(z.Hit.Top+z.Hit.Bottom) / 2
			ring = append(ring, entry{zoneIndex: i, angle: ClockwiseFromTop(px-cx, py-cy)})
		}

		sort.SliceStable(ring, func(a, b int) bool {
			if ring[a].angle != ring[b].angle {
				return ring[a].angle < ring[b].angle
			}
			return ring[a].zoneIndex < ring[b].zoneIndex
		})
		for n, e := range ring {
			slots[e.zoneIndex] = n
		}
	}
	return slots
}

// ClockwiseFromTop returns the angle of (dx, dy) clockwise from straight up, in
// [0, 2pi). Screen y grows downwards, hence the negated dy.
func ClockwiseFromTop(dx, dy float64) float64 {
	a := math.Atan2(dx, -dy)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}
