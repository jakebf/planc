package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	OpenStatus  key.Binding
	CycleStatus key.Binding
	SetStatus   key.Binding // 0-3 direct status set (display-only binding)
	Undo        key.Binding
	ToggleDone  key.Binding
	Labels      key.Binding
	Delete      key.Binding
	Primary     key.Binding
	Editor      key.Binding
	Filter      key.Binding
	CopyFile    key.Binding
	PrevLabel key.Binding
	NextLabel key.Binding
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
		OpenStatus:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status")),
		CycleStatus: key.NewBinding(key.WithKeys("~"), key.WithHelp("~", "cycle status")),
		SetStatus:   key.NewBinding(key.WithKeys("0", "1", "2", "3"), key.WithHelp("0-3", "set status")),
		Undo:        key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undo status")),
		ToggleDone:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle done plans")),
		Labels:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "labels")),
		Delete:      key.NewBinding(key.WithKeys("#"), key.WithHelp("#", "delete plan")),
		Primary:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", commandLabel(cfg.Primary))),
		Editor:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", commandLabel(cfg.Editor))),
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		CopyFile:    key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "copy path")),
		PrevLabel: key.NewBinding(key.WithKeys("["), key.WithHelp("[/]", "cycle label filter")),
		NextLabel: key.NewBinding(key.WithKeys("]")),
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
	return []key.Binding{k.Editor, k.Primary, k.CopyFile, k.OpenStatus, k.Labels, k.Select, k.Filter, k.Help}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Essentials
		{k.Editor, k.Primary, k.CopyFile, k.OpenStatus, k.Labels, k.Select, k.ToggleDone, k.Filter, k.PrevLabel},
		// Power user
		{k.Navigate, k.SwitchPane, k.ScrollDown, k.ScrollUp, k.CycleStatus, k.SetStatus, k.Undo, k.Delete, k.Settings, k.Quit},
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
	labelFilter string

	// Cursor and selection
	prevIndex    int             // tracks cursor changes to trigger preview updates
	selected     map[string]bool // files toggled with 'x' for batch operations
	changedFiles map[string]bool // files recently changed externally (spinner on badge)
	changedSpinID   int
	changedSpinView *string // shared with delegate for spinner frame

	// Modals and transient state
	confirmDelete    bool
	lastStatusChange *statusUpdatedMsg // non-nil during undo window
	batchKeepFiles   []string          // keeps batch-affected items visible until linger expires

	// Label modal
	settingLabels  bool
	labelInput     textinput.Model
	labelChoices   []string        // all known labels
	labelToggled   map[string]bool // tracks which labels are toggled (on = all have it)
	labelMixed     map[string]bool // tracks mixed state in batch mode (some but not all)
	labelCursor    int
	labelBatchMode bool            // true when multiple plans selected
	labelDirty     bool            // true when user has toggled/added a label
	labelFlashIdx  int             // index flashing after enter toggle (-1 = none)
	labelFlashTick int             // remaining flash ticks

	// Inline feedback
	undoFiles      map[string]string // filename → new status (shown inline on plan row during undo window)
	copiedFiles    map[string]bool   // filenames with "Copied!" inline indicator
	copiedID       int               // generation counter for copied clear timer
	notification   string            // right-aligned notification on hint bar
	notificationID int               // generation counter for notification clear timer
	undoID         int               // generation counter for undo expiration
	batchLingerID  int               // generation counter for batch linger expiration

	// Status modal
	settingStatus     bool
	statusModalCursor int

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
		return filterPlans(m.demo.plans, m.showDone, m.keepFiles(), m.labelFilter, fakeInstalled)
	}
	return filterPlans(m.allPlans, m.showDone, m.keepFiles(), m.labelFilter, m.installed)
}

