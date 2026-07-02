// Package tui is the interactive chat harness: a Bubbletea UI with a styled
// header, colored conversation, multi-line input, slash-command autocomplete,
// provider/key config from slash commands, and presets that hit the task store
// directly (so you can create/list tasks without any LLM running).
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/tools"
)

// ChatDeps wires the harness to the rest of the app.
type ChatDeps struct {
	Cfg        *config.Config
	ConfigPath string
	Agent      *agent.Agent
	Store      store.TaskStore
	Tools      *tools.Registry
	Build      func(cfg config.Config, name string) (llm.Provider, error)
}

// RunChat starts the interactive harness.
func RunChat(deps ChatDeps) error {
	m := newChatModel(deps)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newChatModel(deps ChatDeps) *chatModel {
	ta := textarea.New()
	ta.Placeholder = "tell me what you're working on…  (/ for commands)"
	ta.Prompt = "▌ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// Enter submits; Ctrl+J inserts a newline (multi-line input).
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "newline"))
	ta.Focus()

	m := &chatModel{
		deps: deps,
		ta:   ta,
		vp:   viewport.New(80, 20),
	}
	m.add("sys", "planner harness — type a message, or / for commands. Try /help.")
	return m
}

// --- styles ---

var (
	headerStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).Padding(0, 1)
	youLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	botLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	sysStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	cmdStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	sugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	selStyle  = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62"))
	thinkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
)

// --- model ---

type entry struct {
	role string // you | planner | sys | cmd | err
	text string
}

type suggestion struct{ full, desc string }

type chatModel struct {
	deps        ChatDeps
	ta          textarea.Model
	vp          viewport.Model
	width       int
	height      int
	ready       bool
	entries     []entry
	suggestions []suggestion
	selected    int
	thinking    bool
}

type replyMsg struct {
	text string
	err  error
}

func (m *chatModel) Init() tea.Cmd { return textarea.Blink }

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		return m, nil

	case replyMsg:
		m.thinking = false
		if msg.err != nil {
			m.add("err", msg.err.Error())
		} else {
			m.add("planner", strings.TrimSpace(msg.text))
		}
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m *chatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	// Suggestion navigation takes priority when the menu is open.
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
		case "esc":
			m.suggestions = nil
			m.layout()
			return m, nil
		}
	}

	switch msg.String() {
	case "enter":
		if m.thinking {
			return m, nil
		}
		return m.submit()
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.suggestions = computeSuggestions(m.ta.Value(), m.providerNames())
	m.selected = 0
	m.layout()
	return m, cmd
}

func (m *chatModel) submit() (tea.Model, tea.Cmd) {
	val := strings.TrimRight(m.ta.Value(), " \n\t")
	if val == "" {
		return m, nil
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
	m.thinking = true
	m.layout()
	return m, sendCmd(m.deps.Agent, val)
}

func sendCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		out, err := a.Send(context.Background(), input)
		return replyMsg{text: out, err: err}
	}
}

// --- commands ---

var baseCommands = []suggestion{
	{"/help", "show commands"},
	{"/todos", "list tasks"},
	{"/new", "/new <TYPE> <title> — create a task (no LLM)"},
	{"/status", "/status <id> <status> — change a task status"},
	{"/model", "switch LLM provider"},
	{"/key", "/key <provider> <apikey> — set & save an API key"},
	{"/clear", "clear the conversation"},
	{"/quit", "exit"},
}

