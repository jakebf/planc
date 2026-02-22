package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Clod Code ───────────────────────────────────────────────────────────────
//
// Fake "Clod Code" AI assistant screen for demo mode. Pressing enter or e on a
// plan in demo mode opens this full-screen view. The user types a prompt and
// presses enter; a canned animation plays through scripted steps (response
// blocks, tool calls, thinking) and then auto-exits back to planc.

// clodState holds all state for the fake Clod Code screen.
type clodState struct {
	active   bool
	done     bool   // true when animation finished, showing bottom prompt
	tickID   int    // generation counter — stale ticks are ignored
	planFile string // filename shown in prompt
	project  string // project name for ~/code/<project>
	preamble string // pre-filled prompt text (preamble + filename)
	input    string // characters typed at the bottom prompt
	step     int    // current index into clodScript
}

// clodTickMsg drives the Clod animation forward one step.
type clodTickMsg struct {
	id int
}

// clodStepKind describes what a script step renders.
type clodStepKind int

const (
	clodText     clodStepKind = iota // ● response text block
	clodToolCall                      // ● ToolName(args) with ⎿ output
	clodThinking                      // ✻ thinking indicator
)

// clodStep is one unit of the animation script.
type clodStep struct {
	kind   clodStepKind
	text   string        // main content
	output string        // tool call output (shown after ⎿)
	delay  time.Duration // pause after showing this step
}

// clodScript is the fixed sequence of steps that plays after the user submits
// a prompt. The {file} placeholder is replaced with planFile at render time.
var clodScript = []clodStep{
	// Turn 1: read the plan
	{kind: clodThinking, text: "Percolating", delay: 1500 * time.Millisecond},
	{kind: clodText, text: "Let me read through this plan to give you a thorough review.", delay: 400 * time.Millisecond},
	{kind: clodToolCall, text: "Read {file}", output: "", delay: 400 * time.Millisecond},
	{kind: clodThinking, text: "Kneading", delay: 1200 * time.Millisecond},
	{kind: clodThinking, text: "Marinating", delay: 1400 * time.Millisecond},
	{kind: clodText, text: "The scope is well-defined and the milestones are in a good\n" +
		"  order. A few things stood out:\n\n" +
		"  1. The architecture section is clean — splitting by concern\n" +
		"     makes each piece independently testable.\n\n" +
		"  2. I'd recommend adding an explicit error handling strategy\n" +
		"     before starting implementation.\n\n" +
		"  3. The third milestone has some implicit dependencies on the\n" +
		"     first two that should be called out.\n\n" +
		"  Want me to start implementing?", delay: 0},
}

// ─── Lifecycle ───────────────────────────────────────────────────────────────

func (m *model) enterClod(p plan) tea.Cmd {
	preamble := m.cfg.Preamble + p.file
	m.clod = clodState{
		active:   true,
		tickID:   m.clod.tickID + 1,
		planFile: p.file,
		project:  p.project,
		preamble: preamble,
		step:     -1, // not started yet
	}
	return m.clodTick(500 * time.Millisecond)
}

func (m *model) exitClod() {
	m.clod.active = false
	m.clod.tickID++
}

func (m *model) clodTick(d time.Duration) tea.Cmd {
	id := m.clod.tickID
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clodTickMsg{id: id}
	})
}

// ─── State machine ───────────────────────────────────────────────────────────

func (m *model) advanceClod() tea.Cmd {
	m.clod.step++
	if m.clod.step >= len(clodScript) {
		m.clod.done = true
		return nil
	}
	return m.clodTick(clodScript[m.clod.step].delay)
}

// ─── Key handling ────────────────────────────────────────────────────────────