func newModel(plans []plan, dir string, cfg config, watcher *fsnotify.Watcher) model {
	sel := make(map[string]bool)
	chg := make(map[string]bool)
	uf := make(map[string]string)
	cf := make(map[string]bool)
	var installed time.Time
	if cfg.Installed != "" {
		installed, _ = time.Parse(time.RFC3339, cfg.Installed)
	}
	sortPlans(plans)
	var spinView string
	delegate := planDelegate{selected: sel, changed: chg, undoFiles: uf, copiedFiles: cf, spinnerView: &spinView}
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

	li := textinput.New()
	li.Prompt = ""
	li.CharLimit = 50
	li.Width = 30
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
		undoFiles:       uf,
		copiedFiles:     cf,
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
		labelInput:      li,
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

// setNotification shows a right-aligned notification on the hint bar that auto-clears.
func (m *model) setNotification(text string, duration time.Duration) tea.Cmd {
	m.notificationID++
	m.notification = text
	id := m.notificationID
	if duration > 0 {
		return tea.Tick(duration, func(time.Time) tea.Msg {
			return notificationClearMsg{id: id}
		})
	}
	return nil
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
	if m.labelFilter != "" {
		left += " " + labelColor(m.labelFilter).Render(m.labelFilter)
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

func (m model) cmdSetLabels(p plan, labels []string) tea.Cmd {
	return m.store.setLabels(p, labels)
}

func (m model) cmdBatchSetStatus(files []string, status string) tea.Cmd {
	return m.store.batchSetStatus(files, status)
}

func (m model) cmdBatchUpdateLabels(files []string, add []string, remove []string) tea.Cmd {
	return m.store.batchUpdateLabels(files, add, remove)
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
			m.notification = ""
			return m, tea.Batch(
				m.cmdDelete(item),
				m.setNotification("Deleted: "+item.file, 3*time.Second),
			)
		}
	case "n", "esc":
		m.confirmDelete = false
		m.notification = ""
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.confirmDelete = false
		m.notification = ""
		return m, nil
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit
	}
	return m, nil
}

// statusOptions maps cursor index to status values for the status modal.
var statusOptions = []struct {
	key    string
	icon   string
	label  string
	status string
}{
	{"0", "·", "unset", ""},
	{"1", "○", "pending", "pending"},
	{"2", "●", "active", "active"},
	{"3", "✓", "done", "done"},
}

func statusCursorForStatus(s string) int {
	for i, opt := range statusOptions {
		if opt.status == s {
			return i
		}
	}
	return 0
}

func (m model) handleStatusModal(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit, true
	case msg.Type == tea.KeyEsc:
		m.settingStatus = false
		return m, nil, true
	case msg.Type == tea.KeyEnter:
		m.settingStatus = false
		return m, m.applyStatus(statusOptions[m.statusModalCursor].status), true
	case msg.String() == "0":
		m.settingStatus = false
		return m, m.applyStatus(""), true
	case msg.String() == "1":
		m.settingStatus = false
		return m, m.applyStatus("pending"), true
	case msg.String() == "2":
		m.settingStatus = false
		return m, m.applyStatus("active"), true
	case msg.String() == "3":
		m.settingStatus = false
		return m, m.applyStatus("done"), true
	case msg.String() == "j" || msg.String() == "down":
		if m.statusModalCursor < len(statusOptions)-1 {
			m.statusModalCursor++
		}
		return m, nil, true
	case msg.String() == "k" || msg.String() == "up":
		if m.statusModalCursor > 0 {
			m.statusModalCursor--
		}
		return m, nil, true
	}
	return m, nil, true
}

func (m model) applyStatus(status string) tea.Cmd {
	if len(m.selected) > 0 {
		files := m.selectedFiles()
		return m.cmdBatchSetStatus(files, status)
	}
	if item, ok := m.list.SelectedItem().(plan); ok {
		if item.status == status {
			return nil
		}
		return m.cmdSetStatus(item, status)
	}
	return nil
}

// ─── Label Modal ─────────────────────────────────────────────────────────────

