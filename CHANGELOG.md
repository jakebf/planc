# Changelog

All notable changes to `planc` are documented in this file.

## [v0.2.0] - 2026-02-24

### Added
- **Labels** replace projects. Comma-separated tags with a toggle modal (`l`), batch support, and `[`/`]` to filter by label. Existing `project` frontmatter is migrated automatically.
- **Status modal** (`s`) for picking status from a visual picker instead of cycling.
- **Label filter cycling** skips labels with no visible plans, preventing empty-list dead ends.
- **Arrow key pane switching** — `←`/`→` now switch between list and preview panes (in addition to `tab`).
- Inline feedback: undo indicator and "Copied!" shown directly on plan rows.
- Background editor mode (`editor_mode` config option).

### Changed
- Config field `preamble` renamed to `prompt_prefix`.

### Removed
- `S` (reverse cycle status) — use the status modal or `0-3` direct keys instead.
- Old project input modal (`p` key) — replaced by the label system.

## [v0.1.2] - 2026-02-22

### Added
- Initial public OSS release.
- TUI for browsing Claude Code plan files with markdown preview.
- Status and project metadata via YAML frontmatter.
