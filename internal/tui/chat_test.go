package tui

// Input history recall (↑/↓). The prompt is the only thing the user can see
// here, so every case asserts on what the textarea ends up holding.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pressUp / pressDown go through the real key path, so the guards that decide
// whether history recall applies at all are part of what is under test.
func pressUp(m *chatModel)   { m.Update(tea.KeyMsg{Type: tea.KeyUp}) }
func pressDown(m *chatModel) { m.Update(tea.KeyMsg{Type: tea.KeyDown}) }

func seedHistory(m *chatModel, vals ...string) {
	for _, v := range vals {
		m.pushHistory(v)
	}
}

func TestHistoryRecall(t *testing.T) {
	t.Run("the newest entry comes back first", func(t *testing.T) {
		m, _ := newTestModel(t)
		seedHistory(m, "/todo", "revisar el deploy", "/projects")

		pressUp(m)
		if got := m.ta.Value(); got != "/projects" {
			t.Fatalf("first ↑ should recall the newest entry, got %q", got)
		}
	})

	t.Run("walking back past the oldest stays on the oldest", func(t *testing.T) {
		m, _ := newTestModel(t)
		seedHistory(m, "/todo", "/projects")

		for i := 0; i < 5; i++ { // far more presses than there are entries
			pressUp(m)
		}
		if got := m.ta.Value(); got != "/todo" {
			t.Fatalf("walking past the oldest should stay on it, got %q", got)
		}
	})

	t.Run("walking forward past the newest returns to an empty prompt", func(t *testing.T) {
		m, _ := newTestModel(t)
		seedHistory(m, "/todo", "/projects")

		pressUp(m)
		pressUp(m) // at "/todo"
		pressDown(m)
		if got := m.ta.Value(); got != "/projects" {
			t.Fatalf("↓ should walk back towards the newest, got %q", got)
		}
		pressDown(m)
		if got := m.ta.Value(); got != "" {
			t.Fatalf("past the newest the prompt should be empty again, got %q", got)
		}
		pressDown(m) // one press too many must not resurrect anything
		if got := m.ta.Value(); got != "" {
			t.Fatalf("an extra ↓ should leave the prompt empty, got %q", got)
		}

		// And the walk can start over from the newest.
		pressUp(m)
		if got := m.ta.Value(); got != "/projects" {
			t.Fatalf("after returning to the prompt, ↑ should start from the newest, got %q", got)
		}
	})

	t.Run("a command repeated back to back is only walked over once", func(t *testing.T) {
		m, _ := newTestModel(t)
		seedHistory(m, "/todo", "/todo", "/projects")

		pressUp(m)
		pressUp(m)
		if got := m.ta.Value(); got != "/todo" {
			t.Fatalf("second ↑ should reach /todo, got %q", got)
		}
		pressUp(m)
		if got := m.ta.Value(); got != "/todo" {
			t.Fatalf("the repeat should not cost an extra press, got %q", got)
		}
	})

	t.Run("with no history the draft in the prompt is left alone", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.ta.SetValue("media idea sin terminar")

		pressUp(m)
		if got := m.ta.Value(); got != "media idea sin terminar" {
			t.Fatalf("↑ with an empty history must not touch the draft, got %q", got)
		}
		pressDown(m)
		if got := m.ta.Value(); got != "media idea sin terminar" {
			t.Fatalf("↓ outside a walk must not touch the draft, got %q", got)
		}
	})

	t.Run("a multi-line draft is never replaced by history", func(t *testing.T) {
		m, _ := newTestModel(t)
		seedHistory(m, "/todo", "/projects")
		const draft = "primera línea\nsegunda línea"
		m.ta.SetValue(draft)

		pressUp(m)
		pressDown(m)
		if got := m.ta.Value(); got != draft {
			t.Fatalf("↑/↓ inside a multi-line draft move the cursor, not the history; got %q", got)
		}
	})

	t.Run("a credential is never one arrow away", func(t *testing.T) {
		m, _ := newTestModel(t)
		const secret = "sk-live-never-recall-me"

		m.ta.SetValue("/todo")
		m.submit()
		m.ta.SetValue("/key kimi " + secret)
		m.submit()

		pressUp(m)
		got := m.ta.Value()
		if strings.Contains(got, secret) {
			t.Fatalf("↑ resurfaced the credential: %q", got)
		}
		if got != "/todo" {
			t.Fatalf("↑ should skip the secret and recall the previous line, got %q", got)
		}
	})
}