func (m *model) openLabelModal(batchMode bool) {
	m.settingLabels = true
	m.labelBatchMode = batchMode
	m.labelChoices = recentLabels(*m.planSource())
	m.labelToggled = make(map[string]bool)
	m.labelMixed = make(map[string]bool)
	m.labelDirty = false

	if batchMode && len(m.selected) > 0 {
		// Count how many selected plans have each label
		counts := make(map[string]int)
		total := 0
		for _, item := range m.list.Items() {
			if p, ok := item.(plan); ok && m.selected[p.file] {
				total++
				for _, l := range p.labels {
					counts[l]++
				}
			}
		}
		for l, c := range counts {
			if c == total {
				m.labelToggled[l] = true
			} else {
				m.labelMixed[l] = true
			}
		}
	} else if item, ok := m.list.SelectedItem().(plan); ok {
		for _, l := range item.labels {
			m.labelToggled[l] = true
		}
	}

	m.labelCursor = 0
	m.labelFlashIdx = -1
	m.labelFlashTick = 0
	m.labelInput.SetValue("")
	m.labelInput.Focus()
}

// filteredLabelChoices returns label choices filtered by the current input.
func (m model) filteredLabelChoices() []string {
	filter := strings.ToLower(strings.TrimSpace(m.labelInput.Value()))
	if filter == "" {
		return m.labelChoices
	}
	var filtered []string
	for _, l := range m.labelChoices {
		if strings.Contains(l, filter) {
			filtered = append(filtered, l)
		}
	}
	return filtered
}

func (m model) handleLabelModal(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	// Ignore keys during flash animation
	if m.labelFlashTick > 0 {
		return m, nil, true
	}
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return m, tea.Quit, true
	case msg.Type == tea.KeyEsc:
		m.settingLabels = false
		if m.hasLabelChanges() {
			return m, m.applyLabelChanges(), true
		}
		return m, nil, true
	case msg.Type == tea.KeyEnter:
		filtered := m.filteredLabelChoices()
		filter := strings.ToLower(strings.TrimSpace(m.labelInput.Value()))
		if filter != "" && len(filtered) == 0 {
			// Create new label
			newLabel := filter
			m.labelToggled[newLabel] = true
			m.labelDirty = true
			m.settingLabels = false
			return m, m.applyLabelChanges(), true
		}
		if filter == "" && m.hasLabelChanges() {
			// Apply accumulated changes
			m.settingLabels = false
			return m, m.applyLabelChanges(), true
		}
		if len(filtered) > 0 && m.labelCursor < len(filtered) {
			// Toggle the label under cursor, flash, then dismiss
			l := filtered[m.labelCursor]
			if m.labelMixed[l] {
				delete(m.labelMixed, l)
				m.labelToggled[l] = true
			} else {
				m.labelToggled[l] = !m.labelToggled[l]
			}
			m.labelDirty = true
			m.labelFlashIdx = m.labelCursor
			m.labelFlashTick = 5 // 5 ticks × 80ms = 400ms
			return m, tea.Tick(80*time.Millisecond, func(_ time.Time) tea.Msg {
				return labelFlashMsg{}
			}), true
		}
		m.settingLabels = false
		return m, nil, true
	case msg.String() == " ":
		// Space = toggle without dismissing (accumulate mode)
		filtered := m.filteredLabelChoices()
		if m.labelCursor < len(filtered) {
			l := filtered[m.labelCursor]
			if m.labelMixed[l] {
				// mixed → on
				delete(m.labelMixed, l)
				m.labelToggled[l] = true
			} else {
				m.labelToggled[l] = !m.labelToggled[l]
			}
			m.labelDirty = true
		}
		return m, nil, true
	case msg.String() == "j" || msg.String() == "down":
		filtered := m.filteredLabelChoices()
		if m.labelCursor < len(filtered)-1 {
			m.labelCursor++
		}
		return m, nil, true
	case msg.String() == "k" || msg.String() == "up":
		if m.labelCursor > 0 {
			m.labelCursor--
		}
		return m, nil, true
	case msg.Type == tea.KeyBackspace:
		if m.labelInput.Value() == "" {
			m.settingLabels = false
			return m, nil, true
		}
		var cmd tea.Cmd
		m.labelInput, cmd = m.labelInput.Update(msg)
		m.labelCursor = 0
		return m, cmd, true
	default:
		var cmd tea.Cmd
		m.labelInput, cmd = m.labelInput.Update(msg)
		m.labelCursor = 0
		return m, cmd, true
	}
}

