# Changelog

All notable changes to `planc` are documented in this file.

## [v0.2.1] - 2026-02-25

### Fixed
- ToC pane scroll windowing miscounted header lines and didn't reserve space for ↑/↓ overflow indicators, causing entries to be clipped or hidden.
- Entering/exiting comment mode now recalculates layout (previously only happened on terminal resize).

### Changed
- Setup wizard (`--setup`) now shows descriptive hints for each field, clearer field names ("Additional plans" instead of "Project plans"), and a `"none"` option to clear the project plans glob.

## [v0.2.0] - 2026-02-24

### Added
- **Comment mode** (`enter`/`o`). Opens a table-of-contents pane built from the plan's headings with a synchronized rendered preview. Navigate headings with `j`/`k`, add inline `> **[comment]:**` annotations on any heading, edit or delete existing comments, and set status/labels without leaving the mode. `n`/`p` jump between plan files.
- **Project plan directories** via the `project_plans_glob` config field. A glob pattern (supporting `**`) is expanded at startup to find plan directories alongside the central `~/.claude/plans/` folder. Matched directories are watched for live updates. Known heavy directories (`node_modules`, `.git`, etc.) are skipped during glob resolution for fast startup.
- **Labels** replace projects. Comma-separated tags with a toggle modal (`l`), batch support, and `[`/`]` to filter by label. Existing `project` frontmatter is migrated automatically.
- **Status modal** (`s`) for picking status from a visual picker instead of cycling.
- **Label filter cycling** skips labels with no visible plans, preventing empty-list dead ends.
- **Arrow key pane switching** — `←`/`→` now switch between list and preview panes (in addition to `tab`).
- Inline feedback: undo indicator and "Copied!" shown directly on plan rows.
- Background editor mode (`editor_mode` config option); terminal editors (vim, nvim, nano, etc.) auto-detected as foreground.

### Changed
- **`enter` opens comment mode** instead of launching the primary command. The primary (coding agent) command is now `c`.
- **`C` (uppercase) copies file path** to clipboard (was lowercase `c`).
- **Status values** renamed: `pending` is now `reviewed`. Existing `pending` frontmatter is migrated on read. The four statuses are: `new` (unset), `reviewed`, `active`, `done`.
- **`a` toggles done plans** (previously described as "show all / active only").
- **`~` cycles status** (replaces the old `s` cycling behavior; `s` now opens the modal).
- Config field `preamble` renamed to `prompt_prefix`.
- Default `prompt_prefix` updated to `"Read this plan file and review any comments: "`.
- Project plan rows show their parent directory and path in the list and preview title.

### Removed
- `S` (reverse cycle status) — use the status modal or `0-3` direct keys instead.
- Old project input modal (`p` key) — replaced by the label system.

## [v0.1.2] - 2026-02-22

### Added
- Initial public OSS release.
- TUI for browsing Claude Code plan files with markdown preview.
- Status and project metadata via YAML frontmatter.
