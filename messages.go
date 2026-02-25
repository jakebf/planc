package main

// ─── Messages ────────────────────────────────────────────────────────────────
//
// All messages are internal to the Update loop. Async tea.Cmd functions
// (in commands.go) produce these; Update handles them. Messages with an
// `id` field use generation counters to ignore stale timers.

// planContentMsg delivers glamour-rendered markdown for the preview cache.
type planContentMsg struct {
	file    string
	content string
}

// statusUpdatedMsg carries the before/after plan for status changes and undo.
type statusUpdatedMsg struct {
	oldPlan plan
	newPlan plan
}

type labelsUpdatedMsg struct {
	plan plan
}

// reloadMsg replaces the full plan list after a delete or external rescan.
type reloadMsg struct {
	plans []plan
}

// fileChangedMsg is sent by the fsnotify watcher after debounce.
type fileChangedMsg struct {
	files []string // base filenames of changed .md files
}

// configUpdatedMsg is sent after the setup wizard completes.
type configUpdatedMsg struct{}

// undoExpiredMsg fires after the 3-second undo window closes.
type undoExpiredMsg struct {
	id int
}

// batchLingerExpiredMsg fires after batch-affected items have lingered visibly.
type batchLingerExpiredMsg struct {
	id int
}

type statusClearMsg struct {
	id int
}

type notificationClearMsg struct {
	id int
}

type copiedClearMsg struct {
	id int
}

type changedSpinExpiredMsg struct {
	id int
}

type editorLaunchedMsg struct{}

type labelFlashMsg struct{}

type errMsg struct {
	err error
}

// batchDoneMsg is returned by batch status/label operations with the full
// updated plan list and a summary message for the status bar.
type batchDoneMsg struct {
	plans   []plan
	files   []string
	message string
}

type updateAvailableMsg struct {
	version string
	url     string
}

type releaseNotesMsg struct {
	version  string
	markdown string
}

type startupUpdateMsg struct {
	update       *updateAvailableMsg
	releaseNotes *releaseNotesMsg
}
