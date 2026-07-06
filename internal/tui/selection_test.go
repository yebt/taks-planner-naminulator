package tui

import "testing"

func TestAnsiStrip(t *testing.T) {
	in := "\x1b[1;38;5;42mplanner\x1b[0m\nplain"
	if got := ansiStrip(in); got != "planner\nplain" {
		t.Fatalf("ansiStrip = %q", got)
	}
}

func TestSelectedTextSingleLine(t *testing.T) {
	m := &chatModel{contentLines: []string{"hello world"}}
	m.selSL, m.selSC = 0, 6 // "world" (cols 6..10 inclusive)
	m.selEL, m.selEC = 0, 10
	if got := m.selectedText(); got != "world" {
		t.Fatalf("single-line select = %q", got)
	}
	// reversed endpoints must normalize to the same result
	m.selSL, m.selSC = 0, 10
	m.selEL, m.selEC = 0, 6
	if got := m.selectedText(); got != "world" {
		t.Fatalf("reversed select = %q", got)
	}
}

func TestSelectedTextMultiLine(t *testing.T) {
	m := &chatModel{contentLines: []string{"foobar", "middle", "bazqux"}}
	m.selSL, m.selSC = 0, 3 // from "bar"
	m.selEL, m.selEC = 2, 2 // to "baz"
	if got := m.selectedText(); got != "bar\nmiddle\nbaz" {
		t.Fatalf("multi-line select = %q", got)
	}
}

func TestHighlightLine(t *testing.T) {
	// Text content must be preserved regardless of the terminal color profile
	// (lipgloss strips styling when stdout is not a TTY, e.g. under `go test`).
	if got := ansiStrip(highlightLine("hello world", 6, 11)); got != "hello world" {
		t.Fatalf("highlight changed text: %q", got)
	}
	// Out-of-range columns must not panic and must keep the text.
	if got := ansiStrip(highlightLine("hi", 0, 99)); got != "hi" {
		t.Fatalf("clamped highlight = %q", got)
	}
}
