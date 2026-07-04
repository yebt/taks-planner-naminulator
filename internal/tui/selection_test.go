package tui

import "testing"

func TestAnsiStrip(t *testing.T) {
	in := "\x1b[1;38;5;42mplanner\x1b[0m\nplain"
	if got := ansiStrip(in); got != "planner\nplain" {
		t.Fatalf("ansiStrip = %q", got)
	}
}

func TestSelectedText(t *testing.T) {
	m := &chatModel{contentLines: []string{"line0", "line1", "line2", "line3"}}
	m.selStart, m.selEnd = 2, 1 // reversed range should normalize
	if got := m.selectedText(); got != "line1\nline2" {
		t.Fatalf("selectedText = %q", got)
	}
	m.selStart, m.selEnd = 0, 99 // clamps to available lines
	if got := m.selectedText(); got != "line0\nline1\nline2\nline3" {
		t.Fatalf("clamped selectedText = %q", got)
	}
}
