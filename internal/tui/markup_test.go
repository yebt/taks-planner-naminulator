package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The assertions read the ANSI-stripped output on purpose: lipgloss emits no
// escape codes when it detects no colour profile (as in CI), so asserting on
// the codes themselves would make these tests pass or fail by environment.
// What must hold everywhere is that the *markers* are gone and the text stays.
func TestRenderMarkupRemovesMarkers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bold section title", "**Trabajo:**", "Trabajo:"},
		{"italic note", "__una observación__", "una observación"},
		{"code span", "`migración de DNS`", "migración de DNS"},
		{"project mention stays", "deploy de `+liquida`", "deploy de +liquida"},
		{"plain text untouched", "  - nada que pintar", "  - nada que pintar"},
		{"mixed", "**Notas:** __ojo con__ `README.md`", "Notas: ojo con README.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansiStrip(renderMarkup(tt.in))
			if got != tt.want {
				t.Fatalf("renderMarkup(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Markers inside a code span are content, not formatting — the same guarantee
// internal/telegram/markup.go makes. This is why code spans are extracted to
// placeholders before bold/italic run.
func TestRenderMarkupKeepsMarkersInsideCodeSpans(t *testing.T) {
	if got := ansiStrip(renderMarkup("`**x**`")); got != "**x**" {
		t.Fatalf("code span content must stay literal, got %q", got)
	}
}

// The daily header carries a real date; make sure the whole line survives.
func TestRenderMarkupKeepsDailyHeader(t *testing.T) {
	got := ansiStrip(renderMarkup("**Daily:**  2026-07-30 JUL"))
	if got != "Daily:  2026-07-30 JUL" {
		t.Fatalf("header mangled: %q", got)
	}
}

// A daily is model-authored, so nothing has pre-wrapped it. Before this, the
// "raw" passthrough assumed pre-wrapped text: long lines overflowed the
// viewport AND desynced the character-granular selection mapping, which indexes
// contentLines by rune.
func TestDailyEntryIsWrappedToViewport(t *testing.T) {
	m, _ := newTestModel(t) // 80x24
	m.entries = nil

	m.add("daily", "**Trabajo:**\n  - "+strings.Repeat("palabra ", 60))

	if m.vp.Width <= 0 {
		t.Fatalf("viewport not sized: %d", m.vp.Width)
	}
	for i, line := range m.contentLines {
		if w := lipgloss.Width(line); w > m.vp.Width {
			t.Fatalf("line %d overflows the viewport: %d > %d\n%q", i, w, m.vp.Width, line)
		}
	}
}

// No literal markers should reach the screen for a realistic daily.
func TestDailyEntryRendersWithoutMarkers(t *testing.T) {
	m, _ := newTestModel(t)
	m.entries = nil // drop the startup banner; assert on the daily alone

	m.add("daily", "**Daily:**  2026-07-30 JUL\n\n**Trabajo:**\n"+
		"  - avance en `migración de DNS`\n\n**Notas:**\n  >> __revisar el VPN__")

	shown := strings.Join(m.contentLines, "\n")
	for _, marker := range []string{"**", "__", "`"} {
		if strings.Contains(shown, marker) {
			t.Fatalf("marker %q still visible on screen:\n%s", marker, shown)
		}
	}
	// ...and the content itself survived.
	for _, want := range []string{"Daily:", "Trabajo:", "migración de DNS", "revisar el VPN"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("content %q lost in rendering:\n%s", want, shown)
		}
	}
}
