// The monitor package tracks the current display topology.
//
// The original enumerated once at startup and never again, so after an RDP
// disconnect rebuilt the display set, every rule resolved against dead geometry.
// Two things prevent that here: a Set is rebuilt from scratch on every display,
// session and DPI change, and indices are assigned deterministically (primary
// first, then left to right, top to bottom) rather than taken from
// EnumDisplayMonitors order, which is not stable across a rebuild.
package monitor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BjarkeM/windock-go/internal/win"
)

// Monitor is one display in the current topology.
type Monitor struct {
	// Index is the stable position of this monitor in the sorted set. Profile
	// rules refer to monitors by this number.
	Index int

	Handle   win.HMONITOR
	Bounds   win.RECT // full monitor rectangle, in virtual-desktop coordinates
	WorkArea win.RECT // Bounds minus the taskbar and other appbars
	Device   string   // e.g. \\.\DISPLAY1
	Primary  bool
}

// Area returns the rectangle rules resolve against.
func (m Monitor) Area(useWorkArea bool) win.RECT {
	if useWorkArea {
		return m.WorkArea
	}
	return m.Bounds
}

func (m Monitor) String() string {
	tag := ""
	if m.Primary {
		tag = " primary"
	}
	return fmt.Sprintf("[%d] %s %dx%d at (%d,%d)%s",
		m.Index, m.Device,
		m.Bounds.Width(), m.Bounds.Height(),
		m.Bounds.Left, m.Bounds.Top, tag)
}

// Set is an immutable snapshot of the display topology.
type Set struct {
	monitors []Monitor
}

// Enumerate builds a fresh snapshot from the OS. It is cheap enough to call on
// every topology change and must never be skipped in favour of a cached value.
func Enumerate() *Set {
	handles := win.EnumDisplayMonitors()
	mons := make([]Monitor, 0, len(handles))
	for _, h := range handles {
		mi, ok := win.GetMonitorInfo(h)
		if !ok {
			continue
		}
		if mi.RcMonitor.IsEmpty() {
			// A disconnected or not-yet-initialised display. Including it would
			// shift every later index.
			continue
		}
		mons = append(mons, Monitor{
			Handle:   h,
			Bounds:   mi.RcMonitor,
			WorkArea: mi.RcWork,
			Device:   mi.Device(),
			Primary:  mi.DwFlags&win.MONITORINFOF_PRIMARY != 0,
		})
	}

	// Primary first, then by position. EnumDisplayMonitors order is not
	// guaranteed and does change when the session display set is rebuilt.
	sort.SliceStable(mons, func(i, j int) bool {
		a, b := mons[i], mons[j]
		if a.Primary != b.Primary {
			return a.Primary
		}
		if a.Bounds.Left != b.Bounds.Left {
			return a.Bounds.Left < b.Bounds.Left
		}
		if a.Bounds.Top != b.Bounds.Top {
			return a.Bounds.Top < b.Bounds.Top
		}
		return a.Device < b.Device
	})

	for i := range mons {
		mons[i].Index = i
	}
	return &Set{monitors: mons}
}

// All returns the monitors in stable index order.
func (s *Set) All() []Monitor {
	if s == nil {
		return nil
	}
	return s.monitors
}

// Len returns the number of active monitors.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.monitors)
}

// VirtualBounds is the smallest rectangle covering every attached monitor, and
// the coordinate space every rectangle here is expressed in.
func (s *Set) VirtualBounds() win.RECT {
	if s == nil || len(s.monitors) == 0 {
		return win.RECT{}
	}
	out := s.monitors[0].Bounds
	for _, m := range s.monitors[1:] {
		if m.Bounds.Left < out.Left {
			out.Left = m.Bounds.Left
		}
		if m.Bounds.Top < out.Top {
			out.Top = m.Bounds.Top
		}
		if m.Bounds.Right > out.Right {
			out.Right = m.Bounds.Right
		}
		if m.Bounds.Bottom > out.Bottom {
			out.Bottom = m.Bounds.Bottom
		}
	}
	return out
}

// ByIndex returns the monitor a rule refers to, or nil if it is not attached.
// Nil rather than a fallback: such a rule should do nothing, not snap a window
// onto a different screen.
func (s *Set) ByIndex(i int) *Monitor {
	if s == nil || i < 0 || i >= len(s.monitors) {
		return nil
	}
	return &s.monitors[i]
}

// FromPoint returns the monitor containing p, or nil.
func (s *Set) FromPoint(p win.POINT) *Monitor {
	if s == nil {
		return nil
	}
	for i := range s.monitors {
		if s.monitors[i].Bounds.Contains(p) {
			return &s.monitors[i]
		}
	}
	return nil
}

// FromHandle returns the monitor with the given handle, or nil. Handles are not
// stable across a topology change, so this is only valid against a current Set.
func (s *Set) FromHandle(h win.HMONITOR) *Monitor {
	if s == nil {
		return nil
	}
	for i := range s.monitors {
		if s.monitors[i].Handle == h {
			return &s.monitors[i]
		}
	}
	return nil
}

// Fingerprint is a compact description of the topology, compared to tell
// whether a display notification changed anything.
func (s *Set) Fingerprint() string {
	if s == nil {
		return "<none>"
	}
	var b strings.Builder
	for _, m := range s.monitors {
		fmt.Fprintf(&b, "%s:%d,%d,%d,%d;", m.Device,
			m.Bounds.Left, m.Bounds.Top, m.Bounds.Right, m.Bounds.Bottom)
	}
	if b.Len() == 0 {
		return "<empty>"
	}
	return b.String()
}

// Describe renders the topology for logs.
func (s *Set) Describe() string {
	if s.Len() == 0 {
		return "no monitors"
	}
	parts := make([]string, 0, s.Len())
	for _, m := range s.All() {
		parts = append(parts, m.String())
	}
	return strings.Join(parts, ", ")
}
