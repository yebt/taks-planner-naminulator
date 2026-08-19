package tui

// Input box geometry: how tall it should be, and what is out of sight.
//
// The textarea keeps its viewport, its scroll offset and its word-wrapping
// private, so none of that can simply be read. Reimplementing the wrapping here
// would be a second copy free to drift from the first, which is exactly the
// trap internal/daily was created to get out of. So the measuring is delegated
// back to Bubbles: a second textarea, configured identically and never drawn,
// is handed one line at a time and asked how tall it came out.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The input never gets smaller than this, so the prompt keeps looking like a
// prompt, and never larger than half the screen, so it cannot swallow the
// conversation it is a reply to.
const minInputRows = 3

// setInputValue replaces the whole buffer, which ↑/↓ history recall does.
//
// The textarea's own SetValue drops its scroll back to the top while leaving
// the caret at the end, so a long recalled message would be drawn from its
// first rows with the cursor nowhere on screen — and the indicator would then
// describe a position the box is not actually in. Rendering once hands the
// textarea's viewport the new content, and the no-op update makes it reposition
// onto the caret: exactly what the next keystroke would have done, except one
// frame too late to be right.
func (m *chatModel) setInputValue(s string) {
	m.ta.SetValue(s)
	m.taOffset = 0
	m.layout() // size the box before it works out what fits in it
	_ = m.ta.View()
	m.ta, _ = m.ta.Update(nil)
}

// inputMetrics is what View needs to draw the box and its indicators.
type inputMetrics struct {
	total int // visual rows the content occupies
	shown int // rows actually drawn
	above int // rows scrolled off the top
	below int // rows below the last drawn one
}

// rowHeights returns how many visual rows each logical line takes, measured by
// the scratch textarea so the answer comes from Bubbles' own wrapping.
//
// Measured results are cached by line, because the measurement is not cheap —
// each one rebuilds the scratch buffer — and a keystroke changes exactly one
// line out of however many are in the box. Measuring all of them on every
// keypress cost 8ms on a 60-line message, which is lag you can feel.
func (m *chatModel) rowHeights(lines []string) []int {
	if m.rowCache == nil || m.rowCacheWidth != m.scratch.Width() {
		// A different width is a different answer for every line.
		m.rowCache = make(map[string]int, len(lines))
		m.rowCacheWidth = m.scratch.Width()
	}
	if len(m.rowCache) > rowCacheLimit {
		m.rowCache = make(map[string]int, len(lines)) // a session's worth of edits
	}
	out := make([]int, len(lines))
	for i, ln := range lines {
		h, ok := m.rowCache[ln]
		if !ok {
			m.scratch.SetValue(ln)
			h = m.scratch.LineInfo().Height
			m.rowCache[ln] = h
		}
		out[i] = h
	}
	return out
}

// rowCacheLimit keeps a long session from accumulating every line ever typed.
const rowCacheLimit = 4096

// caretRow is the caret's position in visual rows from the top of the content —
// the same number textarea.cursorLineNumber computes internally.
func caretRow(heights []int, logicalRow, rowOffset int) int {
	row := 0
	for i := 0; i < logicalRow && i < len(heights); i++ {
		row += heights[i]
	}
	return row + rowOffset
}

// wantedRows is how tall the box would like to be, before any clamping.
func (m *chatModel) wantedRows() int {
	total := 0
	for _, h := range m.rowHeights(strings.Split(m.ta.Value(), "\n")) {
		total += h
	}
	return total
}

// syncInputScroll mirrors textarea.repositionView: the textarea scrolls only to
// keep the caret visible, and only by the minimum needed. Mirroring that rule is
// the only way to know what sits off-screen, and it converges on the same offset
// because it is driven by the same caret through the same idempotent rule.
func (m *chatModel) syncInputScroll(total, shown, caret int) {
	if caret < m.taOffset {
		m.taOffset = caret
	} else if caret > m.taOffset+shown-1 {
		m.taOffset = caret - shown + 1
	}
	if maxOff := total - shown; m.taOffset > maxOff {
		m.taOffset = maxOff
	}
	if m.taOffset < 0 {
		m.taOffset = 0
	}
}

// inputMetrics measures the box as it currently stands. It reads the height the
// textarea already has, so layout must size the box before calling it.
func (m *chatModel) inputMetrics() inputMetrics {
	heights := m.rowHeights(strings.Split(m.ta.Value(), "\n"))
	total := 0
	for _, h := range heights {
		total += h
	}
	shown := m.ta.Height()
	m.syncInputScroll(total, shown, caretRow(heights, m.ta.Line(), m.ta.LineInfo().RowOffset))

	below := total - shown - m.taOffset
	if below < 0 {
		below = 0
	}
	return inputMetrics{total: total, shown: shown, above: m.taOffset, below: below}
}

// inputRule draws the horizontal rule that frames the input, carrying a count
// when there is something out of sight behind it. A rule is drawn either way:
// the separator has to be there, so the count rides on it for free instead of
// costing a row of its own.
func (m *chatModel) inputRule(arrow string, hidden int) string {
	if hidden <= 0 {
		return dividerStyle.Render(strings.Repeat("─", max(m.width, 0)))
	}
	label := fmt.Sprintf(" %s %d more ", arrow, hidden)
	const lead = 2
	fill := m.width - lead - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return dividerStyle.Render(strings.Repeat("─", lead)) +
		helpStyle.Render(label) +
		dividerStyle.Render(strings.Repeat("─", fill))
}
