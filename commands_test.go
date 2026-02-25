package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetPlanStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-plan.md")
	writeFile(t, path, "# Test Plan\n\nContent here\n")

	p := plan{dir: dir, status: "", project: "", title: "Test Plan", file: "test-plan.md"}
	cmd := setPlanStatus(p, "active")
	msg := cmd()
	updated, ok := msg.(statusUpdatedMsg)
	if !ok {
		t.Fatalf("expected statusUpdatedMsg, got %T", msg)
	}
	if updated.newPlan.status != "active" {
		t.Errorf("status = %q, want active", updated.newPlan.status)
	}

	// Verify frontmatter was written
	data, _ := os.ReadFile(path)
	fields, _ := parseFrontmatter(string(data))
	if fields["status"] != "active" {
		t.Errorf("file frontmatter status = %q, want active", fields["status"])
	}
}

func TestBatchSetStatus(t *testing.T) {
	dir := t.TempDir()

	// Create test plan files
	writeFile(t, filepath.Join(dir, "plan-a.md"), "# Plan A\n\nContent A\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "---\nstatus: pending\n---\n# Plan B\n\nContent B\n")
	writeFile(t, filepath.Join(dir, "plan-c.md"), "# Plan C\n\nContent C\n")

	// Batch set status to active (using full paths)
	paths := []string{filepath.Join(dir, "plan-a.md"), filepath.Join(dir, "plan-b.md")}
	cmd := batchSetStatus(dir, "", paths, "active")
	msg := cmd()
	result, ok := msg.(batchDoneMsg)
	if !ok {
		t.Fatalf("expected batchDoneMsg, got %T", msg)
	}
	if !strings.Contains(result.message, "2 plans") {
		t.Errorf("expected message with '2 plans', got %q", result.message)
	}

	// Verify frontmatter was written
	for _, file := range []string{"plan-a.md", "plan-b.md"} {
		data, _ := os.ReadFile(filepath.Join(dir, file))
		fields, _ := parseFrontmatter(string(data))
		if fields["status"] != "active" {
			t.Errorf("%s: status = %q, want active", file, fields["status"])
		}
	}

	// Verify plan-c was untouched
	data, _ := os.ReadFile(filepath.Join(dir, "plan-c.md"))
	fields, _ := parseFrontmatter(string(data))
	if fields["status"] != "" {
		t.Errorf("plan-c.md should be untouched, got status %q", fields["status"])
	}

	// Batch unset status
	cmd = batchSetStatus(dir, "", paths, "")
	msg = cmd()
	result, ok = msg.(batchDoneMsg)
	if !ok {
		t.Fatalf("expected batchDoneMsg, got %T", msg)
	}
	if !strings.Contains(result.message, "new") {
		t.Errorf("expected message with 'new', got %q", result.message)
	}
	for _, file := range []string{"plan-a.md", "plan-b.md"} {
		data, _ := os.ReadFile(filepath.Join(dir, file))
		fields, _ := parseFrontmatter(string(data))
		if fields["status"] != "" {
			t.Errorf("%s: status should be empty after unset, got %q", file, fields["status"])
		}
	}
}

func TestBatchUpdateLabels(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "plan-a.md"), "# Plan A\n\nContent\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "---\nlabels: existing\n---\n# Plan B\n\nContent\n")

	paths := []string{filepath.Join(dir, "plan-a.md"), filepath.Join(dir, "plan-b.md")}
	cmd := batchUpdateLabels(dir, "", paths, []string{"myproject"}, nil)
	msg := cmd()
	result, ok := msg.(batchDoneMsg)
	if !ok {
		t.Fatalf("expected batchDoneMsg, got %T", msg)
	}
	if !strings.Contains(result.message, "+myproject") {
		t.Errorf("expected message with '+myproject', got %q", result.message)
	}

	// plan-a should have labels: myproject
	data, _ := os.ReadFile(filepath.Join(dir, "plan-a.md"))
	fields, _ := parseFrontmatter(string(data))
	if fields["labels"] != "myproject" {
		t.Errorf("plan-a: labels = %q, want myproject", fields["labels"])
	}

	// plan-b should have labels: existing, myproject
	data, _ = os.ReadFile(filepath.Join(dir, "plan-b.md"))
	fields, _ = parseFrontmatter(string(data))
	labels := parseLabels(fields["labels"])
	if !hasLabel(labels, "existing") || !hasLabel(labels, "myproject") {
		t.Errorf("plan-b: labels = %v, want [existing, myproject]", labels)
	}
}

func TestSetLabelsWritesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan-a.md")
	writeFile(t, path, "# Plan A\n\nBody\n")

	cmd := setLabels(plan{dir: dir, file: "plan-a.md"}, []string{"atlas", "infra"})
	msg := cmd()
	updated, ok := msg.(labelsUpdatedMsg)
	if !ok {
		t.Fatalf("expected labelsUpdatedMsg, got %T", msg)
	}
	if len(updated.plan.labels) != 2 || updated.plan.labels[0] != "atlas" {
		t.Fatalf("labels = %v, want [atlas, infra]", updated.plan.labels)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	fields, _ := parseFrontmatter(string(data))
	if fields["labels"] != "atlas, infra" {
		t.Fatalf("frontmatter labels = %q, want 'atlas, infra'", fields["labels"])
	}
	// project should be removed
	if fields["project"] != "" {
		t.Fatalf("frontmatter project should be empty after setLabels, got %q", fields["project"])
	}
}

func TestDeletePlanRemovesFileAndReloads(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan-a.md"), "# Plan A\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "# Plan B\n")

	cmd := deletePlan(dir, "", plan{dir: dir, file: "plan-a.md"})
	msg := cmd()
	reload, ok := msg.(reloadMsg)
	if !ok {
		t.Fatalf("expected reloadMsg, got %T", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "plan-a.md")); !os.IsNotExist(err) {
		t.Fatalf("plan-a.md should be deleted, err=%v", err)
	}
	if len(reload.plans) != 1 || reload.plans[0].file != "plan-b.md" {
		t.Fatalf("reload plans = %+v, want only plan-b.md", reload.plans)
	}
}

func TestReloadAllPlansEmptyForMissingDir(t *testing.T) {
	msg := reloadAllPlans(filepath.Join(t.TempDir(), "missing"), "")
	// Missing agent dir is non-fatal; returns empty plan list (project glob may still have results)
	reload, ok := msg.(reloadMsg)
	if !ok {
		t.Fatalf("expected reloadMsg, got %T", msg)
	}
	if len(reload.plans) != 0 {
		t.Fatalf("expected 0 plans, got %d", len(reload.plans))
	}
}
