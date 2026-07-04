package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/plane"
	"github.com/webcloster-dev/planner/internal/telegram"
)

// planeGroups are Plane's five fixed state groups, in workflow order.
var planeGroups = []string{"backlog", "unstarted", "started", "completed", "cancelled"}

// cfgField is one editable row. A plain field opens a text input on enter; a
// field with choices cycles its value on enter; a field with an action runs it.
type cfgField struct {
	label   string
	secret  bool
	get     func() string
	set     func(string)
	choices []string      // cycle-picker when non-nil
	action  func() string // runs on enter; returns a status line
}

type cfgSection struct {
	name  string
	ready func() bool
}

type configModel struct {
	cfg     *config.Config
	path    string
	section int // -1 = main menu
	fields  []cfgField
	cursor  int
	editing bool
	input   textinput.Model
	status  string
	width   int
	height  int
}

// RunConfig opens the sectioned configuration TUI. It mutates cfg in place and
// writes to path when the user presses 's'.
func RunConfig(cfg *config.Config, path string) error {
	ti := textinput.New()
	ti.Prompt = "› "
	m := &configModel{cfg: cfg, path: path, input: ti, section: -1}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *configModel) sections() []cfgSection {
	return []cfgSection{
		{"Providers", m.cfg.ProvidersReady},
		{"Plane", m.cfg.Plane.Ready},
		{"Telegram", m.cfg.Telegram.Ready},
	}
}

func (m *configModel) enterSection(i int) {
	m.section = i
	m.cursor = 0
	m.status = ""
	switch i {
	case 0:
		m.fields = m.providerFields()
	case 1:
		m.fields = m.planeFields()
	case 2:
		m.fields = m.telegramFields()
	}
}

// --- section field builders ---

func (m *configModel) providerFields() []cfgField {
	names := m.providerNames()
	fields := []cfgField{
		{label: "active provider", choices: names,
			get: func() string { return m.cfg.ActiveProvider },
			set: func(v string) { m.cfg.ActiveProvider = strings.TrimSpace(v) }},
		{label: "context budget (chars)",
			get: func() string { return strconv.Itoa(m.cfg.ContextBudget) },
			set: func(v string) {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					m.cfg.ContextBudget = n
				}
			}},
		{label: "memory project",
			get: func() string { return m.cfg.Memory.Project },
			set: func(v string) { m.cfg.Memory.Project = strings.TrimSpace(v) }},
	}
	for _, name := range names {
		n := name // capture
		fields = append(fields,
			cfgField{label: n + " · model",
				get: func() string { return m.cfg.Providers[n].Model },
				set: func(v string) { pc := m.cfg.Providers[n]; pc.Model = strings.TrimSpace(v); m.cfg.Providers[n] = pc }},
			cfgField{label: n + " · api key", secret: true,
				get: func() string { return m.cfg.Providers[n].APIKey },
				set: func(v string) { pc := m.cfg.Providers[n]; pc.APIKey = strings.TrimSpace(v); m.cfg.Providers[n] = pc }},
		)
	}
	return fields
}

func (m *configModel) planeFields() []cfgField {
	P := &m.cfg.Plane
	fields := []cfgField{
		{label: "base url",
			get: func() string { return P.BaseURL },
			set: func(v string) { P.BaseURL = strings.TrimSpace(v) }},
		{label: "api token", secret: true,
			get: func() string { return P.APIToken },
			set: func(v string) { P.APIToken = strings.TrimSpace(v) }},
		{label: "workspace slug",
			get: func() string { return P.WorkspaceSlug },
			set: func(v string) { P.WorkspaceSlug = strings.TrimSpace(v) }},
		{label: "project id",
			get: func() string { return P.ProjectID },
			set: func(v string) { P.ProjectID = strings.TrimSpace(v) }},
		{label: "default estimate",
			get: func() string { return P.DefaultEstimate },
			set: func(v string) { P.DefaultEstimate = strings.TrimSpace(v) }},
		{label: "↻ fetch states from Plane", action: m.fetchStates},
	}
	// Once states are cached, offer a default-state picker per non-empty group.
	for _, g := range planeGroups {
		states := P.StatesByGroup(g)
		if len(states) == 0 {
			continue
		}
		grp := g // capture
		names := []string{"—"}
		for _, s := range states {
			names = append(names, s.Name)
		}
		fields = append(fields, cfgField{
			label:   "default · " + grp,
			choices: names,
			get:     func() string { return m.groupDefaultName(grp) },
			set:     func(v string) { m.setGroupDefault(grp, v) },
		})
	}
	return fields
}

func (m *configModel) telegramFields() []cfgField {
	T := &m.cfg.Telegram
	return []cfgField{
		{label: "bot token", secret: true,
			get: func() string { return T.BotToken },
			set: func(v string) { T.BotToken = strings.TrimSpace(v) }},
		{label: "chat id",
			get: func() string { return T.ChatID },
			set: func(v string) { T.ChatID = strings.TrimSpace(v) }},
		{label: "thread id (optional)",
			get: func() string { return T.ThreadID },
			set: func(v string) { T.ThreadID = strings.TrimSpace(v) }},
		{label: "✈ send test notification", action: m.testTelegram},
	}
}

// testTelegram sends a test message so the user can confirm token/chat/thread
// end-to-end from within the config.
func (m *configModel) testTelegram() string {
	T := m.cfg.Telegram
	if !T.Ready() {
		return "fill bot token + chat id first"
	}
	cl := telegram.New(T.BotToken, T.ChatID, T.ThreadID)
	if err := cl.Test(context.Background()); err != nil {
		return "test failed: " + err.Error()
	}
	return "test sent ✓ — check your Telegram chat"
}

