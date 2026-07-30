package tui

// Message submission, agent replies, tool echo, and input history.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/agent"
)

func (m *chatModel) submit() (tea.Model, tea.Cmd) {
	val := strings.TrimRight(m.ta.Value(), " \n\t")
	if val == "" {
		return m, nil
	}
	// A credential must not stay one ↑ away for the rest of the session.
	if !carriesSecret(val) {
		m.pushHistory(val)
	}
	m.ta.Reset()
	m.suggestions = nil
	m.selected = 0

	if strings.HasPrefix(val, "/") {
		cmd := m.runCommand(val)
		m.layout()
		return m, cmd
	}

	m.add("you", val)
	// The user sees the clean message; the agent also gets any +project/@person
	// context so it drafts coherently.
	input := val
	if pre := m.buildMentionContext(context.Background(), val); pre != "" {
		input = pre + "\n\n" + val
	}
	m.thinking = true
	m.thinkStart = time.Now()
	m.layout()
	return m, tea.Batch(sendCmd(m.deps.Agent, input), spinnerTick())
}

func sendCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		out, err := a.Send(context.Background(), input)
		return replyMsg{text: out, err: err}
	}
}

// renderToolEvents surfaces what the agent did this turn (e.g. the label of a
// created task), so the user always sees the effect even if the model is terse.
func (m *chatModel) renderToolEvents() {
	for _, ev := range m.deps.Agent.LastTools() {
		var v struct {
			ID     int64  `json:"id"`
			Label  string `json:"label"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal([]byte(ev.Result), &v)
		tag := v.Label
		if tag == "" {
			tag = ev.Name
		} else if v.ID != 0 {
			tag = fmt.Sprintf("%s (#%d)", v.Label, v.ID)
		}
		switch ev.Name {
		case "create_task":
			m.add("tool", "+ "+tag)
		case "drop_task":
			m.add("tool", "- "+tag)
		case "set_status":
			if v.Status != "" {
				tag += " → " + v.Status
			}
			m.add("tool", "~ "+tag)
		case "set_state", "set_details":
			m.add("tool", "~ "+tag)
		case "remember_note":
			m.add("tool", "+ memory")
		case "recall_memory":
			m.add("tool", "? memory")
		case "upsert_project", "upsert_person":
			m.add("tool", "+ "+tag)
		case "add_project_note", "add_person_note":
			m.add("tool", "~ note "+tag)
		case "save_daily":
			m.add("tool", "~ daily "+tag)
		case "send_daily":
			m.add("tool", "✈ daily "+tag+" sent")
		default:
			m.add("tool", "· "+ev.Name)
		}
	}
}

// --- input history ---

func (m *chatModel) pushHistory(val string) {
	if n := len(m.history); n == 0 || m.history[n-1] != val {
		m.history = append(m.history, val)
	}
	m.histPos = -1
}

func (m *chatModel) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.histPos == -1 {
		m.histPos = len(m.history)
	}
	if m.histPos > 0 {
		m.histPos--
		m.ta.SetValue(m.history[m.histPos])
	}
	m.suggestions = nil
	m.layout()
}

func (m *chatModel) historyNext() {
	if m.histPos == -1 {
		return
	}
	m.histPos++
	if m.histPos >= len(m.history) {
		m.histPos = -1
		m.ta.SetValue("")
	} else {
		m.ta.SetValue(m.history[m.histPos])
	}
	m.suggestions = nil
	m.layout()
}
