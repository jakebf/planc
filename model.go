package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// ─── Key Map ─────────────────────────────────────────────────────────────────

type keyMap struct {
	Navigate    key.Binding
	SwitchPane  key.Binding
	CycleStatus key.Binding
	RevStatus   key.Binding
	SetStatus   key.Binding // 0-3 direct status set (display-only binding)
	Undo        key.Binding
	ToggleDone  key.Binding
	Project     key.Binding
	Delete      key.Binding
	Primary     key.Binding
	Editor      key.Binding
	Filter      key.Binding
	CopyFile    key.Binding
	PrevProject key.Binding
	NextProject key.Binding
	Select      key.Binding
	SelectAll   key.Binding
	ScrollDown  key.Binding
	ScrollUp    key.Binding
	Help        key.Binding
	Settings    key.Binding
	Quit        key.Binding
	ForceQuit   key.Binding
	Demo        key.Binding
}

func newKeyMap(cfg config) keyMap {
	return keyMap{
		Navigate:    key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "navigate / scroll")),
		SwitchPane:  key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "switch pane")),
		CycleStatus: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "next status")),
		RevStatus:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "reverse status")),
		SetStatus:   key.NewBinding(key.WithKeys("0", "1", "2", "3"), key.WithHelp("0-3", "set status")),
		Undo:        key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undo status")),
		ToggleDone:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle done plans")),
		Project:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "set project")),
		Delete:      key.NewBinding(key.WithKeys("#"), key.WithHelp("#", "delete plan")),
		Primary:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", commandLabel(cfg.Primary))),
		Editor:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", commandLabel(cfg.Editor))),
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		CopyFile:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy path")),
		PrevProject: key.NewBinding(key.WithKeys("["), key.WithHelp("[/]", "cycle project filter")),
		NextProject: key.NewBinding(key.WithKeys("]")),
		Select:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "select")),
		SelectAll:   key.NewBinding(key.WithKeys("a")),
		ScrollDown:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "page down")),
		ScrollUp:    key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "page up")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Settings:    key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),
		Quit:        key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit:   key.NewBinding(key.WithKeys("ctrl+c")),
		Demo:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "demo mode")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Primary, k.Editor, k.CycleStatus, k.Project, k.Select, k.Filter, k.Help}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Actions
		{k.Primary, k.Editor, k.CopyFile, k.CycleStatus, k.RevStatus, k.SetStatus, k.Undo, k.Project, k.PrevProject, k.Select, k.Delete},
		// Navigation / app
		{k.Navigate, k.SwitchPane, k.ScrollDown, k.ScrollUp, k.Filter, k.ToggleDone, k.Help, k.Settings, k.Quit},
	}
}

// ─── Model ───────────────────────────────────────────────────────────────────

const statusTimeout = 3 * time.Second

type demoState struct {
	active  bool
	plans   []plan
	content map[string]string
}

type releaseNotesState struct {
	on       bool
	version  string
	markdown string
	viewport viewport.Model
}

type statusBarState struct {
	text    string
	id      int
	spinner spinner.Model
}

type model struct {
	// Layout
	list     list.Model
	viewport viewport.Model
	keys     keyMap
	help     help.Model
	focused  pane
	width    int
	height   int
	ready    bool // true after first WindowSizeMsg

	// Preview rendering
	previewCache map[string]string // filename → glamour-rendered markdown
	refreshing   map[string]bool   // files being re-rendered due to external change
	previewWidth int               // cached width for invalidation on resize
	prerendered  bool              // true after first render pass
	glamourStyle string            // "dark" or "light" based on terminal background

	// Plan data
	allPlans      []plan
	dir           string
	cfg           config
	installed     time.Time // first-run timestamp; controls unset-plan visibility
	store         planStore
	watcher       *fsnotify.Watcher
	showDone      bool
	projectFilter string

	// Cursor and selection
	prevIndex    int             // tracks cursor changes to trigger preview updates
	selected     map[string]bool // files toggled with 'x' for batch operations
	changedFiles map[string]bool // files recently changed externally (spinner on badge)
	changedSpinID   int
	changedSpinView *string // shared with delegate for spinner frame

	// Modals and transient state
	confirmDelete    bool
	settingProject   bool
	projectInput     textinput.Model
	projectChoices   []string
	lastStatusChange *statusUpdatedMsg // non-nil during undo window
	batchKeepFiles   []string          // keeps batch-affected items visible until linger expires

	// Sub-states
	clod            clodState
	demo            demoState
	status          statusBarState
	updateAvailable *updateAvailableMsg
	releaseNotes    releaseNotesState
}

func (m *model) planSource() *[]plan {
	if m.demo.active {
		return &m.demo.plans
	}
	return &m.allPlans
}

