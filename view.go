package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ─── Colors ──────────────────────────────────────────────────────────────────

var (
	colorBlack   = lipgloss.Color("0")
	colorAccent  = lipgloss.Color("5")  // magenta — brand, focused borders, keys
	colorDim     = lipgloss.Color("8")  // gray — secondary text, unfocused borders
	colorFull    = lipgloss.Color("7")  // white — full help descriptions
	colorGreen   = lipgloss.Color("10") // active status, welcome checkmark
	colorYellow  = lipgloss.Color("11") // pending status, update notices
	colorMagenta = lipgloss.Color("13") // selection highlight, status bar messages
)

// ─── Styles ──────────────────────────────────────────────────────────────────

var (
	focusedBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent)
	unfocusedBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorDim)
	paneTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Padding(0, 1)
	helpTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).MarginBottom(1)
	helpBoxStyle    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 3)
	statusTextStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMagenta)
	updateTextStyle = lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
)

func truncateForWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	limit := maxWidth - 1
	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw > limit {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	return b.String() + "…"
}

func (m *model) releaseNotesDims() (modalW, modalH, contentW, contentH int) {
	modalW = m.width - 4
	if modalW > 96 {
		modalW = 96
	}
	if modalW < 32 {
		modalW = 32
	}

	modalH = m.height - 4
	if modalH > 36 {
		modalH = 36
	}
	if modalH < 10 {
		modalH = 10
	}

	// helpBoxStyle has 1-col borders + 3-col horizontal padding per side.
	contentW = modalW - 8
	if contentW < 20 {
		contentW = 20
	}

	// Two lines reserved for header and footer hint.
	contentH = modalH - 6
	if contentH < 3 {
		contentH = 3
	}
	return modalW, modalH, contentW, contentH
}

func renderMarkdownBody(markdown, style string, width int) string {
	pw := width
	if pw < 20 {
		pw = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(pw),
	)
	if err != nil {
		return markdown
	}
	rendered, err := r.Render(markdown)
	if err != nil {
		return markdown
	}
	return rendered
}

