package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ─── Comment Mode Types ──────────────────────────────────────────────────────

var commentRegex = regexp.MustCompile(`^>\s*\*\*\[comment\]:\*\*\s*(.+)$`)

type tocEntry struct {
	level      int    // 1-6 for headings, 0 for comments
	text       string // heading text (no #) or comment text
	rawLine    int    // line number in raw body (after frontmatter)
	renderLine int    // line number in glamour-rendered output
	isComment  bool
}

type commentState struct {
	active       bool
	toc          []tocEntry
	cursor       int
	editing      bool           // text input is open
	editTarget   int            // toc index being commented on
	editExisting bool           // editing vs adding
	commentInput textinput.Model
	planFile     string
	rawBody      string // cached raw markdown body (sans frontmatter)
}

// bodyHasComments returns true if the markdown body contains any comment blockquotes.
func bodyHasComments(body string) bool {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if commentRegex.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// ─── ToC Extraction ──────────────────────────────────────────────────────────

// extractToc scans raw markdown body and builds a table of contents from
// headings and comment blockquotes. Skips headings inside fenced code blocks.
func extractToc(rawBody string) []tocEntry {
	lines := strings.Split(rawBody, "\n")
	var toc []tocEntry
	inFence := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track fenced code blocks
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		// Check for comment
		if m := commentRegex.FindStringSubmatch(trimmed); m != nil {
			toc = append(toc, tocEntry{
				level:     0,
				text:      m[1],
				rawLine:   i,
				isComment: true,
			})
			continue
		}

		// Check for heading
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, c := range trimmed {
				if c == '#' {
					level++
				} else {
					break
				}
			}
			if level >= 1 && level <= 6 && len(trimmed) > level && trimmed[level] == ' ' {
				text := strings.TrimSpace(trimmed[level+1:])
				toc = append(toc, tocEntry{
					level:   level,
					text:    text,
					rawLine: i,
				})
			}
		}
	}

	return toc
}

// headingWords extracts match tokens from a heading: strips backticks,
// splits on whitespace, and trims trailing punctuation that glamour may
// detach from code spans (e.g. "`foo`,") so matching stays robust.
func headingWords(s string) []string {
	fields := strings.Fields(strings.ReplaceAll(s, "`", ""))
	words := make([]string, 0, len(fields))
	for _, f := range fields {
		trimmed := strings.TrimRight(f, ",.;:!?)")
		if trimmed != "" {
			words = append(words, trimmed)
		}
	}
	return words
}

// containsWordsInOrder returns true if all words appear in s in order.
func containsWordsInOrder(s string, words []string) bool {
	pos := 0
	for _, w := range words {
		idx := strings.Index(s[pos:], w)
		if idx < 0 {
			return false
		}
		pos += idx + len(w)
	}
	return true
}

// computeRenderLines maps each toc entry to the corresponding line in the
// glamour-rendered output. Searches forward sequentially (headings appear in
// order) using word-order matching to handle glamour's code span rendering
// which strips backticks, adds padding, and detaches punctuation.
func computeRenderLines(toc []tocEntry, rendered string) {
	if len(toc) == 0 {
		return
	}

	lines := strings.Split(rendered, "\n")
	// Pre-strip ANSI from all lines once.
	stripped := make([]string, len(lines))
	for j, line := range lines {
		stripped[j] = ansi.Strip(line)
	}

	searchFrom := 0
	for i := range toc {
		text := strings.TrimSpace(toc[i].text)
		if text == "" {
			continue
		}
		words := headingWords(text)
		if len(words) == 0 {
			continue
		}
		for j := searchFrom; j < len(stripped); j++ {
			if containsWordsInOrder(stripped[j], words) {
				toc[i].renderLine = j
				searchFrom = j + 1
				break
			}
		}
	}
}

// ─── Comment Manipulation ────────────────────────────────────────────────────

