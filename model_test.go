package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func testPlans() []plan {
	now := time.Now()
	day := 24 * time.Hour
	return []plan{
		{status: "active", labels: []string{"kokua"}, title: "Material component migration playbook", created: now.Add(-1 * day), file: "humming-marinating-narwhal.md"},
		{status: "active", labels: []string{"pulse"}, title: "Synthetic EHR database generator", created: now.Add(-7 * day), file: "deep-crunching-sprout.md"},
		{status: "pending", labels: []string{"atlas"}, title: "Route optimization service", created: now.Add(-9 * day), file: "bright-sailing-otter.md"},
		{status: "done", labels: []string{"orion"}, title: "Legacy API deprecation tracker", created: now.Add(-22 * day), file: "calm-drifting-whale.md"},
	}
}

func testModel() model {
	plans := testPlans()
	m := newModel(plans, "/tmp/test-plans", newDefaultConfig(), nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = m2.(model)
	// Pre-populate cache with placeholder content
	for _, item := range m.list.Items() {
		if p, ok := item.(plan); ok {
			m.previewCache[p.file] = "# " + p.title + "\n\nTest content for " + p.title
		}
	}
	return m
}

// execCmd runs a tea.Cmd synchronously and feeds resulting messages back into the model.
// For tea.BatchMsg (from tea.Batch), it recursively executes each sub-command.
func execCmd(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	// tea.Batch returns a function that returns a tea.BatchMsg (which is []tea.Cmd)
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			execCmd(t, m, sub)
		}
		return
	}
	m2, newCmd := m.Update(msg)
	*m = m2.(model)
	if newCmd != nil {
		execCmd(t, m, newCmd)
	}
}

// TestProfileStartupAndNavigate profiles the real user flow:
// startup with real plans → toggle show all → navigate j/k.
// Run with: go test -run TestProfileStartupAndNavigate -v -cpuprofile cpu.prof -memprofile mem.prof
func TestProfileStartupAndNavigate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := defaultPlansDir()
	plans, err := scanPlans(dir)
	if err != nil || len(plans) == 0 {
		t.Skipf("no plans in %s (need real data for profiling)", dir)
	}
	t.Logf("profiling with %d real plans from %s", len(plans), dir)

	cfg := newDefaultConfig()
	cfg.PlansDir = dir
	m := newModel(plans, dir, cfg, nil)

	// WindowSizeMsg → triggers prerenderAll
	t0 := time.Now()
	m2, cmd := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = m2.(model)
	t.Logf("WindowSizeMsg: %v", time.Since(t0))

	// Execute prerender commands synchronously
	t0 = time.Now()
	if cmd != nil {
		execCmd(t, &m, cmd)
	}
	t.Logf("prerenderAll completed: %v (%d cached)", time.Since(t0), len(m.previewCache))

	// Press 'a' to show all
	t0 = time.Now()
	aKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m2, cmd = m.Update(aKey)
	m = m2.(model)
	if cmd != nil {
		execCmd(t, &m, cmd)
	}
	_ = m.View()
	t.Logf("toggle show all + view: %v (%d items)", time.Since(t0), len(m.list.Items()))

	// Navigate j/k 20 times
	jKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	kKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	for i := 0; i < 20; i++ {
		var k tea.KeyMsg
		if i%2 == 0 {
			k = jKey
		} else {
			k = kKey
		}
		t0 = time.Now()
		m2, _ = m.Update(k)
		m = m2.(model)
		_ = m.View()
		d := time.Since(t0)
		if d > 5*time.Millisecond {
			t.Logf("nav[%02d]: %v (SLOW)", i, d)
		}
	}
	t.Logf("navigation complete, cache size: %d", len(m.previewCache))
}

func BenchmarkUpdateJK(b *testing.B) {
	m := testModel()

	jKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	kKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var m2 tea.Model
		if i%2 == 0 {
			m2, _ = m.Update(jKey)
		} else {
			m2, _ = m.Update(kKey)
		}
		m = m2.(model)
	}
}

func BenchmarkViewSteadyState(b *testing.B) {
	m := testModel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFullCycleJKView(b *testing.B) {
	m := testModel()

	jKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m2, _ := m.Update(jKey)
		m = m2.(model)
		_ = m.View()
	}
}

func TestSelectToggle(t *testing.T) {
	m := testModel()
	plans := testPlans()

	xKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	jKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}

	// Press x — first item should be selected, cursor stays
	m2, _ := m.Update(xKey)
	m = m2.(model)

	if !m.selected[plans[0].file] {
		t.Errorf("expected %q to be selected after first x", plans[0].file)
	}
	if m.list.Index() != 0 {
		t.Errorf("expected cursor at 0 after x, got %d", m.list.Index())
	}

	// Move down, press x — second item selected
	m2, _ = m.Update(jKey)
	m = m2.(model)
	m2, _ = m.Update(xKey)
	m = m2.(model)
	if !m.selected[plans[1].file] {
		t.Errorf("expected %q to be selected after second x", plans[1].file)
	}
	if len(m.selected) != 2 {
		t.Errorf("expected 2 selected, got %d", len(m.selected))
	}

	// Press x again on same item — should deselect
	m2, _ = m.Update(xKey)
	m = m2.(model)
	if m.selected[plans[1].file] {
		t.Errorf("expected %q to be deselected after toggle", plans[1].file)
	}
	if len(m.selected) != 1 {
		t.Errorf("expected 1 selected after toggle, got %d", len(m.selected))
	}
}

