package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/config"
)

// Opening the configuration used to mean quitting the chat and re-running
// `planner config`, which is a strange thing to ask of someone mid-conversation.
// /config now opens the very same configModel as a modal inside the chat
// program, sharing the one *config.Config so the two can never diverge.

func TestConfigCommandOpensTheModal(t *testing.T) {
	m, _ := newTestModel(t)
	m.runCommand("/config")
	if m.config == nil {
		t.Fatal("/config did not open the configuration modal")
	}
}

// While the modal is up it must own the keyboard: a keystroke that means
// something in the config screen must not also land in the chat textarea.
func TestConfigModalOwnsTheKeystrokes(t *testing.T) {
	m, _ := newTestModel(t)
	m.runCommand("/config")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := m.ta.Value(); got != "" {
		t.Fatalf("keystroke leaked into the chat input: %q", got)
	}
}

// The whole point: leaving the config screen returns to the conversation. If
// the embedded model still answered with tea.Quit it would take the chat — and
// the unsaved conversation — down with it.
func TestConfigModalExitDoesNotQuitTheProgram(t *testing.T) {
	m, _ := newTestModel(t)
	m.runCommand("/config")

	_, cmd := m.config.updateMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("leaving the modal produced no command at all")
	}
	switch msg := cmd().(type) {
	case configClosedMsg:
		// correct: the chat model decides what happens next
	case tea.QuitMsg:
		t.Fatal("the embedded config screen quit the whole program")
	default:
		t.Fatalf("unexpected exit message %T", msg)
	}
}

// Standalone `planner config` has no chat to return to, so there the same key
// must still end the program.
func TestStandaloneConfigStillQuits(t *testing.T) {
	cfg := config.Default()
	m := newConfigModel(&cfg, filepath.Join(t.TempDir(), "config.json"), tea.Quit)

	_, cmd := m.updateMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("standalone config did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("standalone config must quit, got %T", cmd())
	}
}

func TestClosingTheModalReturnsToTheConversation(t *testing.T) {
	m, _ := newTestModel(t)
	m.runCommand("/config")
	m.Update(configClosedMsg{})
	if m.config != nil {
		t.Fatal("the modal stayed open after closing")
	}
	// And the chat must be usable again, not swallowed by a stale modal.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if got := m.ta.Value(); got != "h" {
		t.Fatalf("chat input dead after closing the modal: %q", got)
	}
}

// Editing Plane or Telegram settings and coming back to a chat still holding the
// adapters built from the OLD config would be worse than not offering /config at
// all: the screen would say one thing and every sync would do another.
func TestClosingTheModalRebuildsTheAdapters(t *testing.T) {
	m, _ := newTestModel(t)
	before := m.deps.Syncer

	m.runCommand("/config")
	m.deps.Cfg.Plane.BaseURL = "https://plane.example"
	m.deps.Cfg.Plane.APIToken = "tok"
	m.deps.Cfg.Plane.WorkspaceSlug = "acme"
	m.deps.Cfg.Plane.ProjectID = "proj"
	m.Update(configClosedMsg{})

	if m.deps.Syncer == before {
		t.Fatal("the Plane syncer was not rebuilt from the edited config")
	}
	if m.deps.Syncer == nil || !m.deps.Syncer.Configured() {
		t.Fatal("the rebuilt syncer does not see the new Plane settings")
	}
}

func TestConfigIsOfferedInTheCommandMenu(t *testing.T) {
	for _, c := range baseCommands {
		if c.full == "/config" {
			return
		}
	}
	t.Fatal("/config is implemented but never offered in the command menu")
}

// The modal renders instead of the conversation, not on top of half of it.
func TestConfigModalReplacesTheChatView(t *testing.T) {
	m, _ := newTestModel(t)
	m.add("you", "a message that must not show through")
	m.runCommand("/config")
	if strings.Contains(m.View(), "a message that must not show through") {
		t.Fatal("the conversation bled through the config modal")
	}
}
