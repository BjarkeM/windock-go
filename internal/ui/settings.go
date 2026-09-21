package ui

import (
	"fmt"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	lxnwin "github.com/lxn/win"

	"github.com/BjarkeM/windock-go/internal/autostart"
	"github.com/BjarkeM/windock-go/internal/config"
	"github.com/BjarkeM/windock-go/internal/monitor"
)

// SettingsWindow is the profile and zone editor.
//
// Edits are staged - the window works on a copy and writes nothing until OK or
// Apply is hit.
type SettingsWindow struct {
	Form *walk.MainWindow

	app *App

	// draft is the configuration being edited. It is only merged back into the
	// application on Apply or OK.
	draft *config.File

	// autostartDraft stages the registry setting alongside the file ones, so
	// that every checkbox in the window behaves the same way.
	autostartDraft bool

	dirty bool

	profileList  *walk.ListBox
	profileModel *profileListModel
	ruleList     *walk.ListBox
	ruleModel    *ruleListModel

	// canvas is the layout map: the monitors with this profile's zones drawn
	// on them, in the same colours the engine puts on the screen.
	canvas *ZoneCanvas
	mons   *monitor.Set

	// ins holds the fields bound to whichever zone is selected.
	ins inspectorWidgets

	// curProfile and curRule are which profile and rule are being edited.
	//
	// Held here rather than read back from the ListBoxes: walk's resetItems
	// ends in SetCurrentIndex(-1) for any list without a bound Value property,
	// so a model reset silently clears the selection. With the widget as the
	// only record, adding one rule left the window with no profile at all.
	curProfile int
	curRule    int

	// selecting suppresses the lists' own change handlers while the canvas and
	// the lists are being brought into line with each other.
	selecting bool

	applyBtn *walk.PushButton

	cbDocking   *walk.CheckBox
	cbAutostart *walk.CheckBox
	cbMaximize  *walk.CheckBox
	cbPreview   *walk.CheckBox
	cbGuides    *walk.CheckBox
	cbColors    *walk.CheckBox

	// loading suppresses the change handlers while the checkboxes are being
	// populated, so filling the form does not mark it dirty.
	loading bool
}

// ShowSettings opens the editor, or raises it if already open.
func (a *App) ShowSettings() {
	if a.settings != nil && a.settings.Form != nil {
		f := a.settings.Form
		f.Show()
		f.SetFocus()
		return
	}
	if err := a.openSettings(); err != nil {
		a.errorBox("Could not open the settings window", err)
	}
}

