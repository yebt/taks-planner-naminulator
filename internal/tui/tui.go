// Package tui is the interactive chat harness: a Bubbletea UI with a bold
// header, a status bar (provider/model/context/memory/chat), colored
// conversation, multi-line input (alt+enter), slash-command autocomplete,
// input history (↑/↓), conversation save/load, and long-term memory recall.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	Build      func(cfg config.Config, name string) (llm.Provider, error)
}

// RunChat starts the interactive harness.
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
	return m
}

// --- styles ---

var (
	headerStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).Padding(0, 1)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).Background(lipgloss.Color("237"))
	youLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	botLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	sysStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	cmdStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	sugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	selStyle  = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62"))
	thinkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	confirmStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160")).Padding(0, 1)
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("108"))
	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	userBubbleBG = lipgloss.Color("236")
	inputBG      = lipgloss.Color("236")
)

// --- model ---

type entry struct {
	role string // you | planner | sys | cmd | err
	text string
}

type suggestion struct{ full, desc string }

// pendingConfirm is a y/n gate for a destructive action (e.g. cloud delete).
type pendingConfirm struct {
	prompt string
	action func()
}

// statePicker is the active "/state <id>" selection: the menu lists the real
// Plane states and enter applies the highlighted one to the task.
type statePicker struct {
	taskID int64
	states []config.PlaneState
}

type chatModel struct {
	deps         ChatDeps
	ta           textarea.Model
	vp           viewport.Model
	width        int
	height       int
	ready        bool
	entries      []entry
	suggestions  []suggestion
	selected     int
	thinking     bool
	quitArmed    bool            // first ctrl+c clears; second quits
	confirm      *pendingConfirm // non-nil while awaiting y/n
	statePick    *statePicker    // non-nil while picking a Plane state
	dailyDraft   string          // last generated/edited daily digest
	dailyEditing bool            // true while editing the daily in the textarea
	history      []string        // submitted inputs, for ↑/↓ recall
	histPos      int             // -1 = not navigating
	convID       int64           // 0 = unsaved
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
			m.renderToolEvents()
			if txt := strings.TrimSpace(msg.text); txt != "" {
				m.add("planner", txt)
			}
			m.autosave()
		}
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

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
			m.add("sys", "daily updated. use /daily send to deliver it.")
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
	m.pushHistory(val)
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

// --- commands ---

var baseCommands = []suggestion{
	{"/help", "show commands"},
	{"/todos", "list tasks"},
	{"/task", "/task <id> — show a task in full"},
	{"/new", "/new <TYPE> <title> — create a task (no LLM)"},
	{"/status", "/status <id> <status> — change a task status"},
	{"/state", "/state <id> — pick a Plane state from the real list"},
	{"/drop", "/drop <id> [sync] — delete a task (sync also removes it in Plane)"},
	{"/model", "switch LLM provider"},
	{"/fav", "/fav [save|del] <name> — save/switch a provider+model favorite"},
	{"/key", "/key <provider> <apikey> — set & save an API key"},
	{"/save", "save this conversation"},
	{"/chats", "list saved conversations"},
	{"/load", "/load <id> — restore a conversation"},
	{"/newchat", "start a fresh conversation"},
	{"/recall", "/recall <query> — search long-term memory"},
	{"/remember", "/remember <note> — save to long-term memory"},
	{"/sync", "push local tasks to Plane"},
	{"/pull", "pull states from Plane"},
	{"/daily", "/daily [edit|send] — build/edit/send today's digest to Telegram"},
	{"/clear", "clear the conversation"},
	{"/quit", "exit"},
}