func (m model) handleClodKey(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit, true
	}

	// Done: bottom prompt accepts input, enter exits
	if m.clod.done {
		switch msg.Type {
		case tea.KeyEnter:
			m.exitClod()
			return m, m.renderWindow(), true
		case tea.KeyBackspace:
			if len(m.clod.input) > 0 {
				m.clod.input = m.clod.input[:len(m.clod.input)-1]
			}
			return m, nil, true
		case tea.KeyRunes:
			m.clod.input += string(msg.Runes)
			return m, nil, true
		case tea.KeySpace:
			m.clod.input += " "
			return m, nil, true
		case tea.KeyEsc:
			m.exitClod()
			return m, m.renderWindow(), true
		}
		return m, nil, true
	}

	// Animation playing: swallow everything
	return m, nil, true
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m model) clodView() string {
	var b strings.Builder

	// Mascot + branding
	b.WriteString(m.clodHeader())
	b.WriteString("\n")

	// Top rule + prompt
	rule := clodRule(m.width)
	b.WriteString(rule + "\n")
	promptStyle := lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	b.WriteString(promptStyle.Render("❯") + " " + m.clod.preamble + "\n")
	b.WriteString(rule + "\n")

	// Render all completed steps + current step
	if m.clod.step >= 0 {
		b.WriteString("\n")
		bulletStyle := lipgloss.NewStyle().Bold(true).Foreground(colorFull)
		thinkStyle := lipgloss.NewStyle().Foreground(colorYellow)
		dimStyle := lipgloss.NewStyle().Foreground(colorDim)
		outputStyle := lipgloss.NewStyle().Foreground(colorDim)

		lastThinking := -1 // track which thinking step to show (only latest)
		for i := 0; i <= m.clod.step && i < len(clodScript); i++ {
			s := clodScript[i]
			text := strings.ReplaceAll(s.text, "{file}", m.clod.planFile)
			switch s.kind {
			case clodText:
				lastThinking = -1
				b.WriteString(bulletStyle.Render("●") + " " + text + "\n\n")
			case clodToolCall:
				lastThinking = -1
				b.WriteString(bulletStyle.Render("●") + " " + text + "\n")
				out := strings.ReplaceAll(s.output, "{file}", m.clod.planFile)
				if out == "" {
					out = "(No output)"
				}
				b.WriteString("  ⎿  " + outputStyle.Render(out) + "\n\n")
			case clodThinking:
				lastThinking = i
			}
		}
		// Show the latest thinking indicator (replaces previous ones)
		if lastThinking >= 0 {
			text := strings.ReplaceAll(clodScript[lastThinking].text, "{file}", m.clod.planFile)
			b.WriteString(thinkStyle.Render("✻ "+text+"…") + " " + dimStyle.Render("(thinking)") + "\n")
		}
	}

	// Bottom prompt after animation completes
	if m.clod.done {
		rule := clodRule(m.width)
		promptStyle := lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
		b.WriteString(rule + "\n")
		b.WriteString(promptStyle.Render("❯") + " " + m.clod.input + "█\n")
		b.WriteString(rule + "\n")
	}

	// Pad to fill terminal
	content := b.String()
	lines := strings.Count(content, "\n")
	for i := lines; i < m.height-1; i++ {
		content += "\n"
	}
	return content
}

func (m model) clodHeader() string {
	accentStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	eyeStyle := lipgloss.NewStyle().Foreground(colorGreen)
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim)

	type headerLine struct {
		left  string
		right string
	}

	project := m.clod.project
	if project == "" {
		project = "myproject"
	}

	lines := []headerLine{
		{
			left:  "        " + accentStyle.Render("*"),
			right: "",
		},
		{
			left:  "       " + bodyStyle.Render("╱ ╲"),
			right: "",
		},
		{
			left:  "      " + bodyStyle.Render("╱   ╲"),
			right: accentStyle.Render("Clod Code") + dimStyle.Render(" v9.0.0"),
		},
		{
			left:  "     " + bodyStyle.Render("╱ ") + eyeStyle.Render("▪ ▪") + bodyStyle.Render(" ╲"),
			right: dimStyle.Render("Gnopus 7 · Clod Ultra"),
		},
		{
			left:  "    " + bodyStyle.Render("╱  ") + bodyStyle.Render("───") + bodyStyle.Render("  ╲"),
			right: dimStyle.Render("~/code/" + project),
		},
		{
			left:  "   " + bodyStyle.Render("╱_________╲"),
			right: "",
		},
	}

	var b strings.Builder
	b.WriteString("\n")
	for _, l := range lines {
		leftW := lipgloss.Width(l.left)
		pad := 24 - leftW
		if pad < 0 {
			pad = 0
		}
		b.WriteString(l.left + strings.Repeat(" ", pad) + l.right + "\n")
	}
	return b.String()
}

func clodRule(width int) string {
	w := width - 2
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", w))
}