func (a *App) openSettings() error {
	draft := a.Config().Clone()
	s := &SettingsWindow{
		app:        a,
		draft:      draft,
		curProfile: draft.GlobalSettings.ActiveProfile,
	}
	if on, _, err := autostart.Enabled(); err == nil {
		s.autostartDraft = on
	}
	s.profileModel = &profileListModel{win: s}
	s.ruleModel = &ruleListModel{win: s}
	s.mons = monitor.Enumerate()

	s.canvas = newZoneCanvas()
	s.canvas.OnSelect = s.onCanvasSelect
	s.canvas.OnCreate = s.onCanvasCreate

	var okBtn, cancelBtn *walk.PushButton

	err := d.MainWindow{
		AssignTo: &s.Form,
		Title:    "WinDock - Settings",
		Icon:     a.windowIcon(),
		MinSize:  d.Size{Width: 900, Height: 640},
		Size:     d.Size{Width: 1020, Height: 720},
		Layout:   d.VBox{},
		Children: []d.Widget{
			// Three labelled columns of related settings, each column only as
			// wide as its widest checkbox: the fixed spacers separate the
			// groups and the greedy one on the right absorbs the slack, so
			// nothing drifts apart when the window is widened.
			//
			// Every widget but the first carries its own Row and Column. walk
			// reads a (0, 0) pair as "place me automatically", so the heading
			// that belongs in that cell is simply declared first.
			d.GroupBox{
				Title:  "General",
				Layout: d.Grid{Columns: 6},
				Children: []d.Widget{
					d.Label{Text: "Docking", Font: d.Font{Bold: true}},
					d.Label{Row: 0, Column: 2, Text: "Overlay", Font: d.Font{Bold: true}},
					d.Label{Row: 0, Column: 4, Text: "Start-up", Font: d.Font{Bold: true}},

					d.HSpacer{Row: 0, Column: 1, Size: 18},
					d.HSpacer{Row: 0, Column: 3, Size: 18},
					d.HSpacer{Row: 0, Column: 5},

					d.CheckBox{Row: 1, Column: 0, AssignTo: &s.cbDocking, Text: "Enable Docking", OnCheckedChanged: s.onCheckChanged},
					d.CheckBox{Row: 2, Column: 0, AssignTo: &s.cbMaximize, Text: "Maximize [0,0,100,100]", OnCheckedChanged: s.onCheckChanged},

					d.CheckBox{Row: 1, Column: 2, AssignTo: &s.cbPreview, Text: "Show zone preview", OnCheckedChanged: s.onCheckChanged},
					d.CheckBox{Row: 2, Column: 2, AssignTo: &s.cbGuides, Text: "Show trigger guides", OnCheckedChanged: s.onCheckChanged},
					d.CheckBox{Row: 3, Column: 2, AssignTo: &s.cbColors, Text: "A colour per trigger", OnCheckedChanged: s.onCheckChanged},

					d.CheckBox{Row: 1, Column: 4, AssignTo: &s.cbAutostart, Text: "Start with Windows", OnCheckedChanged: s.onCheckChanged},
				},
			},

			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.GroupBox{
						Title:   "Profiles",
						MinSize: d.Size{Width: 300},
						MaxSize: d.Size{Width: 330},
						Layout:  d.HBox{},
						Children: []d.Widget{
							// A list must not be declared straight into a group
							// box. CurrentIndexChanged never fires on a listbox declared directly within a groupbox.
							d.Composite{
								Layout: d.HBox{MarginsZero: true},
								Children: []d.Widget{
									d.ListBox{
										AssignTo:              &s.profileList,
										Model:                 s.profileModel,
										OnCurrentIndexChanged: s.onProfileSelected,
										OnItemActivated:       s.setActiveProfile,
									},
									d.Composite{
										MaxSize: d.Size{Width: 104},
										Layout:  d.VBox{MarginsZero: true},
										Children: []d.Widget{
											d.PushButton{Text: "Set Active", OnClicked: s.setActiveProfile},
											d.PushButton{Text: "Rename", OnClicked: s.renameProfile},
											d.VSpacer{Size: 14},
											d.PushButton{Text: "New", OnClicked: s.newProfile},
											d.PushButton{Text: "From layout", OnClicked: s.newProfileFromTemplate},
											d.PushButton{Text: "Duplicate", OnClicked: s.duplicateProfile},
											d.PushButton{Text: "Delete", OnClicked: s.deleteProfile},
											d.VSpacer{Size: 14},
											d.PushButton{Text: "Import", OnClicked: s.importProfile},
											d.PushButton{Text: "Export", OnClicked: s.exportProfile},
											d.VSpacer{},
										},
									},
								},
							},
						},
					},

					d.GroupBox{
						// Instructions in the title: a tooltip on the map would
						// sit under the pointer, covering what it described.
						Title:  "Zones - drag a screen edge to add one, or drag a box in open space",
						Layout: d.VBox{},
						Children: []d.Widget{
							s.canvas.Declarative(),

							d.Composite{
								Layout:        d.HBox{MarginsZero: true},
								StretchFactor: 1,
								Children: []d.Widget{
									d.ListBox{
										AssignTo:              &s.ruleList,
										Model:                 s.ruleModel,
										MinSize:               d.Size{Width: 290},
										MaxSize:               d.Size{Width: 330},
										OnCurrentIndexChanged: s.onRuleSelected,
										OnItemActivated:       s.editRule,
									},
									d.Composite{
										MaxSize: d.Size{Width: 104},
										Layout:  d.VBox{MarginsZero: true},
										Children: []d.Widget{
											d.PushButton{Text: "Add", OnClicked: s.addRule},
											d.PushButton{Text: "Duplicate", OnClicked: s.duplicateRule},
											d.PushButton{Text: "Remove", OnClicked: s.removeRule},
											d.VSpacer{Size: 14},
											d.PushButton{Text: "Move Up", OnClicked: func() { s.moveRule(-1) }},
											d.PushButton{Text: "Move Down", OnClicked: func() { s.moveRule(1) }},
											d.VSpacer{},
										},
									},
									s.inspectorDeclarative(),
								},
							},
						},
					},
				},
			},

			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.PushButton{Text: "About", MaxSize: d.Size{Width: 90}, OnClicked: s.app.showAbout},
					d.HSpacer{},
					d.PushButton{AssignTo: &okBtn, Text: "OK", MaxSize: d.Size{Width: 90}, OnClicked: s.onOK},
					d.PushButton{AssignTo: &cancelBtn, Text: "Cancel", MaxSize: d.Size{Width: 90}, OnClicked: s.onCancel},
					d.PushButton{AssignTo: &s.applyBtn, Text: "Apply", MaxSize: d.Size{Width: 90}, Enabled: false, OnClicked: s.onApply},
				},
			},
		},
	}.Create()
	if err != nil {
		return err
	}

	s.Form.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		// Closing with the X discards, like Cancel, but says so first.
		if s.dirty && reason == walk.CloseReasonUnknown {
			if !s.confirmDiscard() {
				*canceled = true
				return
			}
		}
		a.settings = nil
	})

	a.settings = s
	s.loadGeneral()
	s.Refresh()
	s.selectProfile(s.draft.GlobalSettings.ActiveProfile)
	s.Form.Show()
	return nil
}