func TestSelectAll(t *testing.T) {
	m := testModel()

	// Select one item first to enter select mode
	xKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	m2, _ := m.Update(xKey)
	m = m2.(model)

	// Press 'a' in select mode — selects all visible items
	aKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m2, _ = m.Update(aKey)
	m = m2.(model)

	visibleCount := len(m.list.Items())
	if len(m.selected) != visibleCount {
		t.Errorf("expected %d selected (all visible), got %d", visibleCount, len(m.selected))
	}
}

func TestSelectEscClears(t *testing.T) {
	m := testModel()

	// Select two items (x, j, x)
	xKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	jKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	m2, _ := m.Update(xKey)
	m = m2.(model)
	m2, _ = m.Update(jKey)
	m = m2.(model)
	m2, _ = m.Update(xKey)
	m = m2.(model)
	if len(m.selected) != 2 {
		t.Fatalf("expected 2 selected, got %d", len(m.selected))
	}

	// Press esc — clears selection
	escKey := tea.KeyMsg{Type: tea.KeyEsc}
	m2, _ = m.Update(escKey)
	m = m2.(model)
	if len(m.selected) != 0 {
		t.Errorf("expected 0 selected after esc, got %d", len(m.selected))
	}
}

func TestSelectCycleStatus(t *testing.T) {
	dir := t.TempDir()

	// Create plans — both active so the cycle result is deterministic (active → done)
	writeFile(t, filepath.Join(dir, "plan-a.md"), "---\nstatus: active\n---\n# Plan A\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "---\nstatus: active\n---\n# Plan B\n")

	plans, _ := scanPlans(dir)
	m := newModel(plans, dir, newDefaultConfig(), nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = m2.(model)

	// Select both items (x, j, x)
	xKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	jKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	m2, _ = m.Update(xKey)
	m = m2.(model)
	m2, _ = m.Update(jKey)
	m = m2.(model)
	m2, _ = m.Update(xKey)
	m = m2.(model)

	if len(m.selected) != 2 {
		t.Fatalf("expected 2 selected, got %d", len(m.selected))
	}

	// ~ cycles based on first selected: active → done
	tildeKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'~'}}
	m2, cmd := m.Update(tildeKey)
	m = m2.(model)

	// Selection should be preserved (esc to deselect)
	if len(m.selected) != 2 {
		t.Errorf("expected selection preserved after ~, got %d", len(m.selected))
	}

	// Execute the batch command and verify
	if cmd != nil {
		msg := cmd()
		if result, ok := msg.(batchDoneMsg); ok {
			if !strings.Contains(result.message, "done") {
				t.Errorf("expected status 'done' in message, got %q", result.message)
			}
		}
	}

	// Verify both files got status from cycle
	for _, file := range []string{"plan-a.md", "plan-b.md"} {
		data, _ := os.ReadFile(filepath.Join(dir, file))
		fields, _ := parseFrontmatter(string(data))
		if fields["status"] != "done" {
			t.Errorf("%s: status = %q, want done", file, fields["status"])
		}
	}
}

