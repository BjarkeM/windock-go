package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const oneEdgeRule = `{
  "dock":    {"monitor": 0, "values": [0, 0, 50, 100]},
  "trigger": {"monitor": 0, "pos": "left", "type": "edge", "values": [0, 100]}
}`

// TestLoadExportedProfile covers pointing --config at a single exported profile
// such as 21-9-profile.json. That parsed into an empty, docking-disabled
// configuration that started up and silently did nothing.
func TestLoadExportedProfile(t *testing.T) {
	path := write(t, "21-9-profile.json", `{"name":"21:9 Profile","rules":[`+oneEdgeRule+`]}`)

	f, err := Load(path)
	if err != nil {
		t.Fatalf("loading an exported profile: %v", err)
	}
	if !f.IsExportedProfile() {
		t.Error("the file should be recognised as an exported profile")
	}
	if len(f.Profiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(f.Profiles))
	}
	if f.Profiles[0].Name != "21:9 Profile" {
		t.Errorf("profile name = %q", f.Profiles[0].Name)
	}
	if len(f.ActiveRules()) != 1 {
		t.Errorf("got %d active rules, want 1", len(f.ActiveRules()))
	}
	// The whole point: it must not come up disabled.
	if !f.GlobalSettings.DockingEnabled {
		t.Error("docking must default to enabled for an exported profile")
	}
}

func TestExportedProfileWithoutNameFallsBackToFilename(t *testing.T) {
	path := write(t, "my-zones.json", `{"rules":[`+oneEdgeRule+`]}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Profiles[0].Name; got != "my-zones" {
		t.Errorf("profile name = %q, want %q", got, "my-zones")
	}
}

// TestSaveRefusesToClobberExportedProfile makes sure a command like
// "windock -config 21-9-profile.json enable" cannot overwrite the user's
// exported profile with a full configuration document.
func TestSaveRefusesToClobberExportedProfile(t *testing.T) {
	original := `{"name":"21:9 Profile","rules":[` + oneEdgeRule + `]}`
	path := write(t, "21-9-profile.json", original)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, f); err == nil {
		t.Fatal("saving over an exported profile should fail")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Error("the exported profile was modified despite the refusal")
	}
}

// TestLoadConfigurationFile also carries tray_popup_enabled, a key the original
// WinDock wrote and this port no longer has: old files must still load.
func TestLoadConfigurationFile(t *testing.T) {
	path := write(t, "profile.json", `{
      "global_settings": {"active_profile": 1, "docking_enabled": false,
                          "maximize_full_window": false, "tray_popup_enabled": true},
      "profiles": [
        {"name": "A", "rules": []},
        {"name": "B", "rules": [`+oneEdgeRule+`]}
      ]}`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.IsExportedProfile() {
		t.Error("a full configuration must not be flagged as an exported profile")
	}
	if len(f.Profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(f.Profiles))
	}
	// An explicit false must be honoured, not overridden by our default.
	if f.GlobalSettings.DockingEnabled {
		t.Error("docking_enabled: false must be preserved")
	}
	if p := f.ActiveProfileOrNil(); p == nil || p.Name != "B" {
		t.Errorf("active profile = %v, want B", p)
	}
}

// TestConfigWithoutGlobalSettingsGetsDefaults covers a hand-written file that
// omits the block entirely; it must not come up silently disabled.
func TestConfigWithoutGlobalSettingsGetsDefaults(t *testing.T) {
	path := write(t, "profile.json", `{"profiles":[{"name":"A","rules":[`+oneEdgeRule+`]}]}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !f.GlobalSettings.DockingEnabled {
		t.Error("a config with no global_settings must default to docking enabled")
	}
}

func TestLoadRejectsFileWithNeitherShape(t *testing.T) {
	path := write(t, "junk.json", `{"something_else": 1}`)
	if _, err := Load(path); err == nil {
		t.Fatal("a file with neither profiles nor rules should be rejected")
	}
}

func TestLoadReportsMissingFileAsNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v, want os.ErrNotExist so LoadOrInit can fall back", err)
	}
}

// TestLoadOrInitFallsBackToDefaults covers running any subcommand before a
// configuration exists. Every subcommand used to call Load directly, so
// "windock diag" failed outright on a fresh machine.
func TestLoadOrInitFallsBackToDefaults(t *testing.T) {
	// Point LOCALAPPDATA somewhere empty so the legacy import cannot succeed.
	t.Setenv("LOCALAPPDATA", t.TempDir())

	path := filepath.Join(t.TempDir(), "WinDock-Go", "profile.json")
	f, source, err := LoadOrInit(path)
	if err != nil {
		t.Fatalf("LoadOrInit on a fresh machine: %v", err)
	}
	if source != "defaults" {
		t.Errorf("source = %q, want %q", source, "defaults")
	}
	if len(f.Profiles) == 0 {
		t.Error("the default configuration must contain at least one profile")
	}
	if !f.GlobalSettings.DockingEnabled {
		t.Error("the default configuration must have docking enabled")
	}
	if len(f.ActiveRules()) == 0 {
		t.Error("the default profile must contain rules")
	}
}

