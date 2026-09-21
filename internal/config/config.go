// The config package reads and writes WinDock profile files. The schema is the
// original's; fields this port adds are optional keys in global_settings, so a
// file written here still loads in the original.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Trigger types.
const (
	TypeEdge   = "edge"
	TypeCorner = "corner"
	TypeArea   = "area"
)

// Trigger positions. Edges use the first four, corners the last four.
const (
	PosLeft   = "left"
	PosRight  = "right"
	PosTop    = "top"
	PosBottom = "bottom"

	PosTopLeft     = "top_left"
	PosTopRight    = "top_right"
	PosBottomLeft  = "bottom_left"
	PosBottomRight = "bottom_right"
)

// File is the whole profile.json document.
type File struct {
	GlobalSettings Global    `json:"global_settings"`
	Profiles       []Profile `json:"profiles"`

	// exportedProfile marks a single-profile export, which Save refuses to
	// write a full configuration over.
	exportedProfile bool
}

// IsExportedProfile reports whether this was loaded from a single exported
// profile file rather than a configuration file.
func (f *File) IsExportedProfile() bool { return f != nil && f.exportedProfile }

// Global holds the settings shared across profiles. The first three keys are
// the original WinDock ones; the rest are additions made by this port.
type Global struct {
	ActiveProfile      int  `json:"active_profile"`
	DockingEnabled     bool `json:"docking_enabled"`
	MaximizeFullWindow bool `json:"maximize_full_window"`

	// EdgeThicknessPx is how close to an edge the cursor must be. A 1px band is
	// unreachable on the inner edges of a multi-monitor desktop.
	EdgeThicknessPx int `json:"edge_thickness_px,omitempty"`

	// CornerSizePx is the side of the hit box at each corner. Corner triggers
	// carry no values, so this has to come from a setting.
	CornerSizePx int `json:"corner_size_px,omitempty"`

	// ShowPreview draws a translucent overlay over the zone that a release
	// would snap to.
	ShowPreview *bool `json:"show_preview,omitempty"`

	// PreviewColor is the overlay fill as 0xRRGGBB.
	PreviewColor *uint32 `json:"preview_color,omitempty"`

	// PreviewAlpha is the overlay opacity, 0-255.
	PreviewAlpha *uint8 `json:"preview_alpha,omitempty"`

	// UseWorkArea is the pre-per-zone setting. Load migrates it onto every dock
	// and clears it, so it is read but never written.
	UseWorkArea *bool `json:"use_work_area,omitempty"`

	// ShowTriggerGuides marks every trigger while dragging. Edge triggers are a
	// few pixels deep, so without this there is no way to see them.
	ShowTriggerGuides *bool `json:"show_trigger_guides,omitempty"`

	// CategoricalColors gives every trigger its own colour and paints the
	// preview to match. Off falls back to PreviewColor everywhere.
	UseCategoricalColors *bool `json:"categorical_colors,omitempty"`

	// GuideThicknessPx is how thick the guides are drawn. This is presentation
	// only: the region that actually triggers is EdgeThicknessPx deep.
	GuideThicknessPx int `json:"guide_thickness_px,omitempty"`
}

