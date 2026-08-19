package tui

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The other paste tests hand handleKey a KeyMsg with Paste already set, which
// takes the terminal, the parser and the event loop entirely on trust. These do
// not: they feed the real bracketed-paste escape sequence into a real
// tea.Program and look at what comes out the other end.
//
// That gap is exactly where a paste feature breaks in practice — the model can
// be perfect while what actually arrives is nothing like what the tests inject.

// syncBuf collects the rendered frames. The renderer writes from its own
// goroutine, so the test must not read the bytes without a lock.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// bracketed wraps text the way a terminal does when bracketed paste is on.
func bracketed(s string) string { return "\x1b[200~" + s + "\x1b[201~" }

// runWire drives a real program with raw terminal bytes and returns the final
// model plus everything that was drawn.
func runWire(t *testing.T, in string) (*chatModel, string) {
	t.Helper()
	m, _ := newTestModel(t)
	pr, pw := io.Pipe()
	out := &syncBuf{}

	p := tea.NewProgram(m,
		tea.WithInput(pr),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
		tea.WithoutCatchPanics(),
	)

	done := make(chan tea.Model, 1)
	go func() {
		final, err := p.Run()
		if err != nil {
			t.Errorf("program: %v", err)
		}
		done <- final
	}()

	if _, err := pw.Write([]byte(in)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond) // let a frame render before quitting
	// Two ctrl+c: the first clears the prompt, the second quits. The registry
	// survives both, so the capture is still there to inspect.
	if _, err := pw.Write([]byte("\x03\x03")); err != nil {
		t.Fatal(err)
	}

	select {
	case final := <-done:
		pw.Close()
		return final.(*chatModel), out.String()
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("the program never quit")
		return nil, ""
	}
}

func assertCollapsed(t *testing.T, fm *chatModel, drawn, want string) {
	t.Helper()
	if n := len(fm.pastes); n != 1 {
		t.Fatalf("a real bracketed paste registered %d blocks, want 1", n)
	}
	if fm.pastes[0].text != want {
		t.Fatalf("the captured text is not what was pasted:\n got %q\nwant %q",
			fm.pastes[0].text, want)
	}
	if !strings.Contains(drawn, "[Pasted text #1 +16 lines]") {
		t.Fatalf("the reference was never drawn; frames were:\n%s", ansiStrip(drawn))
	}
	if strings.Contains(ansiStrip(drawn), "una línea de texto pegado") {
		t.Fatal("the raw paste reached the screen instead of the reference")
	}
}

func TestBracketedPasteFromTheTerminalIsCollapsed(t *testing.T) {
	paste := bigPaste(16)
	fm, drawn := runWire(t, bracketed(paste))
	assertCollapsed(t, fm, drawn, paste)
}

// A terminal sends CR for the Enter key, and most of them send CR as the line
// separator inside a bracketed paste too — so what arrives is not what sits on
// the clipboard. The textarea already normalises that on insert, which is why a
// paste that slips through still LOOKS right while never having been collapsed.
func TestAPasteWithCarriageReturnsIsStillAPaste(t *testing.T) {
	paste := bigPaste(16)
	fm, drawn := runWire(t, bracketed(strings.ReplaceAll(paste, "\n", "\r")))
	assertCollapsed(t, fm, drawn, paste)
}

// A clipboard filled on Windows, or from a file with CRLF endings.
func TestAPasteWithCRLFIsStillAPaste(t *testing.T) {
	paste := bigPaste(16)
	fm, drawn := runWire(t, bracketed(strings.ReplaceAll(paste, "\n", "\r\n")))
	assertCollapsed(t, fm, drawn, paste)
}
