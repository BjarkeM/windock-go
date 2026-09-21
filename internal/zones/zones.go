// The zones package turns a profile's percentage rules into pixel rectangles for
// the attached monitors, and hit-tests the cursor against them. A Layout is
// only valid for the monitor Set it was built from.
package zones

import (
	"math"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
)

// Trigger priority classes, as the original documents: corners beat edges beat
// areas, regardless of profile order. Ties go to the earlier rule.
const (
	prioCorner = 0
	prioEdge   = 1
	prioArea   = 2
)

// Zone is one resolved rule: where the cursor has to be, and where the window
// then goes.
type Zone struct {
	// RuleIndex is the position of the source rule in the profile.
	RuleIndex int

	// Hit is the cursor region that activates this zone, in virtual-desktop
	// pixels.
	Hit win.RECT

	// Dock is where the dragged window is placed, in virtual-desktop pixels.
	Dock win.RECT

	// TriggerMonitor and DockMonitor are the stable monitor indices involved.
	TriggerMonitor int
	DockMonitor    int

	// TriggerType and TriggerPos are carried through so the UI can draw a guide
	// shaped like the trigger.
	TriggerType string
	TriggerPos  string

	// WorkArea is the source dock's setting, which the engine needs to tell a
	// full-screen dock from one that merely fills the work area.
	WorkArea bool

	priority int
}

// Layout is the set of zones active for one profile on one monitor topology.
type Layout struct {
	zones []Zone

	// Skipped counts rules that could not be resolved, almost always because
	// they name a monitor that is not currently attached.
	Skipped int
}

// Zones returns the resolved zones, already in match order.
func (l *Layout) Zones() []Zone {
	if l == nil {
		return nil
	}
	return l.zones
}

// Len returns the number of active zones.
func (l *Layout) Len() int {
	if l == nil {
		return 0
	}
	return len(l.zones)
}

// Resolve builds the layout for a profile against the current monitors. Rules
// naming an absent monitor are skipped rather than redirected, which is what
// stops a disconnected RDP session snapping windows into nonsense positions.
func Resolve(rules []config.Rule, mons *monitor.Set, g config.Global) *Layout {
	l := &Layout{}
	if mons.Len() == 0 {
		l.Skipped = len(rules)
		return l
	}

	edgeThickness := int32(g.EdgeThickness())
	cornerSize := int32(g.CornerSize())

	for i, r := range rules {
		if err := r.Validate(); err != nil {
			l.Skipped++
			continue
		}
		tm := mons.ByIndex(r.Trigger.Monitor)
		dm := mons.ByIndex(r.Dock.Monitor)
		if tm == nil || dm == nil {
			l.Skipped++
			continue
		}

		hit, prio, ok := triggerRect(r.Trigger, tm.Bounds, tm.WorkArea, edgeThickness, cornerSize)
		if !ok {
			l.Skipped++
			continue
		}

		// Per zone: a dock that keeps clear of the taskbar measures against the
		// work area, one that does not measures against the full bounds.
		dock := dockRect(r.Dock, dm.Area(r.Dock.WorkArea()))
		if dock.IsEmpty() {
			l.Skipped++
			continue
		}

		l.zones = append(l.zones, Zone{
			RuleIndex:      i,
			Hit:            hit,
			Dock:           dock,
			TriggerMonitor: tm.Index,
			DockMonitor:    dm.Index,
			TriggerType:    r.Trigger.Type,
			TriggerPos:     r.Trigger.Pos,
			WorkArea:       r.Dock.WorkArea(),
			priority:       prio,
		})
	}

	// Stable sort by priority alone keeps profile order within each class.
	stableSortByPriority(l.zones)
	return l
}

func stableSortByPriority(z []Zone) {
	// Insertion sort: zone counts are small (a profile is a handful of rules)
	// and this keeps equal priorities in profile order.
	for i := 1; i < len(z); i++ {
		for j := i; j > 0 && z[j].priority < z[j-1].priority; j-- {
			z[j], z[j-1] = z[j-1], z[j]
		}
	}
}

// Match returns the zone the cursor is currently inside, or nil.
func (l *Layout) Match(p win.POINT) *Zone {
	i := l.MatchIndex(p)
	if i < 0 {
		return nil
	}
	return &l.zones[i]
}

// MatchIndex returns the index of the zone the cursor is inside, or -1. The
// index is what the guide overlay uses to mark one trigger as active.
func (l *Layout) MatchIndex(p win.POINT) int {
	if l == nil {
		return -1
	}
	for i := range l.zones {
		if l.zones[i].Hit.Contains(p) {
			return i
		}
	}
	return -1
}

