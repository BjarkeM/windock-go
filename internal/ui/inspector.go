package ui

import (
	"fmt"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"

	"github.com/BjarkeM/windock-go/internal/config"
)

// The inspector: the numbers for the selected rule, beside the canvas. The
// canvas is for finding out what you want; this is for saying it. Typing in a
// field redraws the map as you type.

// inspectorWidgets are the fields bound to the selected rule.
type inspectorWidgets struct {
	box *walk.GroupBox

	trigScreen *walk.ComboBox
	dockScreen *walk.ComboBox

	typeCB *walk.ComboBox
	posCB  *walk.ComboBox

	edgeRow *walk.Composite
	from    *walk.NumberEdit
	to      *walk.NumberEdit

	areaRow *walk.Composite
	areaL   *walk.NumberEdit
	areaT   *walk.NumberEdit
	areaR   *walk.NumberEdit
	areaB   *walk.NumberEdit

	dockL *walk.NumberEdit
	dockT *walk.NumberEdit
	dockR *walk.NumberEdit
	dockB *walk.NumberEdit

	cbWorkArea *walk.CheckBox

	hint *walk.Label

	// screens is the monitor index behind each entry of both dropdowns.
	screens []int
}

// triggerTypes is the order the type combo offers, and the order its index maps
// to.
var triggerTypes = []string{config.TypeEdge, config.TypeCorner, config.TypeArea}

// inspectorDeclarative builds the field panel.
func (s *SettingsWindow) inspectorDeclarative() d.Widget {
	w := &s.ins
	pct := func(assign **walk.NumberEdit) d.Widget {
		return d.NumberEdit{
			AssignTo:       assign,
			Decimals:       1,
			MinValue:       0,
			MaxValue:       100,
			Increment:      5,
			MinSize:        d.Size{Width: 62},
			MaxSize:        d.Size{Width: 68},
			OnValueChanged: s.onInspectorChanged,
		}
	}

	return d.GroupBox{
		AssignTo: &w.box,
		Title:    "Selected zone",
		MinSize:  d.Size{Width: 330},
		Layout:   d.VBox{},
		Children: []d.Widget{
			// Two labelled rows. A zone has two ends - where you point and
			// where the window goes - and one line naming both was unreadable.
			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Point at", MinSize: d.Size{Width: 58}},
					d.ComboBox{
						AssignTo:              &w.trigScreen,
						MinSize:               d.Size{Width: 186},
						OnCurrentIndexChanged: s.onTriggerScreenChanged,
						ToolTipText:           "The screen whose edge, corner or area the pointer has to reach.",
					},
					d.HSpacer{},
				},
			},
			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Window to", MinSize: d.Size{Width: 58}},
					d.ComboBox{
						AssignTo:              &w.dockScreen,
						MinSize:               d.Size{Width: 186},
						OnCurrentIndexChanged: s.onInspectorChanged,
						ToolTipText:           "The screen the window is moved to. Set it to a different screen to throw windows across.",
					},
					d.HSpacer{},
				},
			},

			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Trigger"},
					d.ComboBox{
						AssignTo:              &w.typeCB,
						Model:                 []string{"Edge", "Corner", "Area"},
						MinSize:               d.Size{Width: 84},
						MaxSize:               d.Size{Width: 90},
						OnCurrentIndexChanged: s.onTriggerTypeChanged,
					},
					d.ComboBox{
						AssignTo:              &w.posCB,
						MinSize:               d.Size{Width: 108},
						MaxSize:               d.Size{Width: 120},
						OnCurrentIndexChanged: s.onInspectorChanged,
					},
					d.HSpacer{},
				},
			},

			// The edge span. This is the row the whole exercise is about: pick
			// a line, say where along it the zone starts and stops.
			d.Composite{
				AssignTo: &w.edgeRow,
				Layout:   d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Runs"},
					pct(&w.from),
					d.Label{Text: "to"},
					pct(&w.to),
					d.Label{Text: "%"},
					d.HSpacer{},
				},
			},

			d.Composite{
				AssignTo: &w.areaRow,
				Layout:   d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Area"},
					pct(&w.areaL),
					pct(&w.areaT),
					d.Label{Text: "to"},
					pct(&w.areaR),
					pct(&w.areaB),
					d.HSpacer{},
				},
			},

			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.Label{Text: "Window"},
					pct(&w.dockL),
					pct(&w.dockT),
					d.Label{Text: "to"},
					pct(&w.dockR),
					pct(&w.dockB),
					d.HSpacer{},
				},
			},

			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.CheckBox{
						AssignTo:         &w.cbWorkArea,
						Text:             "Keep clear of the taskbar",
						OnCheckedChanged: s.onInspectorChanged,
					},
					d.HSpacer{},
				},
			},

			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.PushButton{Text: "Maximize", MaxSize: d.Size{Width: 78}, OnClicked: s.dockMaximize},
					d.PushButton{Text: "Left half", MaxSize: d.Size{Width: 78}, OnClicked: func() { s.setDock(0, 0, 50, 100) }},
					d.PushButton{Text: "Right half", MaxSize: d.Size{Width: 78}, OnClicked: func() { s.setDock(50, 0, 100, 100) }},
					d.PushButton{Text: "Match trigger", MaxSize: d.Size{Width: 92}, OnClicked: s.dockMatchTrigger},
					d.HSpacer{},
				},
			},

			d.Label{AssignTo: &w.hint, Text: ""},
			d.VSpacer{},
		},
	}
}