// Profile is one named set of rules. An exported single-profile file, such as
// the ones the original WinDock writes, is exactly this object.
type Profile struct {
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

// Rule pairs one trigger with the dock it activates.
type Rule struct {
	Dock    Dock    `json:"dock"`
	Trigger Trigger `json:"trigger"`
}

// Dock is the target position, as percentages of the monitor:
// Values is [left, top, right, bottom], each 0-100.
type Dock struct {
	Monitor int       `json:"monitor"`
	Values  []float64 `json:"values"`

	// UseWorkArea measures this dock against the work area rather than the full
	// monitor bounds, so the window stops at the taskbar. Absent means true;
	// set it false for a zone that should sit behind an auto-hiding taskbar or
	// span the whole screen.
	UseWorkArea *bool `json:"use_work_area,omitempty"`
}

// WorkArea reports whether this dock keeps clear of the taskbar.
func (d Dock) WorkArea() bool {
	if d.UseWorkArea == nil {
		return true
	}
	return *d.UseWorkArea
}

// Trigger is the screen region that activates a Dock. Values is [start, end]
// along the edge for TypeEdge, absent for TypeCorner, and
// [left, top, right, bottom] for TypeArea; all percentages.
type Trigger struct {
	Monitor int       `json:"monitor"`
	Pos     string    `json:"pos,omitempty"`
	Type    string    `json:"type"`
	Values  []float64 `json:"values,omitempty"`
}

// Effective settings, resolving the optional pointers to their defaults.

const (
	DefaultEdgeThicknessPx = 3
	DefaultCornerSizePx    = 120
	DefaultPreviewColor    = 0x0078D7 // Windows accent blue
	DefaultPreviewAlpha    = 150
	DefaultGuideThickness  = 10
	DefaultGuideAlpha      = 170
)

func (g Global) EdgeThickness() int {
	if g.EdgeThicknessPx <= 0 {
		return DefaultEdgeThicknessPx
	}
	return g.EdgeThicknessPx
}

func (g Global) CornerSize() int {
	if g.CornerSizePx <= 0 {
		return DefaultCornerSizePx
	}
	return g.CornerSizePx
}

func (g Global) PreviewEnabled() bool {
	if g.ShowPreview == nil {
		return true
	}
	return *g.ShowPreview
}

func (g Global) PreviewFill() uint32 {
	if g.PreviewColor == nil {
		return DefaultPreviewColor
	}
	return *g.PreviewColor
}

func (g Global) PreviewOpacity() uint8 {
	if g.PreviewAlpha == nil {
		return DefaultPreviewAlpha
	}
	return *g.PreviewAlpha
}

func (g Global) CategoricalColors() bool {
	if g.UseCategoricalColors == nil {
		return true
	}
	return *g.UseCategoricalColors
}

func (g Global) GuidesEnabled() bool {
	if g.ShowTriggerGuides == nil {
		return true
	}
	return *g.ShowTriggerGuides
}

func (g Global) GuideThickness() int {
	if g.GuideThicknessPx <= 0 {
		return DefaultGuideThickness
	}
	return g.GuideThicknessPx
}

// ActiveRules returns the rules of the currently selected profile, or nil if
// the index points nowhere.
func (f *File) ActiveRules() []Rule {
	p := f.ActiveProfileOrNil()
	if p == nil {
		return nil
	}
	return p.Rules
}

// ActiveProfileOrNil returns the selected profile, or nil.
func (f *File) ActiveProfileOrNil() *Profile {
	i := f.GlobalSettings.ActiveProfile
	if i < 0 || i >= len(f.Profiles) {
		return nil
	}
	return &f.Profiles[i]
}

// Default returns the configuration used when no file exists yet: every
// starter layout, simplest selected. An empty first run would demonstrate
// nothing.
func Default() *File {
	tpl := Templates()
	profiles := make([]Profile, 0, len(tpl))
	for _, t := range tpl {
		profiles = append(profiles, t.Profile())
	}
	return &File{
		GlobalSettings: defaultGlobal(),
		Profiles:       profiles,
	}
}

// DefaultPath is where this port keeps its configuration, deliberately not the
// original's directory, so the two can be installed side by side.
func DefaultPath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, "WinDock-Go", "profile.json"), nil
}

// LegacyPath is where the original WinDock keeps its configuration. It is read
// once, on first run, to seed this port with the profiles already in use.
func LegacyPath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, "WinDock", "profile.json"), nil
}