func TestLoadOrInitImportsLegacyConfig(t *testing.T) {
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)

	legacy := filepath.Join(local, "WinDock", "profile.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{
        "global_settings": {"active_profile": 0, "docking_enabled": true},
        "profiles": [{"name":"Legacy","rules":[`+oneEdgeRule+`]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	f, source, err := LoadOrInit(filepath.Join(local, "WinDock-Go", "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if source == "defaults" {
		t.Fatal("an existing legacy configuration should have been imported")
	}
	if len(f.Profiles) != 1 || f.Profiles[0].Name != "Legacy" {
		t.Errorf("imported profiles = %v, want one named Legacy", f.Profiles)
	}
}

// TestSaveRoundTripsThroughOriginalSchema checks a file we write loads back
// identically, so profiles stay compatible with the original WinDock.
func TestSaveRoundTripsThroughOriginalSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "profile.json")
	want := Default()
	want.GlobalSettings.ActiveProfile = 0

	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Profiles) != len(want.Profiles) {
		t.Fatalf("got %d profiles, want %d", len(got.Profiles), len(want.Profiles))
	}
	if got.GlobalSettings.DockingEnabled != want.GlobalSettings.DockingEnabled {
		t.Error("docking_enabled did not survive the round trip")
	}
	if len(got.ActiveRules()) != len(want.ActiveRules()) {
		t.Error("rules did not survive the round trip")
	}
	for i, r := range got.ActiveRules() {
		if err := r.Validate(); err != nil {
			t.Errorf("rule %d invalid after round trip: %v", i, err)
		}
	}
}

func TestLoadProfileReadsExportFormat(t *testing.T) {
	path := write(t, "exported.json", `{"name":"Exported","rules":[`+oneEdgeRule+`]}`)
	p, err := LoadProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Exported" || len(p.Rules) != 1 {
		t.Errorf("got %+v", p)
	}
}

// TestCloneIsDeep matters because the settings window edits a copy and throws
// it away on Cancel. A shallow copy would let a cancelled edit change the live
// configuration through a shared backing array.
func TestCloneIsDeep(t *testing.T) {
	// Built here rather than from Default() so the test keeps checking copying
	// rather than whatever the starter layouts happen to contain.
	orig := &File{
		GlobalSettings: defaultGlobal(),
		Profiles: []Profile{
			{Name: "First"},
			{Name: "Second", Rules: []Rule{{
				Dock:    Dock{Monitor: 0, Values: []float64{0, 0, 50, 50}},
				Trigger: Trigger{Monitor: 0, Pos: PosLeft, Type: TypeEdge, Values: []float64{0, 100}},
			}}},
		},
	}
	show := true
	orig.GlobalSettings.ShowPreview = &show

	c := orig.Clone()

	c.GlobalSettings.DockingEnabled = !orig.GlobalSettings.DockingEnabled
	c.GlobalSettings.ActiveProfile = 1
	*c.GlobalSettings.ShowPreview = false
	c.Profiles[0].Name = "Renamed"
	c.Profiles[1].Rules[0].Dock.Values[0] = 99
	c.Profiles[1].Rules[0].Trigger.Values[1] = 7
	c.Profiles = append(c.Profiles, Profile{Name: "Third"})

	if orig.GlobalSettings.DockingEnabled == c.GlobalSettings.DockingEnabled {
		t.Error("docking_enabled was shared between the copy and the original")
	}
	if orig.GlobalSettings.ActiveProfile == 1 {
		t.Error("active_profile was shared")
	}
	if !*orig.GlobalSettings.ShowPreview {
		t.Error("the show_preview pointer was shared, not copied")
	}
	if orig.Profiles[0].Name == "Renamed" {
		t.Error("profile names were shared")
	}
	if orig.Profiles[1].Rules[0].Dock.Values[0] == 99 {
		t.Error("dock values share a backing array with the copy")
	}
	if orig.Profiles[1].Rules[0].Trigger.Values[1] == 7 {
		t.Error("trigger values share a backing array with the copy")
	}
	if len(orig.Profiles) != 2 {
		t.Errorf("the original grew to %d profiles when the copy was appended to", len(orig.Profiles))
	}
}

func TestCloneOfNilIsNil(t *testing.T) {
	var f *File
	if f.Clone() != nil {
		t.Error("cloning nil should give nil, not an empty configuration")
	}
}

