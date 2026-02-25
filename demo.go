package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

//go:embed demo_content.json
var demoContentJSON []byte

func demoPlans() []plan {
	now := time.Now()
	day := 24 * time.Hour
	return []plan{
		{status: "active", labels: []string{"planc"}, title: "Terminal dashboard for plan management", created: now.Add(-0 * day), file: "glowing-spinning-falcon.md"},
		{status: "", labels: []string{"garden"}, title: "Raspberry Pi irrigation controller", created: now.Add(-1 * day), modified: now.Add(-1 * day), file: "optimistic-watering-pi.md"},
		{status: "active", labels: []string{"lunch"}, title: "Descope back to a Slack bot", created: now.Add(-2 * day), file: "humble-returning-sandwich.md"},
		{status: "active", labels: []string{"fittrack"}, title: "Add social challenges and leaderboard", created: now.Add(-3 * day), file: "competitive-flexing-sneaker.md"},
		{status: "reviewed", labels: []string{"fittrack"}, title: "Add heart rate zone training", created: now.Add(-4 * day), file: "eager-pulsing-heart.md"},
		{status: "done", labels: []string{"planc"}, title: "Rewrite back in Go because lifetimes", created: now.Add(-6 * day), file: "relieved-idiomatic-gopher.md"},
		{status: "active", labels: []string{"agent"}, title: "Write comprehensive postmortem", created: now.Add(-8 * day), file: "reflective-documenting-octopus.md"},
		{status: "done", labels: []string{"lunch"}, title: "Pivot to full delivery logistics platform", created: now.Add(-9 * day), file: "ambitious-routing-van.md"},
		{status: "done", labels: []string{"agent"}, title: "Sunset personal agent and sell remaining IP", created: now.Add(-11 * day), file: "sunset-selling-octopus.md"},
		{status: "done", labels: []string{"agent"}, title: "Emergency rollback after agent negotiated my rent", created: now.Add(-12 * day), file: "panicked-revoking-octopus.md"},
		{status: "done", labels: []string{"planc"}, title: "Rewrite in Rust for performance", created: now.Add(-13 * day), file: "blazing-fast-crab.md"},
		{status: "done", labels: []string{"agent"}, title: "Let personal agent handle purchases and negotiation", created: now.Add(-15 * day), file: "reckless-negotiating-tentacle.md"},
		{status: "done", labels: []string{"lunch"}, title: "Add restaurant recommendation engine", created: now.Add(-16 * day), file: "hungry-learning-fork.md"},
		{status: "done", labels: []string{"agent"}, title: "Personal agent alpha for inbox and calendar triage", created: now.Add(-18 * day), file: "eager-orchestrating-claw.md"},
		{status: "done", labels: []string{"fittrack"}, title: "Remove ML, just count steps", created: now.Add(-20 * day), file: "humbled-stepping-shoe.md"},
		{status: "done", labels: []string{"lunch"}, title: "Slack bot for lunch orders", created: now.Add(-24 * day), file: "simple-ordering-bot.md"},
		{status: "done", labels: []string{"fittrack"}, title: "Add ML-powered activity recognition", created: now.Add(-30 * day), file: "eager-classifying-neuron.md"},
		{status: "done", labels: []string{"planc"}, title: "Shell script to list plan files", created: now.Add(-34 * day), file: "tiny-listing-script.md"},
		{status: "done", labels: []string{"fittrack"}, title: "Step counter CLI tool", created: now.Add(-42 * day), file: "fresh-counting-pedometer.md"},
	}
}

func demoPlanContents() map[string]string {
	var contents map[string]string
	if err := json.Unmarshal(demoContentJSON, &contents); err != nil {
		// Embedded data is compile-time constant; panic is appropriate.
		panic("demo_content.json: " + err.Error())
	}
	return contents
}

// ─── demoStore ───────────────────────────────────────────────────────────────

// demoStore implements planStore with in-memory operations (no disk I/O).
type demoStore struct {
	plans *[]plan // points to model.demo.plans
}

