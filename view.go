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
	colorYellow  = lipgloss.Color("11") // reviewed status, update notices
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

// renderFooter combines left-aligned help hints with a right-aligned notification.
// If width is too narrow, the notification is truncated first.
func renderFooter(help, notification string, width int) string {
	if notification == "" {
		return help
	}
	styled := statusTextStyle.Render(notification) + " "
	notifW := lipgloss.Width(styled)
	helpW := lipgloss.Width(help)
	gap := 2
	if helpW+gap+notifW <= width {
		pad := width - helpW - notifW
		return help + strings.Repeat(" ", pad) + styled
	}
	// Not enough room — truncate notification
	avail := width - helpW - gap
	if avail > 3 {
		return help + strings.Repeat(" ", gap) + statusTextStyle.Render(truncateForWidth(notification, avail-1)) + " "
	}
	return help
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "Loading..."
	}
	if m.clod.active {
		return m.clodView()
	}

	listW, previewW := m.layoutWidths()

	innerH := m.height - 3 // -2 for borders, -1 for hint bar

	var leftStyle, rightStyle lipgloss.Style
	if m.comment.active {
		// In comment mode: left (ToC) is focused, right (preview) is unfocused
		leftStyle = focusedBorder.Width(listW - 2).Height(innerH)
		rightStyle = unfocusedBorder.Width(previewW - 2).Height(innerH)
	} else if m.focused == listPane {
		leftStyle = focusedBorder.Width(listW - 2).Height(innerH)
		rightStyle = unfocusedBorder.Width(previewW - 2).Height(innerH)
	} else {
		leftStyle = unfocusedBorder.Width(listW - 2).Height(innerH)
		rightStyle = focusedBorder.Width(previewW - 2).Height(innerH)
	}

	var leftContent string
	if m.comment.active {
		leftContent = renderTocPane(m, listW-2, innerH)
	} else if m.demo.active && len(m.list.Items()) == 0 {
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
		msg := "No active plans\n\na show all  ·  s set status  ·  l set labels\n\nStatus and labels are stored as YAML\nfrontmatter in your plan files."
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
	if m.comment.active {
		previewTitle = paneTitleStyle.Render(m.comment.planFile + " (comments)")
	} else if item, ok := m.list.SelectedItem().(plan); ok {
		previewTitle = paneTitleStyle.Render(item.file)
	}
	rightContent := previewTitle + "\n" + m.viewport.View()

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(rightContent),
	)

	var statusBar string
	if m.comment.active {
		hintStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
		dimStyle := lipgloss.NewStyle().Foreground(colorDim)
		sep := dimStyle.Render(" | ")
		if m.comment.editing {
			statusBar = " " + m.comment.commentInput.View()
		} else if m.focused == previewPane {
			statusBar = " " +
				hintStyle.Render("j/k") + dimStyle.Render(" scroll") + sep +
				hintStyle.Render("tab") + dimStyle.Render(" ToC") + sep +
				hintStyle.Render("n/p") + dimStyle.Render(" files") + sep +
				hintStyle.Render("esc") + dimStyle.Render(" back")
		} else {
			statusBar = " " +
				hintStyle.Render("enter") + dimStyle.Render(" comment") + sep
			if len(m.comment.toc) > 0 && m.comment.cursor < len(m.comment.toc) && m.comment.toc[m.comment.cursor].isComment {
				statusBar += hintStyle.Render("d") + dimStyle.Render(" delete comment") + sep
			}
			statusBar +=
				hintStyle.Render("s/l") + dimStyle.Render(" status/labels") + sep +
				hintStyle.Render("n/p") + dimStyle.Render(" files") + sep +
				hintStyle.Render("esc") + dimStyle.Render(" back")
		}
	} else if len(m.selected) > 0 {
		count := len(m.selected)
		hintStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
		dimStyle := lipgloss.NewStyle().Foreground(colorDim)
		statusBar = " " + statusTextStyle.Render(fmt.Sprintf("%d selected", count)) + "  " +
			hintStyle.Render("s") + dimStyle.Render(" status") + dimStyle.Render(" | ") +
			hintStyle.Render("l") + dimStyle.Render(" labels") + dimStyle.Render(" | ") +
			hintStyle.Render("C") + dimStyle.Render(" copy path") + dimStyle.Render(" | ") +
			hintStyle.Render("a") + dimStyle.Render(" all") + dimStyle.Render(" | ") +
			hintStyle.Render("esc") + dimStyle.Render(" clear")
	} else if m.updateAvailable != nil {
		notice := fmt.Sprintf("Update %s available · go install github.com/jakebf/planc@latest", m.updateAvailable.version)
		statusBar = " " + updateTextStyle.Render(truncateForWidth(notice, m.width-1))
	} else {
		statusBar = " " + m.help.ShortHelpView(m.keys.ShortHelp())
	}
	statusBar = renderFooter(statusBar, m.notification, m.width)
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

	if m.settingLabels {
		base = m.renderLabelModal()
	}

	if m.settingStatus {
		base = m.renderStatusModal(base)
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

func (m model) renderStatusModal(_ string) string {
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	accentStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	// Context line: plan title or batch count
	var context string
	if len(m.selected) > 0 {
		context = fmt.Sprintf("%d plans selected", len(m.selected))
	} else if item, ok := m.list.SelectedItem().(plan); ok {
		context = item.file
	}

	var b strings.Builder
	b.WriteString(helpTitleStyle.Render("Set Status") + "\n")
	b.WriteString(dimStyle.Render(context) + "\n\n")

	for i, opt := range statusOptions {
		isCursor := i == m.statusModalCursor
		var icon string
		switch opt.status {
		case "active":
			icon = activeStyle.Render(opt.icon)
		case "reviewed":
			icon = reviewedStyle.Render(opt.icon)
		case "done":
			icon = doneStyle.Render(opt.icon)
		default:
			icon = unsetStyle.Render(opt.icon)
		}
		cursor := "  "
		if isCursor {
			cursor = accentStyle.Render("> ")
		}
		if isCursor {
			b.WriteString(fmt.Sprintf("%s%s  %s  %s\n", cursor, accentStyle.Render(opt.key), icon, accentStyle.Render(opt.label)))
		} else {
			b.WriteString(fmt.Sprintf("%s%s  %s  %s\n", cursor, opt.key, icon, opt.label))
		}
	}

	b.WriteString("\n" + dimStyle.Render("j/k navigate · 0-3 select · esc cancel"))

	overlay := helpBoxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(colorBlack),
	)
}

