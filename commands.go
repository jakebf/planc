package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/fsnotify/fsnotify"
)

// lastSelfWrite tracks when we last wrote to a plan file ourselves.
// The file watcher checks this to skip events caused by our own writes.
var lastSelfWrite atomic.Int64

// rendererPool caches glamour renderers keyed by "style:width".
// Each key maps to a sync.Pool so concurrent goroutines get their own instance.
var (
	rendererPoolMu sync.Mutex
	rendererPools  = make(map[string]*sync.Pool)
)

func getRenderer(style string, width int) (*glamour.TermRenderer, error) {
	key := fmt.Sprintf("%s:%d", style, width)
	rendererPoolMu.Lock()
	pool, ok := rendererPools[key]
	if !ok {
		pool = &sync.Pool{}
		rendererPools[key] = pool
	}
	rendererPoolMu.Unlock()

	// Try to reuse a pooled renderer; create a new one on miss.
	if r, _ := pool.Get().(*glamour.TermRenderer); r != nil {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, fmt.Errorf("could not create renderer for %s: %w", key, err)
	}
	return r, nil
}

func putRenderer(style string, width int, r *glamour.TermRenderer) {
	key := fmt.Sprintf("%s:%d", style, width)
	rendererPoolMu.Lock()
	pool := rendererPools[key]
	rendererPoolMu.Unlock()
	if pool != nil {
		pool.Put(r)
	}
}

// ─── Commands ────────────────────────────────────────────────────────────────

func glamourRender(markdown, style string, width int) string {
	pw := width - 4
	if pw < 20 {
		pw = 80
	}
	r, err := getRenderer(style, pw)
	if err != nil {
		return markdown
	}
	rendered, err := r.Render(markdown)
	putRenderer(style, pw, r)
	if err != nil {
		return markdown
	}
	return rendered
}

func renderMarkdown(file, markdown, style string, width int) tea.Cmd {
	return func() tea.Msg {
		return planContentMsg{file: file, content: glamourRender(markdown, style, width)}
	}
}

func renderPlan(dir, file, style string, width int) tea.Cmd {
	return func() tea.Msg {
		path := filepath.Join(dir, file)
		data, err := os.ReadFile(path)
		if err != nil {
			return planContentMsg{file: file, content: fmt.Sprintf("Error reading %s: %v", file, err)}
		}
		_, body := parseFrontmatter(string(data))
		return planContentMsg{file: file, content: glamourRender(body, style, width)}
	}
}

func reloadPlans(dir string) tea.Msg {
	plans, err := scanPlans(dir)
	if err != nil {
		return errMsg{err}
	}
	return reloadMsg{plans: plans}
}

func deletePlan(dir string, p plan) tea.Cmd {
	return func() tea.Msg {
		if err := os.Remove(filepath.Join(dir, p.file)); err != nil && !os.IsNotExist(err) {
			return errMsg{fmt.Errorf("could not delete file: %w", err)}
		}
		plans, err := scanPlans(dir)
		if err != nil {
			return errMsg{err}
		}
		return reloadMsg{plans: plans}
	}
}

func setPlanStatus(dir string, p plan, newStatus string) tea.Cmd {
	return func() tea.Msg {
		if err := setFrontmatter(filepath.Join(dir, p.file), map[string]string{"status": newStatus}); err != nil {
			return errMsg{err}
		}
		updated := p
		updated.status = newStatus
		return statusUpdatedMsg{oldPlan: p, newPlan: updated}
	}
}

func setLabels(dir string, p plan, labels []string) tea.Cmd {
	return func() tea.Msg {
		updates := map[string]string{
			"labels":  labelsString(labels),
			"project": "", // migrate away from project
		}
		if err := setFrontmatter(filepath.Join(dir, p.file), updates); err != nil {
			return errMsg{err}
		}
		updated := p
		updated.labels = labels
		updated.project = ""
		return labelsUpdatedMsg{plan: updated}
	}
}

func batchSetStatus(dir string, files []string, status string) tea.Cmd {
	return func() tea.Msg {
		var failed int
		for _, file := range files {
			if err := setFrontmatter(filepath.Join(dir, file), map[string]string{"status": status}); err != nil {
				failed++
			}
		}
		plans, err := scanPlans(dir)
		if err != nil {
			return errMsg{err}
		}
		label := status
		if label == "" {
			label = "unset"
		}
		msg := fmt.Sprintf("%d plans → %s", len(files), label)
		if failed > 0 {
			msg += fmt.Sprintf(" (%d failed)", failed)
		}
		return batchDoneMsg{
			plans:   plans,
			files:   files,
			message: msg,
		}
	}
}

