package main

import (
	"strings"
	"testing"
)

func TestExtractToc(t *testing.T) {
	body := `# Main Title

Some intro text.

## Section One

Content here.

> **[comment]:** This needs more detail.

### Subsection

More content.

## Section Two

Final text.
`
	toc := extractToc(body)

	if len(toc) != 5 {
		t.Fatalf("expected 5 toc entries, got %d", len(toc))
	}

	// # Main Title
	if toc[0].level != 1 || toc[0].text != "Main Title" || toc[0].isComment {
		t.Errorf("entry 0: got level=%d text=%q comment=%v", toc[0].level, toc[0].text, toc[0].isComment)
	}

	// ## Section One
	if toc[1].level != 2 || toc[1].text != "Section One" {
		t.Errorf("entry 1: got level=%d text=%q", toc[1].level, toc[1].text)
	}

	// comment
	if !toc[2].isComment || toc[2].text != "This needs more detail." {
		t.Errorf("entry 2: got comment=%v text=%q", toc[2].isComment, toc[2].text)
	}

	// ### Subsection
	if toc[3].level != 3 || toc[3].text != "Subsection" {
		t.Errorf("entry 3: got level=%d text=%q", toc[3].level, toc[3].text)
	}

	// ## Section Two
	if toc[4].level != 2 || toc[4].text != "Section Two" {
		t.Errorf("entry 4: got level=%d text=%q", toc[4].level, toc[4].text)
	}
}

func TestExtractTocSkipsCodeBlocks(t *testing.T) {
	body := "# Real Heading\n\n```\n# Not a heading\n## Also not\n```\n\n## Another Real\n"
	toc := extractToc(body)

	if len(toc) != 2 {
		t.Fatalf("expected 2 toc entries, got %d", len(toc))
	}
	if toc[0].text != "Real Heading" {
		t.Errorf("entry 0: got %q", toc[0].text)
	}
	if toc[1].text != "Another Real" {
		t.Errorf("entry 1: got %q", toc[1].text)
	}
}

func TestExtractTocNoHeadings(t *testing.T) {
	body := "Just some plain text\nwith no headings at all.\n"
	toc := extractToc(body)
	if len(toc) != 0 {
		t.Fatalf("expected 0 toc entries, got %d", len(toc))
	}
}

func TestInjectComment(t *testing.T) {
	body := "# Title\n\nSome content.\n\n## Section\n\nMore content.\n"
	result := injectComment(body, 0, "My comment here")

	if !strings.Contains(result, "> **[comment]:** My comment here") {
		t.Errorf("comment not found in result:\n%s", result)
	}

	// Verify comment comes after the heading
	lines := strings.Split(result, "\n")
	headingIdx := -1
	commentIdx := -1
	for i, l := range lines {
		if l == "# Title" {
			headingIdx = i
		}
		if strings.Contains(l, "[comment]") {
			commentIdx = i
		}
	}
	if headingIdx >= commentIdx {
		t.Errorf("comment (line %d) should come after heading (line %d)", commentIdx, headingIdx)
	}
}

func TestInjectCommentAtEnd(t *testing.T) {
	body := "# Title\n\n## Last Section"
	result := injectComment(body, 2, "End comment")

	if !strings.Contains(result, "> **[comment]:** End comment") {
		t.Errorf("comment not found in result:\n%s", result)
	}
}

func TestRemoveComment(t *testing.T) {
	body := "# Title\n\n> **[comment]:** Remove me\n\nContent here.\n"
	toc := extractToc(body)

	var commentLine int
	for _, e := range toc {
		if e.isComment {
			commentLine = e.rawLine
			break
		}
	}

	result := removeComment(body, commentLine)
	if strings.Contains(result, "[comment]") {
		t.Errorf("comment should have been removed:\n%s", result)
	}
	if !strings.Contains(result, "# Title") {
		t.Errorf("heading should still be present:\n%s", result)
	}
	if !strings.Contains(result, "Content here.") {
		t.Errorf("content should still be present:\n%s", result)
	}
}

func TestReplaceComment(t *testing.T) {
	body := "# Title\n\n> **[comment]:** Old text\n\nContent.\n"
	toc := extractToc(body)

	var commentLine int
	for _, e := range toc {
		if e.isComment {
			commentLine = e.rawLine
			break
		}
	}

	result := replaceComment(body, commentLine, "New text")
	if !strings.Contains(result, "> **[comment]:** New text") {
		t.Errorf("comment not replaced:\n%s", result)
	}
	if strings.Contains(result, "Old text") {
		t.Errorf("old text should be gone:\n%s", result)
	}
}

func TestHeadingWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"Simple heading", []string{"Simple", "heading"}},
		{"With `backticks` here", []string{"With", "backticks", "here"}},
		{"Trailing comma `foo`,", []string{"Trailing", "comma", "foo"}},
		{"Map keys: `file` → `path()` — throughout `model.go`, `delegate.go`", []string{"Map", "keys", "file", "→", "path(", "—", "throughout", "model.go", "delegate.go"}},
		{"File watcher — `main.go:81`, `model.go`", []string{"File", "watcher", "—", "main.go:81", "model.go"}},
		{"", nil},
		{"` `", nil}, // only backticks and spaces
	}
	for _, tt := range tests {
		got := headingWords(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("headingWords(%q) = %v (len %d), want %v (len %d)", tt.in, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("headingWords(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestContainsWordsInOrder(t *testing.T) {
	tests := []struct {
		s     string
		words []string
		want  bool
	}{
		{"hello world foo", []string{"hello", "world"}, true},
		{"hello world foo", []string{"world", "hello"}, false}, // wrong order
		{"  ### 6. Map keys:  file  →  path()  — throughout  model.go ,  delegate.go  ", []string{"Map", "keys", "file", "→", "path(", "—", "throughout", "model.go", "delegate.go"}, true},
		{"  ### 8. File watcher —  main.go:81 ,  model.go  ", []string{"File", "watcher", "—", "main.go:81", "model.go"}, true},
		{"no match here", []string{"missing"}, false},
		{"abc", nil, true}, // vacuously true — callers guard against empty words
	}
	for _, tt := range tests {
		got := containsWordsInOrder(tt.s, tt.words)
		if got != tt.want {
			t.Errorf("containsWordsInOrder(%q, %v) = %v, want %v", tt.s, tt.words, got, tt.want)
		}
	}
}

func TestComputeRenderLinesWithCodeSpans(t *testing.T) {
	// Simulate glamour output where code spans get extra padding and
	// punctuation gets detached (e.g. "model.go ," instead of "model.go,").
	rendered := strings.Join([]string{
		"",
		"  ### 1. Config struct — config.go:19-27                                       ",
		"",
		"  Content                                                                      ",
		"",
		"  ### 6. Map keys:  file  →  path()  — throughout  model.go ,  delegate.go     ",
		"",
		"  Content                                                                      ",
		"",
		"  ### 8. File watcher —  main.go:81 ,  model.go                                ",
		"",
		"  Content                                                                      ",
	}, "\n")

	toc := []tocEntry{
		{level: 3, text: "1. Config struct — `config.go:19-27`"},
		{level: 3, text: "6. Map keys: `file` → `path()` — throughout `model.go`, `delegate.go`"},
		{level: 3, text: "8. File watcher — `main.go:81`, `model.go`"},
	}

	computeRenderLines(toc, rendered)

	if toc[0].renderLine != 1 {
		t.Errorf("toc[0] renderLine = %d, want 1", toc[0].renderLine)
	}
	if toc[1].renderLine != 5 {
		t.Errorf("toc[1] renderLine = %d, want 5", toc[1].renderLine)
	}
	if toc[2].renderLine != 9 {
		t.Errorf("toc[2] renderLine = %d, want 9", toc[2].renderLine)
	}
}

func TestComputeRenderLinesSimple(t *testing.T) {
	rendered := strings.Join([]string{
		"",
		"  # Main Title                                                                 ",
		"",
		"  Intro text                                                                   ",
		"",
		"  ## Section One                                                               ",
		"",
		"  Content                                                                      ",
	}, "\n")

	toc := []tocEntry{
		{level: 1, text: "Main Title"},
		{level: 2, text: "Section One"},
	}
	computeRenderLines(toc, rendered)

	if toc[0].renderLine != 1 {
		t.Errorf("toc[0] renderLine = %d, want 1", toc[0].renderLine)
	}
	if toc[1].renderLine != 5 {
		t.Errorf("toc[1] renderLine = %d, want 5", toc[1].renderLine)
	}
}

func TestMultipleCommentsOnSameHeading(t *testing.T) {
	body := "# Title\n\n> **[comment]:** First\n\n> **[comment]:** Second\n\nContent.\n"
	toc := extractToc(body)

	comments := 0
	for _, e := range toc {
		if e.isComment {
			comments++
		}
	}
	if comments != 2 {
		t.Errorf("expected 2 comments, got %d", comments)
	}
}