func (m model) visiblePlans() []plan {
	if m.demo.active {
		// Use a fake installed time so unset-status plans with recent
		// modified times are visible, just like in real usage.
		fakeInstalled := time.Now().Add(-48 * time.Hour)
		return filterPlans(m.demo.plans, m.showDone, m.keepFiles(), m.projectFilter, fakeInstalled)
	}
	return filterPlans(m.allPlans, m.showDone, m.keepFiles(), m.projectFilter, m.installed)
}

func newModel(plans []plan, dir string, cfg config, watcher *fsnotify.Watcher) model {
	sel := make(map[string]bool)
	chg := make(map[string]bool)
	var installed time.Time
	if cfg.Installed != "" {
		installed, _ = time.Parse(time.RFC3339, cfg.Installed)
	}
	sortPlans(plans)
	var spinView string
	delegate := planDelegate{selected: sel, changed: chg, spinnerView: &spinView}
	visible := filterPlans(plans, cfg.ShowAll, nil, "", installed)
	l := list.New(plansToItems(visible), delegate, 0, 0)
	l.Title = "Planc Active · All"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.Styles.Title = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 1, 1, 2)
	l.KeyMap.Quit.SetKeys("q") // don't quit on esc
	l.FilterInput.Prompt = "Search: "

	keys := newKeyMap(cfg)

	h := help.New()
	h.ShortSeparator = " | "
	h.Styles.ShortKey = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(colorDim)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(colorDim)
	h.Styles.FullKey = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Width(10)
	h.Styles.FullDesc = lipgloss.NewStyle().Foreground(colorFull)
	h.Styles.FullSeparator = lipgloss.NewStyle()

	s := spinner.New()
	s.Spinner = spinner.Pulse
	s.Style = lipgloss.NewStyle().Foreground(colorAccent)

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 50
	ti.Width = 30
	rnvp := viewport.New(0, 0)

	style := "dark"
	if !lipgloss.HasDarkBackground() {
		style = "light"
	}

	return model{
		list:            l,
		viewport:        viewport.New(0, 0),
		keys:            keys,
		help:            h,
		focused:         listPane,
		prevIndex:       -1,
		previewCache:    make(map[string]string),
		changedFiles:    chg,
		changedSpinView: &spinView,
		watcher:         watcher,
		allPlans:        plans,
		showDone:        cfg.ShowAll,
		dir:             dir,
		cfg:             cfg,
		installed:       installed,
		selected:        sel,
		store:           diskStore{dir: dir},
		glamourStyle:    style,
		status:          statusBarState{spinner: s},
		projectInput:    ti,
		releaseNotes:    releaseNotesState{viewport: rnvp},
	}
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.watcher != nil {
		cmds = append(cmds, watchDir(m.watcher))
	}
	if !m.demo.active {
		if cmd := startupUpdateCmd(getVersion()); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// keepFiles returns files that should remain visible even if their status
// would normally hide them. This covers the undo window (single status change)
// and the linger period after batch operations.
func (m model) keepFiles() map[string]bool {
	keep := make(map[string]bool)
	if m.lastStatusChange != nil {
		keep[m.lastStatusChange.newPlan.file] = true
	}
	for _, f := range m.batchKeepFiles {
		keep[f] = true
	}
	return keep
}

// setStatus shows a transient message in the status bar with a spinner animation.
// If duration > 0, the message auto-clears after that time.
func (m *model) setStatus(text string, duration time.Duration) tea.Cmd {
	m.status.id++
	m.status.text = text
	id := m.status.id
	var cmds []tea.Cmd
	cmds = append(cmds, m.status.spinner.Tick)
	if duration > 0 {
		cmds = append(cmds, tea.Tick(duration, func(time.Time) tea.Msg {
			return statusClearMsg{id: id}
		}))
	}
	return tea.Batch(cmds...)
}

func (m *model) clearStatus() {
	m.status.text = ""
}

// updateHelpKeys refreshes the toggle-done help text to reflect current state.
func (m *model) updateHelpKeys() {
	if m.showDone {
		m.keys.ToggleDone.SetHelp("a", "show active")
	} else {
		m.keys.ToggleDone.SetHelp("a", "show all")
	}
}

func (m *model) restoreTitle() {
	brand := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	tab := lipgloss.NewStyle().Bold(true)
	ghost := lipgloss.NewStyle().Foreground(colorDim)

	var tabs string
	if m.showDone {
		tabs = ghost.Render("a ") + ghost.Render("Active") + ghost.Render(" · ") + tab.Render("All")
	} else {
		tabs = ghost.Render("a ") + tab.Render("Active") + ghost.Render(" · ") + ghost.Render("All")
	}
	tabsW := lipgloss.Width(tabs)

	left := brand.Render("Planc")
	if m.demo.active {
		maxW := m.list.Width() - 2
		baseW := lipgloss.Width(left)
		// Pick the longest demo hint that still leaves room for tabs
		for _, hint := range []string{"demo · press d to exit", "demo · d", "demo"} {
			if baseW+1+len(hint)+1+tabsW <= maxW {
				left += " " + ghost.Render(hint)
				break
			}
		}
	}
	if m.projectFilter != "" {
		left += " " + projectColor(m.projectFilter).Render(m.projectFilter)
	}
	if m.list.IsFiltered() {
		filterText := m.list.FilterValue()
		if filterText != "" {
			left += " " + dateStyle.Render("/"+filterText)
		}
	}

	maxW := m.list.Width() - 3 // TitleBar padding: left (2) + right (1)
	leftW := lipgloss.Width(left)
	avail := maxW - leftW - tabsW
	if avail > 0 {
		m.list.Title = left + strings.Repeat(" ", avail) + tabs
	} else {
		// Not enough room — drop tabs to avoid wrapping
		m.list.Title = left
	}
}

func (m model) selectedFile() string {
	if item, ok := m.list.SelectedItem().(plan); ok {
		return item.file
	}
	return ""
}

// selectFile moves the cursor to the item matching file, or stays at the
// current index if file is not found (clamped to list length).
func (m *model) selectFile(file string) {
	for i, item := range m.list.Items() {
		if p, ok := item.(plan); ok && p.file == file {
			m.list.Select(i)
			return
		}
	}
	// File not in list (e.g. filtered out) — clamp to bounds
	if idx := m.list.Index(); idx >= len(m.list.Items()) && len(m.list.Items()) > 0 {
		m.list.Select(len(m.list.Items()) - 1)
	}
}

func (m model) cmdSetStatus(p plan, status string) tea.Cmd {
	return m.store.setStatus(p, status)
}

func (m model) cmdDelete(p plan) tea.Cmd {
	return m.store.deletePlan(p)
}

func (m model) cmdSetProject(p plan, project string) tea.Cmd {
	return m.store.setProject(p, project)
}

func (m model) cmdBatchSetStatus(files []string, status string) tea.Cmd {
	return m.store.batchSetStatus(files, status)
}

func (m model) cmdBatchSetProject(files []string, project string) tea.Cmd {
	return m.store.batchSetProject(files, project)
}

func (m model) applyProject(proj string) tea.Cmd {
	if len(m.selected) > 0 {
		files := m.selectedFiles()
		return m.cmdBatchSetProject(files, proj)
	}
	if item, ok := m.list.SelectedItem().(plan); ok {
		return m.cmdSetProject(item, proj)
	}
	return nil
}

// pruneSelection removes selected files that are no longer in the visible list.
func (m *model) pruneSelection() {
	visible := make(map[string]bool)
	for _, item := range m.list.Items() {
		if p, ok := item.(plan); ok {
			visible[p.file] = true
		}
	}
	for f := range m.selected {
		if !visible[f] {
			delete(m.selected, f)
		}
	}
}

func (m model) selectedFiles() []string {
	var files []string
	for f := range m.selected {
		files = append(files, f)
	}
	return files
}

// firstSelectedPlan returns the first selected plan in visible list order.
func (m model) firstSelectedPlan() plan {
	for _, item := range m.list.Items() {
		if p, ok := item.(plan); ok && m.selected[p.file] {
			return p
		}
	}
	return plan{}
}

func (m model) previewW() int {
	return m.width - (m.width * 40 / 100) - 2
}

// renderWindow renders the selected plan plus a few neighbors (±2) if not cached.
// The selected plan gets its own goroutine for fast first paint; neighbors render
// in parallel so they're warm by the time the user navigates to them.
func (m model) renderWindow() tea.Cmd {
	items := m.list.Items()
	idx := m.list.Index()
	if len(items) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for i := idx - 2; i <= idx+2; i++ {
		if i < 0 || i >= len(items) {
			continue
		}
		p, ok := items[i].(plan)
		if !ok {
			continue
		}
		if _, cached := m.previewCache[p.file]; cached {
			continue
		}
		if m.demo.active {
			md, ok := m.demo.content[p.file]
			if !ok {
				md = "*No preview available*"
			}
			cmds = append(cmds, renderMarkdown(p.file, md, m.glamourStyle, m.previewW()))
		} else {
			cmds = append(cmds, renderPlan(m.dir, p.file, m.glamourStyle, m.previewW()))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// ─── Modal Key Handlers ──────────────────────────────────────────────────────

func (m model) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if item, ok := m.list.SelectedItem().(plan); ok {
			m.confirmDelete = false
			m.clearStatus()
			return m, m.cmdDelete(item)
		}
	case "n", "esc":
		m.confirmDelete = false
		m.clearStatus()
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.confirmDelete = false
		m.clearStatus()
		return m, nil
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleProjectInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit
	case msg.Type == tea.KeyEsc:
		m.settingProject = false
		return m, nil
	case msg.Type == tea.KeyEnter:
		proj := strings.TrimSpace(m.projectInput.Value())
		m.settingProject = false
		if proj != "" {
			return m, m.applyProject(proj)
		}
		return m, nil
	case msg.Type == tea.KeyBackspace && m.projectInput.Value() == "":
		// Backspace on empty → clear project
		m.settingProject = false
		return m, m.applyProject("")
	default:
		// Number keys 1-9 when input is empty → pick from choices
		if m.projectInput.Value() == "" && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			r := msg.Runes[0]
			if r == '0' {
				m.settingProject = false
				return m, m.applyProject("")
			}
			if r >= '1' && r <= '9' {
				idx := int(r - '1')
				if idx < len(m.projectChoices) {
					m.settingProject = false
					return m, m.applyProject(m.projectChoices[idx])
				}
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.projectInput, cmd = m.projectInput.Update(msg)
		return m, cmd
	}
}

// handleSelectMode handles keys when items are selected.
// Returns handled=true if the key was consumed and the caller should return.
func (m model) handleSelectMode(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit, true
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit, true
	case msg.String() == "esc":
		clear(m.selected)
		return m, nil, true
	case key.Matches(msg, m.keys.CycleStatus):
		first := m.firstSelectedPlan()
		target := nextStatus[first.status]
		if target == "" {
			target = "pending"
		}
		files := m.selectedFiles()
		return m, m.cmdBatchSetStatus(files, target), true
	case msg.String() == "0":
		files := m.selectedFiles()
		return m, m.cmdBatchSetStatus(files, ""), true
	case msg.String() == "1":
		files := m.selectedFiles()
		return m, m.cmdBatchSetStatus(files, "pending"), true
	case msg.String() == "2":
		files := m.selectedFiles()
		return m, m.cmdBatchSetStatus(files, "active"), true
	case msg.String() == "3":
		files := m.selectedFiles()
		return m, m.cmdBatchSetStatus(files, "done"), true
	case key.Matches(msg, m.keys.Project):
		m.settingProject = true
		m.projectChoices = recentProjects(*m.planSource())
		m.projectInput.SetValue("")
		m.projectInput.Focus()
		return m, textinput.Blink, true
	case msg.String() == "a":
		for _, item := range m.list.Items() {
			if p, ok := item.(plan); ok {
				m.selected[p.file] = true
			}
		}
		return m, nil, true
	case key.Matches(msg, m.keys.Select):
		if item, ok := m.list.SelectedItem().(plan); ok {
			if m.selected[item.file] {
				delete(m.selected, item.file)
			} else {
				m.selected[item.file] = true
			}
			m.list.CursorDown()
		}
		return m, nil, true
	}
	// Fall through for j/k navigation, ?, etc.
	return m, nil, false
}

// ─── Key Handling ─────────────────────────────────────────────────────────────

// handleKeyMsg processes keyboard input, returning handled=true for keys that
// should short-circuit Update (modals, commands, etc.) and handled=false for
// keys that should fall through to list.Update for default navigation/search.
func (m model) handleKeyMsg(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	// Settings — accessible from anywhere
	if key.Matches(msg, m.keys.Settings) {
		m.help.ShowAll = false
		m.confirmDelete = false
		m.settingProject = false
		exe, err := os.Executable()
		if err != nil {
			return m, func() tea.Msg { return errMsg{fmt.Errorf("could not find executable: %w", err)} }, true
		}
		c := exec.Command(exe, "--setup")
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			if err != nil {
				return errMsg{fmt.Errorf("setup failed: %w", err)}
			}
			return configUpdatedMsg{}
		}), true
	}

	// Clod fake AI screen — swallows all input when active
	if m.clod.active {
		return m.handleClodKey(msg)
	}

	// Release notes modal
	if m.releaseNotes.on {
		switch {
		case key.Matches(msg, m.keys.ForceQuit):
			return m, tea.Quit, true
		case key.Matches(msg, m.keys.Quit), msg.Type == tea.KeyEsc, msg.Type == tea.KeyEnter:
			m.releaseNotes.on = false
			return m, markReleaseNotesSeen(m.releaseNotes.version), true
		case key.Matches(msg, m.keys.ScrollDown):
			m.releaseNotes.viewport.HalfViewDown()
			return m, nil, true
		case key.Matches(msg, m.keys.ScrollUp):
			m.releaseNotes.viewport.HalfViewUp()
			return m, nil, true
		}
		switch msg.String() {
		case "j", "down":
			m.releaseNotes.viewport.LineDown(1)
			return m, nil, true
		case "k", "up":
			m.releaseNotes.viewport.LineUp(1)
			return m, nil, true
		}
		return m, nil, true
	}

	// Space / shift+space — scroll preview regardless of pane focus
	if !m.help.ShowAll && !m.confirmDelete && !m.settingProject && !m.list.SettingFilter() {
		switch {
		case key.Matches(msg, m.keys.ScrollDown):
			m.viewport.HalfViewDown()
			return m, nil, true
		case key.Matches(msg, m.keys.ScrollUp):
			m.viewport.HalfViewUp()
			return m, nil, true
		}
	}

	// Demo toggle — accessible from any pane, blocked during modals/filters
	if key.Matches(msg, m.keys.Demo) && !m.list.SettingFilter() && !m.list.IsFiltered() && !m.confirmDelete && !m.settingProject {
		if m.demo.active {
			m.exitDemoMode()
			return m, m.renderWindow(), true
		}
		m.enterDemoMode()
		return m, m.renderWindow(), true
	}

	// Help modal — swallow everything except ?, esc, q
	if m.help.ShowAll {
		switch {
		case key.Matches(msg, m.keys.Help) || msg.String() == "esc":
			m.help.ShowAll = false
		case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.ForceQuit):
			return m, tea.Quit, true
		}
		return m, nil, true
	}

	if m.confirmDelete {
		mod, cmd := m.handleDeleteConfirm(msg)
		return mod.(model), cmd, true
	}
	if m.settingProject {
		mod, cmd := m.handleProjectInput(msg)
		return mod.(model), cmd, true
	}

	filtering := m.list.SettingFilter()

	if len(m.selected) > 0 {
		if mod, cmd, handled := m.handleSelectMode(msg); handled {
			return mod, cmd, true
		}
	}

	// Preview pane: scrolling
	if m.focused == previewPane && !filtering {
		switch msg.String() {
		case "j", "down":
			m.viewport.LineDown(1)
			return m, nil, true
		case "k", "up":
			m.viewport.LineUp(1)
			return m, nil, true
		case "pgdown":
			m.viewport.HalfViewDown()
			return m, nil, true
		case "u", "pgup":
			m.viewport.HalfViewUp()
			return m, nil, true
		}
		switch {
		case key.Matches(msg, m.keys.SwitchPane):
			m.focused = listPane
			return m, nil, true
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = true
			return m, nil, true
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit, true
		case key.Matches(msg, m.keys.ForceQuit):
			return m, tea.Quit, true
		}
		return m, nil, true
	}

	// List pane keys
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit, true
	case key.Matches(msg, m.keys.Quit):
		if !filtering {
			return m, tea.Quit, true
		}
	case key.Matches(msg, m.keys.Help):
		if !filtering {
			m.help.ShowAll = true
			return m, nil, true
		}
	case key.Matches(msg, m.keys.SwitchPane):
		if !filtering {
			m.focused = previewPane
			return m, nil, true
		}
	case msg.String() == "esc":
		if !filtering && (m.showDone || m.projectFilter != "") {
			m.showDone = false
			m.projectFilter = ""
			if !m.demo.active && m.cfg.ShowAll {
				m.cfg.ShowAll = false
				if path, err := configPath(); err == nil {
					saveConfig(path, m.cfg)
				}
			}
			visible := m.visiblePlans()
			m.list.SetItems(plansToItems(visible))
			m.list.ResetSelected()
			m.restoreTitle()
			return m, nil, true
		}
	case key.Matches(msg, m.keys.CycleStatus):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				status := nextStatus[item.status]
				if status == "" {
					status = "pending"
				}
				return m, m.cmdSetStatus(item, status), true
			}
		}
	case key.Matches(msg, m.keys.RevStatus):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				prev := prevStatus[item.status]
				if prev == "" {
					prev = "done"
				}
				return m, m.cmdSetStatus(item, prev), true
			}
		}
	case msg.String() == "0" || msg.String() == "1" || msg.String() == "2" || msg.String() == "3":
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				status := map[string]string{"0": "", "1": "pending", "2": "active", "3": "done"}[msg.String()]
				if item.status == status {
					return m, nil, true
				}
				return m, m.cmdSetStatus(item, status), true
			}
		}
	case key.Matches(msg, m.keys.Undo):
		if !filtering && m.lastStatusChange != nil {
			target := m.lastStatusChange.oldPlan.status
			p := m.lastStatusChange.newPlan
			m.lastStatusChange = nil
			m.clearStatus()
			return m, m.cmdSetStatus(p, target), true
		}
	case key.Matches(msg, m.keys.ToggleDone):
		if !filtering {
			m.showDone = !m.showDone
			if !m.demo.active {
				m.cfg.ShowAll = m.showDone
				if path, err := configPath(); err == nil {
					saveConfig(path, m.cfg)
				}
			}
			visible := m.visiblePlans()
			m.list.SetItems(plansToItems(visible))
			m.list.ResetSelected()
			m.restoreTitle()
			if file := m.selectedFile(); file != "" {
				if content, ok := m.previewCache[file]; ok {
					m.viewport.SetContent(content)
					m.viewport.GotoTop()
				}
			}
			return m, nil, true
		}
	case key.Matches(msg, m.keys.NextProject), key.Matches(msg, m.keys.PrevProject):
		if !filtering {
			projects := recentProjects(*m.planSource())
			if len(projects) > 0 {
				forward := key.Matches(msg, m.keys.NextProject)
				cur := m.projectFilter
				idx := -1
				for i, p := range projects {
					if p == cur {
						idx = i
						break
					}
				}
				if forward {
					if idx < len(projects)-1 {
						m.projectFilter = projects[idx+1]
					} else {
						m.projectFilter = ""
					}
				} else {
					if idx <= 0 && cur != "" {
						m.projectFilter = ""
					} else if cur == "" {
						m.projectFilter = projects[len(projects)-1]
					} else {
						m.projectFilter = projects[idx-1]
					}
				}
				m.restoreTitle()
				visible := m.visiblePlans()
				m.list.SetItems(plansToItems(visible))
				m.list.ResetSelected()
				return m, m.renderWindow(), true
			}
		}
	case key.Matches(msg, m.keys.Project):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				m.settingProject = true
				m.projectChoices = recentProjects(*m.planSource())
				m.projectInput.SetValue(item.project)
				m.projectInput.Focus()
				m.projectInput.CursorEnd()
				return m, textinput.Blink, true
			}
		}
	case key.Matches(msg, m.keys.Delete):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				m.confirmDelete = true
				m.status.id++
				m.status.text = fmt.Sprintf("Delete %s? (y/n)", item.file)
				return m, m.status.spinner.Tick, true
			}
		}
	case key.Matches(msg, m.keys.CopyFile):
		if !filtering && !m.demo.active {
			if item, ok := m.list.SelectedItem().(plan); ok {
				path := filepath.Join(m.dir, item.file)
				if err := clipboard.WriteAll(path); err != nil {
					return m, func() tea.Msg { return errMsg{fmt.Errorf("clipboard: %w", err)} }, true
				}
				return m, m.setStatus("Copied: "+path, statusTimeout), true
			}
		}
	case key.Matches(msg, m.keys.Select):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				m.selected[item.file] = true
				m.list.CursorDown()
			}
		}
	}

	// Demo mode: enter/e opens fake Clod Code screen
	if !filtering && m.demo.active {
		if key.Matches(msg, m.keys.Primary) || key.Matches(msg, m.keys.Editor) {
			if item, ok := m.list.SelectedItem().(plan); ok {
				cmd := m.enterClod(item)
				return m, cmd, true
			}
		}
	}

	// Config-driven openers
	if !filtering && !m.demo.active {
		var cmd []string
		var prefix string
		switch {
		case key.Matches(msg, m.keys.Primary):
			cmd = m.cfg.Primary
			prefix = m.cfg.Preamble
		case key.Matches(msg, m.keys.Editor):
			cmd = m.cfg.Editor
		}
		if len(cmd) > 0 {
			if item, ok := m.list.SelectedItem().(plan); ok {
				args := expandCommand(cmd, filepath.Join(m.dir, item.file), prefix)
				c := shellCommand(args...)
				dir := m.dir
				return m, tea.ExecProcess(c, func(err error) tea.Msg {
					if err != nil {
						return errMsg{fmt.Errorf("command failed: %w", err)}
					}
					return reloadPlans(dir)
				}), true
			}
		}
	}

	// Not handled — fall through to list.Update for default navigation/search
	return m, nil, false
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		mod, cmd, handled := m.handleKeyMsg(msg)
		m = mod // Always apply model changes (e.g. select toggle)
		if handled {
			return m, cmd
		}

	case tea.MouseMsg:
		if m.clod.active || msg.Action != tea.MouseActionPress {
			return m, nil
		}
		listW := m.width * 40 / 100
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if msg.X < listW {
				m.list.CursorUp()
			} else {
				m.viewport.LineUp(3)
			}
		case tea.MouseButtonWheelDown:
			if msg.X < listW {
				m.list.CursorDown()
			} else {
				m.viewport.LineDown(3)
			}
		default:
			return m, nil
		}
		if msg.X < listW && m.list.Index() != m.prevIndex {
			m.prevIndex = m.list.Index()
			if file := m.selectedFile(); file != "" {
				if content, ok := m.previewCache[file]; ok {
					m.viewport.SetContent(content)
					m.viewport.GotoTop()
				}
			}
			cmds = append(cmds, m.renderWindow())
		}
		return m, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		listW := m.width * 40 / 100
		innerListW := listW - 2
		innerPreviewW := m.previewW()
		innerH := m.height - 3 // -2 for borders, -1 for hint bar

		if innerListW < 10 {
			innerListW = 10
		}
		if innerPreviewW < 10 {
			innerPreviewW = 10
		}
		if innerH < 5 {
			innerH = 5
		}

		m.list.SetSize(innerListW, innerH-1)
		m.viewport.Width = innerPreviewW
		m.viewport.Height = innerH - 1
		m.restoreTitle()
		m.refreshReleaseNotesView()

		if !m.prerendered || m.previewWidth != innerPreviewW {
			m.prerendered = true
			m.previewWidth = innerPreviewW
			m.previewCache = make(map[string]string)
			cmds = append(cmds, m.renderWindow())
		}

	case planContentMsg:
		isRefresh := m.refreshing[msg.file]
		delete(m.refreshing, msg.file)
		m.previewCache[msg.file] = msg.content
		if msg.file == m.selectedFile() {
			if isRefresh {
				off := m.viewport.YOffset
				m.viewport.SetContent(msg.content)
				m.viewport.SetYOffset(off)
			} else {
				m.viewport.SetContent(msg.content)
				m.viewport.GotoTop()
			}
		}
		return m, nil

	case statusUpdatedMsg:
		m.lastStatusChange = &msg
		plans := m.planSource()
		for i, p := range *plans {
			if p.file == msg.newPlan.file {
				updated := msg.newPlan
				updated.modified = time.Now()
				(*plans)[i] = updated
				break
			}
		}
		visible := m.visiblePlans()
		m.list.SetItems(plansToItems(visible))
		m.selectFile(msg.newPlan.file)
		statusText := fmt.Sprintf("%s → %s (u to undo)", msg.newPlan.file, msg.newPlan.status)
		cmd := m.setStatus(statusText, 0)
		id := m.status.id
		return m, tea.Batch(
			cmd,
			tea.Tick(statusTimeout, func(time.Time) tea.Msg {
				return undoExpiredMsg{id: id}
			}),
		)

	case projectUpdatedMsg:
		plans := m.planSource()
		for i, p := range *plans {
			if p.file == msg.plan.file {
				updated := msg.plan
				updated.modified = time.Now()
				(*plans)[i] = updated
				break
			}
		}
		visible := m.visiblePlans()
		m.list.SetItems(plansToItems(visible))
		m.selectFile(msg.plan.file)
		return m, m.setStatus(fmt.Sprintf("Project set: %s", msg.plan.project), statusTimeout)

	case batchDoneMsg:
		plans := m.planSource()
		*plans = msg.plans
		sortPlans(*plans)
		m.batchKeepFiles = msg.files
		visible := m.visiblePlans()
		m.list.SetItems(plansToItems(visible))
		m.previewCache = make(map[string]string)
		m.prerendered = true
		cmds = append(cmds, m.renderWindow())
		cmd := m.setStatus(msg.message, statusTimeout)
		id := m.status.id
		cmds = append(cmds, cmd)
		cmds = append(cmds, tea.Tick(statusTimeout, func(time.Time) tea.Msg {
			return batchLingerExpiredMsg{id: id}
		}))
		return m, tea.Batch(cmds...)

	case batchLingerExpiredMsg:
		if len(m.batchKeepFiles) > 0 && msg.id == m.status.id {
			m.batchKeepFiles = nil
			visible := m.visiblePlans()
			idx := m.list.Index()
			m.list.SetItems(plansToItems(visible))
			if idx >= len(visible) && len(visible) > 0 {
				m.list.Select(len(visible) - 1)
			}
			m.pruneSelection()
		}
		return m, nil

	case undoExpiredMsg:
		if m.lastStatusChange != nil && msg.id == m.status.id {
			m.lastStatusChange = nil
			m.clearStatus()
			visible := m.visiblePlans()
			idx := m.list.Index()
			m.list.SetItems(plansToItems(visible))
			if idx >= len(visible) && len(visible) > 0 {
				m.list.Select(len(visible) - 1)
			}
			m.pruneSelection()
		}
		return m, nil

	case changedSpinExpiredMsg:
		if msg.id == m.changedSpinID {
			clear(m.changedFiles)
			*m.changedSpinView = ""
		}
		return m, nil

	case clodTickMsg:
		if msg.id != m.clod.tickID {
			return m, nil
		}
		return m, m.advanceClod()

	case fileChangedMsg:
		// Re-scan plans from disk and re-render nearby previews.
		// Preserves cursor position and scroll offset for refreshed files.
		if !m.demo.active {
			prevFile := m.selectedFile()
			clear(m.selected)
			plans, err := scanPlans(m.dir)
			if err == nil {
				m.allPlans = plans
				sortPlans(m.allPlans)
				visible := filterPlans(plans, m.showDone, m.keepFiles(), m.projectFilter, m.installed)
				m.list.SetItems(plansToItems(visible))
				m.selectFile(prevFile)
				m.refreshing = make(map[string]bool)
				items := m.list.Items()
				listIdx := m.list.Index()
				for i := listIdx - 2; i <= listIdx+2; i++ {
					if i < 0 || i >= len(items) {
						continue
					}
					if p, ok := items[i].(plan); ok {
						if _, wasCached := m.previewCache[p.file]; wasCached {
							m.refreshing[p.file] = true
						}
						delete(m.previewCache, p.file)
					}
				}
				cmds = append(cmds, m.renderWindow())

				if len(msg.files) > 0 {
					for _, f := range msg.files {
						m.changedFiles[f] = true
					}
					m.changedSpinID++
					id := m.changedSpinID
					cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
						return changedSpinExpiredMsg{id: id}
					}))
					label := msg.files[0]
					if len(msg.files) > 1 {
						label = fmt.Sprintf("%d files", len(msg.files))
					}
					cmds = append(cmds, m.setStatus("Updated: "+label, 3*time.Second))
				}
			}
		}
		if m.watcher != nil {
			cmds = append(cmds, watchDir(m.watcher))
		}
		return m, tea.Batch(cmds...)

	case reloadMsg:
		clear(m.selected)
		plans := m.planSource()
		*plans = msg.plans
		sortPlans(*plans)
		visible := m.visiblePlans()
		m.list.SetItems(plansToItems(visible))
		m.previewCache = make(map[string]string)
		m.prerendered = true
		if len(visible) == 0 {
			m.viewport.SetContent("")
		}
		cmds = append(cmds, m.renderWindow())
		return m, tea.Batch(cmds...)

	case configUpdatedMsg:
		clear(m.selected)
		cfg := loadConfig()
		m.cfg = cfg
		m.keys = newKeyMap(cfg)
		if cfg.PlansDir != m.dir {
			plans, err := scanPlans(cfg.PlansDir)
			if err == nil {
				oldDir := m.dir
				m.dir = cfg.PlansDir
				if m.watcher != nil {
					_ = m.watcher.Remove(oldDir)
					_ = m.watcher.Add(m.dir)
				}
				m.allPlans = plans
				sortPlans(m.allPlans)
				visible := filterPlans(plans, m.showDone, m.keepFiles(), m.projectFilter, m.installed)
				m.list.SetItems(plansToItems(visible))
				m.previewCache = make(map[string]string)
				cmds = append(cmds, m.renderWindow())
			} else {
				cmds = append(cmds, m.setStatus("Error: "+err.Error(), statusTimeout))
			}
		}
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		if m.status.text != "" || len(m.changedFiles) > 0 {
			var cmd tea.Cmd
			m.status.spinner, cmd = m.status.spinner.Update(msg)
			*m.changedSpinView = m.status.spinner.View()
			return m, cmd
		}
		return m, nil

	case statusClearMsg:
		if msg.id == m.status.id {
			m.clearStatus()
		}
		return m, nil

	case updateAvailableMsg:
		m.updateAvailable = &msg
		return m, nil

	case releaseNotesMsg:
		m.releaseNotes.on = true
		m.releaseNotes.version = msg.version
		m.releaseNotes.markdown = msg.markdown
		m.refreshReleaseNotesView()
		return m, nil

	case startupUpdateMsg:
		if msg.update != nil {
			m.updateAvailable = msg.update
		}
		if msg.releaseNotes != nil {
			m.releaseNotes.on = true
			m.releaseNotes.version = msg.releaseNotes.version
			m.releaseNotes.markdown = msg.releaseNotes.markdown
			m.refreshReleaseNotesView()
		}
		return m, nil

	case errMsg:
		return m, m.setStatus(fmt.Sprintf("Error: %v", msg.err), statusTimeout)
	}

	// Search: temporarily show all plans so filter matches across done/hidden items.
	// On search exit (esc or empty filter), restore the active visibility filter.
	wasSearching := m.list.SettingFilter() || m.list.IsFiltered()
	if kmsg, isKey := msg.(tea.KeyMsg); isKey && !wasSearching && key.Matches(kmsg, m.keys.Filter) {
		m.list.SetItems(plansToItems(*m.planSource()))
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	if isSearching := m.list.SettingFilter() || m.list.IsFiltered(); wasSearching && !isSearching {
		m.list.SetItems(plansToItems(m.visiblePlans()))
	}

	m.restoreTitle()
	m.updateHelpKeys()

	if m.settingProject {
		var tiCmd tea.Cmd
		m.projectInput, tiCmd = m.projectInput.Update(msg)
		cmds = append(cmds, tiCmd)
	}

	// On cursor change, swap the preview to the newly selected plan.
	// Cached content is shown immediately; uncached triggers renderWindow.
	if m.list.Index() != m.prevIndex {
		m.prevIndex = m.list.Index()
		if file := m.selectedFile(); file != "" {
			if content, ok := m.previewCache[file]; ok {
				m.viewport.SetContent(content)
				m.viewport.GotoTop()
			}
		}
		cmds = append(cmds, m.renderWindow())
	}

	return m, tea.Batch(cmds...)
}
