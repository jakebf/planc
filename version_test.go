package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckForUpdateSkipsDev(t *testing.T) {
	if cmd := checkForUpdate("dev"); cmd != nil {
		t.Fatal("expected nil cmd for dev version")
	}
}

func TestStartupUpdateCmdSkipsDev(t *testing.T) {
	if cmd := startupUpdateCmd("dev"); cmd != nil {
		t.Fatal("expected nil cmd for dev version")
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		newer   bool
	}{
		{current: "v0.1.0", latest: "v0.2.0", newer: true},
		{current: "0.1.0", latest: "v0.1.0", newer: false},
		{current: "v1.0.0-beta.1", latest: "v1.0.0", newer: true},
		{current: "v1.2.3", latest: "v1.2.3+build.7", newer: false},
		{current: "v1.2.3", latest: "not-a-version", newer: false},
	}
	for _, tc := range tests {
		if got := isNewerVersion(tc.current, tc.latest); got != tc.newer {
			t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.newer)
		}
	}
}

func TestCheckForUpdateUsesFreshCache(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	fixedNow := time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)
	restore := overrideUpdateGlobals(t, fixedNow)
	defer restore()

	st := updateState{
		CheckedAt:     fixedNow.Add(-1 * time.Hour),
		LatestVersion: "v0.2.0",
		ReleaseURL:    "https://github.com/jakebf/planc/releases/tag/v0.2.0",
	}
	if err := saveUpdateState(statePath, st); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	var calls int
	fetchLatestReleaseF = func(owner, repo string) (*releaseInfo, error) {
		calls++
		return &releaseInfo{
			TagName: "v9.9.9",
			HTMLURL: "https://example.invalid",
		}, nil
	}

	cmd := checkForUpdate("v0.1.0")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	upd, ok := msg.(updateAvailableMsg)
	if !ok {
		t.Fatalf("expected updateAvailableMsg, got %T", msg)
	}
	if upd.version != "v0.2.0" {
		t.Fatalf("cached version = %q, want v0.2.0", upd.version)
	}
	if calls != 0 {
		t.Fatalf("expected 0 API calls when cache is fresh, got %d", calls)
	}
}

func TestCheckForUpdateFetchSuccessWritesCache(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	fixedNow := time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)
	restore := overrideUpdateGlobals(t, fixedNow)
	defer restore()

	var calls int
	fetchLatestReleaseF = func(owner, repo string) (*releaseInfo, error) {
		calls++
		return &releaseInfo{
			TagName: "v0.3.0",
			HTMLURL: "https://github.com/jakebf/planc/releases/tag/v0.3.0",
		}, nil
	}

	cmd := checkForUpdate("v0.1.0")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	upd, ok := msg.(updateAvailableMsg)
	if !ok {
		t.Fatalf("expected updateAvailableMsg, got %T", msg)
	}
	if upd.version != "v0.3.0" {
		t.Fatalf("update version = %q, want v0.3.0", upd.version)
	}
	if calls != 1 {
		t.Fatalf("expected 1 API call, got %d", calls)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var st updateState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	if !st.CheckedAt.Equal(fixedNow.UTC()) {
		t.Fatalf("checked_at = %s, want %s", st.CheckedAt, fixedNow.UTC())
	}
	if st.LatestVersion != "v0.3.0" {
		t.Fatalf("latest_version = %q, want v0.3.0", st.LatestVersion)
	}
}

func TestCheckForUpdateFetchFailureDoesNotWriteCache(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	fixedNow := time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)
	restore := overrideUpdateGlobals(t, fixedNow)
	defer restore()

	fetchLatestReleaseF = func(owner, repo string) (*releaseInfo, error) {
		return nil, fmt.Errorf("boom")
	}

	cmd := checkForUpdate("v0.1.0")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	if msg != nil {
		t.Fatalf("expected nil msg on failed fetch, got %T", msg)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no cache file to be written, err=%v", err)
	}
}

func TestCheckForReleaseNotesFirstRunStoresVersion(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	restore := overrideUpdateGlobals(t, time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC))
	defer restore()
	bundledChangelog = "## [v0.2.0] - 2026-02-21\n\n- Something\n"

	cmd := checkForReleaseNotes("v0.2.0")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil message on first run, got %T", msg)
	}

	st, err := loadUpdateState(statePath)
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if st.LastSeenVersion != "v0.2.0" {
		t.Fatalf("last_seen_version = %q, want v0.2.0", st.LastSeenVersion)
	}
}