func TestStatusUpdateKeepsDoneVisibleUntilUndoExpires(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan-a.md"), "---\nstatus: active\n---\n# Plan A\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "---\nstatus: active\n---\n# Plan B\n")

	plans, err := scanPlans(dir)
	if err != nil {
		t.Fatalf("scanPlans: %v", err)
	}
	m := newModel(plans, dir, newDefaultConfig(), nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(model)

	m2, _ = m.Update(statusUpdatedMsg{
		oldPlan: plan{status: "active", file: "plan-a.md"},
		newPlan: plan{status: "done", file: "plan-a.md"},
	})
	m = m2.(model)

	if len(m.list.Items()) != 2 {
		t.Fatalf("expected done item to stay visible for undo, got %d items", len(m.list.Items()))
	}

	m2, _ = m.Update(undoExpiredMsg{id: m.undoID})
	m = m2.(model)
	if len(m.list.Items()) != 1 {
		t.Fatalf("expected done item to disappear after undo expires, got %d items", len(m.list.Items()))
	}
	item, ok := m.list.Items()[0].(plan)
	if !ok || item.file != "plan-b.md" {
		t.Fatalf("expected only plan-b.md to remain visible, got %+v", m.list.Items())
	}
}

func TestLabelModalToggleAppliesLabels(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan-a.md"), "---\nstatus: active\n---\n# Plan A\n")

	plans, err := scanPlans(dir)
	if err != nil {
		t.Fatalf("scanPlans: %v", err)
	}
	m := newModel(plans, dir, newDefaultConfig(), nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(model)

	// Open label modal, type a new label, and press enter to create it
	m.openLabelModal(false)
	m.labelInput.SetValue("atlas")

	enterKey := tea.KeyMsg{Type: tea.KeyEnter}
	m2, cmd := m.Update(enterKey)
	m = m2.(model)
	if m.settingLabels {
		t.Fatal("label modal should close after creating new label")
	}
	if cmd == nil {
		t.Fatal("expected label apply command")
	}

	msg := cmd()
	updated, ok := msg.(labelsUpdatedMsg)
	if !ok {
		t.Fatalf("expected labelsUpdatedMsg, got %T", msg)
	}
	if len(updated.plan.labels) != 1 || updated.plan.labels[0] != "atlas" {
		t.Fatalf("labels = %v, want [atlas]", updated.plan.labels)
	}

	data, err := os.ReadFile(filepath.Join(dir, "plan-a.md"))
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	fields, _ := parseFrontmatter(string(data))
	if fields["labels"] != "atlas" {
		t.Fatalf("frontmatter labels = %q, want atlas", fields["labels"])
	}
}

func TestBatchLabelModalEscNoChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan-a.md"), "---\nstatus: active\nlabels: shared\n---\n# Plan A\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "---\nstatus: active\nlabels: shared\n---\n# Plan B\n")

	plans, err := scanPlans(dir)
	if err != nil {
		t.Fatalf("scanPlans: %v", err)
	}
	m := newModel(plans, dir, newDefaultConfig(), nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(model)

	// Select both plans
	for _, item := range m.list.Items() {
		if p, ok := item.(plan); ok {
			m.selected[p.file] = true
		}
	}

	// Open batch label modal (pre-seeds "shared" as toggled)
	m.openLabelModal(true)
	if !m.labelToggled["shared"] {
		t.Fatal("expected 'shared' to be pre-toggled in batch mode")
	}

	// Press Esc without making any changes
	escKey := tea.KeyMsg{Type: tea.KeyEsc}
	m2, cmd := m.Update(escKey)
	m = m2.(model)
	if m.settingLabels {
		t.Fatal("label modal should close on Esc")
	}
	if cmd != nil {
		t.Fatal("expected no command when Esc pressed without changes")
	}
}

func TestLabelCycleSkipsEmptyResults(t *testing.T) {
	// "orion" label only exists on a done plan. With showDone=false,
	// cycling to it should be skipped — never producing an empty list.
	m := testModel()
	m.showDone = false

	// Cycle forward through all labels and back to ""
	labels := recentLabels(m.allPlans)
	bracketRight := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}
	for i := 0; i <= len(labels); i++ {
		m2, _ := m.Update(bracketRight)
		m = m2.(model)
		if len(m.list.Items()) == 0 {
			t.Fatalf("empty list after %d presses of ], labelFilter=%q", i+1, m.labelFilter)
		}
	}
}

func TestLabelCycleUpdatesPreview(t *testing.T) {
	m := testModel()
	m.showDone = true // show all plans so every label has results

	// Record the initial viewport content (first plan's preview)
	initialContent := m.viewport.View()

	// Cycle to next label
	bracketRight := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}}
	m2, cmd := m.Update(bracketRight)
	m = m2.(model)
	execCmd(t, &m, cmd)

	// The selected plan changed, so the viewport should reflect the new plan
	if file := m.selectedFile(); file != "" {
		if cached, ok := m.previewCache[file]; ok {
			if m.viewport.View() == initialContent && cached != initialContent {
				t.Fatal("viewport was not updated after label cycle changed the selected plan")
			}
		}
	}
}

func TestReleaseNotesDismissMarksSeen(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	if err := saveUpdateState(statePath, updateState{LastSeenVersion: "v0.1.0"}); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	m := testModel()
	m.releaseNotes.on = true
	m.releaseNotes.version = "v0.3.0"

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	if m.releaseNotes.on {
		t.Fatal("release notes should close on enter")
	}
	if cmd == nil {
		t.Fatal("expected markReleaseNotesSeen command")
	}
	_ = cmd()

	st, err := loadUpdateState(statePath)
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if st.LastSeenVersion != "v0.3.0" {
		t.Fatalf("last_seen_version = %q, want v0.3.0", st.LastSeenVersion)
	}
}

func TestStartupUpdateMessageUpdatesModelState(t *testing.T) {
	m := testModel()
	msg := startupUpdateMsg{
		update: &updateAvailableMsg{
			version: "v0.4.0",
			url:     "https://example.invalid/v0.4.0",
		},
		releaseNotes: &releaseNotesMsg{
			version:  "v0.3.0",
			markdown: "## [v0.3.0]\n\n- New",
		},
	}

	m2, _ := m.Update(msg)
	m = m2.(model)
	if m.updateAvailable == nil || m.updateAvailable.version != "v0.4.0" {
		t.Fatalf("updateAvailable = %+v, want version v0.4.0", m.updateAvailable)
	}
	if !m.releaseNotes.on || m.releaseNotes.version != "v0.3.0" {
		t.Fatalf("releaseNotes state not applied: on=%v ver=%q", m.releaseNotes.on, m.releaseNotes.version)
	}
}
