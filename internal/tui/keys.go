package tui

// Keyboard handling.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *chatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		if m.quitArmed {
			return m, tea.Quit // second consecutive ctrl+c → quit
		}
		m.quitArmed = true // first ctrl+c → clear the prompt, arm quit
		m.ta.Reset()
		m.suggestions = nil
		m.layout()
		return m, nil
	}
	m.quitArmed = false // any other key disarms
	if m.selActive {    // any key clears a lingering selection highlight (esc cancels)
		m.selActive = false
		m.setContent()
	}

	// ctrl+l clears the on-screen content (keeps agent context), unless busy.
	if msg.Type == tea.KeyCtrlL {
		if !m.thinking {
			m.entries = nil
			m.setContent()
		}
		return m, nil
	}

	// Awaiting a y/n confirmation: swallow every other key until decided.
	if m.confirm != nil {
		switch msg.String() {
		case "y", "Y":
			act := m.confirm.action
			m.confirm = nil
			act()
		case "n", "N", "esc":
			m.confirm = nil
			m.add("sys", "cancelled.")
		}
		m.layout()
		return m, nil
	}

	// Picking a Plane state: navigate the list, enter applies, esc cancels.
	if m.statePick != nil {
		switch msg.String() {
		case "up", "ctrl+p":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "ctrl+n":
			if m.selected < len(m.suggestions)-1 {
				m.selected++
			}
		case "enter":
			m.applyStatePick()
		case "esc":
			m.statePick = nil
			m.suggestions = nil
			m.add("sys", "cancelled.")
		}
		m.layout()
		return m, nil
	}

	// Editing the daily draft in the textarea: enter commits (does NOT send),
	// esc discards. Other keys edit the text (alt+enter for newlines).
	if m.dailyEditing {
		switch msg.String() {
		case "enter":
			m.dailyDraft = strings.TrimRight(m.ta.Value(), " \n\t")
			m.dailyEditing = false
			m.ta.Reset()
			if err := m.persistDaily(m.dailyDraftDate, m.dailyDraft); err != nil {
				m.add("err", "edit kept in this session but NOT saved: "+err.Error())
			} else {
				m.add("sys", "daily updated. use /daily send to deliver it.")
			}
			m.layout()
			return m, nil
		case "esc":
			m.dailyEditing = false
			m.ta.Reset()
			m.add("sys", "daily edit cancelled (draft kept).")
			m.layout()
			return m, nil
		}
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		m.layout()
		return m, cmd
	}

	// Suggestion menu open: navigate / complete / submit-if-complete.
	if len(m.suggestions) > 0 {
		switch msg.String() {
		case "up", "ctrl+p":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.selected < len(m.suggestions)-1 {
				m.selected++
			}
			return m, nil
		case "tab":
			m.acceptSuggestion()
			return m, nil
		case "enter":
			// A mention completes the token and keeps typing; only a ready slash
			// command submits.
			if isMention(m.suggestions[m.selected]) {
				m.acceptSuggestion()
				return m, nil
			}
			if completedValue(m.suggestions[m.selected]) == m.ta.Value() {
				return m.submit()
			}
			m.acceptSuggestion()
			return m, nil
		case "esc":
			m.suggestions = nil
			m.layout()
			return m, nil
		}
	}

	key := msg.String()
	switch {
	case key == "up" && !strings.Contains(m.ta.Value(), "\n"):
		m.historyPrev()
		return m, nil
	case key == "down" && !strings.Contains(m.ta.Value(), "\n"):
		m.historyNext()
		return m, nil
	case key == "enter":
		if m.thinking {
			return m, nil
		}
		return m.submit()
	case key == "pgup" || key == "pgdown" || key == "ctrl+u" || key == "ctrl+d":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	val := m.ta.Value()
	if s := computeSuggestions(val, m.providerNames()); s != nil {
		m.suggestions = s
	} else {
		m.suggestions = m.mentionSuggestions(val)
	}
	m.selected = 0
	m.layout()
	return m, cmd
}