// rawFile carries both shapes a WinDock .json file can have, so Load can tell
// a full configuration from a single exported profile.
type rawFile struct {
	// Configuration file shape.
	GlobalSettings *Global   `json:"global_settings"`
	Profiles       []Profile `json:"profiles"`

	// Exported single-profile shape, as produced by the original WinDock's
	// export button.
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

// defaultGlobal is used when a file carries no global_settings block. Docking
// defaults to on: parsing successfully and then doing nothing is worse.
func defaultGlobal() Global {
	return Global{
		ActiveProfile:  0,
		DockingEnabled: true,
	}
}

// Load reads a WinDock .json file and validates it. It accepts both shapes the
// format has: a full configuration, and a single exported profile.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r rawFile
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	f := &File{}
	switch {
	case len(r.Profiles) > 0:
		f.Profiles = r.Profiles
		if r.GlobalSettings != nil {
			f.GlobalSettings = *r.GlobalSettings
		} else {
			f.GlobalSettings = defaultGlobal()
		}

	case len(r.Rules) > 0:
		name := r.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		f.Profiles = []Profile{{Name: name, Rules: r.Rules}}
		f.GlobalSettings = defaultGlobal()
		f.exportedProfile = true

	default:
		return nil, fmt.Errorf(
			"%s has no rules: it contains neither a %q list nor a %q list",
			path, "profiles", "rules")
	}

	f.migrateWorkArea()
	f.Normalize()
	return f, nil
}

// migrateWorkArea moves the old global use_work_area down onto each dock, which
// is where the setting lives now. Docks that already carry their own are left
// alone, and the global is cleared so Save does not write it back.
func (f *File) migrateWorkArea() {
	if f.GlobalSettings.UseWorkArea == nil {
		return
	}
	was := *f.GlobalSettings.UseWorkArea
	f.GlobalSettings.UseWorkArea = nil
	if was {
		return // already what a dock without the key means
	}
	for i := range f.Profiles {
		for j := range f.Profiles[i].Rules {
			d := &f.Profiles[i].Rules[j].Dock
			if d.UseWorkArea == nil {
				v := was
				d.UseWorkArea = &v
			}
		}
	}
}

// LoadOrInit loads path, falling back to importing the original WinDock config,
// and finally to Default. It reports which source was used.
func LoadOrInit(path string) (*File, string, error) {
	f, err := Load(path)
	if err == nil {
		return f, "config", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}

	if legacy, lerr := LegacyPath(); lerr == nil {
		if f, lerr := Load(legacy); lerr == nil {
			// Start disabled-free: adopt the profiles but keep our own defaults
			// for the settings this port added.
			return f, "imported from " + legacy, nil
		}
	}
	return Default(), "defaults", nil
}

