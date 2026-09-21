package ui

import (
	"fmt"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"

	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
)

func defaultRule() config.Rule {
	return config.Rule{
		Dock:    config.Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}},
		Trigger: config.Trigger{Monitor: 0, Pos: config.PosLeft, Type: config.TypeEdge, Values: []float64{0, 100}},
	}
}

// editRuleDialog edits one rule, returning it and whether the user accepted.
// Everything is in percentages, as the file format is.
func (a *App) editRuleDialog(owner walk.Form, in config.Rule, title string) (config.Rule, bool) {
	out := in
	// Work on copies so a cancelled edit leaves the original untouched.
	out.Dock.Values = append([]float64(nil), in.Dock.Values...)
	out.Trigger.Values = append([]float64(nil), in.Trigger.Values...)
	for len(out.Dock.Values) < 4 {
		out.Dock.Values = append(out.Dock.Values, 0)
	}

	var (
		dlg        *walk.Dialog
		acceptBtn  *walk.PushButton
		cancelBtn  *walk.PushButton
		typeCB     *walk.ComboBox
		posCB      *walk.ComboBox
		trigMon    *walk.NumberEdit
		dockMon    *walk.NumberEdit
		trigFrom   *walk.NumberEdit
		trigTo     *walk.NumberEdit
		trigLeft   *walk.NumberEdit
		trigTop    *walk.NumberEdit
		trigRight  *walk.NumberEdit
		trigBottom *walk.NumberEdit
		dockLeft   *walk.NumberEdit
		dockTop    *walk.NumberEdit
		dockRight  *walk.NumberEdit
		dockBottom *walk.NumberEdit
		edgeBox    *walk.Composite
		areaBox    *walk.Composite
		hint       *walk.Label
		preview    *walk.Label
		accepted   bool
	)

	types := []string{config.TypeEdge, config.TypeCorner, config.TypeArea}
	typeIndex := 0
	for i, t := range types {
		if t == in.Trigger.Type {
			typeIndex = i
		}
	}

	monitors := monitor.Enumerate()

	// syncVisibility shows only the fields the chosen trigger type uses; a
	// corner carries no values at all.
	syncVisibility := func() {
		kind := types[maxInt(typeCB.CurrentIndex(), 0)]
		edgeBox.SetVisible(kind == config.TypeEdge)
		areaBox.SetVisible(kind == config.TypeArea)

		positions := sortedPositions(kind)
		items := make([]string, len(positions))
		for i, p := range positions {
			items[i] = prettyPos(p)
		}
		current := posCB.CurrentIndex()
		posCB.SetModel(items)
		if current >= 0 && current < len(items) {
			posCB.SetCurrentIndex(current)
		} else {
			posCB.SetCurrentIndex(0)
		}
		posCB.SetEnabled(kind != config.TypeArea)

		switch kind {
		case config.TypeEdge:
			hint.SetText("The pointer activates this rule when it reaches that edge, " +
				"between the two percentages along it.")
		case config.TypeCorner:
			hint.SetText("The pointer activates this rule in that corner. Corners take " +
				"priority over edges, whatever the rule order.")
		case config.TypeArea:
			hint.SetText("The pointer activates this rule inside that rectangle of the " +
				"screen. Areas have the lowest priority.")
		}
	}

	updatePreview := func() {
		l, t, r, b := dockLeft.Value(), dockTop.Value(), dockRight.Value(), dockBottom.Value()
		if r <= l || b <= t {
			preview.SetText("The dock is empty: right must exceed left, and bottom must exceed top.")
			return
		}
		m := monitors.ByIndex(int(dockMon.Value()))
		if m == nil {
			preview.SetText(fmt.Sprintf("Covers %g%% x %g%% of the work area. "+
				"Monitor %d is not attached, so this rule is currently inactive.",
				r-l, b-t, int(dockMon.Value())))
			return
		}
		area := m.Area(out.Dock.WorkArea())
		w := float64(area.Width()) * (r - l) / 100
		h := float64(area.Height()) * (b - t) / 100
		against := "work area"
		if !out.Dock.WorkArea() {
			against = "whole screen"
		}
		preview.SetText(fmt.Sprintf("Covers %g%% x %g%% of the %s - about %.0f x %.0f pixels today.",
			r-l, b-t, against, w, h))
	}

	pct := func(assign **walk.NumberEdit, value float64, onChange func()) d.NumberEdit {
		return d.NumberEdit{
			AssignTo:       assign,
			Value:          value,
			MinValue:       0,
			MaxValue:       100,
			Decimals:       0,
			Suffix:         " %",
			MaxSize:        d.Size{Width: 90},
			OnValueChanged: onChange,
		}
	}

	trigVals := in.Trigger.Values
	get := func(v []float64, i int) float64 {
		if i < len(v) {
			return v[i]
		}
		return 0
	}

	err := d.Dialog{
		AssignTo:      &dlg,
		Title:         title,
		Icon:          a.windowIcon(),
		MinSize:       d.Size{Width: 560, Height: 0},
		DefaultButton: &acceptBtn,
		CancelButton:  &cancelBtn,
		Layout:        d.VBox{},
		Children: []d.Widget{
			d.GroupBox{
				Title:  "Trigger - where the pointer has to go",
				Layout: d.VBox{},
				Children: []d.Widget{
					d.Composite{
						Layout: d.HBox{MarginsZero: true},
						Children: []d.Widget{
							d.Label{Text: "Type:"},
							d.ComboBox{
								AssignTo:              &typeCB,
								Model:                 []string{"Edge", "Corner", "Area"},
								CurrentIndex:          typeIndex,
								MaxSize:               d.Size{Width: 110},
								OnCurrentIndexChanged: func() { syncVisibility() },
							},
							d.Label{Text: "Position:"},
							d.ComboBox{AssignTo: &posCB, MaxSize: d.Size{Width: 130}},
							d.Label{Text: "Monitor:"},
							d.NumberEdit{AssignTo: &trigMon, Value: float64(in.Trigger.Monitor),
								MinValue: 0, MaxValue: 15, Decimals: 0, MaxSize: d.Size{Width: 60}},
							d.HSpacer{},
						},
					},
					d.Composite{
						AssignTo: &edgeBox,
						Layout:   d.HBox{MarginsZero: true},
						Children: []d.Widget{
							d.Label{Text: "Along the edge, from"},
							pct(&trigFrom, get(trigVals, 0), nil),
							d.Label{Text: "to"},
							pct(&trigTo, orDefault(get(trigVals, 1), 100), nil),
							d.HSpacer{},
						},
					},
					d.Composite{
						AssignTo: &areaBox,
						Layout:   d.HBox{MarginsZero: true},
						Children: []d.Widget{
							d.Label{Text: "Left"}, pct(&trigLeft, get(trigVals, 0), nil),
							d.Label{Text: "Top"}, pct(&trigTop, get(trigVals, 1), nil),
							d.Label{Text: "Right"}, pct(&trigRight, orDefault(get(trigVals, 2), 50), nil),
							d.Label{Text: "Bottom"}, pct(&trigBottom, orDefault(get(trigVals, 3), 50), nil),
							d.HSpacer{},
						},
					},
					d.Label{AssignTo: &hint},
				},
			},
			d.GroupBox{
				Title:  "Dock - where the window lands, as a percentage of the work area",
				Layout: d.VBox{},
				Children: []d.Widget{
					d.Composite{
						Layout: d.HBox{MarginsZero: true},
						Children: []d.Widget{
							d.Label{Text: "Left"}, pct(&dockLeft, out.Dock.Values[0], func() { updatePreview() }),
							d.Label{Text: "Top"}, pct(&dockTop, out.Dock.Values[1], func() { updatePreview() }),
							d.Label{Text: "Right"}, pct(&dockRight, out.Dock.Values[2], func() { updatePreview() }),
							d.Label{Text: "Bottom"}, pct(&dockBottom, out.Dock.Values[3], func() { updatePreview() }),
							d.Label{Text: "Monitor:"},
							d.NumberEdit{AssignTo: &dockMon, Value: float64(in.Dock.Monitor),
								MinValue: 0, MaxValue: 15, Decimals: 0, MaxSize: d.Size{Width: 60},
								OnValueChanged: func() { updatePreview() }},
							d.HSpacer{},
						},
					},
					d.Label{AssignTo: &preview},
				},
			},
			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.HSpacer{},
					d.PushButton{AssignTo: &acceptBtn, Text: "OK", OnClicked: func() {
						accepted = true
						dlg.Accept()
					}},
					d.PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		a.errorBox("Could not open the rule editor", err)
		return in, false
	}

	// Select the incoming position once the model exists.
	syncVisibility()
	positions := sortedPositions(types[typeIndex])
	for i, p := range positions {
		if p == in.Trigger.Pos {
			posCB.SetCurrentIndex(i)
		}
	}
	updatePreview()

	dlg.Run()
	if !accepted {
		return in, false
	}

	kind := types[maxInt(typeCB.CurrentIndex(), 0)]
	out.Trigger.Type = kind
	out.Trigger.Monitor = int(trigMon.Value())
	out.Dock.Monitor = int(dockMon.Value())
	out.Dock.Values = []float64{
		dockLeft.Value(), dockTop.Value(), dockRight.Value(), dockBottom.Value(),
	}

	posList := sortedPositions(kind)
	pi := maxInt(posCB.CurrentIndex(), 0)
	if pi >= len(posList) {
		pi = 0
	}

	switch kind {
	case config.TypeEdge:
		out.Trigger.Pos = posList[pi]
		out.Trigger.Values = []float64{trigFrom.Value(), trigTo.Value()}
	case config.TypeCorner:
		out.Trigger.Pos = posList[pi]
		// A corner carries no values; leaving stale ones behind would be
		// written back out to the file and confuse the original WinDock.
		out.Trigger.Values = nil
	case config.TypeArea:
		out.Trigger.Pos = ""
		out.Trigger.Values = []float64{
			trigLeft.Value(), trigTop.Value(), trigRight.Value(), trigBottom.Value(),
		}
	}

	if err := out.Validate(); err != nil {
		walk.MsgBox(owner, "That rule is not usable",
			err.Error()+"\n\nThe rule has not been saved.", walk.MsgBoxIconWarning)
		return in, false
	}
	return out, true
}

func orDefault(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