func (m *chatModel) runCommand(val string) tea.Cmd {
	m.add("cmd", val)
	fields := strings.Fields(val)
	ctx := context.Background()

	switch fields[0] {
	case "/quit", "/exit", "/q":
		return tea.Quit

	case "/help":
		var b strings.Builder
		b.WriteString("commands:\n")
		for _, c := range baseCommands {
			b.WriteString(fmt.Sprintf("  %-8s %s\n", c.full, c.desc))
		}
		b.WriteString("\nkeys: enter=send · ctrl+j=newline · tab=complete · ↑/↓=pick · esc=close menu")
		b.WriteString("\nAPI keys live in " + m.deps.ConfigPath + " (or use /key).")
		m.add("sys", strings.TrimRight(b.String(), "\n"))

	case "/clear":
		m.entries = nil
		m.deps.Agent.Reset()
		m.add("sys", "conversation cleared.")

	case "/todos":
		m.showTodos(ctx)

	case "/new":
		if len(fields) < 3 {
			m.add("err", "usage: /new <TYPE> <title>")
			break
		}
		args, _ := json.Marshal(map[string]string{"type": fields[1], "title": strings.Join(fields[2:], " ")})
		out, err := m.deps.Tools.Dispatch(ctx, "create_task", string(args))
		if err != nil {
			m.add("err", err.Error())
		} else {
			m.add("sys", "created: "+out)
		}

	case "/status":
		if len(fields) < 3 {
			m.add("err", "usage: /status <id> <status>")
			break
		}
		if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
			m.add("err", "id must be a number")
			break
		}
		args := fmt.Sprintf(`{"id":%s,"status":%q}`, fields[1], fields[2])
		out, err := m.deps.Tools.Dispatch(ctx, "set_status", args)
		if err != nil {
			m.add("err", err.Error())
		} else {
			m.add("sys", "updated: "+out)
		}

	case "/model":
		if len(fields) < 2 {
			m.add("sys", "providers: "+strings.Join(m.providerNames(), ", ")+"\nuse: /model <name>")
			break
		}
		m.switchModel(fields[1])

	case "/key":
		if len(fields) < 3 {
			m.add("err", "usage: /key <provider> <apikey>")
			break
		}
		m.setKey(fields[1], strings.Join(fields[2:], " "))

	default:
		m.add("err", "unknown command: "+fields[0]+" (try /help)")
	}
	return nil
}

func (m *chatModel) showTodos(ctx context.Context) {
	tasks, err := m.deps.Store.List(ctx, store.Filter{})
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(tasks) == 0 {
		m.add("sys", "no tasks yet.")
		return
	}
	var b strings.Builder
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("%3d  [%s] %-6s %-32s %s\n",
			t.ID, t.Label, t.Type, trunc(t.Title, 32), t.Status))
	}
	m.add("sys", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) switchModel(name string) {
	if _, ok := m.deps.Cfg.Providers[name]; !ok {
		m.add("err", "provider not found: "+name)
		return
	}
	p, err := m.deps.Build(*m.deps.Cfg, name)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.deps.Agent.SetProvider(p)
	m.deps.Cfg.ActiveProvider = name
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "switched, but save failed: "+err.Error())
		return
	}
	m.add("sys", "provider → "+name)
}

func (m *chatModel) setKey(name, key string) {
	pc, ok := m.deps.Cfg.Providers[name]
	if !ok {
		m.add("err", "provider not found: "+name)
		return
	}
	pc.APIKey = key
	m.deps.Cfg.Providers[name] = pc
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	// If it's the active provider, rebuild so the key takes effect now.
	if m.deps.Cfg.ActiveProvider == name {
		if p, err := m.deps.Build(*m.deps.Cfg, name); err == nil {
			m.deps.Agent.SetProvider(p)
		}
	}
	m.add("sys", "API key saved for "+name+".")
}

// --- suggestions ---

