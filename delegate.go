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
	reviewedStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorYellow)
	doneStyle    = lipgloss.NewStyle().Foreground(colorDim)
	unsetStyle   = lipgloss.NewStyle().Foreground(colorDim)
	dateStyle    = lipgloss.NewStyle().Foreground(colorDim)
	selectedBar  = lipgloss.NewStyle().Foreground(colorAccent).SetString("│ ")
	normalBar    = lipgloss.NewStyle().SetString("  ")
)

// labelColors are 256-color palette values chosen for readable contrast
// on dark terminals. Avoids black, white, grays, and overly dim colors.
// Prime-length palette for better hash distribution.
var labelColors = []string{
	"204", "209", "215", "179", "149", "114", "80", "75", "111",
	"147", "183", "176", "168", "131", "173", "137", "109", "73",
	"167", "143", "103", "69", "212",
}

// labelColor returns a consistent lipgloss.Style for a label name,
// derived from FNV-1a hash for good distribution with short strings.
func labelColor(name string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(name))
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(labelColors[h.Sum32()%uint32(len(labelColors))]))
}

type planDelegate struct {
	selected    map[string]bool
	changed     map[string]bool
	undoFiles   map[string]string // filename → new status string (shown inline during undo window)
	copiedFiles map[string]bool   // filenames with "Copied!" inline indicator
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

	inSelectMode := len(d.selected) > 0
	isCursor := index == m.Index()

	var badge string
	if inSelectMode {
		if marked {
			badge = activeStyle.Render("✓")
		} else if isCursor {
			badge = unsetStyle.Render("✓")
		} else {
			badge = unsetStyle.Render("·")
		}
	} else {
		switch p.status {
		case "active":
			badge = activeStyle.Render("●")
		case "reviewed":
			badge = reviewedStyle.Render("○")
		case "done":
			badge = doneStyle.Render("✓")
		default:
			badge = unsetStyle.Render("·")
		}
		if changed && d.spinnerView != nil && *d.spinnerView != "" {
			badge = *d.spinnerView
		}
	}
	badgeW := lipgloss.Width(badge)

	// Inline indicators replace the date column
	var date string
	var dateW int
	commentPrefix := ""
	if p.hasComments {
		commentPrefix = dateStyle.Render("💬") + " "
	}
	commentPrefixW := lipgloss.Width(commentPrefix)

	if undoStatus, hasUndo := d.undoFiles[p.file]; hasUndo && !marked {
		label := undoStatus
		if label == "" {
			label = "new"
		}
		undoText := "→ " + label + " (u)"
		if d.spinnerView != nil && *d.spinnerView != "" {
			date = *d.spinnerView + " " + lipgloss.NewStyle().Foreground(colorAccent).Render(undoText)
		} else {
			date = lipgloss.NewStyle().Foreground(colorAccent).Render(undoText)
		}
		dateW = lipgloss.Width(date) + 1
	} else if d.copiedFiles[p.file] {
		date = lipgloss.NewStyle().Foreground(colorAccent).Render("Copied!")
		dateW = lipgloss.Width(date) + 1
	} else {
		// Show MM-DD for current year, full YYYY-MM-DD otherwise.
		ts := p.created
		currentYear := strconv.Itoa(time.Now().Year())
		displayDate := ts.Format("2006-01-02")
		if strings.HasPrefix(displayDate, currentYear+"-") {
			displayDate = displayDate[len(currentYear)+1:]
		}
		date = commentPrefix + displayDate
		dateW = lipgloss.Width(displayDate) + commentPrefixW + 1 // +1 for leading space
	}

	// Build label prefix and title, truncating trailing labels if needed.
	avail := maxW - badgeW - dateW
	minTitle := 10 // reserve at least this much for the title
	var visibleLabels []string
	var labelPrefixW int
	if len(p.labels) > 0 && avail > minTitle {
		w := 2 // leading + trailing space around labels
		for i, l := range p.labels {
			lw := lipgloss.Width(l)
			sep := 0
			if i > 0 {
				sep = 1 // space between labels
			}
			// Check if adding this label still leaves room for the title
			extra := 0
			if i < len(p.labels)-1 {
				extra = 3 // room for " +N" suffix
			}
			if w+sep+lw+extra+minTitle > avail {
				remaining := len(p.labels) - i
				visibleLabels = append(visibleLabels, fmt.Sprintf("+%d", remaining))
				w += sep + lipgloss.Width(visibleLabels[len(visibleLabels)-1])
				break
			}
			visibleLabels = append(visibleLabels, l)
			w += sep + lw
		}
		labelPrefixW = 2 // spaces
		for i, l := range visibleLabels {
			if i > 0 {
				labelPrefixW++
			}
			labelPrefixW += lipgloss.Width(l)
		}
	} else {
		labelPrefixW = 1 // just leading space
	}
	title := p.title
	plainW := labelPrefixW + lipgloss.Width(title)
	if avail > 0 && plainW > avail {
		titleAvail := avail - labelPrefixW - 1
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
		plainW = labelPrefixW + lipgloss.Width(title)
	}
	pad := ""
	if avail > 0 && plainW < avail {
		pad = strings.Repeat(" ", avail-plainW)
	}

	// Apply styling
	var styledText string
	if len(visibleLabels) > 0 {
		var styledLabels string
		for i, l := range visibleLabels {
			if i > 0 {
				styledLabels += " "
			}
			if strings.HasPrefix(l, "+") {
				styledLabels += dateStyle.Render(l)
			} else {
				styledLabels += labelColor(l).Render(l)
			}
		}
		styledText = " " + styledLabels + " " + title + pad
	} else {
		styledText = " " + title + pad
	}

	fmt.Fprintf(w, "%s%s%s %s ", bar, badge, styledText, dateStyle.Render(date))
}
