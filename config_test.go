package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandCommand(t *testing.T) {
	// With {file} placeholder — expands in place, no extra arg
	args := []string{"claude", "--file", "{file}", "--verbose"}
	got := expandCommand(args, "/tmp/plan.md", "prefix: ")
	want := []string{"claude", "--file", "/tmp/plan.md", "--verbose"}
	if len(got) != len(want) {
		t.Fatalf("expandCommand len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("expandCommand[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Without placeholder + prefix — appends prefixed path
	got = expandCommand([]string{"cc"}, "/tmp/plan.md", "Read this plan file: ")
	if len(got) != 2 {
		t.Fatalf("expandCommand with prefix: len = %d, want 2", len(got))
	}
	if got[1] != "Read this plan file: /tmp/plan.md" {
		t.Errorf("expandCommand[1] = %q, want prefixed path", got[1])
	}

	// Without placeholder + empty prefix — appends raw path
	got = expandCommand([]string{"nvim"}, "/tmp/plan.md", "")
	if len(got) != 2 {
		t.Fatalf("expandCommand with empty prefix: len = %d, want 2", len(got))
	}
	if got[1] != "/tmp/plan.md" {
		t.Errorf("expandCommand[1] = %q, want raw path", got[1])
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	tests := []struct {
		in, want string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~/.claude/plans", filepath.Join(home, ".claude/plans")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~", "~"}, // no slash after ~, not expanded
	}
	for _, tt := range tests {
		got := expandHome(tt.in)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadConfigSetsInstalledAndExpandsHome(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := saveConfig(path, config{
		PlansDir: "~/plans",
		Primary:  []string{"claude"},
		Editor:   []string{"vim"},
	}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	loaded := loadConfig()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if loaded.PlansDir != filepath.Join(home, "plans") {
		t.Fatalf("PlansDir = %q, want expanded home path", loaded.PlansDir)
	}
	if loaded.Installed == "" {
		t.Fatal("Installed should be set when missing")
	}

	// Installed timestamp should be persisted to disk for future loads.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var persisted config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal persisted config: %v", err)
	}
	if persisted.Installed == "" {
		t.Fatal("persisted Installed should not be empty")
	}
}

func TestSplitShellWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"claude", []string{"claude"}},
		{"claude --verbose", []string{"claude", "--verbose"}},
		{`bash -c "echo hello"`, []string{"bash", "-c", "echo hello"}},
		{`nvim '+set ft=markdown'`, []string{"nvim", "+set ft=markdown"}},
		{`cmd "arg with spaces" plain`, []string{"cmd", "arg with spaces", "plain"}},
		{`  spaced  `, []string{"spaced"}},
		{"", nil},
		{`a "b \"c\" d" e`, []string{"a", `b "c" d`, "e"}}, // escaped quotes inside double quotes
	}
	for _, tt := range tests {
		got := splitShellWords(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("splitShellWords(%q) = %v (len %d), want %v (len %d)", tt.in, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitShellWords(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestLoadConfigPreservesPromptPrefix(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	custom := "Implement this plan: "
	if err := saveConfig(path, config{
		PlansDir:     "~/plans",
		Primary:      []string{"claude"},
		Editor:       []string{"vim"},
		PromptPrefix: custom,
	}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	loaded := loadConfig()
	if loaded.PromptPrefix != custom {
		t.Fatalf("PromptPrefix = %q, want %q", loaded.PromptPrefix, custom)
	}
}

func TestLoadConfigDefaultPromptPrefix(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	// Config with no prompt_prefix or preamble — should get default
	if err := saveConfig(path, config{
		PlansDir: "~/plans",
		Primary:  []string{"claude"},
		Editor:   []string{"vim"},
	}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	loaded := loadConfig()
	if loaded.PromptPrefix != "Read this plan file: " {
		t.Fatalf("PromptPrefix = %q, want default", loaded.PromptPrefix)
	}
}

func TestLoadConfigRawMissingFile(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	cfg := loadConfigRaw()
	if cfg.PlansDir != newDefaultConfig().PlansDir {
		t.Fatalf("loadConfigRaw with missing file: PlansDir = %q, want default %q", cfg.PlansDir, newDefaultConfig().PlansDir)
	}
}

func TestLoadConfigRawDoesNotTriggerSetup(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	// loadConfigRaw should return defaults without calling setupConfig
	cfg := loadConfigRaw()
	if cfg.Installed != "" {
		t.Fatalf("loadConfigRaw should not set Installed, got %q", cfg.Installed)
	}

	// Verify no config file was created on disk
	path, _ := configPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("loadConfigRaw should not create config file, but %s exists", path)
	}
}

func TestLoadConfigInvalidJSONFallsBackToDefaults(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{invalid"), 0644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	loaded := loadConfig()
	if loaded.PlansDir != newDefaultConfig().PlansDir {
		t.Fatalf("PlansDir = %q, want default %q", loaded.PlansDir, newDefaultConfig().PlansDir)
	}
	if strings.Join(loaded.Primary, " ") != strings.Join(newDefaultConfig().Primary, " ") {
		t.Fatalf("Primary = %v, want %v", loaded.Primary, newDefaultConfig().Primary)
	}
	if strings.Join(loaded.Editor, " ") != strings.Join(newDefaultConfig().Editor, " ") {
		t.Fatalf("Editor = %v, want %v", loaded.Editor, newDefaultConfig().Editor)
	}
}