// triggerRect converts a trigger into pixels within the given monitor bounds.
//
// An edge trigger spans from the work-area edge outward to the physical edge.
// With an appbar on that edge a strip measured from the physical edge alone
// sits underneath it, and applications that draw their own title bar clamp a
// dragged window to the work area - so the window stops while the trigger is
// still a taskbar-height further down. With no appbar the two coincide and this
// is just a strip of edgeThickness.
func triggerRect(t config.Trigger, b, work win.RECT, edgeThickness, cornerSize int32) (win.RECT, int, bool) {
	w := b.Width()
	h := b.Height()

	switch t.Type {
	case config.TypeCorner:
		cw := clampSpan(cornerSize, w)
		ch := clampSpan(cornerSize, h)
		switch t.Pos {
		case config.PosTopLeft:
			return win.RECT{Left: b.Left, Top: b.Top, Right: b.Left + cw, Bottom: b.Top + ch}, prioCorner, true
		case config.PosTopRight:
			return win.RECT{Left: b.Right - cw, Top: b.Top, Right: b.Right, Bottom: b.Top + ch}, prioCorner, true
		case config.PosBottomLeft:
			return win.RECT{Left: b.Left, Top: b.Bottom - ch, Right: b.Left + cw, Bottom: b.Bottom}, prioCorner, true
		case config.PosBottomRight:
			return win.RECT{Left: b.Right - cw, Top: b.Bottom - ch, Right: b.Right, Bottom: b.Bottom}, prioCorner, true
		}
		return win.RECT{}, 0, false

	case config.TypeEdge:
		thick := clampSpan(edgeThickness, minInt32(w, h))
		from, to := t.Values[0], t.Values[1]
		switch t.Pos {
		case config.PosLeft:
			y0 := b.Top + pct(h, from)
			y1 := b.Top + pct(h, to)
			inner := clampInt32(work.Left+thick, b.Left+1, b.Right)
			return win.RECT{Left: b.Left, Top: y0, Right: inner, Bottom: y1}, prioEdge, true
		case config.PosRight:
			y0 := b.Top + pct(h, from)
			y1 := b.Top + pct(h, to)
			inner := clampInt32(work.Right-thick, b.Left, b.Right-1)
			return win.RECT{Left: inner, Top: y0, Right: b.Right, Bottom: y1}, prioEdge, true
		case config.PosTop:
			x0 := b.Left + pct(w, from)
			x1 := b.Left + pct(w, to)
			inner := clampInt32(work.Top+thick, b.Top+1, b.Bottom)
			return win.RECT{Left: x0, Top: b.Top, Right: x1, Bottom: inner}, prioEdge, true
		case config.PosBottom:
			x0 := b.Left + pct(w, from)
			x1 := b.Left + pct(w, to)
			inner := clampInt32(work.Bottom-thick, b.Top, b.Bottom-1)
			return win.RECT{Left: x0, Top: inner, Right: x1, Bottom: b.Bottom}, prioEdge, true
		}
		return win.RECT{}, 0, false

	case config.TypeArea:
		r := win.RECT{
			Left:   b.Left + pct(w, t.Values[0]),
			Top:    b.Top + pct(h, t.Values[1]),
			Right:  b.Left + pct(w, t.Values[2]),
			Bottom: b.Top + pct(h, t.Values[3]),
		}
		if r.IsEmpty() {
			return win.RECT{}, 0, false
		}
		return r, prioArea, true
	}
	return win.RECT{}, 0, false
}

// dockRect converts a dock into pixels within the given area.
func dockRect(d config.Dock, area win.RECT) win.RECT {
	w := area.Width()
	h := area.Height()
	return win.RECT{
		Left:   area.Left + pct(w, d.Values[0]),
		Top:    area.Top + pct(h, d.Values[1]),
		Right:  area.Left + pct(w, d.Values[2]),
		Bottom: area.Top + pct(h, d.Values[3]),
	}
}

// pct converts a 0-100 percentage of span into pixels, rounding to nearest so
// that adjacent zones meet exactly instead of leaving a one-pixel seam.
func pct(span int32, percent float64) int32 {
	return int32(math.Round(float64(span) * percent / 100))
}

// clampSpan keeps a hit region from covering more than half a monitor, which
// would let it swallow the opposite edge on very small displays.
func clampSpan(want, available int32) int32 {
	if available <= 0 {
		return 0
	}
	max := available / 2
	if max < 1 {
		max = 1
	}
	if want > max {
		return max
	}
	if want < 1 {
		return 1
	}
	return want
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// clampInt32 keeps v within [lo, hi], so a monitor with an unusually large
// appbar cannot produce an inverted trigger rectangle.
func clampInt32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