// loadGeneral fills the checkboxes from the draft.
func (s *SettingsWindow) loadGeneral() {
	s.loading = true
	defer func() { s.loading = false }()

	g := s.draft.GlobalSettings
	s.cbDocking.SetChecked(g.DockingEnabled)
	s.cbMaximize.SetChecked(g.MaximizeFullWindow)
	s.cbPreview.SetChecked(g.PreviewEnabled())
	s.cbGuides.SetChecked(g.GuidesEnabled())
	s.cbColors.SetChecked(g.CategoricalColors())
	s.cbAutostart.SetChecked(s.autostartDraft)
}

// storeGeneral copies the checkboxes back into the draft.
func (s *SettingsWindow) storeGeneral() {
	g := &s.draft.GlobalSettings
	g.DockingEnabled = s.cbDocking.Checked()
	g.MaximizeFullWindow = s.cbMaximize.Checked()
	g.ShowPreview = boolPtr(s.cbPreview.Checked())
	g.ShowTriggerGuides = boolPtr(s.cbGuides.Checked())
	g.UseCategoricalColors = boolPtr(s.cbColors.Checked())
	s.autostartDraft = s.cbAutostart.Checked()
}

func boolPtr(b bool) *bool { return &b }

func (s *SettingsWindow) onCheckChanged() {
	if s.loading {
		return
	}
	s.storeGeneral()
	s.setDirty()
	// Two of these change what the map shows: the colour scheme, and whether
	// docks stop at the taskbar.
	s.refreshZones()
}

func (s *SettingsWindow) setDirty() {
	s.dirty = true
	if s.applyBtn != nil {
		s.applyBtn.SetEnabled(true)
	}
}

