package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Types ───────────────────────────────────────────────────────────────────

// planStore abstracts plan mutations so the same model logic works against
// both a real filesystem (diskStore) and an in-memory demo dataset (demoStore).
type planStore interface {
	setStatus(p plan, status string) tea.Cmd
	deletePlan(p plan) tea.Cmd
	setProject(p plan, project string) tea.Cmd
	batchSetStatus(files []string, status string) tea.Cmd
	batchSetProject(files []string, project string) tea.Cmd
}

type pane int

const (
	listPane pane = iota
	previewPane
)

type plan struct {
	status   string    // from frontmatter, or "" (unset)
	project  string    // from frontmatter, or ""
	title    string    // from first # heading
	created  time.Time // file birth time
	modified time.Time // file modification time
	file     string    // base filename
}

var nextStatus = map[string]string{
	"":        "pending",
	"pending": "active",
	"active":  "done",
	"done":    "pending",
}

var prevStatus = map[string]string{
	"":        "done",
	"pending": "done",
	"active":  "pending",
	"done":    "active",
}

func statusIcon(s string) string {
	switch s {
	case "active":
		return "●"
	case "pending":
		return "○"
	case "done":
		return "✓"
	default:
		return "·"
	}
}

func (p plan) Title() string {
	if p.project != "" {
		return fmt.Sprintf("%s %s: %s", statusIcon(p.status), p.project, p.title)
	}
	return fmt.Sprintf("%s %s", statusIcon(p.status), p.title)
}

func (p plan) Description() string {
	return p.created.Format("2006-01-02")
}

func (p plan) FilterValue() string {
	return fmt.Sprintf("%s %s %s %s", p.status, p.project, p.title, p.file)
}

// ─── Plan Scanning ───────────────────────────────────────────────────────────

// parseFrontmatter extracts YAML frontmatter key-value pairs from content.
// Returns the fields and the body (everything after the closing ---).
func parseFrontmatter(content string) (fields map[string]string, body string) {
	fields = make(map[string]string)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return fields, content
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closing = i
			break
		}
	}
	if closing < 0 {
		return fields, content
	}
	for _, line := range lines[1:closing] {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			// Empty values are intentionally dropped: this pairs with
			// setFrontmatter's convention of deleting keys set to "".
			if k != "" && v != "" {
				fields[k] = v
			}
		}
	}
	body = strings.Join(lines[closing+1:], "\n")
	return fields, body
}

// parseHeader returns the text of the first # heading, skipping frontmatter.
func parseHeader(content string) string {
	_, body := parseFrontmatter(content)
	return headerFromBody(body)
}

// headerFromBody returns the text of the first # heading in body text.
func headerFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

// scanPlans reads all .md files in dir and builds a plan list from
// frontmatter, headings, and file creation times. Sorted by created descending.
func scanPlans(dir string) ([]plan, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var plans []plan
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fm, body := parseFrontmatter(string(data))
		title := headerFromBody(body)
		if title == "" {
			title = strings.TrimSuffix(e.Name(), ".md")
		}
		plans = append(plans, plan{
			status:   fm["status"],
			project:  fm["project"],
			title:    title,
			created:  fileCreatedTime(path, info.ModTime()),
			modified: info.ModTime(),
			file:     e.Name(),
		})
	}
	sortPlans(plans)
	return plans, nil
}

func sortPlans(plans []plan) {
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].created.After(plans[j].created)
	})
}

// setFrontmatter merges the given fields into the file's YAML frontmatter.
// Fields with empty values are removed. If no fields remain, frontmatter is stripped.
// Unknown keys are preserved.
func setFrontmatter(filePath string, updates map[string]string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	existing, body := parseFrontmatter(string(data))
	for k, v := range updates {
		if v == "" {
			delete(existing, k)
		} else {
			existing[k] = v
		}
	}
	var result string
	if len(existing) > 0 {
		var buf strings.Builder
		buf.WriteString("---\n")
		written := make(map[string]bool)
		for _, key := range []string{"status", "project"} {
			if v, ok := existing[key]; ok && v != "" {
				fmt.Fprintf(&buf, "%s: %s\n", key, v)
				written[key] = true
			}
		}
		// Preserve unknown keys in sorted order
		var extra []string
		for k := range existing {
			if !written[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		for _, k := range extra {
			if v := existing[k]; v != "" {
				fmt.Fprintf(&buf, "%s: %s\n", k, v)
			}
		}
		buf.WriteString("---\n")
		result = buf.String() + body
	} else {
		result = body
	}
	// Use os.WriteFile (truncate + write) instead of atomic rename to preserve
	// the file's birth time on Linux. Atomic rename creates a new inode which
	// resets btime, causing the plan to jump to the top of the created-sort list.
	lastSelfWrite.Store(time.Now().UnixMilli())
	return os.WriteFile(filePath, []byte(result), perm)
}

// recentProjects returns deduplicated project names from plans, most frequent first.
func recentProjects(plans []plan) []string {
	counts := make(map[string]int)
	for _, p := range plans {
		if p.project != "" {
			counts[p.project]++
		}
	}
	type pc struct {
		name  string
		count int
	}
	var sorted []pc
	for k, v := range counts {
		sorted = append(sorted, pc{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].name < sorted[j].name
	})
	var result []string
	for _, s := range sorted {
		result = append(result, s.name)
	}
	return result
}

func filterPlans(plans []plan, showDone bool, keepFiles map[string]bool, projectFilter string, installed time.Time) []plan {
	var filtered []plan
	for _, p := range plans {
		if projectFilter != "" && p.project != projectFilter {
			continue
		}
		if !showDone && p.status == "done" && !keepFiles[p.file] {
			continue
		}
		if !showDone && p.status == "" && !keepFiles[p.file] {
			// Show unset plans modified after install (they're likely new)
			if installed.IsZero() || p.modified.Before(installed) {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func plansToItems(plans []plan) []list.Item {
	items := make([]list.Item, len(plans))
	for i, p := range plans {
		items[i] = p
	}
	return items
}