var needsArg = map[string]bool{
	"/new": true, "/status": true, "/model": true, "/key": true, "/fav": true,
	"/load": true, "/recall": true, "/remember": true, "/task": true, "/drop": true, "/state": true,
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
			b.WriteString(fmt.Sprintf("  %-10s %s\n", c.full, c.desc))
		}
		b.WriteString("\nkeys: enter=send · alt+enter=newline · tab/enter=complete · ↑/↓=history · esc=close menu")
		b.WriteString("\nAPI keys: " + m.deps.ConfigPath + " (or /key).")
		m.add("sys", strings.TrimRight(b.String(), "\n"))

	case "/clear":
		m.entries = nil
		m.deps.Agent.Reset()
		m.add("sys", "conversation cleared.")

	case "/todos":
		m.showTodos(ctx)

	case "/task":
		if len(fields) < 2 {
			m.add("err", "usage: /task <id>")
			break
		}
		m.showTask(ctx, fields[1])

	case "/new":
		if len(fields) < 3 {
			m.add("err", "usage: /new <TYPE> <title>")
			break
		}
		args, _ := json.Marshal(map[string]string{"type": fields[1], "title": strings.Join(fields[2:], " ")})
		out, err := m.deps.Tools.Dispatch(ctx, "create_task", string(args))
		m.report("created: ", out, err)

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
		m.report("updated: ", out, err)

	case "/state":
		if len(fields) < 2 {
			m.add("err", "usage: /state <id>")
			break
		}
		id, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			m.add("err", "id must be a number")
			break
		}
		m.openStatePicker(id)

	case "/drop":
		if len(fields) < 2 {
			m.add("err", "usage: /drop <id> [sync]")
			break
		}
		id, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			m.add("err", "id must be a number")
			break
		}
		withSync := len(fields) > 2 && (fields[2] == "sync" || fields[2] == "--sync")
		if withSync {
			m.confirm = &pendingConfirm{
				prompt: fmt.Sprintf("delete #%d locally and in Plane?", id),
				action: func() { m.dropTask(context.Background(), id, true) },
			}
			break
		}
		m.dropTask(ctx, id, false)

	case "/model":
		if len(fields) < 2 {
			m.add("sys", "providers: "+strings.Join(m.providerNames(), ", ")+"\nuse: /model <name>")
			break
		}
		m.switchModel(fields[1])

	case "/fav":
		switch {
		case len(fields) == 1:
			m.listFavorites()
		case fields[1] == "save":
			m.saveFavorite(strings.Join(fields[2:], " "))
		case fields[1] == "del" || fields[1] == "drop":
			if len(fields) < 3 {
				m.add("err", "usage: /fav del <name>")
				break
			}
			m.delFavorite(strings.Join(fields[2:], " "))
		default:
			m.applyFavorite(strings.Join(fields[1:], " "))
		}

	case "/key":
		if len(fields) < 3 {
			m.add("err", "usage: /key <provider> <apikey>")
			break
		}
		m.setKey(fields[1], strings.Join(fields[2:], " "))

	case "/save":
		title := m.convTitle()
		if len(fields) > 1 {
			title = strings.Join(fields[1:], " ")
		}
		m.saveConversation(title)

	case "/chats":
		m.showChats(ctx)

	case "/load":
		if len(fields) < 2 {
			m.add("err", "usage: /load <id>")
			break
		}
		m.loadConversation(ctx, fields[1])

	case "/newchat":
		m.deps.Agent.Reset()
		m.convID = 0
		m.entries = nil
		m.add("sys", "started a new conversation.")

	case "/recall":
		if len(fields) < 2 {
			m.add("err", "usage: /recall <query>")
			break
		}
		out, err := m.deps.Memory.Recall(ctx, strings.Join(fields[1:], " "), 5)
		if err != nil {
			m.add("err", err.Error())
		} else {
			m.add("sys", out)
		}

	case "/remember":
		if len(fields) < 2 {
			m.add("err", "usage: /remember <note>")
			break
		}
		note := strings.Join(fields[1:], " ")
		if err := m.deps.Memory.Save(ctx, trunc(note, 48), note); err != nil {
			m.add("err", err.Error())
		} else {
			m.add("sys", "remembered: "+trunc(note, 48))
		}

	case "/sync":
		m.syncAll(ctx)

	case "/pull":
		if m.deps.Syncer == nil || !m.deps.Syncer.Configured() {
			m.add("err", "Plane not configured (set base_url/token/slug/project in config)")
			break
		}
		n, err := m.deps.Syncer.PullStates(ctx)
		if err != nil {
			m.add("err", err.Error())
		} else {
			m.add("sys", fmt.Sprintf("pulled states: %d task(s) updated", n))
		}

	case "/daily":
		switch {
		case len(fields) > 1 && fields[1] == "edit":
			m.editDaily()
		case len(fields) > 1 && fields[1] == "send":
			m.sendDaily(ctx)
		default:
			m.generateDaily(ctx)
		}

	default:
		m.add("err", "unknown command: "+fields[0]+" (try /help)")
	}
	return nil
}