// injectComment inserts a comment blockquote after the given heading line.
func injectComment(rawBody string, headingLine int, text string) string {
	lines := strings.Split(rawBody, "\n")
	if headingLine < 0 || headingLine >= len(lines) {
		return rawBody
	}

	comment := fmt.Sprintf("> **[comment]:** %s", text)

	// Insert after the heading line with blank lines for clean formatting
	var result []string
	result = append(result, lines[:headingLine+1]...)
	result = append(result, "")
	result = append(result, comment)
	result = append(result, "")
	if headingLine+1 < len(lines) {
		// Skip a leading blank line after heading to avoid double blanks
		rest := lines[headingLine+1:]
		if len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
			rest = rest[1:]
		}
		result = append(result, rest...)
	}

	return strings.Join(result, "\n")
}

// removeComment removes a comment line and any adjacent blank line.
func removeComment(rawBody string, commentLine int) string {
	lines := strings.Split(rawBody, "\n")
	if commentLine < 0 || commentLine >= len(lines) {
		return rawBody
	}

	// Remove the comment line
	var result []string
	result = append(result, lines[:commentLine]...)

	// Skip trailing blank line if present
	rest := lines[commentLine+1:]
	if len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}

	// Also remove preceding blank line if present
	if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	result = append(result, rest...)
	return strings.Join(result, "\n")
}

// replaceComment replaces the text of an existing comment in-place.
func replaceComment(rawBody string, commentLine int, newText string) string {
	lines := strings.Split(rawBody, "\n")
	if commentLine < 0 || commentLine >= len(lines) {
		return rawBody
	}

	lines[commentLine] = fmt.Sprintf("> **[comment]:** %s", newText)
	return strings.Join(lines, "\n")
}

// writeCommentBody writes a new body back to the plan file, preserving frontmatter.
func writeCommentBody(filePath, newBody string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	existing, _ := parseFrontmatter(string(data))

	var result string
	if len(existing) > 0 {
		var buf strings.Builder
		buf.WriteString("---\n")
		written := make(map[string]bool)
		for _, key := range []string{"status", "labels", "project"} {
			if v, ok := existing[key]; ok && v != "" {
				fmt.Fprintf(&buf, "%s: %s\n", key, v)
				written[key] = true
			}
		}
		var extra []string
		for k := range existing {
			if !written[k] {
				extra = append(extra, k)
			}
		}
		if len(extra) > 0 {
			sortStrings(extra)
			for _, k := range extra {
				if v := existing[k]; v != "" {
					fmt.Fprintf(&buf, "%s: %s\n", k, v)
				}
			}
		}
		buf.WriteString("---\n")
		result = buf.String() + newBody
	} else {
		result = newBody
	}

	lastSelfWrite.Store(time.Now().UnixMilli())
	return os.WriteFile(filePath, []byte(result), perm)
}

// sortStrings sorts a string slice in-place (avoids import cycle with sort).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ─── Async Commands ──────────────────────────────────────────────────────────

// loadCommentMode reads a plan file, extracts ToC, renders markdown,
// and computes render line mappings. planPath is the full path to the plan file.
func loadCommentMode(planPath, style string, width int) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(planPath)
		if err != nil {
			return commentContentMsg{file: planPath}
		}
		_, body := parseFrontmatter(string(data))
		toc := extractToc(body)
		rendered := glamourRender(body, style, width)
		computeRenderLines(toc, rendered)
		return commentContentMsg{
			file:     planPath,
			rawBody:  body,
			rendered: rendered,
			toc:      toc,
		}
	}
}

// saveComment writes updated body to disk, re-extracts ToC, and re-renders.
// planPath is the full path to the plan file.
func saveComment(planPath, newBody, style string, width int) tea.Cmd {
	return func() tea.Msg {
		if err := writeCommentBody(planPath, newBody); err != nil {
			return errMsg{err}
		}
		toc := extractToc(newBody)
		rendered := glamourRender(newBody, style, width)
		computeRenderLines(toc, rendered)
		return commentSavedMsg{
			file:     planPath,
			rawBody:  newBody,
			rendered: rendered,
			toc:      toc,
		}
	}
}

// loadCommentModeFromContent builds comment mode state from in-memory content.
func loadCommentModeFromContent(file, body, style string, width int) tea.Cmd {
	return func() tea.Msg {
		toc := extractToc(body)
		rendered := glamourRender(body, style, width)
		computeRenderLines(toc, rendered)
		return commentContentMsg{
			file:     file,
			rawBody:  body,
			rendered: rendered,
			toc:      toc,
		}
	}
}