// LoadProfile reads a single exported profile file, the format the original
// WinDock export button produces.
func LoadProfile(path string) (*Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if p.Name == "" {
		p.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	normalizeRules(p.Rules)
	return &p, nil
}

// Save writes the file atomically so an interrupted write cannot truncate an
// existing configuration.
func Save(path string, f *File) error {
	if f.exportedProfile {
		return fmt.Errorf(
			"%s is a single exported profile, not a configuration file; "+
				"writing a configuration over it would destroy it. "+
				"Use 'windock import %s' to add it to your configuration instead",
			path, filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "    ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// SaveProfile writes one profile in the original export format.
func SaveProfile(path string, p *Profile) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Clone returns a deep copy. The settings window edits a copy and commits on
// OK or Apply, so a shared backing array would let a cancelled edit through.
func (f *File) Clone() *File {
	if f == nil {
		return nil
	}
	out := &File{
		GlobalSettings:  f.GlobalSettings,
		Profiles:        make([]Profile, len(f.Profiles)),
		exportedProfile: f.exportedProfile,
	}
	out.GlobalSettings.ShowPreview = clonePtr(f.GlobalSettings.ShowPreview)
	out.GlobalSettings.PreviewColor = clonePtr(f.GlobalSettings.PreviewColor)
	out.GlobalSettings.PreviewAlpha = clonePtr(f.GlobalSettings.PreviewAlpha)
	out.GlobalSettings.UseWorkArea = clonePtr(f.GlobalSettings.UseWorkArea)
	out.GlobalSettings.ShowTriggerGuides = clonePtr(f.GlobalSettings.ShowTriggerGuides)
	out.GlobalSettings.UseCategoricalColors = clonePtr(f.GlobalSettings.UseCategoricalColors)

	for i, p := range f.Profiles {
		out.Profiles[i] = Profile{
			Name:  p.Name,
			Rules: make([]Rule, len(p.Rules)),
		}
		for j, r := range p.Rules {
			out.Profiles[i].Rules[j] = Rule{
				Dock: Dock{
					Monitor:     r.Dock.Monitor,
					Values:      append([]float64(nil), r.Dock.Values...),
					UseWorkArea: clonePtr(r.Dock.UseWorkArea),
				},
				Trigger: Trigger{
					Monitor: r.Trigger.Monitor,
					Pos:     r.Trigger.Pos,
					Type:    r.Trigger.Type,
					Values:  append([]float64(nil), r.Trigger.Values...),
				},
			}
		}
	}
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Normalize clamps values into range and drops rules that cannot be resolved.
func (f *File) Normalize() {
	for i := range f.Profiles {
		normalizeRules(f.Profiles[i].Rules)
	}
	if f.GlobalSettings.ActiveProfile >= len(f.Profiles) {
		f.GlobalSettings.ActiveProfile = 0
	}
	if f.GlobalSettings.ActiveProfile < 0 {
		f.GlobalSettings.ActiveProfile = 0
	}
}

func normalizeRules(rules []Rule) {
	for i := range rules {
		r := &rules[i]
		r.Trigger.Type = strings.ToLower(strings.TrimSpace(r.Trigger.Type))
		r.Trigger.Pos = strings.ToLower(strings.TrimSpace(r.Trigger.Pos))
		clampPercents(r.Dock.Values)
		clampPercents(r.Trigger.Values)
	}
}

func clampPercents(v []float64) {
	for i := range v {
		if v[i] < 0 {
			v[i] = 0
		}
		if v[i] > 100 {
			v[i] = 100
		}
	}
}

// Validate reports the problems that would make a rule unusable. It is used by
// the CLI to explain a bad hand-edited file rather than silently ignoring it.
func (r Rule) Validate() error {
	if len(r.Dock.Values) != 4 {
		return fmt.Errorf("dock needs 4 values, got %d", len(r.Dock.Values))
	}
	if r.Dock.Values[0] >= r.Dock.Values[2] || r.Dock.Values[1] >= r.Dock.Values[3] {
		return errors.New("dock values must be [left, top, right, bottom] with left<right and top<bottom")
	}
	switch r.Trigger.Type {
	case TypeEdge:
		if len(r.Trigger.Values) != 2 {
			return fmt.Errorf("edge trigger needs 2 values, got %d", len(r.Trigger.Values))
		}
		if r.Trigger.Values[0] >= r.Trigger.Values[1] {
			return errors.New("edge trigger values must be [start, end] with start<end")
		}
		switch r.Trigger.Pos {
		case PosLeft, PosRight, PosTop, PosBottom:
		default:
			return fmt.Errorf("edge trigger has unknown pos %q", r.Trigger.Pos)
		}
	case TypeCorner:
		switch r.Trigger.Pos {
		case PosTopLeft, PosTopRight, PosBottomLeft, PosBottomRight:
		default:
			return fmt.Errorf("corner trigger has unknown pos %q", r.Trigger.Pos)
		}
	case TypeArea:
		if len(r.Trigger.Values) != 4 {
			return fmt.Errorf("area trigger needs 4 values, got %d", len(r.Trigger.Values))
		}
	default:
		return fmt.Errorf("unknown trigger type %q", r.Trigger.Type)
	}
	return nil
}