func batchUpdateLabels(dir string, files []string, add []string, remove []string) tea.Cmd {
	return func() tea.Msg {
		var failed int
		for _, file := range files {
			// Read current labels from file
			data, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil {
				failed++
				continue
			}
			fm, _ := parseFrontmatter(string(data))
			existing := parseLabels(fm["labels"])
			if len(existing) == 0 && fm["project"] != "" {
				existing = []string{fm["project"]}
			}
			newLabels := applyLabelChanges(existing, add, remove)
			updates := map[string]string{
				"labels":  labelsString(newLabels),
				"project": "", // migrate away from project
			}
			if err := setFrontmatter(filepath.Join(dir, file), updates); err != nil {
				failed++
			}
		}
		plans, err := scanPlans(dir)
		if err != nil {
			return errMsg{err}
		}
		var parts []string
		if len(add) > 0 {
			parts = append(parts, "+"+strings.Join(add, ","))
		}
		if len(remove) > 0 {
			parts = append(parts, "-"+strings.Join(remove, ","))
		}
		msg := fmt.Sprintf("%d plans %s", len(files), strings.Join(parts, " "))
		if failed > 0 {
			msg += fmt.Sprintf(" (%d failed)", failed)
		}
		return batchDoneMsg{
			plans:   plans,
			files:   files,
			message: msg,
		}
	}
}

// applyLabelChanges applies add/remove to existing labels, returning a new slice.
func applyLabelChanges(existing []string, add []string, remove []string) []string {
	removeSet := make(map[string]bool)
	for _, r := range remove {
		removeSet[r] = true
	}
	var result []string
	seen := make(map[string]bool)
	for _, l := range existing {
		if !removeSet[l] && !seen[l] {
			result = append(result, l)
			seen[l] = true
		}
	}
	for _, a := range add {
		if !seen[a] {
			result = append(result, a)
			seen[a] = true
		}
	}
	return result
}

// runBackgroundEditor launches the editor in the background (for GUI editors).
// Returns editorLaunchedMsg immediately. A goroutine waits for the process
// to prevent zombies; the file watcher picks up any changes.
func runBackgroundEditor(args []string) tea.Cmd {
	return func() tea.Msg {
		c := shellCommand(args...)
		if err := c.Start(); err != nil {
			return errMsg{fmt.Errorf("editor start: %w", err)}
		}
		go func() { _ = c.Wait() }()
		return editorLaunchedMsg{}
	}
}

// ─── diskStore ───────────────────────────────────────────────────────────────

// diskStore implements planStore by reading and writing real plan files.
type diskStore struct {
	dir string
}

func (s diskStore) setStatus(p plan, status string) tea.Cmd {
	return setPlanStatus(s.dir, p, status)
}

func (s diskStore) deletePlan(p plan) tea.Cmd {
	return deletePlan(s.dir, p)
}

func (s diskStore) setLabels(p plan, labels []string) tea.Cmd {
	return setLabels(s.dir, p, labels)
}

func (s diskStore) batchSetStatus(files []string, status string) tea.Cmd {
	return batchSetStatus(s.dir, files, status)
}

func (s diskStore) batchUpdateLabels(files []string, add []string, remove []string) tea.Cmd {
	return batchUpdateLabels(s.dir, files, add, remove)
}

// watchDir watches the plans directory for .md file changes.
// Sends a fileChangedMsg each time a write/create/remove is detected,
// with a small debounce to coalesce rapid writes.
func watchDir(watcher *fsnotify.Watcher) tea.Cmd {
	return func() tea.Msg {
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return nil
				}
				if !strings.HasSuffix(ev.Name, ".md") {
					continue
				}
				if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Remove) {
					changed := map[string]bool{filepath.Base(ev.Name): true}
					time.Sleep(100 * time.Millisecond)
				drain:
					for {
						select {
						case extra, ok := <-watcher.Events:
							if !ok {
								break drain
							}
							if strings.HasSuffix(extra.Name, ".md") {
								changed[filepath.Base(extra.Name)] = true
							}
						default:
							break drain
						}
					}
					// Skip events caused by our own writes (status/project changes)
					if time.Since(time.UnixMilli(lastSelfWrite.Load())) < 500*time.Millisecond {
						continue
					}
					files := make([]string, 0, len(changed))
					for f := range changed {
						files = append(files, f)
					}
					return fileChangedMsg{files: files}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return nil
				}
			}
		}
	}
}
