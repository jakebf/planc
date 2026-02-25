package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFile is a test helper that writes content to a file and fails the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		fields map[string]string
		body   string
	}{
		{
			name:   "no frontmatter",
			input:  "# Title\n\nBody text",
			fields: map[string]string{},
			body:   "# Title\n\nBody text",
		},
		{
			name:   "with status and project",
			input:  "---\nstatus: active\nproject: planc\n---\n# Title\n\nBody",
			fields: map[string]string{"status": "active", "project": "planc"},
			body:   "# Title\n\nBody",
		},
		{
			name:   "empty frontmatter",
			input:  "---\n---\n# Title",
			fields: map[string]string{},
			body:   "# Title",
		},
		{
			name:   "status only",
			input:  "---\nstatus: done\n---\n# My Plan",
			fields: map[string]string{"status": "done"},
			body:   "# My Plan",
		},
		{
			name:   "no closing delimiter",
			input:  "---\nstatus: active\n# Title",
			fields: map[string]string{},
			body:   "---\nstatus: active\n# Title",
		},
		{
			name:   "unknown keys preserved",
			input:  "---\nstatus: active\nbranch: feat/foo\n---\nBody",
			fields: map[string]string{"status": "active", "branch": "feat/foo"},
			body:   "Body",
		},
		{
			name:   "value containing colons",
			input:  "---\nstatus: active\nurl: https://example.com\n---\nBody",
			fields: map[string]string{"status": "active", "url": "https://example.com"},
			body:   "Body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, body := parseFrontmatter(tt.input)
			if len(fields) != len(tt.fields) {
				t.Errorf("fields count: got %d, want %d", len(fields), len(tt.fields))
			}
			for k, want := range tt.fields {
				if got := fields[k]; got != want {
					t.Errorf("fields[%q] = %q, want %q", k, got, want)
				}
			}
			if body != tt.body {
				t.Errorf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain heading", "# My Plan Title\n\nBody", "My Plan Title"},
		{"with frontmatter", "---\nstatus: active\n---\n# Plan With FM\n\nBody", "Plan With FM"},
		{"no heading", "Just some text\nNo heading here", ""},
		{"h2 not h1", "## Sub heading\n\n# Real heading", "Real heading"},
		{"heading after blank lines", "\n\n# After Blanks", "After Blanks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHeader(tt.input)
			if got != tt.want {
				t.Errorf("parseHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanPlans(t *testing.T) {
	dir := t.TempDir()
	// File with frontmatter
	writeFile(t, filepath.Join(dir, "plan-a.md"), "---\nstatus: active\nproject: foo\n---\n# Alpha Plan\n\nContent")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "# Beta Plan\n\nNo frontmatter here")
	writeFile(t, filepath.Join(dir, "plan-c.md"), "Just raw text, no heading")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a plan")

	plans, err := scanPlans(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(plans))
	}

	// Find each plan by file
	byFile := make(map[string]plan)
	for _, p := range plans {
		byFile[p.file] = p
	}

	a := byFile["plan-a.md"]
	if a.status != "active" || !hasLabel(a.labels, "foo") || a.title != "Alpha Plan" {
		t.Errorf("plan-a: got %+v", a)
	}

	b := byFile["plan-b.md"]
	if b.status != "" || len(b.labels) != 0 || b.title != "Beta Plan" {
		t.Errorf("plan-b: got %+v", b)
	}

	c := byFile["plan-c.md"]
	if c.title != "plan-c" {
		t.Errorf("plan-c: expected title from filename, got %q", c.title)
	}
}

func TestSetFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	// Start with no frontmatter
	writeFile(t, path, "# My Plan\n\nBody content\n")

	// Add status
	if err := setFrontmatter(path, map[string]string{"status": "active"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.HasPrefix(content, "---\nstatus: active\n---\n") {
		t.Errorf("expected frontmatter with status, got:\n%s", content)
	}
	if !strings.Contains(content, "# My Plan") {
		t.Error("body content lost after adding frontmatter")
	}

	// Add project (should merge with existing status)
	if err := setFrontmatter(path, map[string]string{"project": "planc"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	content = string(data)
	if !strings.Contains(content, "status: active") {
		t.Error("existing status lost after adding project")
	}
	if !strings.Contains(content, "project: planc") {
		t.Error("project not added")
	}

	// Remove status (set to empty)
	if err := setFrontmatter(path, map[string]string{"status": ""}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	content = string(data)
	if strings.Contains(content, "status:") {
		t.Error("status should have been removed")
	}
	if !strings.Contains(content, "project: planc") {
		t.Error("project should still be present")
	}
}

func TestSetFrontmatterPreservesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	body := "# Important Plan\n\n## Section One\n\nDetailed content here.\n\n## Section Two\n\nMore content.\n"
	writeFile(t, path, body)

	// Set frontmatter
	setFrontmatter(path, map[string]string{"status": "active", "project": "test"})
	// Then change it
	setFrontmatter(path, map[string]string{"status": "done"})

	data, _ := os.ReadFile(path)
	content := string(data)
	_, extractedBody := parseFrontmatter(content)
	if extractedBody != body {
		t.Errorf("body changed after frontmatter updates:\ngot:  %q\nwant: %q", extractedBody, body)
	}
}

func TestFilterPlans(t *testing.T) {
	plans := testPlans()
	active := filterPlans(plans, false, nil, "", time.Time{})
	all := filterPlans(plans, true, nil, "", time.Time{})
	if len(all) != 4 {
		t.Errorf("expected 4 plans with showDone=true, got %d", len(all))
	}
	if len(active) != 3 {
		t.Errorf("expected 3 plans with showDone=false, got %d", len(active))
	}
}

func TestFilterPlansUnsetStatus(t *testing.T) {
	plans := []plan{
		{status: "", title: "Unset plan", file: "a.md"},
		{status: "active", title: "Active plan", file: "b.md"},
		{status: "done", title: "Done plan", file: "c.md"},
	}
	filtered := filterPlans(plans, false, nil, "", time.Time{})
	if len(filtered) != 1 {
		t.Errorf("expected 1 plan (active only), got %d", len(filtered))
	}
	if filtered[0].status != "active" {
		t.Errorf("expected active plan, got %q", filtered[0].status)
	}
}

func TestFilterPlansInstalledTime(t *testing.T) {
	now := time.Now()
	installed := now.Add(-1 * time.Hour)
	plans := []plan{
		{status: "", title: "New plan", file: "new.md", modified: now},                                // after install
		{status: "", title: "Old plan", file: "old.md", modified: now.Add(-2 * time.Hour)},            // before install
		{status: "active", title: "Active plan", file: "active.md", modified: now.Add(-2 * time.Hour)},
	}

	// With installed time: new unset plan shows, old unset plan hidden
	filtered := filterPlans(plans, false, nil, "", installed)
	if len(filtered) != 2 {
		t.Errorf("expected 2 plans (active + new unset), got %d", len(filtered))
	}
	names := make(map[string]bool)
	for _, p := range filtered {
		names[p.title] = true
	}
	if !names["New plan"] {
		t.Error("expected new unset plan to be visible")
	}
	if names["Old plan"] {
		t.Error("expected old unset plan to be hidden")
	}

	// Without installed time (zero): all unset plans hidden
	filtered = filterPlans(plans, false, nil, "", time.Time{})
	if len(filtered) != 1 {
		t.Errorf("expected 1 plan (active only), got %d", len(filtered))
	}
}

func TestScanPlansMigratesProjectToLabels(t *testing.T) {
	dir := t.TempDir()
	// File with old project field (no labels)
	writeFile(t, filepath.Join(dir, "old.md"), "---\nstatus: active\nproject: foo\n---\n# Old Plan\n")
	// File with new labels field
	writeFile(t, filepath.Join(dir, "new.md"), "---\nstatus: active\nlabels: bar, baz\n---\n# New Plan\n")

	plans, err := scanPlans(dir)
	if err != nil {
		t.Fatal(err)
	}
	byFile := make(map[string]plan)
	for _, p := range plans {
		byFile[p.file] = p
	}

	old := byFile["old.md"]
	if len(old.labels) != 1 || old.labels[0] != "foo" {
		t.Errorf("old plan: labels = %v, want [foo] (migrated from project)", old.labels)
	}

	new := byFile["new.md"]
	if len(new.labels) != 2 || new.labels[0] != "bar" || new.labels[1] != "baz" {
		t.Errorf("new plan: labels = %v, want [bar, baz]", new.labels)
	}
}

func TestSetLabelsWritesMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	// Start with old project field
	writeFile(t, path, "---\nstatus: active\nproject: foo\n---\n# Plan\n\nBody\n")

	// Write labels via setFrontmatter
	if err := setFrontmatter(path, map[string]string{"labels": "bar, baz", "project": ""}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	fields, _ := parseFrontmatter(string(data))
	if fields["labels"] != "bar, baz" {
		t.Errorf("labels = %q, want 'bar, baz'", fields["labels"])
	}
	if fields["project"] != "" {
		t.Errorf("project should be removed after labels write, got %q", fields["project"])
	}
}

func TestRecentLabels(t *testing.T) {
	plans := testPlans()
	recent := recentLabels(plans)
	if len(recent) != 4 {
		t.Errorf("expected 4 unique labels, got %d", len(recent))
	}
	// All have count 1, so order is non-deterministic. Just check all are present.
	found := make(map[string]bool)
	for _, l := range recent {
		found[l] = true
	}
	for _, want := range []string{"kokua", "pulse", "atlas", "orion"} {
		if !found[want] {
			t.Errorf("missing label %q", want)
		}
	}
}
