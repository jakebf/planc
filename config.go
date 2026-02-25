package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ─── Config ──────────────────────────────────────────────────────────────────

type config struct {
	PlansDir        string   `json:"plans_dir"`                    // path to agent plans directory
	ProjectPlanGlob string   `json:"project_plans_glob,omitempty"` // glob pattern for project plan directories
	Primary         []string `json:"primary"`                      // enter: main AI assistant
	Editor          []string `json:"editor"`                       // e: text editor
	PromptPrefix    string   `json:"prompt_prefix"`                // prefix for primary command path arg
	EditorMode      string   `json:"editor_mode,omitempty"`        // "background", "foreground", or "" (auto)
	ShowAll         bool     `json:"show_all,omitempty"`           // persist active vs all filter
	Installed       string   `json:"installed,omitempty"`          // RFC3339 timestamp of first setup
}

func defaultPlansDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "plans")
}

// newDefaultConfig returns a fresh default config. Must be a function (not a var)
// because json.Unmarshal can mutate slice elements in-place via the shared backing
// array from a shallow struct copy.
func newDefaultConfig() config {
	return config{
		PlansDir:     defaultPlansDir(),
		Primary:      []string{"claude"},
		Editor:       []string{"code"},
		PromptPrefix: "Read this plan file and review any comments: ",
	}
}

func configPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config directory: %w", err)
	}
	return filepath.Join(cfgDir, "planc", "config.json"), nil
}

// expandHome expands a leading "~/" to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// contractHome replaces the user's home directory prefix with "~/" for display.
func contractHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rel
	}
	return path
}

// loadConfigRaw reads the config file without triggering first-time setup.
// Returns defaults if the file is missing or unreadable.
func loadConfigRaw() config {
	path, err := configPath()
	if err != nil {
		return newDefaultConfig()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return newDefaultConfig()
	}
	cfg := newDefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return newDefaultConfig()
	}
	cfg.PlansDir = expandHome(cfg.PlansDir)
	if cfg.PromptPrefix == "" {
		cfg.PromptPrefix = newDefaultConfig().PromptPrefix
	}
	return cfg
}

func loadConfig() config {
	path, err := configPath()
	if err != nil {
		return newDefaultConfig()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return setupConfig(path)
		}
		return newDefaultConfig()
	}
	cfg := newDefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: corrupt config (%v), using defaults. Run `planc --setup` to fix.\n", err)
		return newDefaultConfig()
	}
	cfg.PlansDir = expandHome(cfg.PlansDir)
	if cfg.PromptPrefix == "" {
		cfg.PromptPrefix = newDefaultConfig().PromptPrefix
	}
	if cfg.Installed == "" {
		cfg.Installed = time.Now().Format(time.RFC3339)
		_ = saveConfig(path, cfg)
	}
	return cfg
}

