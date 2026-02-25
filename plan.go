package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Types ───────────────────────────────────────────────────────────────────

// planStore abstracts plan mutations so the same model logic works against
// both a real filesystem (diskStore) and an in-memory demo dataset (demoStore).
type planStore interface {
	setStatus(p plan, status string) tea.Cmd
	deletePlan(p plan) tea.Cmd
	setLabels(p plan, labels []string) tea.Cmd
	batchSetStatus(files []string, status string) tea.Cmd
	batchUpdateLabels(files []string, add []string, remove []string) tea.Cmd
}

type pane int

const (
	listPane pane = iota
	previewPane
)

type plan struct {
	dir         string    // directory containing this plan file
	status      string    // from frontmatter, or "" (unset)
	project     string    // from frontmatter, or "" (deprecated; use labels)
	labels      []string  // from frontmatter, or migrated from project
	title       string    // from first # heading
	created     time.Time // file birth time
	modified    time.Time // file modification time
	file        string    // base filename
	hasComments bool      // true if body contains comment blockquotes
}

func (p plan) path() string {
	return filepath.Join(p.dir, p.file)
}

var nextStatus = map[string]string{
	"":         "reviewed",
	"reviewed": "active",
	"active":   "done",
	"done":     "reviewed",
}

func statusIcon(s string) string {
	switch s {
	case "active":
		return "●"
	case "reviewed":
		return "○"
	case "done":
		return "✓"
	default:
		return "·"
	}
}

func (p plan) Title() string {
	if len(p.labels) > 0 {
		return fmt.Sprintf("%s %s: %s", statusIcon(p.status), strings.Join(p.labels, ","), p.title)
	}
	return fmt.Sprintf("%s %s", statusIcon(p.status), p.title)
}

func (p plan) Description() string {
	return p.created.Format("2006-01-02")
}

func (p plan) FilterValue() string {
	return fmt.Sprintf("%s %s %s %s", p.status, strings.Join(p.labels, " "), p.title, p.file)
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
		labels := parseLabels(fm["labels"])
		project := fm["project"]
		// Backward compat: migrate project → labels
		if len(labels) == 0 && project != "" {
			labels = []string{project}
		}
		// Backward compat: migrate pending → reviewed
		status := fm["status"]
		if status == "pending" {
			status = "reviewed"
		}
		plans = append(plans, plan{
			dir:         dir,
			status:      status,
			project:     project,
			labels:      labels,
			title:       title,
			created:     fileCreatedTime(path, info.ModTime()),
			modified:    info.ModTime(),
			file:        e.Name(),
			hasComments: bodyHasComments(body),
		})
	}
	sortPlans(plans)
	return plans, nil
}

// skipDirs lists directory names that are typically very large and
// will never contain user plan files. Skipping them during glob
// resolution avoids walking hundreds of thousands of entries
// (e.g. node_modules trees) that make startup unacceptably slow.
var skipDirs = map[string]bool{
	"node_modules":    true,
	".git":            true,
	".hg":             true,
	".svn":            true,
	".venv":           true,
	"venv":            true,
	"__pycache__":     true,
	".cache":          true,
	".next":           true,
	".nuxt":           true,
	".output":         true,
	".angular":        true,
	".gradle":         true,
	".cargo":          true,
	".npm":            true,
	".pnpm":           true,
	".tox":            true,
	".mypy_cache":     true,
	".pytest_cache":   true,
	".generated":      true,
	"target":          true,
	"dist":            true,
	"build":           true,
	"coverage":        true,
	".turbo":          true,
	".parcel-cache":   true,
	".docusaurus":     true,
}

// resolveProjectDirs expands a glob pattern (supporting **) and returns
// matching directories. Uses filepath.WalkDir from the static prefix of
// the pattern, skipping known heavy directories for performance.
func resolveProjectDirs(glob string) []string {
	if glob == "" {
		return nil
	}
	glob = expandHome(glob)

	base := globBase(glob)
	if _, err := os.Stat(base); err != nil {
		return nil
	}

	var dirs []string
	filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		if path != base && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		matched, _ := doublestar.PathMatch(glob, path)
		if matched {
			dirs = append(dirs, path)
		}
		return nil
	})
	return dirs
}

// globBase returns the longest directory prefix of a glob pattern
// that contains no wildcard characters (* ? [ {).
func globBase(pattern string) string {
	for i, c := range pattern {
		if c == '*' || c == '?' || c == '[' || c == '{' {
			dir := pattern[:i]
			if j := strings.LastIndex(dir, string(filepath.Separator)); j >= 0 {
				return pattern[:j]
			}
			return "."
		}
	}
	return pattern
}

// scanAllPlans scans the agent plans dir and any project dirs matched by glob.
// Plans are deduplicated by full path and sorted by creation time descending.
func scanAllPlans(agentDir string, projectGlob string) ([]plan, error) {
	plans, err := scanPlans(agentDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, p := range plans {
		seen[p.path()] = true
	}
	for _, dir := range resolveProjectDirs(projectGlob) {
		dirPlans, err := scanPlans(dir)
		if err != nil {
			continue
		}
		for _, p := range dirPlans {
			if !seen[p.path()] {
				seen[p.path()] = true
				plans = append(plans, p)
			}
		}
	}
	sortPlans(plans)
	return plans, nil
}

func sortPlans(plans []plan) {
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].created.After(plans[j].created)
	})
}

// parseLabels splits a comma-separated labels string, normalizes to lowercase,
// and returns them sorted alphabetically.
func parseLabels(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var labels []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.ToLower(p)
		if p != "" {
			labels = append(labels, p)
		}
	}
	sort.Strings(labels)
	return labels
}

// labelsString joins labels with ", " for frontmatter serialization.
func labelsString(labels []string) string {
	return strings.Join(labels, ", ")
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
		for _, key := range []string{"status", "labels", "project"} {
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

// recentLabels returns deduplicated label names from plans, most frequent first.
func recentLabels(plans []plan) []string {
	counts := make(map[string]int)
	for _, p := range plans {
		for _, l := range p.labels {
			counts[l]++
		}
	}
	type lc struct {
		name  string
		count int
	}
	var sorted []lc
	for k, v := range counts {
		sorted = append(sorted, lc{k, v})
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

func filterPlans(plans []plan, showDone bool, keepFiles map[string]bool, labelFilter string, installed time.Time) []plan {
	var filtered []plan
	for _, p := range plans {
		if labelFilter != "" && !hasLabel(p.labels, labelFilter) {
			continue
		}
		if !showDone && p.status == "done" && !keepFiles[p.path()] {
			continue
		}
		if !showDone && p.status == "" && !keepFiles[p.path()] {
			// Show unset plans modified after install (they're likely new)
			if installed.IsZero() || p.modified.Before(installed) {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func plansToItems(plans []plan) []list.Item {
	items := make([]list.Item, len(plans))
	for i, p := range plans {
		items[i] = p
	}
	return items
}
