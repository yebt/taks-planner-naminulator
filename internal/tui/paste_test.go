package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/llm"
)

// A big paste used to land in the textarea verbatim and bury the prompt under
// its own content. It now collapses to a reference the user can see, move past
// and delete as one thing, while the agent still receives the real text.

func pasteMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

func press(s string) tea.KeyMsg {
	switch s {
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// bigPaste is over both thresholds: many lines and well past 100 characters.
func bigPaste(lines int) string {
	rows := make([]string, lines)
	for i := range rows {
		rows[i] = "una línea de texto pegado suficientemente larga"
	}
	return strings.Join(rows, "\n")
}

func TestBulkPasteCollapsesToABlock(t *testing.T) {
	m, _ := newTestModel(t)

	m.handleKey(pasteMsg(bigPaste(16)))

	if got := m.ta.Value(); got != "[Pasted text #1 +16 lines]" {
		t.Fatalf("paste did not collapse: %q", got)
	}
	if n := len(m.pastes); n != 1 {
		t.Fatalf("registry holds %d blocks, want 1", n)
	}
	if m.pastes[0].text != bigPaste(16) {
		t.Fatal("the block does not hold the original text verbatim")
	}
}

// The threshold is deliberately two-sided. A paste that is merely long, or
// merely multi-line, is still ordinary typing and must not be hidden behind a
// reference the user then has to expand mentally.
func TestSmallPasteStaysLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"long but single line", strings.Repeat("x", 400)},
		{"multi-line but short", "uno\ndos\ntres"},
		{"exactly at both limits", strings.Repeat("y", 50) + "\n" + strings.Repeat("z", 49)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t)
			m.handleKey(pasteMsg(tt.in))
			if got := m.ta.Value(); got != tt.in {
				t.Fatalf("paste was collapsed but should have stayed literal: %q", got)
			}
			if len(m.pastes) != 0 {
				t.Fatal("a block was registered for a small paste")
			}
		})
	}
}

// The reference is what the user sees; the agent gets the real thing. Getting
// this backwards is the whole feature failing silently: the conversation would
// look right and the model would answer about a placeholder.
func TestSubmitSendsTheExpandedTextAndShowsTheReference(t *testing.T) {
	m, _ := newTestModel(t)
	rec := &recordingProvider{}
	m.deps.Agent.SetProvider(rec)

	body := bigPaste(4)
	m.handleKey(pasteMsg(body))
	m.handleKey(press(" "))
	m.handleKey(press("?"))

	_, cmd := m.submit()
	if cmd == nil {
		t.Fatal("submit produced no command")
	}
	// submit answers with a batch (the agent call plus the spinner tick), and a
	// batch does not run its members — it hands them back to the runtime.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("submit did not batch, got %T", cmd())
	}
	for _, c := range batch {
		c()
	}

	sent := rec.lastUser()
	if !strings.Contains(sent, body) {
		t.Fatalf("the agent never received the pasted text, got:\n%s", sent)
	}
	if strings.Contains(sent, "[Pasted text #1") {
		t.Fatalf("the agent received the placeholder instead of the text:\n%s", sent)
	}

	var shown string
	for _, e := range m.entries {
		if e.role == "you" {
			shown = e.text
		}
	}
	if !strings.Contains(shown, "[Pasted text #1 +4 lines]") {
		t.Fatalf("the conversation lost the reference: %q", shown)
	}
	if strings.Contains(shown, body) {
		t.Fatalf("the conversation shows the whole paste, which is what this avoids: %q", shown)
	}
}

// "Borrado total": the block is one thing, so one backspace removes all of it.
// Character-by-character deletion would leave a broken reference on screen that
// still expands to nothing useful.
func TestBackspaceDeletesTheWholeBlock(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(16)))

	m.handleKey(press("backspace"))

	if got := m.ta.Value(); got != "" {
		t.Fatalf("the block was not deleted whole: %q", got)
	}
	if len(m.pastes) != 0 {
		t.Fatal("the block stayed in the registry after being deleted")
	}
}

func TestBackspaceInsideTheBlockDeletesItWhole(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(16)))
	for i := 0; i < 6; i++ { // park the caret in the middle of the reference
		m.handleKey(press("left"))
	}

	m.handleKey(press("backspace"))

	if got := m.ta.Value(); got != "" {
		t.Fatalf("a backspace inside the block left a remnant: %q", got)
	}
}

func TestDeleteRemovesTheWholeBlock(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(16)))
	for i := 0; i < 26; i++ { // back to the very start of the reference
		m.handleKey(press("left"))
	}

	m.handleKey(press("delete"))

	if got := m.ta.Value(); got != "" {
		t.Fatalf("forward delete left a remnant: %q", got)
	}
}

