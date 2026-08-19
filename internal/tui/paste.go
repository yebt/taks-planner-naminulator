package tui

// Paste blocks. A bulk paste collapses to a one-line reference in the input and
// expands back to the real text on the way to the agent, so dropping a log or a
// file into the prompt does not bury everything else that was typed.
//
// The reference behaves as one object rather than as the 26 characters it is
// made of: it cannot be typed into, and a single backspace removes all of it.
// A half-deleted reference would still look like one on screen while expanding
// to nothing, which is the worst of both.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A paste collapses only when it is BOTH multi-line and long. Either alone is
// still ordinary input: one long line takes one line on screen, and a handful
// of short lines cost less room than the reference replacing them would.
const (
	pasteMinLines = 2
	pasteMinChars = 101
)

var pasteStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("111")).Background(lipgloss.Color("237"))

// pasteTokenRe matches a reference. The number ties it back to the registry;
// the line count is for the reader and is never parsed back.
var pasteTokenRe = regexp.MustCompile(`\[Pasted text #(\d+) \+\d+ lines?\]`)

// pasteBlock is one captured paste. text is the original with its line endings
// normalised and nothing else touched — this is the only copy, so no trimming,
// no collapsing, no reformatting may happen to it here.
type pasteBlock struct {
	n     int
	lines int
	text  string
}

func pasteToken(n, lines int) string {
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	return fmt.Sprintf("[Pasted text #%d +%d %s]", n, lines, unit)
}

func (b pasteBlock) token() string { return pasteToken(b.n, b.lines) }

// normalizePaste turns a terminal's line endings into plain newlines.
//
// A terminal sends CR for the Enter key, and most send CR (or CRLF) as the
// separator inside a bracketed paste too, so what arrives is not what sits on
// the clipboard. Counting "\n" on that text sees one enormous line and lets a
// whole pasted file through uncollapsed — while still LOOKING pasted, because
// the textarea does this same normalisation on insert. Doing it here first is
// what keeps the two paths describing the same text.

func normalizePaste(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// isBulkPaste reports whether a paste is big enough to be worth hiding.
func isBulkPaste(s string) bool {
	return strings.Count(s, "\n")+1 >= pasteMinLines &&
		utf8.RuneCountInString(s) >= pasteMinChars
}

// capturePaste registers s and returns the reference to put in the buffer.
func (m *chatModel) capturePaste(s string) string {
	m.pasteSeq++
	b := pasteBlock{n: m.pasteSeq, lines: strings.Count(s, "\n") + 1, text: s}
	m.pastes = append(m.pastes, b)
	return b.token()
}

// dropPaste forgets a block. Only the atomic delete calls this: a block whose
// reference merely left the buffer is still reachable through ↑ history recall,
// and forgetting it there would turn a recalled message into a placeholder.
func (m *chatModel) dropPaste(n int) {
	for i, b := range m.pastes {
		if b.n == n {
			m.pastes = append(m.pastes[:i], m.pastes[i+1:]...)
			return
		}
	}
}

// expandPastes puts the real text back. A reference with no block behind it —
// typed by hand, or left over from an earlier session — is returned untouched:
// inventing content for it would be worse than showing it verbatim.
func (m *chatModel) expandPastes(s string) string {
	return pasteTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		sub := pasteTokenRe.FindStringSubmatch(tok)
		n, err := strconv.Atoi(sub[1])
		if err != nil {
			return tok
		}
		for _, b := range m.pastes {
			if b.n == n && b.token() == tok {
				return b.text
			}
		}
		return tok
	})
}

// pasteSpan locates one reference in the buffer, in rune offsets.
type pasteSpan struct{ start, end, n int }

// pasteSpans returns the registered references in val, in order. A reference
// that matches the shape but no block is deliberately skipped, so text the user
// typed themselves keeps behaving like the text it is.
func (m *chatModel) pasteSpans(val string) []pasteSpan {
	var out []pasteSpan
	for _, loc := range pasteTokenRe.FindAllStringSubmatchIndex(val, -1) {
		tok := val[loc[0]:loc[1]]
		n, err := strconv.Atoi(val[loc[2]:loc[3]])
		if err != nil {
			continue
		}
		for _, b := range m.pastes {
			if b.n == n && b.token() == tok {
				out = append(out, pasteSpan{
					start: utf8.RuneCountInString(val[:loc[0]]),
					end:   utf8.RuneCountInString(val[:loc[1]]),
					n:     n,
				})
				break
			}
		}
	}
	return out
}

// caretOffset returns the cursor as a rune index into ta.Value(). The textarea
// exposes the row and the column within it; a buffer-wide edit needs the offset.
func caretOffset(ta textarea.Model) int {
	li := ta.LineInfo()
	rows := strings.Split(ta.Value(), "\n")
	off := 0
	for i := 0; i < ta.Line() && i < len(rows); i++ {
		off += utf8.RuneCountInString(rows[i]) + 1 // the newline counts
	}
	return off + li.StartColumn + li.ColumnOffset
}

// handlePasteKeys enforces the "one object" rule. It reports whether it
// consumed the key; when it returns false the caller carries on as usual, which
// is also how an insertion nudged past a reference gets applied.
func (m *chatModel) handlePasteKeys(msg tea.KeyMsg) bool {
	if len(m.pastes) == 0 {
		return false
	}
	spans := m.pasteSpans(m.ta.Value())
	if len(spans) == 0 {
		return false
	}
	caret := caretOffset(m.ta)

	switch msg.Type {
	case tea.KeyBackspace:
		// Deleting backwards owns (start, end]: at start there is nothing of the
		// reference behind the caret, so that backspace belongs to the text before it.
		for _, s := range spans {
			if caret > s.start && caret <= s.end {
				m.deleteSpan(s, caret)
				return true
			}
		}
	case tea.KeyDelete:
		// Deleting forwards owns [start, end) for the mirror-image reason.
		for _, s := range spans {
			if caret >= s.start && caret < s.end {
				m.deleteSpan(s, caret)
				return true
			}
		}
	case tea.KeyRunes, tea.KeySpace:
		if msg.Paste {
			return false // a paste is captured before this, never nudged
		}
		// A keystroke aimed inside the reference lands after it. Swallowing it
		// would feel broken; letting it through would corrupt the reference into
		// something that still looks like one but expands to nothing.
		for _, s := range spans {
			if caret > s.start && caret < s.end {
				m.moveCaretRight(s.end - caret)
				return false
			}
		}
	}
	return false
}

// deleteSpan removes a whole reference by driving the textarea's own editing.
// Rewriting the value directly would be shorter and would drop the cursor
// somewhere the user did not put it.
func (m *chatModel) deleteSpan(s pasteSpan, caret int) {
	m.moveCaretRight(s.end - caret)
	for i := s.start; i < s.end; i++ {
		m.ta, _ = m.ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m.dropPaste(s.n)
}

func (m *chatModel) moveCaretRight(n int) {
	for i := 0; i < n; i++ {
		m.ta, _ = m.ta.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
}

// paintPasteTokens colours the references in an already-rendered view. It runs
// on the rendered string, not on the buffer, because the textarea measures its
// own content and would count the escape codes as columns. The style is purely
// a colour, so the visible width is unchanged and the caret stays aligned.
func paintPasteTokens(view string) string {
	return pasteTokenRe.ReplaceAllStringFunc(view, func(tok string) string {
		return pasteStyle.Render(tok)
	})
}