func (m *chatModel) report(prefix, out string, err error) {
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.add("sys", prefix+out)
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
	typeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))

	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("todos · %d", len(tasks))) + "\n\n")
	for _, t := range tasks {
		st := statusStyleFor(t.Status)
		line := fmt.Sprintf("%s %s  %s  %-40s  %s",
			st.Render("●"),
			idStyle.Render(fmt.Sprintf("%3d", t.ID)),
			typeStyle.Render(fmt.Sprintf("%-6s", t.Type)),
			trunc(t.Title, 40),
			st.Render(string(t.Status)))
		switch {
		case t.WorkItemSeq > 0:
			line += helpStyle.Render(fmt.Sprintf("  #%d", t.WorkItemSeq))
		case t.WorkItemID != "":
			line += helpStyle.Render("  ⇅")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("/task <id> for detail · /drop <id> [sync] to remove"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

// statusStyleFor maps a task status to a color so the list scans at a glance.
func statusStyleFor(s domain.Status) lipgloss.Style {
	var c lipgloss.Color
	switch s {
	case domain.StatusDone:
		c = "42" // green
	case domain.StatusInProgress:
		c = "214" // orange
	case domain.StatusTodo:
		c = "39" // blue
	case domain.StatusBlocked, domain.StatusRejected:
		c = "203" // red
	case domain.StatusBacklog, domain.StatusPostponed:
		c = "245" // gray
	case domain.StatusCancelled:
		c = "240" // dim gray
	default:
		c = "252"
	}
	return lipgloss.NewStyle().Foreground(c)
}

// openStatePicker shows a menu of the real Plane states (from the config cache)
// so the user selects instead of guessing. Requires a prior fetch in config.
func (m *chatModel) openStatePicker(id int64) {
	states := m.deps.Cfg.Plane.States
	if len(states) == 0 {
		m.add("err", "no states cached — run: planner config → Plane → fetch states")
		return
	}
	if _, err := m.deps.Store.Get(context.Background(), id); err != nil {
		m.add("err", err.Error())
		return
	}
	m.statePick = &statePicker{taskID: id, states: states}
	m.suggestions = m.suggestions[:0]
	for _, s := range states {
		m.suggestions = append(m.suggestions, suggestion{full: s.Name, desc: "(" + s.Group + ")"})
	}
	m.selected = 0
	m.layout()
}

// applyStatePick sets the highlighted Plane state on the task via set_state.
func (m *chatModel) applyStatePick() {
	sp := m.statePick
	m.statePick = nil
	if sp == nil || m.selected >= len(sp.states) {
		m.suggestions = nil
		return
	}
	name := sp.states[m.selected].Name
	m.suggestions = nil
	args := fmt.Sprintf(`{"id":%d,"state":%q}`, sp.taskID, name)
	out, err := m.deps.Tools.Dispatch(context.Background(), "set_state", args)
	m.report("state: ", out, err)
}

// dropTask deletes a task locally, and (when withSync) removes its Plane work
// item first — aborting the local delete if the cloud delete fails.
func (m *chatModel) dropTask(ctx context.Context, id int64, withSync bool) {
	if withSync && m.deps.Syncer != nil && m.deps.Syncer.Configured() {
		if t, err := m.deps.Store.Get(ctx, id); err == nil && t.WorkItemID != "" {
			if err := m.deps.Syncer.Delete(ctx, &t); err != nil {
				m.add("err", "Plane delete failed (task kept): "+err.Error())
				return
			}
		}
	}
	out, err := m.deps.Tools.Dispatch(ctx, "drop_task", fmt.Sprintf(`{"id":%d}`, id))
	m.report("dropped: ", out, err)
}

// generateDaily builds the digest from today's activity, stores it as the draft,
// and shows it (copyable from the terminal).
func (m *chatModel) generateDaily(ctx context.Context) {
	tasks, err := m.deps.Store.List(ctx, store.Filter{TouchedToday: true})
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.dailyDraft = buildDaily(tasks)
	m.add("raw", m.dailyDraft)
	m.add("sys", "daily draft ready — /daily edit to tweak, /daily send to deliver.")
}

// editDaily loads the current draft into the textarea for inline editing.
func (m *chatModel) editDaily() {
	if strings.TrimSpace(m.dailyDraft) == "" {
		m.add("err", "no daily yet — run /daily first")
		return
	}
	m.ta.SetValue(m.dailyDraft)
	m.dailyEditing = true
	m.suggestions = nil
	m.layout()
}

// sendDaily delivers the draft to Telegram, degrading with a clear warning when
// the integration is not configured.
func (m *chatModel) sendDaily(ctx context.Context) {
	if strings.TrimSpace(m.dailyDraft) == "" {
		m.add("err", "no daily to send — run /daily first")
		return
	}
	if m.deps.Telegram == nil || !m.deps.Telegram.Configured() {
		m.add("err", "can't send: Telegram not configured (set bot token + chat id in config)")
		return
	}
	if err := m.deps.Telegram.Send(ctx, m.dailyDraft); err != nil {
		m.add("err", err.Error())
		return
	}
	m.add("sys", "daily sent to Telegram ✓")
}

// buildDaily renders today's touched tasks as a markdown digest, grouped by
// status in workflow order.
func buildDaily(tasks []domain.Task) string {
	var b strings.Builder
	b.WriteString("# Daily — " + time.Now().Format("2006-01-02") + "\n")
	if len(tasks) == 0 {
		b.WriteString("\n_Sin actividad registrada hoy._")
		return b.String()
	}
	order := []struct {
		status domain.Status
		label  string
	}{
		{domain.StatusInProgress, "En progreso"},
		{domain.StatusDone, "Hecho"},
		{domain.StatusBlocked, "Bloqueadas"},
		{domain.StatusRejected, "Devueltas por Calidad"},
		{domain.StatusTodo, "Por hacer"},
		{domain.StatusPostponed, "Pospuestas"},
		{domain.StatusBacklog, "Backlog"},
		{domain.StatusCancelled, "Canceladas"},
	}
	for _, grp := range order {
		var lines []string
		for _, t := range tasks {
			if t.Status != grp.status {
				continue
			}
			code := ""
			if t.WorkItemSeq > 0 {
				code = fmt.Sprintf("#%d ", t.WorkItemSeq)
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s%s", t.Type, code, t.Title))
		}
		if len(lines) > 0 {
			b.WriteString("\n## " + grp.label + "\n")
			b.WriteString(strings.Join(lines, "\n") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// syncAll pushes every local task to Plane, reporting failures individually.
func (m *chatModel) syncAll(ctx context.Context) {
	if m.deps.Syncer == nil || !m.deps.Syncer.Configured() {
		m.add("err", "Plane not configured (set base_url/token/slug/project in config)")
		return
	}
	tasks, err := m.deps.Store.List(ctx, store.Filter{})
	if err != nil {
		m.add("err", err.Error())
		return
	}
	pushed, failed := 0, 0
	for _, t := range tasks {
		tt := t
		if err := m.deps.Syncer.Push(ctx, &tt); err != nil {
			m.add("err", fmt.Sprintf("#%d %s: %v", t.ID, t.Label, err))
			failed++
		} else {
			pushed++
		}
	}
	m.add("sys", fmt.Sprintf("sync → Plane: %d pushed, %d failed", pushed, failed))
}

// showTask renders one task expanded following the activity template.
func (m *chatModel) showTask(ctx context.Context, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		m.add("err", "id must be a number")
		return
	}
	t, err := m.deps.Store.Get(ctx, id)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	w := m.vp.Width
	if w < 10 {
		w = 10
	}
	wrap := lipgloss.NewStyle().Width(w)

	var b strings.Builder
	b.WriteString(title.Render(fmt.Sprintf("%s · %s · #%d", t.Type, t.Label, t.ID)) + "\n")
	b.WriteString(title.Render(t.Title) + "\n\n")
	b.WriteString(head.Render("Estado") + "\n")
	workItem := orDash(t.WorkItemID)
	if t.WorkItemSeq > 0 {
		workItem = fmt.Sprintf("#%d (%s)", t.WorkItemSeq, t.WorkItemID)
	}
	b.WriteString(fmt.Sprintf("- status: %s\n- priority: %s\n- state: %s\n- work item: %s\n- fechas: %s → %s\n\n",
		t.Status, t.PlanePriority(), orDash(t.State), workItem, orDash(t.StartDate), orDash(t.DueDate)))
	b.WriteString(head.Render("Descripción") + "\n")
	b.WriteString(wrap.Render(orDash(t.Description)) + "\n\n")
	writeDetails(&b, head, wrap, t.Details)
	b.WriteString(head.Render("Fechas") + "\n")
	b.WriteString(fmt.Sprintf("- creada: %s\n- actualizada: %s\n- última interacción: %s",
		t.CreatedAt.Local().Format("2006-01-02 15:04"),
		t.UpdatedAt.Local().Format("2006-01-02 15:04"),
		t.TouchedAt.Local().Format("2006-01-02 15:04")))
	m.add("raw", b.String())
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// writeDetails appends the filled activity-template sections (skips empties).
func writeDetails(b *strings.Builder, head, wrap lipgloss.Style, d domain.TaskDetails) {
	line := func(label, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		b.WriteString(head.Render(label) + "\n")
		b.WriteString(wrap.Render(val) + "\n\n")
	}
	list := func(label string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString(head.Render(label) + "\n")
		for _, it := range items {
			b.WriteString(wrap.Render("- "+it) + "\n")
		}
		b.WriteString("\n")
	}
	line("Objetivo", d.Objective)
	line("Justificación", d.Justification)
	if d.AsA != "" || d.IWant != "" || d.SoThat != "" {
		b.WriteString(head.Render("Descripción funcional") + "\n")
		b.WriteString(wrap.Render(fmt.Sprintf("Como %s\nQuiero %s\nPara %s",
			orDash(d.AsA), orDash(d.IWant), orDash(d.SoThat))) + "\n\n")
	}
	list("Pre-condiciones", d.Preconditions)
	list("Criterios de aceptación", d.AcceptanceCriteria)
	line("Consideraciones técnicas", d.TechNotes)
	line("Funcionalidad relacionada", d.RelatedFeature)
	line("Ambiente", d.Environment)
	list("Pasos a reproducir", d.StepsToReproduce)
	line("Resultado actual", d.ActualResult)
	line("Resultado esperado", d.ExpectedResult)
	if len(d.Checklist) > 0 {
		b.WriteString(head.Render("Checklist") + "\n")
		for _, it := range d.Checklist {
			mark := "☐"
			if it.Done {
				mark = "☑"
			}
			b.WriteString(wrap.Render(mark+" "+it.Text) + "\n")
		}
		b.WriteString("\n")
	}
	list("Anexos", d.Links)
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

// --- favorites (saved provider+model combos) ---

func (m *chatModel) listFavorites() {
	if len(m.deps.Cfg.Favorites) == 0 {
		m.add("sys", "no favorites yet. use: /fav save <name> to store the current provider+model.")
		return
	}
	var b strings.Builder
	b.WriteString("favorites:\n")
	for _, f := range m.deps.Cfg.Favorites {
		b.WriteString(fmt.Sprintf("  %-16s %s · %s\n", f.Name, f.Provider, f.Model))
	}
	b.WriteString("\nuse: /fav <name> to switch")
	m.add("sys", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) saveFavorite(name string) {
	prov := m.deps.Cfg.ActiveProvider
	model := m.deps.Cfg.Providers[prov].Model
	if strings.TrimSpace(name) == "" {
		name = prov + ":" + model
	}
	fav := config.Favorite{Name: name, Provider: prov, Model: model}
	replaced := false
	for i, f := range m.deps.Cfg.Favorites {
		if strings.EqualFold(f.Name, name) {
			m.deps.Cfg.Favorites[i] = fav
			replaced = true
			break
		}
	}
	if !replaced {
		m.deps.Cfg.Favorites = append(m.deps.Cfg.Favorites, fav)
	}
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	m.add("sys", fmt.Sprintf("saved favorite %q → %s · %s", name, prov, model))
}

func (m *chatModel) delFavorite(name string) {
	out := m.deps.Cfg.Favorites[:0]
	found := false
	for _, f := range m.deps.Cfg.Favorites {
		if strings.EqualFold(f.Name, name) {
			found = true
			continue
		}
		out = append(out, f)
	}
	if !found {
		m.add("err", "favorite not found: "+name)
		return
	}
	m.deps.Cfg.Favorites = out
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	m.add("sys", "removed favorite: "+name)
}

func (m *chatModel) applyFavorite(name string) {
	for _, f := range m.deps.Cfg.Favorites {
		if !strings.EqualFold(f.Name, name) {
			continue
		}
		pc, ok := m.deps.Cfg.Providers[f.Provider]
		if !ok {
			m.add("err", "favorite provider missing from config: "+f.Provider)
			return
		}
		pc.Model = f.Model
		m.deps.Cfg.Providers[f.Provider] = pc
		p, err := m.deps.Build(*m.deps.Cfg, f.Provider)
		if err != nil {
			m.add("err", err.Error())
			return
		}
		m.deps.Agent.SetProvider(p)
		m.deps.Cfg.ActiveProvider = f.Provider
		if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
			m.add("err", "applied, but save failed: "+err.Error())
			return
		}
		m.add("sys", fmt.Sprintf("favorite → %s (%s · %s)", f.Name, f.Provider, f.Model))
		return
	}
	m.add("err", "favorite not found: "+name)
}

func (m *chatModel) setKey(name, apiKey string) {
	pc, ok := m.deps.Cfg.Providers[name]
	if !ok {
		m.add("err", "provider not found: "+name)
		return
	}
	pc.APIKey = apiKey
	m.deps.Cfg.Providers[name] = pc
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	if m.deps.Cfg.ActiveProvider == name {
		if p, err := m.deps.Build(*m.deps.Cfg, name); err == nil {
			m.deps.Agent.SetProvider(p)
		}
	}
	m.add("sys", "API key saved for "+name+".")
}

func (m *chatModel) saveConversation(title string) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	id, err := m.deps.Convos.SaveConversation(context.Background(), m.convID, title, m.deps.Agent.History())
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.convID = id
	m.add("sys", fmt.Sprintf("saved conversation #%d — %s", id, title))
}

func (m *chatModel) autosave() {
	if m.deps.Convos == nil {
		return
	}
	if id, err := m.deps.Convos.SaveConversation(context.Background(), m.convID, m.convTitle(), m.deps.Agent.History()); err == nil {
		m.convID = id
	}
}

func (m *chatModel) showChats(ctx context.Context) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	convs, err := m.deps.Convos.ListConversations(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(convs) == 0 {
		m.add("sys", "no saved conversations yet.")
		return
	}
	var b strings.Builder
	for _, c := range convs {
		b.WriteString(fmt.Sprintf("#%d  %-48s %s\n",
			c.ID, trunc(c.Title, 48), c.UpdatedAt.Local().Format("2006-01-02 15:04")))
	}
	b.WriteString("\nuse: /load <id>")
	m.add("sys", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) loadConversation(ctx context.Context, idStr string) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		m.add("err", "id must be a number")
		return
	}
	msgs, err := m.deps.Convos.LoadConversation(ctx, id)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.deps.Agent.SetHistory(msgs)
	m.convID = id
	m.entries = nil
	for _, msg := range msgs {
		switch msg.Role {
		case llm.RoleUser:
			m.entries = append(m.entries, entry{"you", msg.Content})
		case llm.RoleAssistant:
			if msg.Content != "" {
				m.entries = append(m.entries, entry{"planner", msg.Content})
			}
		}
	}
	m.add("sys", fmt.Sprintf("loaded conversation #%d (%d messages)", id, len(msgs)))
}

func (m *chatModel) convTitle() string {
	for _, e := range m.entries {
		if e.role == "you" {
			return trunc(e.text, 48)
		}
	}
	return "conversation"
}

// --- suggestions ---

func computeSuggestions(val string, providers []string) []suggestion {
	if !strings.HasPrefix(val, "/") || strings.Contains(val, "\n") {
		return nil
	}
	fields := strings.Fields(val)
	endsSpace := strings.HasSuffix(val, " ")
	var out []suggestion

	switch {
	case len(fields) <= 1 && !endsSpace:
		for _, c := range baseCommands {
			if strings.HasPrefix(c.full, val) {
				out = append(out, c)
			}
		}
	case fields[0] == "/model":
		if !completingArg(fields, endsSpace) {
			break
		}
		prefix := argPrefix(fields)
		for _, name := range providers {
			if strings.HasPrefix(name, prefix) {
				out = append(out, suggestion{"/model " + name, "switch to " + name})
			}
		}
	case fields[0] == "/key":
		if !completingArg(fields, endsSpace) {
			break
		}
		prefix := argPrefix(fields)
		for _, name := range providers {
			if strings.HasPrefix(name, prefix) {
				out = append(out, suggestion{"/key " + name + " ", "set API key for " + name})
			}
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// completingArg reports whether the cursor is still on the first argument token
// (so we keep suggesting values for it); once the arg is filled we stop.
func completingArg(fields []string, endsSpace bool) bool {
	return (len(fields) == 1 && endsSpace) || (len(fields) == 2 && !endsSpace)
}

func argPrefix(fields []string) string {
	if len(fields) == 2 {
		return fields[1]
	}
	return ""
}

func completedValue(sel suggestion) string {
	val := sel.full
	if len(strings.Fields(val)) > 1 {
		return val // already carries its argument (e.g. "/model kimi", "/key kimi ")
	}
	if needsArg[val] && !strings.HasSuffix(val, " ") {
		val += " "
	}
	return val
}

func (m *chatModel) acceptSuggestion() {
	if m.selected >= len(m.suggestions) {
		return
	}
	val := completedValue(m.suggestions[m.selected])
	m.ta.SetValue(val)
	next := computeSuggestions(val, m.providerNames())
	// If the only remaining suggestion is exactly what we just completed, the
	// command is ready — close the menu so Enter submits instead of re-selecting.
	if len(next) == 1 && next[0].full == val {
		next = nil
	}
	m.suggestions = next
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
	m.setContent()
	m.vp.GotoBottom() // new content jumps to the bottom…
}

// setContent rebuilds the viewport body WITHOUT moving the scroll position, so
// scrolling up survives keystrokes and relayouts.
func (m *chatModel) setContent() {
	w := m.vp.Width
	if w < 10 {
		w = 10
	}
	body := lipgloss.NewStyle().Width(w)
	bubble := lipgloss.NewStyle().Background(userBubbleBG).Foreground(lipgloss.Color("252")).Padding(0, 1).Width(w)
	cmdBubble := lipgloss.NewStyle().Background(userBubbleBG).Foreground(lipgloss.Color("213")).Padding(0, 1).Width(w)
	blocks := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		switch e.role {
		case "you":
			blocks = append(blocks, youLabel.Render("› you")+"\n"+bubble.Render(e.text))
		case "planner":
			blocks = append(blocks, botLabel.Render("planner")+"\n"+body.Render(e.text))
		case "cmd":
			blocks = append(blocks, cmdBubble.Render(e.text))
		case "tool":
			blocks = append(blocks, toolStyle.Render(e.text))
		case "err":
			blocks = append(blocks, body.Inherit(errStyle).Render("error: "+e.text))
		case "raw":
			blocks = append(blocks, e.text) // pre-styled/wrapped, passthrough
		default:
			blocks = append(blocks, body.Inherit(sysStyle).Render(e.text))
		}
	}
	m.vp.SetContent(strings.Join(blocks, "\n\n"))
}

func (m *chatModel) layout() {
	if !m.ready {
		return
	}
	m.ta.SetWidth(m.width - 1)
	// Input grows with the number of lines (slim when empty, up to 6).
	inputH := strings.Count(m.ta.Value(), "\n") + 1
	// if inputH < 1 {
	// 	inputH = 1
	// }
	// if inputH > 6 {
	// 	inputH = 6
	// }

	inputH = 3
	m.ta.SetHeight(inputH)
	// leaves room for: header + divider + input + help + status + margin
	vpH := m.height - len(m.suggestions) - inputH - 5
	if vpH < 3 {
		vpH = 3
	}
	m.vp.Width = m.width
	m.vp.Height = vpH
	m.setContent() // keep scroll position on resize / keystroke
}

func (m *chatModel) View() string {
	if !m.ready {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render("planner"))
	b.WriteString("\n")
	b.WriteString(m.vp.View())
	b.WriteString("\n")
	// Separator between the conversation and the input.
	b.WriteString(dividerStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")
	if len(m.suggestions) > 0 {
		b.WriteString(m.renderSuggestions())
		b.WriteString("\n")
	}
	// Wrap the input in a full-width background so the panel is uniform.
	// b.WriteString(lipgloss.NewStyle().Width(m.width).Background(inputBG).Render(m.ta.View()))
	b.WriteString(lipgloss.NewStyle().Width(m.width).Render(m.ta.View()))
	b.WriteString("\n")
	b.WriteString(m.footer())
	b.WriteString("\n")
	b.WriteString(m.statusBar())
	return b.String()
}

func (m *chatModel) footer() string {
	if m.confirm != nil {
		return confirmStyle.Width(m.width).Render("⚠ " + m.confirm.prompt + "    y = confirm · n/esc = cancel")
	}
	if m.dailyEditing {
		return thinkStyle.Render("editing daily · enter = save draft · alt+enter = newline · esc = cancel")
	}
	if m.quitArmed {
		return thinkStyle.Render("press ctrl+c again to quit")
	}
	if m.thinking {
		return thinkStyle.Render("⏳ thinking…")
	}
	return helpStyle.Render("enter send · alt+enter newline · ↑/↓ history · pgup/pgdn/wheel scroll · / commands · ctrl+c quit")
}

func (m *chatModel) statusBar() string {
	model := "-"
	if pc, ok := m.deps.Cfg.Providers[m.deps.Cfg.ActiveProvider]; ok && pc.Model != "" {
		model = pc.Model
	}
	mem := "none"
	if m.deps.Memory != nil {
		mem = m.deps.Memory.Name()
	}
	chat := "new"
	if m.convID != 0 {
		chat = fmt.Sprintf("#%d", m.convID)
	}
	info := fmt.Sprintf(" %s · %s · ctx:%dmsg · mem:%s · chat:%s",
		m.deps.Agent.Provider(), model, m.deps.Agent.HistoryLen(), mem, chat)
	return statusStyle.Width(m.width).Render(info)
}

func (m *chatModel) renderSuggestions() string {
	lines := make([]string, 0, len(m.suggestions))
	for i, s := range m.suggestions {
		line := fmt.Sprintf(" %-20s %s", s.full, s.desc)
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