// Surrounding text is ordinary text and must keep behaving that way — the
// atomic delete applies to the reference, not to everything near it.
func TestSurroundingTextIsUntouched(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(press("a"))
	m.handleKey(press("b"))
	m.handleKey(pasteMsg(bigPaste(3)))
	m.handleKey(press("c"))

	if got := m.ta.Value(); got != "ab[Pasted text #1 +3 lines]c" {
		t.Fatalf("unexpected buffer: %q", got)
	}

	m.handleKey(press("backspace")) // eats the "c" only
	if got := m.ta.Value(); got != "ab[Pasted text #1 +3 lines]" {
		t.Fatalf("the backspace after the block ate more than the character: %q", got)
	}
	m.handleKey(press("backspace")) // now the whole block
	if got := m.ta.Value(); got != "ab" {
		t.Fatalf("deleting the block took the typed text with it: %q", got)
	}
}

// The reference is not editable. A keystroke aimed inside it lands after it
// instead of corrupting it, because a corrupted reference expands to nothing.
func TestTypingCannotLandInsideTheBlock(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(3)))
	for i := 0; i < 5; i++ {
		m.handleKey(press("left"))
	}

	m.handleKey(press("x"))

	if got := m.ta.Value(); got != "[Pasted text #1 +3 lines]x" {
		t.Fatalf("a keystroke corrupted the reference: %q", got)
	}
}

func TestBlocksAreNumberedInOrder(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(3)))
	m.handleKey(press(" "))
	m.handleKey(pasteMsg(bigPaste(7)))

	if got := m.ta.Value(); got != "[Pasted text #1 +3 lines] [Pasted text #2 +7 lines]" {
		t.Fatalf("numbering is off: %q", got)
	}
	if got := m.expandPastes(m.ta.Value()); got != bigPaste(3)+" "+bigPaste(7) {
		t.Fatalf("expansion mixed the blocks up:\n%s", got)
	}
}

// Numbers are never reused inside a session: a reference the user already saw
// as #2 must not come back meaning something else after #1 is deleted.
func TestBlockNumbersAreNotReused(t *testing.T) {
	m, _ := newTestModel(t)
	m.handleKey(pasteMsg(bigPaste(3)))
	m.handleKey(press("backspace"))
	m.handleKey(pasteMsg(bigPaste(5)))

	if got := m.ta.Value(); got != "[Pasted text #2 +5 lines]" {
		t.Fatalf("a deleted block's number was reused: %q", got)
	}
}

// A single line reads "+1 line", not "+1 lines". It only shows up when a paste
// is long enough to collapse without being multi-line, which the threshold
// currently rules out — the plural is still handled so a threshold change
// cannot ship a typo.
func TestPasteTokenPluralises(t *testing.T) {
	if got := pasteToken(1, 1); got != "[Pasted text #1 +1 line]" {
		t.Fatalf("singular form wrong: %q", got)
	}
	if got := pasteToken(2, 9); got != "[Pasted text #2 +9 lines]" {
		t.Fatalf("plural form wrong: %q", got)
	}
}

// Painting happens on the way to the screen and must not change what the line
// looks like, only how it looks. A style that added or dropped a column would
// desync the caret from the text under it.
func TestPasteTokensArePaintedWithoutChangingWidth(t *testing.T) {
	const line = "mirá [Pasted text #1 +16 lines] esto"
	painted := paintPasteTokens(line)
	if painted == line {
		t.Skip("no colour profile in this environment; nothing to assert")
	}
	if got := ansiStrip(painted); got != line {
		t.Fatalf("painting changed the text: %q, want %q", got, line)
	}
}

// A reference the user typed by hand, or one left over from a mangled block, is
// not a block. Expanding it would be inventing content.
func TestUnknownReferenceExpandsToItself(t *testing.T) {
	m, _ := newTestModel(t)
	const typed = "ojo con el [Pasted text #7 +3 lines] de ayer"
	if got := m.expandPastes(typed); got != typed {
		t.Fatalf("an unregistered reference was rewritten: %q", got)
	}
}

// --- helpers ---

// recordingProvider captures what the agent actually sent, which is the only
// way to prove the expansion happens on the wire and not just in a helper.
type recordingProvider struct {
	mu   sync.Mutex
	msgs []llm.Message
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) Chat(_ context.Context, msgs []llm.Message, _ []llm.Tool) (llm.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append([]llm.Message(nil), msgs...)
	return llm.Response{Content: "ok"}, nil
}

func (p *recordingProvider) lastUser() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.msgs) - 1; i >= 0; i-- {
		if p.msgs[i].Role == "user" {
			return p.msgs[i].Content
		}
	}
	return ""
}
