package tui

// The configuration screen, opened from inside the chat with /config.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/wiring"
)

// configClosedMsg is what the embedded configuration screen answers with instead
// of tea.Quit, so leaving it returns to the conversation rather than ending the
// program.
type configClosedMsg struct{}

// openConfig puts the configuration screen up as a modal.
//
// It is the same configModel `planner config` runs, over the same
// *config.Config the chat is already holding. That shared pointer is the whole
// reason this is a modal and not a subprocess: an external editor would write
// the file behind our back, and the next /key here would save our stale copy
// straight over it.
func (m *chatModel) openConfig() {
	m.config = newConfigModel(m.deps.Cfg, m.deps.ConfigPath, func() tea.Msg {
		return configClosedMsg{}
	})
	m.config.width, m.config.height = m.width, m.height
	m.config.input.Width = m.width - 6
}

// closeConfig returns to the conversation and rebuilds everything built from
// configuration.
//
// Without this the chat would keep talking to the provider, the Plane project
// and the Telegram chat that were configured when it started, while the screen
// the user just left says otherwise — settings that appear to apply and do not
// are worse than settings you cannot reach.
func (m *chatModel) closeConfig() {
	m.config = nil
	m.rebuildFromConfig()
	m.layout()
}

// rebuildFromConfig re-derives the provider and the adapters from the current
// config. Failures are shown rather than swallowed: an LLM that silently stayed
// on the previous provider is a confusing bug to chase.
func (m *chatModel) rebuildFromConfig() {
	cfg := *m.deps.Cfg

	if m.deps.Build != nil && cfg.ActiveProvider != "" {
		p, err := m.deps.Build(cfg, cfg.ActiveProvider)
		if err != nil {
			m.add("err", "provider not rebuilt: "+err.Error())
		} else {
			m.deps.Agent.SetProvider(p)
		}
	}

	syncer := wiring.PlaneSyncer(cfg, m.deps.Store)
	m.deps.Syncer = syncer
	m.deps.Tools.SetSyncer(syncer)

	tg := wiring.TelegramClient(cfg)
	m.deps.Telegram = tg
	m.deps.Tools.SetTelegram(tg)

	m.add("sys", "configuration closed — provider, Plane and Telegram reloaded.")
}