// Refresh redraws the lists from the draft.
func (s *SettingsWindow) Refresh() {
	if s == nil || s.Form == nil {
		return
	}
	s.selecting = true
	s.profileModel.PublishItemsReset()
	s.ruleModel.PublishItemsReset()
	// Both lists just lost their selection as a side effect of the reset. The
	// indices this window owns are the truth, so they go straight back in.
	setListIndex(s.profileList, s.selectedProfileIndex())
	setListIndex(s.ruleList, s.selectedRuleIndex())
	s.selecting = false

	s.refreshZones()
}

// refreshZones redraws the map and re-reads the fields. Every edit funnels
// through here so the three views cannot disagree.
func (s *SettingsWindow) refreshZones() {
	if s.canvas == nil {
		return
	}
	var rules []config.Rule
	if p := s.editingProfile(); p != nil {
		rules = p.Rules
	}
	s.canvas.SetRules(s.mons, rules, s.draft.GlobalSettings)
	s.canvas.SetSelected(s.selectedRuleIndex())
	s.loadInspector()
}

// selectRule brings the list, the map and the fields onto the same rule.
func (s *SettingsWindow) selectRule(i int) {
	s.curRule = i

	s.selecting = true
	setListIndex(s.ruleList, s.selectedRuleIndex())
	s.selecting = false

	s.refreshZones()
}

// onRuleSelected is the rule list's own change handler.
func (s *SettingsWindow) onRuleSelected() {
	if s.selecting {
		return
	}
	s.curRule = s.ruleList.CurrentIndex()
	s.canvas.SetSelected(s.selectedRuleIndex())
	s.loadInspector()
}

// onCanvasSelect runs when a zone is clicked on the map.
func (s *SettingsWindow) onCanvasSelect(rule int) {
	s.selectRule(rule)
}

// onCanvasCreate runs when a zone has been drawn on the map.
func (s *SettingsWindow) onCanvasCreate(r config.Rule) {
	p := s.editingProfile()
	if p == nil {
		walk.MsgBox(s.Form, "WinDock",
			"Select a profile first; a zone has to belong to one.",
			walk.MsgBoxIconInformation)
		return
	}
	if err := r.Validate(); err != nil {
		// Dropping an unusable zone silently would look like a failed drag.
		walk.MsgBox(s.Form, "WinDock",
			"That zone would not work: "+err.Error(),
			walk.MsgBoxIconWarning)
		return
	}
	p.Rules = append(p.Rules, r)
	s.setDirty()
	s.Refresh()
	s.selectRule(len(p.Rules) - 1)
}

// --- committing -----------------------------------------------------------

func (s *SettingsWindow) onApply() {
	if err := s.commit(); err != nil {
		s.app.errorBox("Could not save the settings", err)
	}
}

func (s *SettingsWindow) onOK() {
	if s.dirty {
		if err := s.commit(); err != nil {
			s.app.errorBox("Could not save the settings", err)
			return
		}
	}
	s.Form.Close()
}

func (s *SettingsWindow) onCancel() {
	if s.dirty && !s.confirmDiscard() {
		return
	}
	s.dirty = false
	s.Form.Close()
}