func (m *model) refreshReleaseNotesView() {
	if !m.releaseNotes.on || m.releaseNotes.markdown == "" || m.width <= 0 || m.height <= 0 {
		return
	}
	_, _, contentW, contentH := m.releaseNotesDims()
	m.releaseNotes.viewport.Width = contentW
	m.releaseNotes.viewport.Height = contentH
	m.releaseNotes.viewport.SetContent(renderMarkdownBody(m.releaseNotes.markdown, m.glamourStyle, contentW))
	m.releaseNotes.viewport.GotoTop()
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "Loading..."
	}
	if m.clod.active {
		return m.clodView()
	}

	// 40/60 split: list pane gets 40% of terminal width, preview gets the rest.
	listW := m.width * 40 / 100
	previewW := m.width - listW

	innerH := m.height - 3 // -2 for borders, -1 for hint bar

	var leftStyle, rightStyle lipgloss.Style
	if m.focused == listPane {
		leftStyle = focusedBorder.Width(listW - 2).Height(innerH)
		rightStyle = unfocusedBorder.Width(previewW - 2).Height(innerH)
	} else {
		leftStyle = unfocusedBorder.Width(listW - 2).Height(innerH)
		rightStyle = focusedBorder.Width(previewW - 2).Height(innerH)
	}

	var leftContent string
	if m.demo.active && len(m.list.Items()) == 0 {
		hint := lipgloss.NewStyle().Foreground(colorDim).
			Width(listW - 4).Align(lipgloss.Center).
			Render("All demo plans deleted\n\nPress d to exit demo mode")
		leftContent = lipgloss.Place(listW-2, innerH, lipgloss.Center, lipgloss.Center, hint)
	} else if !m.demo.active && len(m.allPlans) == 0 {
		hint := lipgloss.NewStyle().Foreground(colorDim).
			Width(listW - 4).Align(lipgloss.Center).
			Render("No plans yet\n\nUse plan mode in Claude Code\nand get planning!\n\n~/.claude/plans/\n\nd  try demo mode")
		leftContent = lipgloss.Place(listW-2, innerH, lipgloss.Center, lipgloss.Center, hint)
	} else if !m.showDone && len(m.list.Items()) == 0 {
		msg := "No active plans\n\na show all  ·  s set status  ·  p set project\n\nStatus and project are stored as YAML\nfrontmatter in your plan files."
		if !m.demo.active {
			msg += "\n\nd  try demo mode"
		}
		hint := lipgloss.NewStyle().Foreground(colorDim).
			Width(listW - 4).Align(lipgloss.Center).
			Render(msg)
		leftContent = lipgloss.Place(listW-2, innerH, lipgloss.Center, lipgloss.Center, hint)
	} else {
		leftContent = m.list.View()
	}
	previewTitle := ""
	if item, ok := m.list.SelectedItem().(plan); ok {
		previewTitle = paneTitleStyle.Render(item.file)
	}
	rightContent := previewTitle + "\n" + m.viewport.View()

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(rightContent),
	)

	var statusBar string
	if len(m.selected) > 0 && !m.settingProject {
		count := len(m.selected)
		first := m.firstSelectedPlan()
		sLabel := nextStatus[first.status]
		if sLabel == "" {
			sLabel = "status"
		}
		hintStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
		dimStyle := lipgloss.NewStyle().Foreground(colorDim)
		statusBar = " " + statusTextStyle.Render(fmt.Sprintf("%d selected", count)) + "  " +
			hintStyle.Render("s") + dimStyle.Render(" "+sLabel) + dimStyle.Render(" | ") +
			hintStyle.Render("0") + dimStyle.Render(" unset") + dimStyle.Render(" | ") +
			hintStyle.Render("1") + dimStyle.Render(" pending") + dimStyle.Render(" | ") +
			hintStyle.Render("2") + dimStyle.Render(" active") + dimStyle.Render(" | ") +
			hintStyle.Render("3") + dimStyle.Render(" done") + dimStyle.Render(" | ") +
			hintStyle.Render("p") + dimStyle.Render(" project") + dimStyle.Render(" | ") +
			hintStyle.Render("a") + dimStyle.Render(" all") + dimStyle.Render(" | ") +
			hintStyle.Render("esc") + dimStyle.Render(" clear")
	} else if m.settingProject {
		hintStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
		dimStyle := lipgloss.NewStyle().Foreground(colorDim)
		statusBar = " project: " + m.projectInput.View() + "  "
		if m.projectInput.Value() == "" {
			max := 9
			if len(m.projectChoices) < max {
				max = len(m.projectChoices)
			}
			for i := 0; i < max; i++ {
				if i > 0 {
					statusBar += dimStyle.Render(" | ")
				}
				statusBar += hintStyle.Render(strconv.Itoa(i+1)) + dimStyle.Render(" "+m.projectChoices[i])
			}
			if max > 0 {
				statusBar += dimStyle.Render(" | ")
			}
			statusBar += hintStyle.Render("0") + dimStyle.Render(" clear")
		}
	} else if m.status.text != "" {
		statusBar = " " + m.status.spinner.View() + " " + statusTextStyle.Render(m.status.text)
	} else if m.updateAvailable != nil {
		notice := fmt.Sprintf("Update %s available · go install github.com/jakebf/planc@latest", m.updateAvailable.version)
		statusBar = " " + updateTextStyle.Render(truncateForWidth(notice, m.width-1))
	} else {
		statusBar = " " + m.help.ShortHelpView(m.keys.ShortHelp())
	}
	base := panes + "\n" + statusBar

	if m.releaseNotes.on {
		modalW, _, contentW, _ := m.releaseNotesDims()
		header := helpTitleStyle.Render("What's New in " + m.releaseNotes.version)
		footer := lipgloss.NewStyle().Foreground(colorDim).
			Render("enter/esc dismiss  ·  j/k or space/B scroll")
		body := lipgloss.NewStyle().MaxWidth(contentW).Render(
			header + "\n" + m.releaseNotes.viewport.View() + "\n" + footer,
		)
		overlay := helpBoxStyle.MaxWidth(modalW).Render(body)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(colorBlack),
		)
	}

	if m.help.ShowAll {
		content := helpTitleStyle.Render("Keybindings") + "\n" + m.help.FullHelpView(m.keys.FullHelp())

		// Keep the help modal comfortably narrow on wide terminals while still
		// fitting on small screens.
		modalMaxW := m.width - 4
		if modalMaxW > 76 {
			modalMaxW = 76
		}
		if modalMaxW < 20 {
			modalMaxW = 20
		}

		// helpBoxStyle uses 1-cell borders and 3-cell horizontal padding.
		contentMaxW := modalMaxW - 8
		if contentMaxW < 12 {
			contentMaxW = 12
		}

		content = lipgloss.NewStyle().MaxWidth(contentMaxW).Render(content)
		overlay := helpBoxStyle.MaxWidth(modalMaxW).Render(content)
		base = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(colorBlack),
		)
	}

	return base
}