// --- screens --------------------------------------------------------------

// fillScreens populates both screen dropdowns. A profile can name a screen that
// is not plugged in, so such a rule gets an entry of its own rather than being
// rewritten to screen 0 on selection.
func (s *SettingsWindow) fillScreens(trigger, dock int) {
	w := &s.ins
	w.screens = w.screens[:0]

	var items []string
	for _, m := range s.mons.All() {
		w.screens = append(w.screens, m.Index)
		label := fmt.Sprintf("Screen %d - %dx%d", m.Index, m.Bounds.Width(), m.Bounds.Height())
		if m.Primary {
			label += " (main)"
		}
		items = append(items, label)
	}
	for _, extra := range []int{trigger, dock} {
		if indexOfInt(w.screens, extra) < 0 {
			w.screens = append(w.screens, extra)
			items = append(items, fmt.Sprintf("Screen %d - not attached", extra))
		}
	}

	w.trigScreen.SetModel(items)
	w.dockScreen.SetModel(items)
	w.trigScreen.SetCurrentIndex(maxInt(indexOfInt(w.screens, trigger), 0))
	w.dockScreen.SetCurrentIndex(maxInt(indexOfInt(w.screens, dock), 0))
}

// screenAt maps a dropdown index back to a monitor index.
func (w *inspectorWidgets) screenAt(i int) int {
	if i < 0 || i >= len(w.screens) {
		return 0
	}
	return w.screens[i]
}

func indexOfInt(xs []int, want int) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

// --- loading and storing --------------------------------------------------

// editingRule returns the rule the inspector is bound to, or nil.
func (s *SettingsWindow) editingRule() *config.Rule {
	p := s.editingProfile()
	i := s.selectedRuleIndex()
	if p == nil || i < 0 || i >= len(p.Rules) {
		return nil
	}
	return &p.Rules[i]
}

