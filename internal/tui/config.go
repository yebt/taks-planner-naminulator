package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/config"
)

type cfgField struct {
	label  string
	secret bool
	get    func() string
	set    func(string)
}

type configModel struct {
	cfg     *config.Config
	path    string
	fields  []cfgField
	cursor  int
	editing bool
	input   textinput.Model
	status  string
	width   int
	height  int
}

// RunConfig opens the configuration TUI. It mutates cfg in place and writes to
// path when the user presses 's'.
func RunConfig(cfg *config.Config, path string) error {
	ti := textinput.New()
	ti.Prompt = "› "
	m := &configModel{cfg: cfg, path: path, input: ti, fields: buildFields(cfg)}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func buildFields(cfg *config.Config) []cfgField {
	fields := []cfgField{
		{"active provider", false,
			func() string { return cfg.ActiveProvider },
			func(v string) { cfg.ActiveProvider = strings.TrimSpace(v) }},
		{"context budget (chars)", false,
			func() string { return strconv.Itoa(cfg.ContextBudget) },
			func(v string) {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					cfg.ContextBudget = n
				}
			}},
		{"memory project", false,
			func() string { return cfg.Memory.Project },
			func(v string) { cfg.Memory.Project = strings.TrimSpace(v) }},
		{"plane api token", true,
			func() string { return cfg.Plane.APIToken },
			func(v string) { cfg.Plane.APIToken = strings.TrimSpace(v) }},
		{"plane workspace slug", false,
			func() string { return cfg.Plane.WorkspaceSlug },
			func(v string) { cfg.Plane.WorkspaceSlug = strings.TrimSpace(v) }},
		{"plane project id", false,
			func() string { return cfg.Plane.ProjectID },
			func(v string) { cfg.Plane.ProjectID = strings.TrimSpace(v) }},
	}

	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		n := name // capture
		fields = append(fields,
			cfgField{n + " · model", false,
				func() string { return cfg.Providers[n].Model },
				func(v string) { pc := cfg.Providers[n]; pc.Model = strings.TrimSpace(v); cfg.Providers[n] = pc }},
			cfgField{n + " · api key", true,
				func() string { return cfg.Providers[n].APIKey },
				func(v string) { pc := cfg.Providers[n]; pc.APIKey = strings.TrimSpace(v); cfg.Providers[n] = pc }},
		)
	}
	return fields
}

func (m *configModel) Init() tea.Cmd { return nil }

func (m *configModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - 6
		return m, nil

	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "enter":
				m.fields[m.cursor].set(m.input.Value())
				m.editing = false
				m.input.Blur()
				m.status = "edited — press s to save"
				return m, nil
			case "esc":
				m.editing = false
				m.input.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.fields)-1 {
				m.cursor++
			}
		case "enter":
			m.input.SetValue(m.fields[m.cursor].get())
			m.input.CursorEnd()
			m.editing = true
			return m, m.input.Focus()
		case "s":
			if err := config.Save(m.path, *m.cfg); err != nil {
				m.status = "save failed: " + err.Error()
			} else {
				m.status = "saved → " + m.path
			}
		}
	}
	return m, nil
}

func (m *configModel) View() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("planner · config") + "\n\n")
	for i, f := range m.fields {
		cursor := "  "
		label := fmt.Sprintf("%-24s", f.label)
		if i == m.cursor {
			cursor = selStyle.Render("›") + " "
			label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111")).Render(label)
		}
		val := f.get()
		switch {
		case f.secret:
			val = maskSecret(val)
		case val == "":
			val = "—"
		}
		b.WriteString(cursor + label + "  " + val + "\n")
	}
	b.WriteString("\n")
	if m.editing {
		b.WriteString(sysStyle.Render("edit "+m.fields[m.cursor].label) + "\n" + m.input.View() + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("↑/↓ move · enter edit · s save · q quit"))
	if m.status != "" {
		b.WriteString("\n" + botLabel.Render(m.status))
	}
	return b.String()
}

func maskSecret(s string) string {
	if s == "" {
		return "(unset)"
	}
	n := len(s)
	if n > 6 {
		n = 6
	}
	return strings.Repeat("•", n) + " (set)"
}
