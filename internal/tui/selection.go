package tui

// Mouse selection and clipboard.

import (
	"regexp"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func copyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(text)
		return copiedMsg{n: len([]rune(text)), err: err}
	}
}

// handleMouse forwards wheel events to the viewport and turns left click-drag
// into a line selection that copies to the clipboard on release. The selection
// is anchored in content-line coordinates, so scrolling mid-drag preserves it.
func (m *chatModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown, tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		if m.selecting {
			m.dragged = true // scrolling mid-drag extends the selection
			m.selActive = true
			m.selEL, m.selEC = m.contentLineAt(msg.Y), msg.X
			m.setContent()
		}
		return m, cmd
	case tea.MouseButtonRight:
		// Right click copies the current selection (this is the trigger — not release).
		if msg.Action == tea.MouseActionPress && m.selActive {
			text := m.selectedText()
			m.selActive = false
			m.setContent()
			if strings.TrimSpace(text) == "" {
				return m, nil
			}
			return m, copyCmd(text)
		}
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			// Anchor but don't highlight yet — a plain click (no drag) does nothing.
			m.selecting = true
			m.dragged = false
			m.selActive = false
			m.selSL, m.selSC = m.contentLineAt(msg.Y), msg.X
			m.selEL, m.selEC = m.selSL, m.selSC
		}
	case tea.MouseActionMotion:
		if m.selecting {
			m.dragged = true
			m.selActive = true
			m.selEL, m.selEC = m.contentLineAt(msg.Y), msg.X
			m.setContent()
		}
	case tea.MouseActionRelease:
		if m.selecting {
			m.selecting = false
			if !m.dragged {
				m.selActive = false // plain click → nothing
				return m, nil
			}
			m.selEL, m.selEC = m.contentLineAt(msg.Y), msg.X
			m.setContent() // keep the highlight; copy waits for right-click
		}
	}
	return m, nil
}

// contentLineAt maps a screen row to an absolute content-line index. The
// viewport starts at screen row 1 (row 0 is the header).
func (m *chatModel) contentLineAt(y int) int {
	line := m.vp.YOffset + (y - 1)
	if line < 0 {
		line = 0
	}
	if n := m.vp.TotalLineCount(); n > 0 && line > n-1 {
		line = n - 1
	}
	return line
}

// orderedSel returns the selection endpoints normalized so start ≤ end, with
// line indices clamped to the available content.
func (m *chatModel) orderedSel() (sl, sc, el, ec int) {
	sl, sc, el, ec = m.selSL, m.selSC, m.selEL, m.selEC
	if el < sl || (el == sl && ec < sc) {
		sl, sc, el, ec = el, ec, sl, sc
	}
	n := len(m.contentLines)
	if sl < 0 {
		sl = 0
	}
	if el > n-1 {
		el = n - 1
	}
	return sl, sc, el, ec
}

func clampCol(c, n int) int {
	if c < 0 {
		return 0
	}
	if c > n {
		return n
	}
	return c
}

// selectedText extracts the character range spanning the selection.
func (m *chatModel) selectedText() string {
	if len(m.contentLines) == 0 {
		return ""
	}
	sl, sc, el, ec := m.orderedSel()
	if sl == el {
		r := []rune(m.contentLines[sl])
		a, b := clampCol(sc, len(r)), clampCol(ec+1, len(r))
		if a >= b {
			return ""
		}
		return string(r[a:b])
	}
	first := []rune(m.contentLines[sl])
	parts := []string{string(first[clampCol(sc, len(first)):])}
	for i := sl + 1; i < el; i++ {
		parts = append(parts, m.contentLines[i])
	}
	last := []rune(m.contentLines[el])
	parts = append(parts, string(last[:clampCol(ec+1, len(last))]))
	return strings.Join(parts, "\n")
}

// highlightLine renders a plain line with runes [a,b) shown reversed.
func highlightLine(line string, a, b int) string {
	r := []rune(line)
	a, b = clampCol(a, len(r)), clampCol(b, len(r))
	if a > b {
		a = b
	}
	return string(r[:a]) + lipgloss.NewStyle().Reverse(true).Render(string(r[a:b])) + string(r[b:])
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func ansiStrip(s string) string { return ansiRe.ReplaceAllString(s, "") }
