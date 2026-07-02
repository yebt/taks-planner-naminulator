// Package tui is the Bubbletea-based board view (the config/review surface).
// For the MVP it's a read-only todo list; editing lives in the chat agent and
// the config file.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/domain"
)

type model struct {
	tasks  []domain.Task
	cursor int
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString("planner — tasks (↑/↓ move · q quit)\n\n")
	if len(m.tasks) == 0 {
		b.WriteString("  no tasks yet — talk to the agent to create some.\n")
		return b.String()
	}
	for i, t := range m.tasks {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s[%s] %-6s %-40s (%s)\n",
			cursor, t.Label, t.Type, truncate(t.Title, 40), t.Status))
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// Run shows the task list until the user quits.
func Run(tasks []domain.Task) error {
	_, err := tea.NewProgram(model{tasks: tasks}).Run()
	return err
}
