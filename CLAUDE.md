# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
go build            # builds ./planc binary
go test ./...       # all tests
go test -run TestX  # single test
go test -bench .    # benchmarks
```

## Screenshot

Optionally regenerate the demo screenshot after UI changes: `go build && vhs demo.tape`
Requires `vhs`, `ttyd`, and `ffmpeg`.

## Architecture

Bubble Tea TUI with Model → Update → View cycle. Single package:

- **main.go** — Entry point and CLI flags
- **model.go** — Model struct, keyMap, constructor, Init, Update, modal key handlers
- **view.go** — View function, styles, rendering helpers
- **version.go** — Version checking, release notes, changelog parsing
- **plan.go** — Plan type, `planStore` interface, scanning, filtering, frontmatter parsing, sorting
- **config.go** — Config struct, setup wizard, shell command helpers
- **commands.go** — Async `tea.Cmd` functions (render, delete, status update, file watcher), `diskStore`
- **messages.go** — Message types for the Update loop
- **delegate.go** — List item delegate (custom rendering)
- **demo.go** — Demo mode: `demoStore` (in-memory `planStore`), embedded `demo_content.json`, `--demo` flag
- **birthtime_\*.go** — Platform-specific file creation time extraction

### Plan pipeline

`scanPlans(dir)` reads `.md` files → `parseFrontmatter()` extracts optional YAML (`status`, `project`) → `parseHeader()` gets first `#` heading as title → sorted by creation time descending. Frontmatter is lazy: only written when the user takes action (`s` to cycle status, `p` to set project). Plans with no user action have no frontmatter.

### Async rendering

All disk/glamour work runs as `tea.Cmd` functions returning typed messages (`planContentMsg`, `statusUpdatedMsg`, `batchDoneMsg`, etc.). Preview cache maps filename → rendered markdown. Lazy rendering prefetches ±2 neighbors around the selected plan. Cache invalidated on resize.

### Key patterns

- **Frontmatter writes**: `setFrontmatter()` uses `os.WriteFile` (not atomic rename) to preserve file birth time for created-sort order
- **File watcher**: fsnotify on the plans dir with 100ms debounce; skipped during demo mode
- **Undo**: 3-second window after status change (`undoExpiredMsg` timer)
- **Batch ops**: `x` to select, then `s`/`0-3`/`p` to bulk update; selection cleared after
- **Shell commands**: Runs through `$SHELL -ic` for alias/rc loading; `{file}` placeholders are expanded and, if missing, the plan path is appended as the final argument