// loadInspector fills the fields from the selected rule, or greys them out.
func (s *SettingsWindow) loadInspector() {
	w := &s.ins
	if w.box == nil {
		return
	}
	r := s.editingRule()
	w.box.SetEnabled(r != nil)
	if r == nil {
		w.hint.SetText("No zone selected. Drag on the map above, or press Add.")
		w.edgeRow.SetVisible(true)
		w.areaRow.SetVisible(false)
		return
	}

	s.loading = true
	defer func() { s.loading = false }()

	ti := 0
	for i, t := range triggerTypes {
		if t == r.Trigger.Type {
			ti = i
		}
	}
	w.typeCB.SetCurrentIndex(ti)
	s.fillPositions(triggerTypes[ti], r.Trigger.Pos)
	s.fillScreens(r.Trigger.Monitor, r.Dock.Monitor)

	switch triggerTypes[ti] {
	case config.TypeEdge:
		from, to := 0.0, 100.0
		if len(r.Trigger.Values) == 2 {
			from, to = r.Trigger.Values[0], r.Trigger.Values[1]
		}
		w.from.SetValue(from)
		w.to.SetValue(to)
	case config.TypeArea:
		v := padValues(r.Trigger.Values, 4)
		w.areaL.SetValue(v[0])
		w.areaT.SetValue(v[1])
		w.areaR.SetValue(v[2])
		w.areaB.SetValue(v[3])
	}

	dv := padValues(r.Dock.Values, 4)
	w.dockL.SetValue(dv[0])
	w.dockT.SetValue(dv[1])
	w.dockR.SetValue(dv[2])
	w.dockB.SetValue(dv[3])

	w.cbWorkArea.SetChecked(r.Dock.WorkArea())

	s.syncInspectorRows(triggerTypes[ti])
	w.hint.SetText(inspectorHint(*r))
}

// storeInspector writes the fields back into the selected rule.
func (s *SettingsWindow) storeInspector() {
	w := &s.ins
	r := s.editingRule()
	if r == nil {
		return
	}

	kind := triggerTypes[maxInt(w.typeCB.CurrentIndex(), 0)]
	r.Trigger.Type = kind
	r.Trigger.Monitor = w.screenAt(w.trigScreen.CurrentIndex())
	r.Dock.Monitor = w.screenAt(w.dockScreen.CurrentIndex())

	positions := sortedPositions(kind)
	switch kind {
	case config.TypeEdge:
		r.Trigger.Pos = positions[clampIndex(w.posCB.CurrentIndex(), len(positions))]
		from, to := orderedPct(w.from.Value(), w.to.Value())
		r.Trigger.Values = []float64{from, to}
	case config.TypeCorner:
		r.Trigger.Pos = positions[clampIndex(w.posCB.CurrentIndex(), len(positions))]
		// A corner carries no values in the file format; leaving stale ones
		// behind would write a file the original WinDock reads differently.
		r.Trigger.Values = nil
	case config.TypeArea:
		r.Trigger.Pos = ""
		l, rt := orderedPct(w.areaL.Value(), w.areaR.Value())
		t, b := orderedPct(w.areaT.Value(), w.areaB.Value())
		r.Trigger.Values = []float64{l, t, rt, b}
	}

	l, rt := orderedPct(w.dockL.Value(), w.dockR.Value())
	t, b := orderedPct(w.dockT.Value(), w.dockB.Value())
	r.Dock.Values = []float64{l, t, rt, b}

	// Left nil when it matches the default, so a profile only carries the key
	// where a zone actually differs.
	if w.cbWorkArea.Checked() {
		r.Dock.UseWorkArea = nil
	} else {
		r.Dock.UseWorkArea = boolPtr(false)
	}

	w.hint.SetText(inspectorHint(*r))
}

// onTriggerScreenChanged moves the dock screen with the trigger screen while
// the two agree, since nearly every zone docks where it fired. Once they have
// been set apart on purpose it stops interfering.
func (s *SettingsWindow) onTriggerScreenChanged() {
	if s.loading {
		return
	}
	w := &s.ins
	if r := s.editingRule(); r != nil && r.Trigger.Monitor == r.Dock.Monitor {
		s.loading = true
		w.dockScreen.SetCurrentIndex(w.trigScreen.CurrentIndex())
		s.loading = false
	}
	s.onInspectorChanged()
}

// onInspectorChanged is attached to every field.
func (s *SettingsWindow) onInspectorChanged() {
	if s.loading {
		return
	}
	s.storeInspector()
	s.setDirty()
	s.refreshZones()
}