func saveConfig(path string, cfg config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// Atomic write: write to temp file then rename, so a crash mid-write
	// can't leave a truncated config file that gets silently replaced with defaults.
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func setupConfig(path string) config {
	scanner := bufio.NewScanner(os.Stdin)
	showWelcome(scanner)
	cfg := newDefaultConfig()
	cfg.Installed = time.Now().Format(time.RFC3339)
	return runSetup(path, cfg, scanner)
}

// showWelcome displays a brief orientation and waits for the user to press
// enter before continuing to setup.
func showWelcome(scanner *bufio.Scanner) {
	brand := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	dim := lipgloss.NewStyle().Foreground(colorDim)
	dimBold := lipgloss.NewStyle().Bold(true).Foreground(colorDim)
	key := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	green := lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	yellow := lipgloss.NewStyle().Bold(true).Foreground(colorYellow)

	name := brand.Render("planc")
	clear := strings.Repeat(" ", 10)

	// Cycle the app's status icons: · → ○ → ● → ✓ → ●
	icons := []struct {
		icon  string
		style lipgloss.Style
	}{
		{"·", dim},
		{"○", yellow},
		{"●", green},
		{"✓", dim},
		{"●", green},
	}

	fmt.Println()
	for i, s := range icons {
		fmt.Printf("\r  %s %s%s", s.style.Render(s.icon), name, clear)
		if i < len(icons)-1 {
			time.Sleep(300 * time.Millisecond)
		}
	}
	fmt.Println()
	time.Sleep(400 * time.Millisecond)
	fmt.Println(dim.Render("  A tiny TUI for browsing and annotating AI agent plans."))
	fmt.Println()

	time.Sleep(400 * time.Millisecond)
	fmt.Println("  " + dim.Render("Scans your ") + dimBold.Render("plans") + dim.Render(" directory for .md files and presents"))
	fmt.Println(dim.Render("  them in a two-pane layout with rendered markdown preview."))
	fmt.Println()
	time.Sleep(300 * time.Millisecond)
	fmt.Println("  " + key.Render("s") + dim.Render(" set status      ") + key.Render("l") + dim.Render(" set labels      ") + key.Render("x") + dim.Render(" batch select"))
	fmt.Println("  " + key.Render("enter") + dim.Render(" view plan   ") + key.Render("e") + dim.Render(" edit plan       ") + key.Render("c") + dim.Render(" coding agent"))
	fmt.Println("  " + key.Render("n/p") + dim.Render("   next/prev   ") + key.Render("?") + dim.Render(" all keybindings"))
	fmt.Println()
	time.Sleep(200 * time.Millisecond)
	fmt.Println(dim.Render("  Status and labels are stored as YAML frontmatter."))
	fmt.Println(dim.Render("  Plans with no user action are not modified at all."))
	fmt.Println()

	fmt.Print(dim.Render("  Press enter to continue to setup..."))
	scanner.Scan()
	fmt.Println()
}

func runSetup(path string, current config, scanner *bufio.Scanner) config {
	promptStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	if scanner == nil {
		scanner = bufio.NewScanner(os.Stdin)
	}

	fmt.Println(promptStyle.Render("  planc setup"))
	fmt.Println(dimStyle.Render("  Press enter to keep the current value."))
	fmt.Println()

	prompt := func(label, defVal string) string {
		fmt.Printf("%s %s: ", promptStyle.Render(label), dimStyle.Render("["+defVal+"]"))
		if scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				return line
			}
		}
		return defVal
	}

	cfg := current

	// Agent plans path
	fmt.Println(dimStyle.Render("  Primary directory to scan for .md plan files."))
	cfg.PlansDir = expandHome(prompt("Agent plans path        ", current.PlansDir))
	fmt.Println()

	// Additional plans glob
	fmt.Println(dimStyle.Render("  Scan additional directories for plans, e.g. per-project plans/"))
	fmt.Println(dimStyle.Render("  folders. Use ** to match across projects: ~/code/**/plans"))
	projectDefault := current.ProjectPlanGlob
	if projectDefault == "" {
		fmt.Printf("%s %s: ", promptStyle.Render("Additional plans (glob) "), dimStyle.Render("[]"))
	} else {
		fmt.Printf("%s %s: ", promptStyle.Render("Additional plans (glob) "), dimStyle.Render("["+projectDefault+"]")+" "+dimStyle.Render(`"none" to clear`))
	}
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			cfg.ProjectPlanGlob = projectDefault
		case strings.EqualFold(line, "none"):
			cfg.ProjectPlanGlob = ""
		default:
			cfg.ProjectPlanGlob = line
		}
	}
	fmt.Println()

	// Editor command
	fmt.Println(dimStyle.Render("  Command to open a plan for editing (e key)."))
	cfg.Editor = splitShellWords(prompt("Editor command          ", strings.Join(current.Editor, " ")))
	fmt.Println()

	// Coding agent command
	fmt.Println(dimStyle.Render("  Command to send a plan to your coding agent (c key)."))
	fmt.Println(dimStyle.Render("  The plan path is appended as the last argument."))
	cfg.Primary = splitShellWords(prompt("Coding agent command    ", strings.Join(current.Primary, " ")))
	fmt.Println()

	// Prompt prefix
	fmt.Println(dimStyle.Render("  Text prepended to the plan path when passed to the coding agent."))
	cfg.PromptPrefix = prompt("Prompt prefix           ", current.PromptPrefix)
	fmt.Println()

	if err := saveConfig(path, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save config: %v\n", err)
	} else {
		fmt.Printf("%s %s\n\n", dimStyle.Render("Saved to"), path)
	}
	return cfg
}

// splitShellWords splits a string into words, respecting single and double quotes.
// Unquoted whitespace separates words. Quotes are consumed (not included in output).
func splitShellWords(s string) []string {
	var words []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\\' && inDouble && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

// expandCommand replaces {file} in the command template with the actual file path.
// If no argument contains {file}, the path is appended as a trailing argument
// with the given prefix (e.g. "Read this plan file: " for AI commands, "" for editors).
func expandCommand(args []string, filePath string, prefix string) []string {
	hasPlaceholder := false
	for _, a := range args {
		if strings.Contains(a, "{file}") {
			hasPlaceholder = true
			break
		}
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "{file}", filePath)
	}
	if !hasPlaceholder {
		out = append(out, prefix+filePath)
	}
	return out
}

// isTerminalEditor returns true if the command appears to be a terminal-based editor.
func isTerminalEditor(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}
	base := filepath.Base(cmd[0])
	switch base {
	case "vim", "vi", "nvim", "nano", "emacs", "hx", "micro":
		return true
	}
	return false
}

// effectiveEditorMode resolves the editor mode: "foreground" for terminal editors,
// "background" for GUI editors, unless explicitly overridden.
func effectiveEditorMode(cfg config) string {
	if cfg.EditorMode == "foreground" || cfg.EditorMode == "background" {
		return cfg.EditorMode
	}
	if isTerminalEditor(cfg.Editor) {
		return "foreground"
	}
	return "background"
}

// commandLabel returns the base name of the first element in a command slice.
func commandLabel(cmd []string) string {
	if len(cmd) == 0 {
		return "unknown"
	}
	return filepath.Base(cmd[0])
}

// shellQuote returns a quoted shell string appropriate for the current platform.
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		// cmd.exe double-quote escaping: double any internal quotes.
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// shellCommand builds an exec.Cmd that runs args through the user's shell.
// On Unix, uses $SHELL -ic for interactive mode (aliases, rc files).
// On Windows, uses cmd.exe /C.
func shellCommand(args ...string) *exec.Cmd {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", append([]string{"/C"}, quoted...)...)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	return exec.Command(shell, "-ic", strings.Join(quoted, " "))
}
