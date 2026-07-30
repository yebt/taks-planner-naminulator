// Package tui is the interactive chat harness: a Bubbletea UI with a bold
// header, a status bar (provider/model/context/memory/chat), colored
// conversation, multi-line input (alt+enter), slash-command autocomplete,
// input history (↑/↓), conversation save/load, and long-term memory recall.
package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/memory"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/tools"
)

// Syncer pushes tasks to Plane and pulls states (implemented by internal/plane).
type Syncer interface {
	Configured() bool
	Push(ctx context.Context, t *domain.Task) error
	PullStates(ctx context.Context) (int, error)
	Delete(ctx context.Context, t *domain.Task) error
}

// Telegram delivers daily digests (implemented by internal/telegram).
type Telegram interface {
	Configured() bool
	Send(ctx context.Context, text string) error
}

// ChatDeps wires the harness to the rest of the app.
type ChatDeps struct {
	Cfg        *config.Config
	ConfigPath string
	Agent      *agent.Agent
	Store      store.TaskStore
	Convos     store.ConversationStore
	Tools      *tools.Registry
	Memory     memory.Memory
	Syncer     Syncer
	Telegram   Telegram
	Dailies    store.DailyStore
	Activity   store.ActivityStore
	Context    store.ContextStore
	Build      func(cfg config.Config, name string) (llm.Provider, error)
}

// RunChat starts the interactive harness. Mouse is captured for wheel scroll
// and click-drag selection (which copies to the clipboard on release).
func RunChat(deps ChatDeps) error {
	m := newChatModel(deps)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func newChatModel(deps ChatDeps) *chatModel {
	ta := textarea.New()
	ta.Placeholder = "tell me what you're working on…  (/ for commands)"
	// ta.Prompt = "▌ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// Enter submits; Alt+Enter inserts a newline (multi-line input).
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"), key.WithHelp("alt+enter", "newline"))
	// fill := lipgloss.NewStyle().Background(inputBG)
	// ta.FocusedStyle.Base = fill
	// ta.FocusedStyle.Text = fill
	// ta.FocusedStyle.CursorLine = fill                                                         // single tone — no diff-looking band
	// ta.FocusedStyle.EndOfBuffer = lipgloss.NewStyle().Background(inputBG).Foreground(inputBG) // hide the "~"
	// ta.FocusedStyle.Prompt = lipgloss.NewStyle().Background(inputBG).Foreground(lipgloss.Color("111"))
	// ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Background(inputBG).Foreground(lipgloss.Color("245"))
	ta.Focus()

	m := &chatModel{
		deps:    deps,
		ta:      ta,
		vp:      viewport.New(80, 20),
		histPos: -1,
	}
	m.add("sys", "planner harness — type a message, or / for commands. Try /help.")
	m.healthCheck()
	return m
}

// healthCheck surfaces, on startup, whether the essentials and integrations are
// configured: a blocking-looking alert when the LLM can't run, and non-blocking
// warnings for Plane/Telegram (their commands degrade gracefully).
func (m *chatModel) healthCheck() {
	cfg := m.deps.Cfg
	if cfg != nil && !cfg.ProvidersReady() {
		m.add("alert", "LLM not functional: provider '"+cfg.ActiveProvider+
			"' has no API key. Set one with /key "+cfg.ActiveProvider+" <key> or run `planner config`.")
	}
	if m.deps.Syncer == nil || !m.deps.Syncer.Configured() {
		m.add("warn", "Plane not configured — /sync, /pull and /state are off until you set it in `planner config` → Plane.")
	}
	if m.deps.Telegram == nil || !m.deps.Telegram.Configured() {
		m.add("warn", "Telegram not configured — you can build/edit dailies, but /daily send won't work until you set it in `planner config` → Telegram.")
	}
}