func (s *SettingsWindow) confirmDiscard() bool {
	return walk.MsgBox(s.Form, "WinDock",
		"Discard your changes?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
}

// commit writes the draft out and hands it to the engine.
func (s *SettingsWindow) commit() error {
	s.storeGeneral()

	if err := autostart.Set(s.autostartDraft); err != nil {
		return err
	}
	if err := s.app.Commit(s.draft); err != nil {
		return err
	}
	// Carry on editing from a fresh copy, so later edits cannot reach back into
	// the configuration the engine is now using.
	s.draft = s.app.Config().Clone()
	s.dirty = false
	s.applyBtn.SetEnabled(false)
	s.Refresh()
	return nil
}

// --- selection ------------------------------------------------------------

func (s *SettingsWindow) selectedProfileIndex() int {
	if s.curProfile < 0 || s.curProfile >= len(s.draft.Profiles) {
		return -1
	}
	return s.curProfile
}

func (s *SettingsWindow) editingProfile() *config.Profile {
	i := s.selectedProfileIndex()
	if i < 0 || i >= len(s.draft.Profiles) {
		return nil
	}
	return &s.draft.Profiles[i]
}

// setListIndex sets a ListBox selection, including clearing it.
func setListIndex(list *walk.ListBox, i int) {
	if i < 0 {
		list.SendMessage(lxnwin.LB_SETCURSEL, ^uintptr(0), 0)
	}
	list.SetCurrentIndex(i)
}

// selectProfile opens a profile, clamped to one that exists.
func (s *SettingsWindow) selectProfile(i int) {
	switch {
	case len(s.draft.Profiles) == 0:
		i = -1
	case i < 0:
		i = 0
	case i >= len(s.draft.Profiles):
		i = len(s.draft.Profiles) - 1
	}
	s.curProfile = i

	s.selecting = true
	setListIndex(s.profileList, i)
	s.selecting = false

	s.profileChanged()
}

// onProfileSelected is the profile list's own change handler.
func (s *SettingsWindow) onProfileSelected() {
	if s.selecting {
		return
	}
	s.curProfile = s.profileList.CurrentIndex()
	s.profileChanged()
}

// profileChanged refills everything that depends on which profile is open.
func (s *SettingsWindow) profileChanged() {
	s.selecting = true
	s.ruleModel.PublishItemsReset()
	s.selecting = false

	first := -1
	if p := s.editingProfile(); p != nil && len(p.Rules) > 0 {
		first = 0
	}
	s.selectRule(first)
}

// --- profile actions ------------------------------------------------------

func (s *SettingsWindow) setActiveProfile() {
	i := s.selectedProfileIndex()
	if i < 0 || i >= len(s.draft.Profiles) {
		return
	}
	s.draft.GlobalSettings.ActiveProfile = i
	s.setDirty()
	s.Refresh()
}

func (s *SettingsWindow) newProfile() {
	name, ok := s.promptText("New profile", "Name for the new profile:", "New Profile")
	if !ok {
		return
	}
	s.draft.Profiles = append(s.draft.Profiles, config.Profile{Name: name})
	s.setDirty()
	s.Refresh()
	s.selectProfile(len(s.draft.Profiles) - 1)
}

// newProfileFromTemplate adds one of the built-in starter layouts.
func (s *SettingsWindow) newProfileFromTemplate() {
	tpl := config.Templates()
	items := make([]string, len(tpl))
	for i, t := range tpl {
		items[i] = fmt.Sprintf("%s - %s", t.Name, t.Note)
	}

	var (
		dlg      *walk.Dialog
		list     *walk.ListBox
		ok       *walk.PushButton
		cancel   *walk.PushButton
		accepted bool
	)
	err := d.Dialog{
		AssignTo:      &dlg,
		Title:         "Start from a layout",
		Icon:          s.app.windowIcon(),
		MinSize:       d.Size{Width: 560, Height: 260},
		DefaultButton: &ok,
		CancelButton:  &cancel,
		Layout:        d.VBox{},
		Children: []d.Widget{
			d.Label{Text: "Pick a layout to copy into a new profile. You can edit it afterwards."},
			d.ListBox{AssignTo: &list, Model: items, OnItemActivated: func() {
				accepted = true
				dlg.Accept()
			}},
			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.HSpacer{},
					d.PushButton{AssignTo: &ok, Text: "Add", MaxSize: d.Size{Width: 90}, OnClicked: func() {
						accepted = true
						dlg.Accept()
					}},
					d.PushButton{AssignTo: &cancel, Text: "Cancel", MaxSize: d.Size{Width: 90}, OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(s.Form)
	if err != nil {
		s.app.errorBox("Could not open the dialog", err)
		return
	}
	list.SetCurrentIndex(0)
	dlg.Run()

	i := list.CurrentIndex()
	if !accepted || i < 0 || i >= len(tpl) {
		return
	}

	p := tpl[i].Profile()
	p.Name = uniqueProfileName(s.draft.Profiles, p.Name)
	s.draft.Profiles = append(s.draft.Profiles, p)
	s.setDirty()
	s.Refresh()
	s.selectProfile(len(s.draft.Profiles) - 1)
}

// uniqueProfileName appends a counter if the name is taken, so adding the same
// layout twice gives two entries that can be told apart in the tray menu.
func uniqueProfileName(existing []config.Profile, want string) string {
	taken := func(n string) bool {
		for _, p := range existing {
			if p.Name == n {
				return true
			}
		}
		return false
	}
	if !taken(want) {
		return want
	}
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s (%d)", want, i)
		if !taken(n) {
			return n
		}
	}
}

func (s *SettingsWindow) renameProfile() {
	p := s.editingProfile()
	if p == nil {
		return
	}
	name, ok := s.promptText("Rename profile", "New name:", p.Name)
	if !ok {
		return
	}
	p.Name = name
	s.setDirty()
	s.Refresh()
}

func (s *SettingsWindow) duplicateProfile() {
	p := s.editingProfile()
	if p == nil {
		return
	}
	dup := config.Profile{
		Name:  p.Name + " (copy)",
		Rules: append([]config.Rule(nil), p.Rules...),
	}
	s.draft.Profiles = append(s.draft.Profiles, dup)
	s.setDirty()
	s.Refresh()
	s.selectProfile(len(s.draft.Profiles) - 1)
}

func (s *SettingsWindow) deleteProfile() {
	i := s.selectedProfileIndex()
	if i < 0 || i >= len(s.draft.Profiles) {
		return
	}
	if len(s.draft.Profiles) == 1 {
		walk.MsgBox(s.Form, "WinDock",
			"The last profile cannot be deleted; there would be nothing to dock to.",
			walk.MsgBoxIconWarning)
		return
	}
	p := s.draft.Profiles[i]
	if walk.MsgBox(s.Form, "Delete profile",
		fmt.Sprintf("Delete the profile %q and all %d of its rules?", p.Name, len(p.Rules)),
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}

	s.draft.Profiles = append(s.draft.Profiles[:i], s.draft.Profiles[i+1:]...)
	switch {
	case s.draft.GlobalSettings.ActiveProfile == i:
		s.draft.GlobalSettings.ActiveProfile = 0
	case s.draft.GlobalSettings.ActiveProfile > i:
		s.draft.GlobalSettings.ActiveProfile--
	}
	s.setDirty()
	s.Refresh()
	s.selectProfile(i)
}

func (s *SettingsWindow) importProfile() {
	dlg := walk.FileDialog{
		Title:          "Import profile",
		Filter:         "Profile files (*.json)|*.json|All files (*.*)|*.*",
		InitialDirPath: s.app.configDir(),
	}
	ok, err := dlg.ShowOpen(s.Form)
	if err != nil {
		s.app.errorBox("Could not open the file dialog", err)
		return
	}
	if !ok {
		return
	}
	p, err := config.LoadProfile(dlg.FilePath)
	if err != nil {
		s.app.errorBox("Could not read that profile", err)
		return
	}
	s.draft.Profiles = append(s.draft.Profiles, *p)
	s.setDirty()
	s.Refresh()
	s.selectProfile(len(s.draft.Profiles) - 1)
}

func (s *SettingsWindow) exportProfile() {
	p := s.editingProfile()
	if p == nil {
		return
	}
	dlg := walk.FileDialog{
		Title:          "Export profile",
		Filter:         "Profile files (*.json)|*.json",
		InitialDirPath: s.app.configDir(),
		FilePath:       p.Name + ".json",
	}
	ok, err := dlg.ShowSave(s.Form)
	if err != nil {
		s.app.errorBox("Could not open the file dialog", err)
		return
	}
	if !ok {
		return
	}
	// Export writes what is on screen, including edits not yet applied.
	if err := config.SaveProfile(dlg.FilePath, p); err != nil {
		s.app.errorBox("Could not write the profile", err)
	}
}

// --- rule actions ---------------------------------------------------------

func (s *SettingsWindow) selectedRuleIndex() int {
	p := s.editingProfile()
	if p == nil || s.curRule < 0 || s.curRule >= len(p.Rules) {
		return -1
	}
	return s.curRule
}

// addRule appends a plain left-edge zone and selects it. No dialog first: the
// fields are beside the canvas anyway, and a zone you can see on the map is
// easier to correct than an empty form.
func (s *SettingsWindow) addRule() {
	p := s.editingProfile()
	if p == nil {
		return
	}
	p.Rules = append(p.Rules, defaultRule())
	s.setDirty()
	s.Refresh()
	s.selectRule(len(p.Rules) - 1)
}

func (s *SettingsWindow) editRule() {
	p := s.editingProfile()
	i := s.selectedRuleIndex()
	if p == nil || i < 0 || i >= len(p.Rules) {
		return
	}
	rule, ok := s.app.editRuleDialog(s.Form, p.Rules[i], "Edit rule")
	if !ok {
		return
	}
	p.Rules[i] = rule
	s.setDirty()
	s.Refresh()
	s.selectRule(i)
}

func (s *SettingsWindow) duplicateRule() {
	p := s.editingProfile()
	i := s.selectedRuleIndex()
	if p == nil || i < 0 || i >= len(p.Rules) {
		return
	}
	dup := p.Rules[i]
	dup.Dock.Values = append([]float64(nil), p.Rules[i].Dock.Values...)
	dup.Trigger.Values = append([]float64(nil), p.Rules[i].Trigger.Values...)

	p.Rules = append(p.Rules, config.Rule{})
	copy(p.Rules[i+2:], p.Rules[i+1:])
	p.Rules[i+1] = dup

	s.setDirty()
	s.Refresh()
	s.selectRule(i + 1)
}

func (s *SettingsWindow) removeRule() {
	p := s.editingProfile()
	i := s.selectedRuleIndex()
	if p == nil || i < 0 || i >= len(p.Rules) {
		return
	}
	p.Rules = append(p.Rules[:i], p.Rules[i+1:]...)
	s.setDirty()
	s.Refresh()
	if i >= len(p.Rules) {
		i = len(p.Rules) - 1
	}
	s.selectRule(i)
}

// moveRule reorders a rule. Order is significant: when two triggers of the same
// kind overlap, the earlier rule wins.
func (s *SettingsWindow) moveRule(delta int) {
	p := s.editingProfile()
	i := s.selectedRuleIndex()
	if p == nil || i < 0 || i >= len(p.Rules) {
		return
	}
	to := i + delta
	if to < 0 || to >= len(p.Rules) {
		return
	}
	p.Rules[i], p.Rules[to] = p.Rules[to], p.Rules[i]
	s.setDirty()
	s.Refresh()
	s.selectRule(to)
}

// promptText shows a small single-field dialog.
func (s *SettingsWindow) promptText(title, prompt, initial string) (string, bool) {
	var (
		dlg      *walk.Dialog
		edit     *walk.LineEdit
		ok       *walk.PushButton
		cancel   *walk.PushButton
		accepted bool
	)
	err := d.Dialog{
		AssignTo:      &dlg,
		Title:         title,
		Icon:          s.app.windowIcon(),
		MinSize:       d.Size{Width: 360, Height: 0},
		DefaultButton: &ok,
		CancelButton:  &cancel,
		Layout:        d.VBox{},
		Children: []d.Widget{
			d.Label{Text: prompt},
			d.LineEdit{AssignTo: &edit, Text: initial},
			d.Composite{
				Layout: d.HBox{MarginsZero: true},
				Children: []d.Widget{
					d.HSpacer{},
					d.PushButton{AssignTo: &ok, Text: "OK", OnClicked: func() {
						accepted = true
						dlg.Accept()
					}},
					d.PushButton{AssignTo: &cancel, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(s.Form)
	if err != nil {
		s.app.errorBox("Could not open the dialog", err)
		return "", false
	}
	dlg.Run()

	text := edit.Text()
	if !accepted || text == "" {
		return "", false
	}
	return text, true
}

// --- models ---------------------------------------------------------------

type profileListModel struct {
	walk.ListModelBase
	win *SettingsWindow
}

func (m *profileListModel) ItemCount() int { return len(m.win.draft.Profiles) }

func (m *profileListModel) Value(i int) interface{} {
	cfg := m.win.draft
	if i < 0 || i >= len(cfg.Profiles) {
		return ""
	}
	// The original marks the active profile with a " - Active" suffix.
	if i == cfg.GlobalSettings.ActiveProfile {
		return cfg.Profiles[i].Name + " - Active"
	}
	return cfg.Profiles[i].Name
}

type ruleListModel struct {
	walk.ListModelBase
	win *SettingsWindow
}

func (m *ruleListModel) rules() []config.Rule {
	p := m.win.editingProfile()
	if p == nil {
		return nil
	}
	return p.Rules
}

func (m *ruleListModel) ItemCount() int { return len(m.rules()) }

func (m *ruleListModel) Value(i int) interface{} {
	rules := m.rules()
	if i < 0 || i >= len(rules) {
		return ""
	}
	return describeRule(rules[i])
}

// describeRule renders one rule as the single line the list shows.
func describeRule(r config.Rule) string {
	mon := fmt.Sprintf("monitor %d", r.Trigger.Monitor)
	if r.Trigger.Monitor != r.Dock.Monitor {
		mon = fmt.Sprintf("monitor %d to %d", r.Trigger.Monitor, r.Dock.Monitor)
	}
	return fmt.Sprintf("%s  ->  %s  (%s)", describeTrigger(r.Trigger), describeDock(r.Dock), mon)
}

func describeTrigger(t config.Trigger) string {
	switch t.Type {
	case config.TypeCorner:
		return "corner " + prettyPos(t.Pos)
	case config.TypeEdge:
		if len(t.Values) == 2 {
			return fmt.Sprintf("%s edge, %g%% to %g%%", prettyPos(t.Pos), t.Values[0], t.Values[1])
		}
		return prettyPos(t.Pos) + " edge"
	case config.TypeArea:
		if len(t.Values) == 4 {
			return fmt.Sprintf("area %g,%g to %g,%g", t.Values[0], t.Values[1], t.Values[2], t.Values[3])
		}
		return "area"
	}
	// A hand-edited profile can carry any string here, including none at all.
	// An empty entry would look like a rendering fault rather than a bad rule.
	if t.Type == "" {
		return "(no trigger type)"
	}
	return fmt.Sprintf("(unknown type %q)", t.Type)
}

func describeDock(k config.Dock) string {
	if len(k.Values) != 4 {
		return "(invalid)"
	}
	v := k.Values
	out := fmt.Sprintf("[%g,%g,%g,%g]", v[0], v[1], v[2], v[3])
	if !k.WorkArea() {
		out += " full screen"
	}
	return out
}

func prettyPos(p string) string {
	switch p {
	case config.PosTopLeft:
		return "top left"
	case config.PosTopRight:
		return "top right"
	case config.PosBottomLeft:
		return "bottom left"
	case config.PosBottomRight:
		return "bottom right"
	}
	return p
}

// sortedPositions keeps the dialog dropdowns in a stable, sensible order.
func sortedPositions(kind string) []string {
	switch kind {
	case config.TypeCorner:
		return []string{config.PosTopLeft, config.PosTopRight, config.PosBottomLeft, config.PosBottomRight}
	default:
		return []string{config.PosLeft, config.PosRight, config.PosTop, config.PosBottom}
	}
}
