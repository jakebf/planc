# planc

A little TUI for browsing your [Claude Code](https://docs.anthropic.com/en/docs/claude-code) plans.

![planc demo](demo.gif)

Claude Code generates plan files with wonderfully unguessable names like `humming-marinating-narwhal.md`. `planc` scans your plans directory, shows them in a navigable list with a rendered markdown preview, and lets you open plans in Claude, your editor, or whatever else you've got — without ever leaving the terminal. You can also provide some minimal organization (statuses, labels) via YAML frontmatter.

## Install

```
go install github.com/jakebf/planc@latest
```

Or clone and build:

```
git clone https://github.com/jakebf/planc.git
cd planc && go build
```

## Quick start

1. Run `planc`
2. On first launch it'll ask you to configure your commands, then you're off

That's it. `planc` scans `~/.claude/plans/` for `.md` files automatically. Plans update live as Claude works on them.

## How it works

`planc` reads each `.md` file in your plans directory and extracts:

- **Title** from the first `# ` heading
- **Date** from the file's creation time
- **Status** and **labels** from optional YAML frontmatter

Plans work with zero frontmatter. Metadata is only written when you take action — setting a status with `s` or adding labels with `l`.

By default, only plans with a status (`pending` or `active`) are shown, plus any untagged plans modified after you first ran `planc`. Older pre-existing files stay hidden until you tag them. Press `a` to toggle visibility of all plans.

`planc` watches the plans directory for changes. When another process (like Claude Code) edits a plan file, the preview updates automatically with scroll position preserved.

### Frontmatter format

```yaml
---
status: active
labels: backend, auth
---
```

Status values: `unset`, `pending`, `active`, `done`. Press `s` to pick from a modal or `0-3` to set directly.

Labels are comma-separated tags for organizing plans. Press `l` to open the label modal, where you can toggle existing labels or type a new one. Use `[`/`]` to filter the plan list by label.

Only non-default fields are written. A plan you've never touched has no frontmatter at all. Plans are sorted by file creation time (newest first).

### Teaching Claude Code about frontmatter

If you want Claude Code to set plan statuses automatically, add something like this to your `~/CLAUDE.md`:

```
Plan files in ~/.claude/plans/ can include YAML frontmatter.
Use:
- status: pending | active | done
- labels: comma-separated tags (e.g. backend, auth)
When creating a plan, set status: pending.
When starting work, set status: active.
When finished, set status: done.
```

## Configuration

On first run, `planc` walks you through setup. Re-run anytime with `planc --setup`. Config lives at:

- **Linux**: `~/.config/planc/config.json`
- **macOS**: `~/Library/Application Support/planc/config.json`
- **Windows**: `%AppData%\planc\config.json`

```json
{
  "plans_dir": "~/.claude/plans",
  "primary": ["claude"],
  "editor": ["code"],
  "prompt_prefix": "Read this plan file: "
}
```

If a command includes `{file}`, it is replaced with the selected plan path. If `{file}` is not present, `planc` appends the plan path as the last argument. For the primary command, the appended path is prefixed with the configurable `prompt_prefix` so AI assistants get context. Edit the config file directly or run `planc --setup` to reconfigure.

`planc` checks for updates once a day at startup.

## Keybindings

| Key | Action |
|-----|--------|
| `j`/`k` | Navigate list / scroll preview |
| `tab` / `←`/`→` | Switch panes |
| `enter` | Open in primary command |
| `e` | Open in editor |
| `s` | Status (pick from modal) |
| `0-3` | Set status directly (0=unset, 1=pending, 2=active, 3=done) |
| `u` | Undo last status change |
| `l` | Labels (toggle/add in modal) |
| `[`/`]` | Cycle label filter |
| `a` | Show all / active only |
| `x` | Select (batch mode) |
| `c` | Copy file path to clipboard |
| `/` | Search |
| `#` | Delete (with confirmation) |
| `d` | Demo mode |
| `?` | Help |
| `,` | Settings |
| `q` | Quit |

## License

[Unlicense](LICENSE) — public domain
