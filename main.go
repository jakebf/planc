package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

var version = ""

func getVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("planc — a TUI for browsing Claude Code plans")
		fmt.Println()
		fmt.Println("Usage: planc [flags]")
		fmt.Println()
		fmt.Println("Flags:")
		fmt.Println("  --help, -h    Show this help")
		fmt.Println("  --version     Print version")
		fmt.Println("  --setup       Re-run first-time configuration")
		fmt.Println("  --demo        Launch with demo data")
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("planc " + getVersion())
		return
	}

	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "-") &&
		os.Args[1] != "--setup" && os.Args[1] != "--demo" {
		fmt.Fprintf(os.Stderr, "unknown flag: %s\nRun planc --help for usage.\n", os.Args[1])
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "--setup" {
		path, err := configPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		runSetup(path, loadConfigRaw(), nil) // loadConfigRaw avoids triggering first-time setup
		return
	}

	cfg := loadConfig()
	dir := cfg.PlansDir
	if dir == "" {
		fmt.Fprintf(os.Stderr, "Error: could not determine plans directory (is $HOME set?)\n")
		os.Exit(1)
	}

	plans, scanErr := scanPlans(dir)
	if scanErr != nil {
		if os.IsNotExist(scanErr) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating plans directory: %v\n", err)
				os.Exit(1)
			}
			plans = nil
		} else {
			fmt.Fprintf(os.Stderr, "Error scanning plans: %v\n", scanErr)
			os.Exit(1)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not start file watcher: %v\n", err)
	} else {
		defer watcher.Close()
		if err := watcher.Add(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not watch directory: %v\n", err)
		}
	}

	m := newModel(plans, dir, cfg, watcher)
	if len(os.Args) > 1 && os.Args[1] == "--demo" {
		m.enterDemoMode()
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
