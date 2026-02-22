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

	p := plan{status: "", project: "", title: "Test Plan", file: "test-plan.md"}
	cmd := setPlanStatus(dir, p, "active")
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

	// Batch set status to active
	cmd := batchSetStatus(dir, []string{"plan-a.md", "plan-b.md"}, "active")
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
	cmd = batchSetStatus(dir, []string{"plan-a.md", "plan-b.md"}, "")
	msg = cmd()
	result, ok = msg.(batchDoneMsg)
	if !ok {
		t.Fatalf("expected batchDoneMsg, got %T", msg)
	}
	if !strings.Contains(result.message, "unset") {
		t.Errorf("expected message with 'unset', got %q", result.message)
	}
	for _, file := range []string{"plan-a.md", "plan-b.md"} {
		data, _ := os.ReadFile(filepath.Join(dir, file))
		fields, _ := parseFrontmatter(string(data))
		if fields["status"] != "" {
			t.Errorf("%s: status should be empty after unset, got %q", file, fields["status"])
		}
	}
}

func TestBatchSetProject(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "plan-a.md"), "# Plan A\n\nContent\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "# Plan B\n\nContent\n")

	cmd := batchSetProject(dir, []string{"plan-a.md", "plan-b.md"}, "myproject")
	msg := cmd()
	result, ok := msg.(batchDoneMsg)
	if !ok {
		t.Fatalf("expected batchDoneMsg, got %T", msg)
	}
	if !strings.Contains(result.message, "project:myproject") {
		t.Errorf("expected message with 'project:myproject', got %q", result.message)
	}

	for _, file := range []string{"plan-a.md", "plan-b.md"} {
		data, _ := os.ReadFile(filepath.Join(dir, file))
		fields, _ := parseFrontmatter(string(data))
		if fields["project"] != "myproject" {
			t.Errorf("%s: project = %q, want myproject", file, fields["project"])
		}
	}
}


func TestSetProjectWritesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan-a.md")
	writeFile(t, path, "# Plan A\n\nBody\n")

	cmd := setProject(dir, plan{file: "plan-a.md"}, "atlas")
	msg := cmd()
	updated, ok := msg.(projectUpdatedMsg)
	if !ok {
		t.Fatalf("expected projectUpdatedMsg, got %T", msg)
	}
	if updated.plan.project != "atlas" {
		t.Fatalf("project = %q, want atlas", updated.plan.project)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	fields, _ := parseFrontmatter(string(data))
	if fields["project"] != "atlas" {
		t.Fatalf("frontmatter project = %q, want atlas", fields["project"])
	}
}

func TestDeletePlanRemovesFileAndReloads(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plan-a.md"), "# Plan A\n")
	writeFile(t, filepath.Join(dir, "plan-b.md"), "# Plan B\n")

	cmd := deletePlan(dir, plan{file: "plan-a.md"})
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

func TestReloadPlansReturnsErrForMissingDir(t *testing.T) {
	msg := reloadPlans(filepath.Join(t.TempDir(), "missing"))
	if _, ok := msg.(errMsg); !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
}
