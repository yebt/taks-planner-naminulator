package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The input used to be three rows, always. Anything longer scrolled silently:
// there was no way to tell a finished message from one with a paragraph parked
// out of sight, and no way to read it back before sending.

func multiline(n int) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("linea %d", i+1)
	}
	return strings.Join(rows, "\n")
}

func TestInputGrowsWithItsContent(t *testing.T) {
	m, _ := newTestModel(t) // 80x24 → half the screen is 12 rows
	m.ta.SetValue(multiline(6))
	m.layout()

	if got := m.ta.Height(); got != 6 {
		t.Fatalf("input height = %d, want 6", got)
	}
}

func TestInputStopsGrowingAtHalfTheScreen(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue(multiline(40))
	m.layout()

	if got := m.ta.Height(); got != 12 {
		t.Fatalf("input height = %d, want 12 (half of 24)", got)
	}
	if m.vp.Height < 3 {
		t.Fatalf("the input ate the conversation: viewport height %d", m.vp.Height)
	}
}

func TestInputKeepsAFloorOfThreeRows(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue("")
	m.layout()

	if got := m.ta.Height(); got != 3 {
		t.Fatalf("empty input height = %d, want 3", got)
	}
}

// A single logical line that wraps takes several rows on screen, and the box
// has to grow for those too — counting "\n" would miss the most common case.
func TestWrappedLinesCountTowardsTheHeight(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue(strings.Repeat("palabra ", 60)) // ~480 cols over ~78
	m.layout()

	if got := m.ta.Height(); got < 6 {
		t.Fatalf("a wrapped line did not grow the input: height %d", got)
	}
}

// Recalling a long message with ↑ replaces the whole buffer at once. The
// textarea's own SetValue drops the scroll back to the top while leaving the
// caret at the end, so without a nudge the box shows the first rows, the cursor
// is nowhere on screen, and the indicator describes a position the box is not in.
func TestRecalledHistoryIsScrolledToTheCaret(t *testing.T) {
	m, _ := newTestModel(t)
	m.pushHistory(multiline(40))
	m.historyPrev()

	mt := m.inputMetrics()
	drawn := strings.Split(ansiStrip(m.ta.View()), "\n")
	if first := strings.TrimSpace(drawn[0]); first != fmt.Sprintf("┃ linea %d", mt.above+1) {
		t.Fatalf("says %d rows above, but the recalled box starts at %q", mt.above, first)
	}
	if last := strings.TrimSpace(drawn[len(drawn)-1]); last != "┃ linea 40" {
		t.Fatalf("the caret is at the end but the box ends at %q", last)
	}
}

// The counts are the point of the feature: "there is more up there" is useless
// without "how much".
func TestHiddenRowsAboveAreAnnounced(t *testing.T) {
	m, _ := newTestModel(t)
	m.pushHistory(multiline(40))
	m.historyPrev() // caret lands at the end

	mt := m.inputMetrics()
	if mt.above != 28 { // 40 rows, 12 visible, caret on the last one
		t.Fatalf("above = %d, want 28", mt.above)
	}
	if mt.below != 0 {
		t.Fatalf("below = %d, want 0 with the caret at the end", mt.below)
	}
	if view := m.View(); !strings.Contains(view, "↑ 28 more") {
		t.Fatalf("the hidden rows are not announced:\n%s", view)
	}
}

// This is the test with teeth: the counts are checked against the rows the
// textarea actually draws, not against the arithmetic that produced them. The
// scroll offset is mirrored from an unexported field, and a mirror that is only
// ever compared with itself is just a second opinion from the same source.
//
// The frames matter. The textarea repositions against the content its own
// View() last handed the viewport, so a test that skips rendering measures a
// scroll position the running program never has.
func TestHiddenRowCountsMatchWhatIsDrawn(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue(multiline(40))
	m.layout()
	for i := 0; i < 20; i++ { // walk the caret back up into the middle
		m.handleKey(press("up"))
		_ = m.View() // Bubbletea renders every frame; the textarea relies on it
	}

	mt := m.inputMetrics()
	if mt.above == 0 || mt.below == 0 {
		t.Fatalf("the caret is mid-content but only one side is reported: above=%d below=%d", mt.above, mt.below)
	}
	if mt.above+mt.below+mt.shown != 40 {
		t.Fatalf("above(%d)+shown(%d)+below(%d) must be the whole 40", mt.above, mt.shown, mt.below)
	}

	drawn := strings.Split(ansiStrip(m.ta.View()), "\n")
	first, last := strings.TrimSpace(drawn[0]), strings.TrimSpace(drawn[len(drawn)-1])
	// "above" rows are hidden, so the first one drawn is the next one after them.
	if want := fmt.Sprintf("┃ linea %d", mt.above+1); first != want {
		t.Fatalf("says %d rows above, but the box starts at %q (want %q)", mt.above, first, want)
	}
	if want := fmt.Sprintf("┃ linea %d", 40-mt.below); last != want {
		t.Fatalf("says %d rows below, but the box ends at %q (want %q)", mt.below, last, want)
	}
	if view := m.View(); !strings.Contains(view, fmt.Sprintf("↓ %d more", mt.below)) {
		t.Fatalf("the rows below are not announced:\n%s", view)
	}
}

func TestNoIndicatorWhenEverythingFits(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue(multiline(4))
	m.layout()

	if mt := m.inputMetrics(); mt.above != 0 || mt.below != 0 {
		t.Fatalf("content fits but reports above=%d below=%d", mt.above, mt.below)
	}
	if view := m.View(); strings.Contains(view, "more") {
		t.Fatalf("an indicator was drawn for content that fits:\n%s", view)
	}
}

// The measurement must come from the textarea's own wrapping, not from a second
// implementation of it. This pins the two together: if Bubbles changes how it
// wraps, this fails instead of the indicator quietly going wrong.
func TestRowHeightsAgreeWithTheTextarea(t *testing.T) {
	m, _ := newTestModel(t)
	long := strings.Repeat("palabra ", 40)

	m.ta.SetValue(long)
	m.layout()
	want := m.ta.LineInfo().Height // the textarea's own count for the caret's line

	if got := m.rowHeights([]string{long}); len(got) != 1 || got[0] != want {
		t.Fatalf("rowHeights = %v, textarea says %d", got, want)
	}
}

// A collapsed paste is one row no matter how big the paste was — the two
// features have to agree, or pasting a file would still blow the box open.
func TestACollapsedPasteIsOneRow(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(200)))

	if got := m.ta.Height(); got != 3 {
		t.Fatalf("a collapsed paste grew the input to %d rows", got)
	}
	if mt := m.inputMetrics(); mt.above != 0 || mt.below != 0 {
		t.Fatalf("a collapsed paste reports hidden rows: above=%d below=%d", mt.above, mt.below)
	}
}

// Resizing must not leave the box taller than the new screen.
func TestInputShrinksWithTheWindow(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue(multiline(40))
	m.layout()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})

	if got := m.ta.Height(); got != 5 {
		t.Fatalf("input height = %d, want 5 (half of 10)", got)
	}
}

// Measuring is not cheap: each miss rebuilds the scratch buffer. Before the
// per-line cache, measuring a 60-line message took ~8ms — and View and layout
// each ask, so that was ~16ms of lag on every single keystroke.
func BenchmarkInputMetrics(b *testing.B) {
	m, _ := newTestModel(b)
	m.layout()
	base := multiline(60)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ta.SetValue(base + fmt.Sprintf("\nescribiendo %d", i)) // one fresh line: one miss
		m.inputMetrics()
	}
}