func (m model) renderLabelModal() string {
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	accentStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	var context string
	if m.labelBatchMode && len(m.selected) > 0 {
		context = fmt.Sprintf("Labels (%d plans)", len(m.selected))
	} else {
		context = "Labels"
	}

	var b strings.Builder
	b.WriteString(helpTitleStyle.Render(context) + "\n")

	if !m.labelBatchMode {
		if item, ok := m.list.SelectedItem().(plan); ok {
			b.WriteString(dimStyle.Render(item.file) + "\n")
		}
	}
	b.WriteString("\n")

	filtered := m.filteredLabelChoices()

	// Scroll windowing: show at most maxVisible items
	maxVisible := 12
	if len(filtered) > 0 || m.labelInput.Value() != "" {
		scrollOff := 0
		if len(filtered) > maxVisible {
			scrollOff = m.labelCursor - maxVisible/2
			if scrollOff < 0 {
				scrollOff = 0
			}
			if scrollOff > len(filtered)-maxVisible {
				scrollOff = len(filtered) - maxVisible
			}
		}
		end := scrollOff + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		if scrollOff > 0 {
			b.WriteString(dimStyle.Render("    ↑ "+strconv.Itoa(scrollOff)+" more") + "\n")
		}

		checkStyle := lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
		mixedStyle := lipgloss.NewStyle().Bold(true).Foreground(colorYellow)

		for i := scrollOff; i < end; i++ {
			l := filtered[i]
			isCursor := i == m.labelCursor
			isFlashing := m.labelFlashIdx == i && m.labelFlashTick > 0
			toggled := m.labelToggled[l]
			mixed := m.labelMixed[l]

			// Icon: ✓ when toggled, - when mixed, · when off
			icon := "·"
			iconStyle := dimStyle
			if mixed {
				icon = "-"
				iconStyle = mixedStyle
			} else if toggled {
				icon = "✓"
				iconStyle = checkStyle
			}

			// Flash effect: alternate icon visibility
			if isFlashing {
				if m.labelFlashTick%2 == 0 {
					icon = "·"
					iconStyle = dimStyle
				} else {
					icon = "✓"
					iconStyle = checkStyle
				}
			}

			cursor := "  "
			if isCursor {
				cursor = accentStyle.Render("> ")
			}

			if isCursor || isFlashing {
				b.WriteString(cursor + accentStyle.Render(icon) + " " + accentStyle.Render(l) + "\n")
			} else {
				b.WriteString(cursor + iconStyle.Render(icon) + " " + labelColor(l).Render(l) + "\n")
			}
		}

		if end < len(filtered) {
			b.WriteString(dimStyle.Render("    ↓ "+strconv.Itoa(len(filtered)-end)+" more") + "\n")
		}
	}

	if len(filtered) == 0 && m.labelInput.Value() != "" {
		b.WriteString(dimStyle.Render("  (new label: "+m.labelInput.Value()+")") + "\n")
	}

	if len(filtered) == 0 && m.labelInput.Value() == "" && len(m.labelChoices) == 0 {
		b.WriteString(dimStyle.Render("  No labels yet. Type to create one.") + "\n")
	}

	b.WriteString("\n")
	if m.labelInput.Value() != "" {
		b.WriteString("filter: " + m.labelInput.View() + "\n")
	}
	b.WriteString(dimStyle.Render("type to filter/add · enter toggle+close · space multi-select"))

	overlay := helpBoxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(colorBlack),
	)
}
