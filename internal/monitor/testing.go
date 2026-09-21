package monitor

// NewSetForTest builds a Set from a fixed list, so geometry can be tested
// without depending on the machine's displays.
func NewSetForTest(mons []Monitor) *Set {
	return &Set{monitors: mons}
}