// saveCommentDemo updates in-memory content and returns a commentSavedMsg.
func saveCommentDemo(file, newBody string, content map[string]string, style string, width int) tea.Cmd {
	return func() tea.Msg {
		content[file] = newBody
		toc := extractToc(newBody)
		rendered := glamourRender(newBody, style, width)
		computeRenderLines(toc, rendered)
		return commentSavedMsg{
			file:     file,
			rawBody:  newBody,
			rendered: rendered,
			toc:      toc,
		}
	}
}

// ─── ToC View Rendering ─────────────────────────────────────────────────────

// renderTocPane renders the table of contents for comment mode.
func renderTocPane(m model, width, height int) string {
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	accentStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	commentStyle := lipgloss.NewStyle().Foreground(colorYellow).Italic(true)

	// Header: status icon + hint + status label + hint + labels
	hintStyle := lipgloss.NewStyle().Foreground(colorDim)
	var header string
	if item, ok := m.list.SelectedItem().(plan); ok {
		var statusIcon, statusLabel string
		var statusStyle lipgloss.Style
		switch item.status {
		case "active":
			statusIcon, statusLabel, statusStyle = "●", "active", activeStyle
		case "reviewed":
			statusIcon, statusLabel, statusStyle = "○", "reviewed", reviewedStyle
		case "done":
			statusIcon, statusLabel, statusStyle = "✓", "done", doneStyle
		default:
			statusIcon, statusLabel, statusStyle = "·", "new", unsetStyle
		}
		header = " " + statusStyle.Render(statusIcon) + " " +
			hintStyle.Render("s") + " " + statusStyle.Render(statusLabel) +
			hintStyle.Render(" · ")
		header += hintStyle.Render("l")
		if len(item.labels) > 0 {
			for _, l := range item.labels {
				header += " " + labelColor(l).Render(l)
			}
		} else {
			header += " " + hintStyle.Render("(none)")
		}
		header = truncateForWidth(header, width) + "\n\n"
	}

	if len(m.comment.toc) == 0 {
		hint := lipgloss.NewStyle().Foreground(colorDim).
			Width(width).Align(lipgloss.Center).
			Render("No headings found")
		return header + lipgloss.Place(width, height-1, lipgloss.Center, lipgloss.Center, hint)
	}

	var lines []string
	for i, entry := range m.comment.toc {
		isCursor := i == m.comment.cursor

		bar := normalBar
		if isCursor {
			bar = selectedBar
		}

		var line string
		if entry.isComment {
			text := truncateForWidth(entry.text, width-6)
			if isCursor {
				line = fmt.Sprintf("%s%s", bar, accentStyle.Render("💬 "+text))
			} else {
				line = fmt.Sprintf("%s%s", bar, commentStyle.Render("💬 "+text))
			}
		} else {
			indent := strings.Repeat("  ", entry.level-1)
			text := truncateForWidth(entry.text, width-6-len(indent))
			if isCursor {
				line = fmt.Sprintf("%s%s%s", bar, indent, accentStyle.Render(text))
			} else {
				line = fmt.Sprintf("%s%s%s", bar, indent, dimStyle.Render(text))
			}
		}
		lines = append(lines, line)
	}

	// Scroll windowing — only show indicators when list actually overflows
	headerLines := 2 // status/labels header + blank line
	maxVisible := height - headerLines
	if maxVisible < 1 {
		maxVisible = 1
	}

	scrollOff := 0
	if len(lines) > maxVisible {
		scrollOff = m.comment.cursor - maxVisible/2
		if scrollOff < 0 {
			scrollOff = 0
		}
		if scrollOff > len(lines)-maxVisible {
			scrollOff = len(lines) - maxVisible
		}
	}
	end := scrollOff + maxVisible
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	b.WriteString(header)
	if scrollOff > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("    ↑ %d more", scrollOff)) + "\n")
	}
	for i := scrollOff; i < end; i++ {
		b.WriteString(lines[i] + "\n")
	}
	if end < len(lines) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("    ↓ %d more", len(lines)-end)) + "\n")
	}

	return b.String()
}
