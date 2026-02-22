package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Custom Delegate ─────────────────────────────────────────────────────────

var (
	activeStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	pendStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	doneStyle    = lipgloss.NewStyle().Foreground(colorDim)
	unsetStyle   = lipgloss.NewStyle().Foreground(colorDim)
	dateStyle    = lipgloss.NewStyle().Foreground(colorDim)
	selectedBar  = lipgloss.NewStyle().Foreground(colorAccent).SetString("│ ")
	normalBar    = lipgloss.NewStyle().SetString("  ")
	markedStyle  = lipgloss.NewStyle().Foreground(colorMagenta)
)

// projectColors are 256-color palette values chosen for readable contrast
// on dark terminals. Avoids black, white, grays, and overly dim colors.
// Prime-length palette for better hash distribution.
var projectColors = []string{
	"204", "209", "215", "179", "149", "114", "80", "75", "111",
	"147", "183", "176", "168", "131", "173", "137", "109", "73",
	"167", "143", "103", "69", "212",
}

// projectColor returns a consistent lipgloss.Style for a project name,
// derived from FNV-1a hash for good distribution with short strings.
func projectColor(name string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(name))
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(projectColors[h.Sum32()%uint32(len(projectColors))]))
}

type planDelegate struct {
	selected    map[string]bool
	changed     map[string]bool
	spinnerView *string
}

func (d planDelegate) Height() int                             { return 1 }
func (d planDelegate) Spacing() int                            { return 0 }
func (d planDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d planDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	p, ok := item.(plan)
	if !ok {
		return
	}

	marked := d.selected[p.file]
	changed := d.changed[p.file]

	bar := normalBar
	if index == m.Index() {
		bar = selectedBar
	}

	maxW := m.Width() - 3 // -2 for bar prefix, -1 for right padding
	if maxW < 10 {
		maxW = 10
	}

	var badge string
	switch p.status {
	case "active":
		badge = activeStyle.Render("●")
	case "pending":
		badge = pendStyle.Render("○")
	case "done":
		badge = doneStyle.Render("✓")
	default:
		badge = unsetStyle.Render("·")
	}
	if marked {
		badge = markedStyle.Render(statusIcon(p.status))
	} else if changed && d.spinnerView != nil && *d.spinnerView != "" {
		badge = *d.spinnerView
	}
	badgeW := lipgloss.Width(badge)

	// Show MM-DD for current year, full YYYY-MM-DD otherwise.
	ts := p.created
	currentYear := strconv.Itoa(time.Now().Year())
	displayDate := ts.Format("2006-01-02")
	if strings.HasPrefix(displayDate, currentYear+"-") {
		displayDate = displayDate[len(currentYear)+1:]
	}
	date := displayDate
	dateW := lipgloss.Width(date) + 1 // +1 for leading space

	// Build text as plain for measurement, then style the project prefix.
	var prefix, title string
	if p.project != "" {
		prefix = " " + p.project + " "
		title = p.title
	} else {
		prefix = " "
		title = p.title
	}
	avail := maxW - badgeW - dateW
	plainText := prefix + title
	textW := lipgloss.Width(plainText)
	if avail > 0 && textW > avail {
		// Truncate from the title portion only
		prefixW := lipgloss.Width(prefix)
		titleAvail := avail - prefixW - 1 // -1 for ellipsis
		if titleAvail > 0 {
			tw := 0
			cut := len(title)
			for i, r := range title {
				rw := lipgloss.Width(string(r))
				if tw+rw > titleAvail {
					cut = i
					break
				}
				tw += rw
			}
			title = title[:cut] + "…"
		} else {
			title = "…"
		}
		textW = lipgloss.Width(prefix + title)
	}
	pad := ""
	if avail > 0 && textW < avail {
		pad = strings.Repeat(" ", avail-textW)
	}

	// Apply styling
	var styledText string
	if marked {
		styledText = markedStyle.Render(prefix+title) + pad
	} else if p.project != "" {
		styledText = projectColor(p.project).Render(prefix) + title + pad
	} else {
		styledText = prefix + title + pad
	}

	if marked {
		fmt.Fprintf(w, "%s%s%s %s ", bar, badge, styledText, markedStyle.Render(date))
	} else {
		fmt.Fprintf(w, "%s%s%s %s ", bar, badge, styledText, dateStyle.Render(date))
	}
}