// fetchStates pulls the project's workflow states from Plane and caches them,
// then rebuilds the section so the per-group default pickers appear.
func (m *configModel) fetchStates() string {
	P := m.cfg.Plane
	if !P.Ready() {
		return "fill base url / token / slug / project first"
	}
	cl := plane.New(plane.Config{
		BaseURL: P.BaseURL, Token: P.APIToken, WorkspaceSlug: P.WorkspaceSlug, ProjectID: P.ProjectID,
	})
	states, err := cl.ListStates(context.Background())
	if err != nil {
		return "fetch failed: " + err.Error()
	}
	out := make([]config.PlaneState, 0, len(states))
	for _, s := range states {
		out = append(out, config.PlaneState{ID: s.ID, Name: s.Name, Group: s.Group})
	}
	m.cfg.Plane.States = out
	m.fields = m.planeFields()
	return fmt.Sprintf("fetched %d states — pick a default per group, then s to save", len(out))
}

func (m *configModel) groupDefaultName(group string) string {
	id := m.cfg.Plane.StateDefaults[group]
	if id == "" {
		return "—"
	}
	for _, s := range m.cfg.Plane.States {
		if s.ID == id {
			return s.Name
		}
	}
	return "—"
}

func (m *configModel) setGroupDefault(group, name string) {
	if m.cfg.Plane.StateDefaults == nil {
		m.cfg.Plane.StateDefaults = map[string]string{}
	}
	if name == "—" {
		delete(m.cfg.Plane.StateDefaults, group)
		return
	}
	for _, s := range m.cfg.Plane.StatesByGroup(group) {
		if s.Name == name {
			m.cfg.Plane.StateDefaults[group] = s.ID
			return
		}
	}
}

func (m *configModel) providerNames() []string {
	names := make([]string, 0, len(m.cfg.Providers))
	for n := range m.cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// --- update ---

func (m *configModel) Init() tea.Cmd { return nil }

func (m *configModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - 6
		return m, nil
	case tea.KeyMsg:
		if m.editing {
			return m.updateEditing(msg)
		}
		if m.section == -1 {
			return m.updateMenu(msg)
		}
		return m.updateSection(msg)
	}
	return m, nil
}

func (m *configModel) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m *configModel) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	secs := m.sections()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(secs)-1 {
			m.cursor++
		}
	case "enter":
		m.enterSection(m.cursor)
	case "s":
		m.save()
	}
	return m, nil
}

func (m *configModel) updateSection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.section = -1
		m.cursor = 0
		m.status = ""
		return m, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.fields)-1 {
			m.cursor++
		}
	case "s":
		m.save()
	case "enter":
		f := m.fields[m.cursor]
		switch {
		case f.action != nil:
			m.status = f.action()
		case f.choices != nil:
			m.cycleChoice()
		default:
			m.input.SetValue(f.get())
			m.input.CursorEnd()
			m.editing = true
			return m, m.input.Focus()
		}
	}
	return m, nil
}

func (m *configModel) cycleChoice() {
	f := m.fields[m.cursor]
	cur := f.get()
	idx := -1
	for i, c := range f.choices {
		if c == cur {
			idx = i
			break
		}
	}
	f.set(f.choices[(idx+1)%len(f.choices)])
	m.status = "changed — press s to save"
}

func (m *configModel) save() {
	if err := config.Save(m.path, *m.cfg); err != nil {
		m.status = "save failed: " + err.Error()
		return
	}
	m.status = "saved → " + m.path
}

// --- view ---

func (m *configModel) View() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("planner · config") + "\n\n")
	if m.section == -1 {
		b.WriteString(m.viewMenu())
	} else {
		b.WriteString(m.viewSection())
	}
	if m.status != "" {
		b.WriteString("\n\n" + botLabel.Render(m.status))
	}
	return b.String()
}

func (m *configModel) viewMenu() string {
	var b strings.Builder
	b.WriteString(sysStyle.Render("select a section to configure") + "\n\n")
	for i, s := range m.sections() {
		mark := errStyle.Render("✗")
		if s.ready() {
			mark = botLabel.Render("✓")
		}
		cursor := "  "
		label := fmt.Sprintf("%-12s", s.name)
		if i == m.cursor {
			cursor = selStyle.Render("›") + " "
			label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111")).Render(label)
		}
		b.WriteString(cursor + mark + " " + label + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("↑/↓ move · enter open · s save · q quit"))
	return b.String()
}

func (m *configModel) viewSection() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	b.WriteString(title.Render(m.sections()[m.section].name) + "\n\n")
	for i, f := range m.fields {
		cursor := "  "
		label := fmt.Sprintf("%-22s", f.label)
		if i == m.cursor {
			cursor = selStyle.Render("›") + " "
			label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111")).Render(label)
		}
		val := ""
		if f.action == nil {
			val = f.get()
			switch {
			case f.secret:
				val = maskSecret(val)
			case val == "":
				val = "—"
			}
			if f.choices != nil {
				val = "‹ " + val + " ›"
			}
		}
		b.WriteString(cursor + label + "  " + val + "\n")
	}
	b.WriteString("\n")
	if m.editing {
		b.WriteString(sysStyle.Render("edit "+m.fields[m.cursor].label) + "\n" + m.input.View() + "\n\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ move · enter edit/cycle/run · s save · esc back · q quit"))
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
