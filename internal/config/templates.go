package config

// Starter layouts, to get something usable rather than an
// empty profile with nothing to drag a window at.

// Template is a named starter layout offered on first run and from the
// settings window.
type Template struct {
	Name  string
	Note  string
	Rules []Rule
}

// Templates returns the built-in starter layouts, simplest first.
func Templates() []Template {
	return []Template{
		{
			Name: "Halves",
			Note: "Left or right half of the screen, and the top edge to maximize.",
			Rules: []Rule{
				edgeRule(PosLeft, 0, 100, 0, 0, 50, 100),
				edgeRule(PosRight, 0, 100, 50, 0, 100, 100),
				edgeRule(PosTop, 0, 100, 0, 0, 100, 100),
			},
		},
		{
			Name: "Quarters",
			Note: "Each half-edge docks to the quarter beside it. The original WinDock default.",
			Rules: []Rule{
				edgeRule(PosLeft, 0, 50, 0, 0, 50, 50),
				edgeRule(PosRight, 0, 50, 50, 0, 100, 50),
				edgeRule(PosLeft, 50, 100, 0, 50, 50, 100),
				edgeRule(PosRight, 50, 100, 50, 50, 100, 100),
				edgeRule(PosTop, 0, 100, 0, 0, 100, 100),
			},
		},
		{
			Name: "Thirds",
			Note: "Three equal columns. The middle one is the centre of the top edge.",
			Rules: []Rule{
				edgeRule(PosLeft, 0, 100, 0, 0, thirdOne, 100),
				edgeRule(PosTop, 25, 75, thirdOne, 0, thirdTwo, 100),
				edgeRule(PosRight, 0, 100, thirdTwo, 0, 100, 100),
				cornerRule(PosTopLeft, 0, 0, 100, 100),
			},
		},
		{
			Name: "Ultrawide 20/60/20",
			Note: "A wide centre column to work in with a narrow reference column each side.",
			Rules: []Rule{
				edgeRule(PosLeft, 0, 100, 0, 0, 20, 100),
				edgeRule(PosTop, 25, 75, 20, 0, 80, 100),
				edgeRule(PosRight, 0, 100, 80, 0, 100, 100),
				// Half the centre column, for a pair of windows side by side in
				// the space that is actually worth splitting on an ultrawide.
				edgeRule(PosBottom, 20, 50, 20, 0, 50, 100),
				edgeRule(PosBottom, 50, 80, 50, 0, 80, 100),
				cornerRule(PosTopLeft, 0, 0, 100, 100),
			},
		},
	}
}

// The thirds are written out rather than computed so the file on disk holds the
// same numbers a person would have typed.
const (
	thirdOne = 33.33
	thirdTwo = 66.66
)

// Profile turns a template into a profile, copying the rules so two profiles
// from one template never share a slice.
func (t Template) Profile() Profile {
	rules := make([]Rule, len(t.Rules))
	for i, r := range t.Rules {
		rules[i] = Rule{
			Dock:    Dock{Monitor: r.Dock.Monitor, Values: append([]float64(nil), r.Dock.Values...)},
			Trigger: Trigger{Monitor: r.Trigger.Monitor, Pos: r.Trigger.Pos, Type: r.Trigger.Type, Values: append([]float64(nil), r.Trigger.Values...)},
		}
	}
	return Profile{Name: t.Name, Rules: rules}
}

func edgeRule(pos string, from, to float64, l, t, r, b float64) Rule {
	return Rule{
		Dock:    Dock{Monitor: 0, Values: []float64{l, t, r, b}},
		Trigger: Trigger{Monitor: 0, Pos: pos, Type: TypeEdge, Values: []float64{from, to}},
	}
}

func cornerRule(pos string, l, t, r, b float64) Rule {
	return Rule{
		Dock:    Dock{Monitor: 0, Values: []float64{l, t, r, b}},
		Trigger: Trigger{Monitor: 0, Pos: pos, Type: TypeCorner},
	}
}