// onTriggerTypeChanged swaps the position list and the visible rows, then
// stores like any other edit.
func (s *SettingsWindow) onTriggerTypeChanged() {
	if s.loading {
		return
	}
	kind := triggerTypes[maxInt(s.ins.typeCB.CurrentIndex(), 0)]

	s.loading = true
	s.fillPositions(kind, "")
	s.syncInspectorRows(kind)
	if kind == config.TypeEdge && s.ins.to.Value() <= s.ins.from.Value() {
		// Coming from a corner, the span fields are whatever was last typed.
		s.ins.from.SetValue(0)
		s.ins.to.SetValue(100)
	}
	s.loading = false

	s.storeInspector()
	s.setDirty()
	s.refreshZones()
}

// fillPositions repopulates the position combo for a trigger type, selecting
// want if it is one of them.
func (s *SettingsWindow) fillPositions(kind, want string) {
	positions := sortedPositions(kind)
	items := make([]string, len(positions))
	sel := 0
	for i, p := range positions {
		items[i] = prettyPos(p)
		if p == want {
			sel = i
		}
	}
	s.ins.posCB.SetModel(items)
	s.ins.posCB.SetCurrentIndex(sel)
	// An area has no position at all, so the combo would be a field with no
	// meaning rather than one with a default.
	s.ins.posCB.SetEnabled(kind != config.TypeArea)
}

func (s *SettingsWindow) syncInspectorRows(kind string) {
	s.ins.edgeRow.SetVisible(kind == config.TypeEdge)
	s.ins.areaRow.SetVisible(kind == config.TypeArea)
}

// --- dock shortcuts -------------------------------------------------------

func (s *SettingsWindow) setDock(l, t, r, b float64) {
	if s.editingRule() == nil {
		return
	}
	s.loading = true
	s.ins.dockL.SetValue(l)
	s.ins.dockT.SetValue(t)
	s.ins.dockR.SetValue(r)
	s.ins.dockB.SetValue(b)
	s.loading = false

	s.storeInspector()
	s.setDirty()
	s.refreshZones()
}

func (s *SettingsWindow) dockMaximize() { s.setDock(0, 0, 100, 100) }

// dockMatchTrigger makes the window fill exactly the region that triggered it.
func (s *SettingsWindow) dockMatchTrigger() {
	r := s.editingRule()
	if r == nil {
		return
	}
	switch r.Trigger.Type {
	case config.TypeArea:
		v := padValues(r.Trigger.Values, 4)
		s.setDock(v[0], v[1], v[2], v[3])
	case config.TypeEdge:
		from, to := 0.0, 100.0
		if len(r.Trigger.Values) == 2 {
			from, to = r.Trigger.Values[0], r.Trigger.Values[1]
		}
		switch r.Trigger.Pos {
		case config.PosLeft:
			s.setDock(0, from, 50, to)
		case config.PosRight:
			s.setDock(50, from, 100, to)
		default:
			s.setDock(from, 0, to, 100)
		}
	case config.TypeCorner:
		s.setDock(0, 0, 50, 50)
	}
}

// --- helpers --------------------------------------------------------------

// inspectorHint says in words what the numbers add up to. An invalid rule is
// dropped silently at resolve time, so it is flagged here instead.
func inspectorHint(r config.Rule) string {
	if err := r.Validate(); err != nil {
		return "This zone will be ignored: " + err.Error()
	}
	if r.Trigger.Monitor != r.Dock.Monitor {
		// The unusual case, spelled out: the two dropdowns disagree on purpose
		// and the window will leave the screen the pointer is on.
		return fmt.Sprintf("%s on screen %d  →  %s on screen %d",
			describeTrigger(r.Trigger), r.Trigger.Monitor,
			describeDock(r.Dock), r.Dock.Monitor)
	}
	return fmt.Sprintf("%s  →  %s", describeTrigger(r.Trigger), describeDock(r.Dock))
}

// padValues returns a slice of exactly n values, so a hand-edited profile with
// a short array cannot panic the editor.
func padValues(v []float64, n int) []float64 {
	out := make([]float64, n)
	copy(out, v)
	return out
}

func clampIndex(i, n int) int {
	if i < 0 || n == 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}