func TestCheckForReleaseNotesUpgradeShowsRange(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	restore := overrideUpdateGlobals(t, time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC))
	defer restore()
	bundledChangelog = `# Changelog

## [v0.3.0] - 2026-02-21
- New release

## [v0.2.0] - 2026-02-20
- Prior release

## [v0.1.0] - 2026-02-19
- Old release
`

	if err := saveUpdateState(statePath, updateState{LastSeenVersion: "v0.1.0"}); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	cmd := checkForReleaseNotes("v0.3.0")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	notes, ok := msg.(releaseNotesMsg)
	if !ok {
		t.Fatalf("expected releaseNotesMsg, got %T", msg)
	}
	if notes.version != "v0.3.0" {
		t.Fatalf("version = %q, want v0.3.0", notes.version)
	}
	if !strings.Contains(notes.markdown, "v0.3.0") || !strings.Contains(notes.markdown, "v0.2.0") {
		t.Fatalf("expected notes for v0.3.0 and v0.2.0, got:\n%s", notes.markdown)
	}
	if strings.Contains(notes.markdown, "v0.1.0") {
		t.Fatalf("did not expect v0.1.0 section in notes:\n%s", notes.markdown)
	}

	st, err := loadUpdateState(statePath)
	if err != nil {
		t.Fatalf("loadUpdateState: %v", err)
	}
	if st.LastSeenVersion != "v0.1.0" {
		t.Fatalf("last_seen_version should not update until modal is dismissed; got %q", st.LastSeenVersion)
	}
}

func TestMarkReleaseNotesSeen(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	restore := overrideUpdateGlobals(t, time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC))
	defer restore()

	if err := saveUpdateState(statePath, updateState{LastSeenVersion: "v0.1.0"}); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	cmd := markReleaseNotesSeen("v0.3.0")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
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

func TestStartupUpdateCmdCombinesUpdateAndReleaseNotes(t *testing.T) {
	statePath := setupUpdateStatePath(t)
	fixedNow := time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)
	restore := overrideUpdateGlobals(t, fixedNow)
	defer restore()

	bundledChangelog = `# Changelog

## [v0.2.0] - 2026-02-20
- New release

## [v0.1.0] - 2026-02-19
- Older release
`
	if err := saveUpdateState(statePath, updateState{LastSeenVersion: "v0.1.0"}); err != nil {
		t.Fatalf("saveUpdateState: %v", err)
	}

	fetchLatestReleaseF = func(owner, repo string) (*releaseInfo, error) {
		return &releaseInfo{
			TagName: "v0.3.0",
			HTMLURL: "https://github.com/jakebf/planc/releases/tag/v0.3.0",
		}, nil
	}

	cmd := startupUpdateCmd("v0.2.0")
	if cmd == nil {
		t.Fatal("expected non-nil startup command")
	}
	msg := cmd()
	startup, ok := msg.(startupUpdateMsg)
	if !ok {
		t.Fatalf("expected startupUpdateMsg, got %T", msg)
	}
	if startup.update == nil || startup.update.version != "v0.3.0" {
		t.Fatalf("update payload = %+v, want v0.3.0", startup.update)
	}
	if startup.releaseNotes == nil || startup.releaseNotes.version != "v0.2.0" {
		t.Fatalf("release notes payload = %+v, want v0.2.0", startup.releaseNotes)
	}
	if !strings.Contains(startup.releaseNotes.markdown, "v0.2.0") {
		t.Fatalf("expected release notes markdown to include v0.2.0, got:\n%s", startup.releaseNotes.markdown)
	}
}

func setupUpdateStatePath(t *testing.T) string {
	t.Helper()
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)
	path, err := updateStatePath()
	if err != nil {
		t.Fatalf("updateStatePath: %v", err)
	}
	return path
}

func overrideUpdateGlobals(t *testing.T, now time.Time) func() {
	t.Helper()
	origBase := updateAPIBaseURL
	origNow := updateNow
	origFetch := fetchLatestReleaseF
	origChangelog := bundledChangelog
	updateNow = func() time.Time { return now }
	return func() {
		updateAPIBaseURL = origBase
		updateNow = origNow
		fetchLatestReleaseF = origFetch
		bundledChangelog = origChangelog
	}
}

func TestUpdateStatePathFollowsConfigDir(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)
	path, err := updateStatePath()
	if err != nil {
		t.Fatalf("updateStatePath: %v", err)
	}
	want := filepath.Join(cfgRoot, "planc", "update-check.json")
	if path != want {
		t.Fatalf("updateStatePath = %q, want %q", path, want)
	}
}