func (s demoStore) setStatus(p plan, status string) tea.Cmd {
	return func() tea.Msg {
		updated := p
		updated.status = status
		return statusUpdatedMsg{oldPlan: p, newPlan: updated}
	}
}

func (s demoStore) deletePlan(p plan) tea.Cmd {
	return func() tea.Msg {
		var remaining []plan
		for _, dp := range *s.plans {
			if dp.file != p.file {
				remaining = append(remaining, dp)
			}
		}
		return reloadMsg{plans: remaining}
	}
}

func (s demoStore) setLabels(p plan, labels []string) tea.Cmd {
	return func() tea.Msg {
		updated := p
		updated.labels = labels
		updated.project = ""
		return labelsUpdatedMsg{plan: updated}
	}
}

func (s demoStore) batchSetStatus(paths []string, status string) tea.Cmd {
	plans := *s.plans
	return func() tea.Msg {
		pathSet := make(map[string]bool)
		for _, p := range paths {
			pathSet[p] = true
		}
		updated := make([]plan, len(plans))
		copy(updated, plans)
		for i, p := range updated {
			if pathSet[p.path()] {
				updated[i].status = status
			}
		}
		label := status
		if label == "" {
			label = "new"
		}
		return batchDoneMsg{
			plans:   updated,
			files:   paths,
			message: fmt.Sprintf("%d plans → %s", len(paths), label),
		}
	}
}

func (s demoStore) batchUpdateLabels(paths []string, add []string, remove []string) tea.Cmd {
	plans := *s.plans
	return func() tea.Msg {
		pathSet := make(map[string]bool)
		for _, p := range paths {
			pathSet[p] = true
		}
		updated := make([]plan, len(plans))
		copy(updated, plans)
		for i, p := range updated {
			if pathSet[p.path()] {
				updated[i].labels = applyLabelChanges(p.labels, add, remove)
				updated[i].project = ""
			}
		}
		var parts []string
		if len(add) > 0 {
			parts = append(parts, "+"+strings.Join(add, ","))
		}
		if len(remove) > 0 {
			parts = append(parts, "-"+strings.Join(remove, ","))
		}
		return batchDoneMsg{
			plans:   updated,
			files:   paths,
			message: fmt.Sprintf("%d plans %s", len(paths), strings.Join(parts, " ")),
		}
	}
}

func (m *model) enterDemoMode() {
	clear(m.selected)
	m.demo.active = true
	m.demo.plans = demoPlans()
	m.demo.content = demoPlanContents()
	m.store = demoStore{plans: &m.demo.plans}
	m.showDone = false
	m.labelFilter = ""
	m.lastStatusChange = nil
	m.batchKeepFiles = nil
	visible := m.visiblePlans()
	m.list.SetItems(plansToItems(visible))
	m.list.ResetSelected()
	m.prevIndex = -1
	m.previewCache = make(map[string]string)
	m.viewport.SetContent("Loading demo...")
	m.viewport.GotoTop()
	m.restoreTitle()
}

func (m *model) exitDemoMode() {
	clear(m.selected)
	m.demo.active = false
	m.demo.plans = nil
	m.demo.content = nil
	m.store = diskStore{agentDir: m.dir, projectGlob: m.cfg.ProjectPlanGlob}
	m.showDone = m.cfg.ShowAll
	m.labelFilter = ""
	m.lastStatusChange = nil
	m.batchKeepFiles = nil
	// Re-scan from disk since watcher was ignoring changes during demo
	if plans, err := scanAllPlans(m.dir, m.cfg.ProjectPlanGlob); err == nil {
		m.allPlans = plans
		sortPlans(m.allPlans)
	}
	visible := m.visiblePlans()
	m.list.SetItems(plansToItems(visible))
	m.list.ResetSelected()
	m.prevIndex = -1
	m.previewCache = make(map[string]string)
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.restoreTitle()
}
