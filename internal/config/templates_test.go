package config

import "testing"

// TestTemplatesAreValid is the test that matters for a starter layout: it is
// the first thing a new user sees, and a rule that does not validate is
// silently skipped, so a broken template would present as "the program does
// nothing".
func TestTemplatesAreValid(t *testing.T) {
	for _, tpl := range Templates() {
		if tpl.Name == "" {
			t.Error("a template has no name")
		}
		if tpl.Note == "" {
			t.Errorf("template %q has no description", tpl.Name)
		}
		if len(tpl.Rules) == 0 {
			t.Errorf("template %q has no rules, which is the empty first run it exists to avoid", tpl.Name)
		}
		for i, r := range tpl.Rules {
			if err := r.Validate(); err != nil {
				t.Errorf("template %q rule %d does not validate: %v", tpl.Name, i, err)
			}
			if r.Trigger.Monitor != 0 || r.Dock.Monitor != 0 {
				t.Errorf("template %q rule %d names a monitor other than 0; a starter layout must work on a single screen",
					tpl.Name, i)
			}
		}
	}
}

func TestTemplateNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range Templates() {
		if seen[tpl.Name] {
			t.Errorf("duplicate template name %q", tpl.Name)
		}
		seen[tpl.Name] = true
	}
}

// TestTemplateProfileCopiesRules guards the case of adding the same template
// twice and editing one copy.
func TestTemplateProfileCopiesRules(t *testing.T) {
	tpl := Templates()[0]
	a, b := tpl.Profile(), tpl.Profile()
	a.Rules[0].Dock.Values[0] = 99
	if b.Rules[0].Dock.Values[0] == 99 {
		t.Error("two profiles made from one template share a values array")
	}
	if tpl.Rules[0].Dock.Values[0] == 99 {
		t.Error("editing a profile reached back into the template itself")
	}
}

// TestDefaultShipsEveryTemplate is what makes the first run non-empty.
func TestDefaultShipsEveryTemplate(t *testing.T) {
	f := Default()
	if len(f.Profiles) != len(Templates()) {
		t.Fatalf("Default has %d profiles, want one per template (%d)", len(f.Profiles), len(Templates()))
	}
	if f.ActiveProfileOrNil() == nil {
		t.Fatal("Default selects no profile, so a first run would still do nothing")
	}
	if len(f.ActiveRules()) == 0 {
		t.Error("the profile selected on first run has no rules")
	}
	if !f.GlobalSettings.DockingEnabled {
		t.Error("docking is off on a first run")
	}
}