func (m model) hasLabelChanges() bool {
	// Compare toggled labels to current plan's labels
	if m.labelBatchMode {
		return m.labelDirty
	}
	if item, ok := m.list.SelectedItem().(plan); ok {
		current := make(map[string]bool)
		for _, l := range item.labels {
			current[l] = true
		}
		for l, on := range m.labelToggled {
			if on != current[l] {
				return true
			}
		}
		for l := range current {
			if !m.labelToggled[l] {
				return true
			}
		}
	}
	return false
}

func (m model) applyLabelChanges() tea.Cmd {
	if m.labelBatchMode && len(m.selected) > 0 {
		// Labels toggled on → add to all plans
		// Labels toggled off (not mixed) → remove from all plans
		// Labels still mixed → leave untouched (no add, no remove)
		var add, remove []string
		for l, on := range m.labelToggled {
			if on {
				add = append(add, l)
			}
		}
		for _, l := range m.labelChoices {
			if !m.labelToggled[l] && !m.labelMixed[l] {
				remove = append(remove, l)
			}
		}
		files := m.selectedFiles()
		return m.cmdBatchUpdateLabels(files, add, remove)
	}
	// Single plan: replace all labels
	var newLabels []string
	for l, on := range m.labelToggled {
		if on {
			newLabels = append(newLabels, l)
		}
	}
	// Sort for deterministic output
	sort.Strings(newLabels)
	if item, ok := m.list.SelectedItem().(plan); ok {
		return m.cmdSetLabels(item, newLabels)
	}
	return nil
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
	case key.Matches(msg, m.keys.OpenStatus):
		first := m.firstSelectedPlan()
		m.settingStatus = true
		m.statusModalCursor = statusCursorForStatus(first.status)
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
	case key.Matches(msg, m.keys.Labels):
		m.openLabelModal(true)
		return m, textinput.Blink, true
	case msg.String() == "a":
		for _, item := range m.list.Items() {
			if p, ok := item.(plan); ok {
				m.selected[p.file] = true
			}
		}
		return m, nil, true
	case key.Matches(msg, m.keys.CopyFile):
		if !m.demo.active {
			files := m.selectedFiles()
			var paths []string
			for _, f := range files {
				paths = append(paths, filepath.Join(m.dir, f))
			}
			if err := clipboard.WriteAll(strings.Join(paths, ", ")); err != nil {
				return m, func() tea.Msg { return errMsg{fmt.Errorf("clipboard: %w", err)} }, true
			}
			clear(m.copiedFiles)
			for _, f := range files {
				m.copiedFiles[f] = true
			}
			m.copiedID++
			id := m.copiedID
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
				return copiedClearMsg{id: id}
			}), true
		}
	case key.Matches(msg, m.keys.Select):
		if item, ok := m.list.SelectedItem().(plan); ok {
			if m.selected[item.file] {
				delete(m.selected, item.file)
			} else {
				m.selected[item.file] = true
			}
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
		m.settingLabels = false
		m.settingStatus = false
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
	if !m.help.ShowAll && !m.confirmDelete && !m.settingStatus && !m.settingLabels && !m.list.SettingFilter() {
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
	if key.Matches(msg, m.keys.Demo) && !m.list.SettingFilter() && !m.list.IsFiltered() && !m.confirmDelete && !m.settingStatus && !m.settingLabels {
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

	if m.settingLabels {
		return m.handleLabelModal(msg)
	}
	if m.settingStatus {
		return m.handleStatusModal(msg)
	}
	if m.confirmDelete {
		mod, cmd := m.handleDeleteConfirm(msg)
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
		case "left":
			m.focused = listPane
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
	case key.Matches(msg, m.keys.SwitchPane), msg.String() == "right":
		if !filtering {
			m.focused = previewPane
			return m, nil, true
		}
	case msg.String() == "esc":
		if !filtering && (m.showDone || m.labelFilter != "") {
			m.showDone = false
			m.labelFilter = ""
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
	case key.Matches(msg, m.keys.OpenStatus):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				m.settingStatus = true
				m.statusModalCursor = statusCursorForStatus(item.status)
				return m, nil, true
			}
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
			clear(m.undoFiles)
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
	case key.Matches(msg, m.keys.NextLabel), key.Matches(msg, m.keys.PrevLabel):
		if !filtering {
			labels := recentLabels(*m.planSource())
			if len(labels) > 0 {
				forward := key.Matches(msg, m.keys.NextLabel)
				cur := m.labelFilter
				idx := -1
				for i, l := range labels {
					if l == cur {
						idx = i
						break
					}
				}
				// Try candidates in cycle order, skipping labels with no visible plans
				tried := 0
				for tried <= len(labels) {
					if forward {
						if idx < len(labels)-1 {
							idx++
							m.labelFilter = labels[idx]
						} else {
							idx = -1
							m.labelFilter = ""
						}
					} else {
						if idx > 0 {
							idx--
							m.labelFilter = labels[idx]
						} else if idx == 0 || cur != "" {
							idx = -1
							m.labelFilter = ""
						} else {
							idx = len(labels) - 1
							m.labelFilter = labels[idx]
						}
					}
					cur = m.labelFilter
					tried++
					visible := m.visiblePlans()
					if len(visible) > 0 || m.labelFilter == "" {
						m.restoreTitle()
						m.list.SetItems(plansToItems(visible))
						m.list.ResetSelected()
						m.prevIndex = 0
						// Update viewport to show the new first item
						if file := m.selectedFile(); file != "" {
							if content, ok := m.previewCache[file]; ok {
								m.viewport.SetContent(content)
								m.viewport.GotoTop()
							}
						}
						return m, m.renderWindow(), true
					}
				}
			}
		}
	case key.Matches(msg, m.keys.Labels):
		if !filtering {
			if _, ok := m.list.SelectedItem().(plan); ok {
				m.openLabelModal(false)
				return m, textinput.Blink, true
			}
		}
	case key.Matches(msg, m.keys.Delete):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				m.confirmDelete = true
				m.notification = fmt.Sprintf("Delete %s? (y/n)", item.file)
				return m, nil, true
			}
		}
	case key.Matches(msg, m.keys.CopyFile):
		if !filtering && !m.demo.active {
			if item, ok := m.list.SelectedItem().(plan); ok {
				path := filepath.Join(m.dir, item.file)
				if err := clipboard.WriteAll(path); err != nil {
					return m, func() tea.Msg { return errMsg{fmt.Errorf("clipboard: %w", err)} }, true
				}
				clear(m.copiedFiles)
				m.copiedFiles[item.file] = true
				m.copiedID++
				id := m.copiedID
				return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
					return copiedClearMsg{id: id}
				}), true
			}
		}
	case key.Matches(msg, m.keys.Select):
		if !filtering {
			if item, ok := m.list.SelectedItem().(plan); ok {
				m.selected[item.file] = true
			}
		}
	}

	// Demo mode: enter/c opens fake Clod Code screen
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
		var cmdArgs []string
		var prefix string
		isEditor := false
		switch {
		case key.Matches(msg, m.keys.Primary):
			cmdArgs = m.cfg.Primary
			prefix = m.cfg.PromptPrefix
		case key.Matches(msg, m.keys.Editor):
			cmdArgs = m.cfg.Editor
			isEditor = true
		}
		if len(cmdArgs) > 0 {
			if item, ok := m.list.SelectedItem().(plan); ok {
				args := expandCommand(cmdArgs, filepath.Join(m.dir, item.file), prefix)
				if isEditor && effectiveEditorMode(m.cfg) == "background" {
					return m, runBackgroundEditor(args), true
				}
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
		// Inline indicator on the affected row (replaces date)
		statusLabel := msg.newPlan.status
		if statusLabel == "" {
			statusLabel = "unset"
		}
		m.undoFiles[msg.newPlan.file] = statusLabel
		m.undoID++
		undoID := m.undoID
		return m, tea.Batch(
			m.status.spinner.Tick,
			tea.Tick(statusTimeout, func(time.Time) tea.Msg {
				return undoExpiredMsg{id: undoID}
			}),
		)

	case labelsUpdatedMsg:
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
		label := strings.Join(msg.plan.labels, ", ")
		if label == "" {
			label = "cleared"
		}
		return m, m.setNotification("Labels: "+label, statusTimeout)

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
		cmds = append(cmds, m.setNotification(msg.message, statusTimeout))
		m.batchLingerID++
		batchID := m.batchLingerID
		cmds = append(cmds, tea.Tick(statusTimeout, func(time.Time) tea.Msg {
			return batchLingerExpiredMsg{id: batchID}
		}))
		clear(m.selected)
		return m, tea.Batch(cmds...)

	case batchLingerExpiredMsg:
		if len(m.batchKeepFiles) > 0 && msg.id == m.batchLingerID {
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
		if m.lastStatusChange != nil && msg.id == m.undoID {
			m.lastStatusChange = nil
			clear(m.undoFiles)
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
				visible := filterPlans(plans, m.showDone, m.keepFiles(), m.labelFilter, m.installed)
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
					// Only show "Updated:" for files that still exist (not deleted).
					planSet := make(map[string]bool)
					for _, p := range plans {
						planSet[p.file] = true
					}
					var changedFiles []string
					for _, f := range msg.files {
						if planSet[f] {
							changedFiles = append(changedFiles, f)
						}
					}
					for _, f := range changedFiles {
						m.changedFiles[f] = true
					}
					if len(changedFiles) > 0 {
						m.changedSpinID++
						id := m.changedSpinID
						cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
							return changedSpinExpiredMsg{id: id}
						}))
						label := changedFiles[0]
						if len(changedFiles) > 1 {
							label = fmt.Sprintf("%d files", len(changedFiles))
						}
						cmds = append(cmds, m.setNotification("Updated: "+label, 3*time.Second))
					}
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
				visible := filterPlans(plans, m.showDone, m.keepFiles(), m.labelFilter, m.installed)
				m.list.SetItems(plansToItems(visible))
				m.previewCache = make(map[string]string)
				cmds = append(cmds, m.renderWindow())
			} else {
				cmds = append(cmds, m.setNotification("Error: "+err.Error(), statusTimeout))
			}
		}
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		if len(m.undoFiles) > 0 || len(m.changedFiles) > 0 {
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

	case notificationClearMsg:
		if msg.id == m.notificationID {
			m.notification = ""
		}
		return m, nil

	case copiedClearMsg:
		if msg.id == m.copiedID {
			clear(m.copiedFiles)
		}
		return m, nil

	case labelFlashMsg:
		if m.labelFlashTick > 0 {
			m.labelFlashTick--
			if m.labelFlashTick > 0 {
				return m, tea.Tick(80*time.Millisecond, func(_ time.Time) tea.Msg {
					return labelFlashMsg{}
				})
			}
			// Flash done — dismiss and apply
			m.settingLabels = false
			m.labelFlashIdx = -1
			return m, m.applyLabelChanges()
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

	case editorLaunchedMsg:
		return m, m.setNotification("Editor opened", 2*time.Second)

	case errMsg:
		return m, m.setNotification(fmt.Sprintf("Error: %v", msg.err), statusTimeout)
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