func TestClonePreservesExportedProfileFlag(t *testing.T) {
	path := write(t, "p.json", `{"name":"X","rules":[`+oneEdgeRule+`]}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Clone().IsExportedProfile() {
		t.Error("the copy lost the exported-profile flag, so Save could clobber the file")
	}
}

// TestCloneCopiesEveryPointerSetting is written with reflection on purpose.
// Clone lists the optional settings by hand, so adding a new *bool to Global
// and forgetting to clone it would let the settings window's draft write
// straight through to the live configuration - and the hand-written clone test
// would keep passing, because it only knows about the fields that existed when
// it was written.
func TestCloneCopiesEveryPointerSetting(t *testing.T) {
	orig := &File{GlobalSettings: defaultGlobal()}

	// Give every pointer field a value so there is something to share.
	g := reflect.ValueOf(&orig.GlobalSettings).Elem()
	for i := 0; i < g.NumField(); i++ {
		f := g.Field(i)
		if f.Kind() != reflect.Ptr {
			continue
		}
		f.Set(reflect.New(f.Type().Elem()))
	}

	c := orig.Clone()
	cg := reflect.ValueOf(&c.GlobalSettings).Elem()
	typ := g.Type()

	for i := 0; i < g.NumField(); i++ {
		if g.Field(i).Kind() != reflect.Ptr {
			continue
		}
		name := typ.Field(i).Name
		if cg.Field(i).IsNil() {
			t.Errorf("Clone dropped the %s setting entirely", name)
			continue
		}
		if g.Field(i).Pointer() == cg.Field(i).Pointer() {
			t.Errorf("Clone shares the %s pointer with the original; "+
				"a cancelled edit would change the live configuration", name)
		}
	}
}

// --- the work-area setting moving from global to per-zone -------------------

// TestLegacyWorkAreaMigratesOntoEachDock keeps a hand-written or pre-upgrade
// file behaving as it did: the global key used to govern every zone.
func TestLegacyWorkAreaMigratesOntoEachDock(t *testing.T) {
	path := write(t, "profile.json", `{
	  "global_settings": {"active_profile": 0, "docking_enabled": true, "use_work_area": false},
	  "profiles": [{"name": "P", "rules": [`+oneEdgeRule+`]}]
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.GlobalSettings.UseWorkArea != nil {
		t.Error("the global key survived the migration and would be written back")
	}
	d := f.Profiles[0].Rules[0].Dock
	if d.WorkArea() {
		t.Error("the dock kept clear of the taskbar; the old global said not to")
	}

	// It must not come back on the next round trip.
	out := filepath.Join(t.TempDir(), "out.json")
	if err := Save(out, f); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"global_settings"`)) &&
		bytes.Contains(bytes.SplitN(raw, []byte(`"profiles"`), 2)[0], []byte("use_work_area")) {
		t.Error("use_work_area was written back into global_settings")
	}
}

// A global of true was always the default, so it should leave no trace.
func TestLegacyWorkAreaTrueLeavesDocksAlone(t *testing.T) {
	path := write(t, "profile.json", `{
	  "global_settings": {"active_profile": 0, "docking_enabled": true, "use_work_area": true},
	  "profiles": [{"name": "P", "rules": [`+oneEdgeRule+`]}]
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.GlobalSettings.UseWorkArea != nil {
		t.Error("the global key survived the migration")
	}
	if d := f.Profiles[0].Rules[0].Dock; d.UseWorkArea != nil {
		t.Error("a redundant key was stamped onto the dock")
	}
}

// A dock that states its own setting wins over the old global.
func TestPerDockWorkAreaBeatsTheLegacyGlobal(t *testing.T) {
	path := write(t, "profile.json", `{
	  "global_settings": {"active_profile": 0, "docking_enabled": true, "use_work_area": false},
	  "profiles": [{"name": "P", "rules": [{
	    "dock":    {"monitor": 0, "values": [0, 0, 50, 100], "use_work_area": true},
	    "trigger": {"monitor": 0, "pos": "left", "type": "edge", "values": [0, 100]}
	  }]}]
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Profiles[0].Rules[0].Dock.WorkArea() {
		t.Error("the migration overwrote a setting the dock stated for itself")
	}
}

// The dock pointer has to survive Clone as its own copy too, or cancelling an
// edit in the settings window would flip the live zone.
func TestCloneCopiesThePerDockWorkArea(t *testing.T) {
	no := false
	orig := &File{
		GlobalSettings: defaultGlobal(),
		Profiles: []Profile{{Name: "P", Rules: []Rule{{
			Dock:    Dock{Monitor: 0, Values: []float64{0, 0, 50, 100}, UseWorkArea: &no},
			Trigger: Trigger{Monitor: 0, Pos: PosLeft, Type: TypeEdge, Values: []float64{0, 100}},
		}}}},
	}
	c := orig.Clone()
	got := c.Profiles[0].Rules[0].Dock.UseWorkArea
	if got == nil {
		t.Fatal("Clone dropped the dock setting")
	}
	if got == orig.Profiles[0].Rules[0].Dock.UseWorkArea {
		t.Error("Clone shares the dock pointer with the original")
	}
	*got = true
	if orig.Profiles[0].Rules[0].Dock.WorkArea() {
		t.Error("writing through the clone reached the original")
	}
}

func TestDockWorkAreaDefaultsToTrue(t *testing.T) {
	if !(Dock{}).WorkArea() {
		t.Error("a dock with no key should keep clear of the taskbar")
	}
}