// computeSuggestions is a pure helper (unit-tested) that returns the completion
// menu for the current input.
func computeSuggestions(val string, providers []string) []suggestion {
	if !strings.HasPrefix(val, "/") || strings.Contains(val, "\n") {
		return nil
	}
	fields := strings.Fields(val)
	endsSpace := strings.HasSuffix(val, " ")
	var out []suggestion

	switch {
	case len(fields) <= 1 && !endsSpace:
		prefix := val
		for _, c := range baseCommands {
			if strings.HasPrefix(c.full, prefix) {
				out = append(out, c)
			}
		}
	case fields[0] == "/model":
		prefix := ""
		if len(fields) > 1 {
			prefix = fields[1]
		}
		for _, name := range providers {
			if strings.HasPrefix(name, prefix) {
				out = append(out, suggestion{"/model " + name, "switch to " + name})
			}
		}
	case fields[0] == "/key":
		prefix := ""
		if len(fields) > 1 {
			prefix = fields[1]
		}
		for _, name := range providers {
			if strings.HasPrefix(name, prefix) {
				out = append(out, suggestion{"/key " + name + " ", "set API key for " + name})
			}
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func (m *chatModel) acceptSuggestion() {
	if m.selected >= len(m.suggestions) {
		return
	}
	val := m.suggestions[m.selected].full
	first := strings.Fields(val)[0]
	needsArg := map[string]bool{"/new": true, "/status": true, "/model": true, "/key": true}
	if needsArg[first] && !strings.HasSuffix(val, " ") {
		val += " "
	}
	m.ta.SetValue(val)
	m.suggestions = computeSuggestions(m.ta.Value(), m.providerNames())
	m.selected = 0
	m.layout()
}

func (m *chatModel) providerNames() []string {
	names := make([]string, 0, len(m.deps.Cfg.Providers))
	for n := range m.deps.Cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// --- rendering ---

func (m *chatModel) add(role, text string) {
	m.entries = append(m.entries, entry{role: role, text: text})
	m.refresh()
}

func (m *chatModel) refresh() {
	w := m.vp.Width
	if w < 10 {
		w = 10
	}
	body := lipgloss.NewStyle().Width(w)
	blocks := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		switch e.role {
		case "you":
			blocks = append(blocks, youLabel.Render("you")+"\n"+body.Render(e.text))
		case "planner":
			blocks = append(blocks, botLabel.Render("planner")+"\n"+body.Render(e.text))
		case "cmd":
			blocks = append(blocks, cmdStyle.Render(e.text))
		case "err":
			blocks = append(blocks, body.Inherit(errStyle).Render("error: "+e.text))
		default:
			blocks = append(blocks, body.Inherit(sysStyle).Render(e.text))
		}
	}
	m.vp.SetContent(strings.Join(blocks, "\n\n"))
	m.vp.GotoBottom()
}

func (m *chatModel) layout() {
	if !m.ready {
		return
	}
	inputH := 3
	helpH := 1
	sugH := len(m.suggestions)
	m.ta.SetWidth(m.width - 2)
	m.ta.SetHeight(inputH)
	vpH := m.height - 1 - inputH - helpH - sugH - 1
	if vpH < 3 {
		vpH = 3
	}
	m.vp.Width = m.width
	m.vp.Height = vpH
	m.refresh()
}

func (m *chatModel) View() string {
	if !m.ready {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render(m.header()))
	b.WriteString("\n")
	b.WriteString(m.vp.View())
	b.WriteString("\n")
	if len(m.suggestions) > 0 {
		b.WriteString(m.renderSuggestions())
		b.WriteString("\n")
	}
	b.WriteString(m.ta.View())
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m *chatModel) header() string {
	return fmt.Sprintf("planner · %s", m.deps.Agent.Provider())
}

func (m *chatModel) footer() string {
	if m.thinking {
		return thinkStyle.Render("⏳ thinking…")
	}
	return helpStyle.Render("enter send · ctrl+j newline · / commands · tab complete · ctrl+c quit")
}

func (m *chatModel) renderSuggestions() string {
	lines := make([]string, 0, len(m.suggestions))
	for i, s := range m.suggestions {
		line := fmt.Sprintf(" %-18s %s", s.full, s.desc)
		if i == m.selected {
			line = selStyle.Render(line)
		} else {
			line = sugStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
