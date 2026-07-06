// Package tui is the interactive chat harness: a Bubbletea UI with a bold
// header, a status bar (provider/model/context/memory/chat), colored
// conversation, multi-line input (alt+enter), slash-command autocomplete,
// input history (↑/↓), conversation save/load, and long-term memory recall.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
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
	Dailies    store.DailyStore
	Activity   store.ActivityStore
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
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	sugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	selStyle  = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62"))
	thinkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	confirmStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160")).Padding(0, 1)
	toastStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("28")).Padding(0, 1)
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
	deps           ChatDeps
	ta             textarea.Model
	vp             viewport.Model
	width          int
	height         int
	ready          bool
	entries        []entry
	suggestions    []suggestion
	selected       int
	thinking       bool
	thinkStart     time.Time       // when the current LLM call began (for the elapsed timer)
	spinner        int             // animation frame
	quitArmed      bool            // first ctrl+c clears; second quits
	confirm        *pendingConfirm // non-nil while awaiting y/n
	statePick      *statePicker    // non-nil while picking a Plane state
	dailyDraft     string          // last generated/edited daily digest
	dailyDraftDate string          // YYYY-MM-DD the current draft belongs to
	dailyEditing   bool            // true while editing the daily in the textarea
	history        []string        // submitted inputs, for ↑/↓ recall
	histPos        int             // -1 = not navigating
	convID         int64           // 0 = unsaved

	// mouse selection: character-granular, anchored in content coords (line,col)
	// so it survives scroll. Left-drag selects; right-click copies; esc cancels.
	selecting    bool
	dragged      bool // true once motion/scroll happens after press
	selActive    bool
	selSL, selSC int      // selection start (line, col)
	selEL, selEC int      // selection end (line, col)
	contentLines []string // plain (ANSI-stripped) conversation lines
	toast        string   // transient status (e.g. "copied N chars")
}

type replyMsg struct {
	text string
	err  error
}

// dailyMsg carries the result of an async daily generation; fallback is the
// deterministic digest used when the model call fails or returns nothing.
type dailyMsg struct {
	dateKey  string
	text     string
	fallback string
	err      error
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

	case dailyMsg:
		m.thinking = false
		text := strings.TrimSpace(msg.text)
		if msg.err != nil || text == "" {
			text = msg.fallback
			if msg.err != nil {
				m.add("sys", "LLM daily failed, using basic format: "+msg.err.Error())
			}
		}
		m.dailyDraft = text
		m.dailyDraftDate = msg.dateKey
		m.persistDaily(msg.dateKey, text)
		m.add("raw", m.dailyDraft)
		m.add("sys", "daily ("+msg.dateKey+") ready — /daily edit to tweak, /daily send to deliver.")
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case copiedMsg:
		m.selActive = false
		if msg.err != nil {
			m.toast = "clipboard error: " + msg.err.Error()
		} else {
			m.toast = fmt.Sprintf("copied %d chars ✓", msg.n)
		}
		m.setContent()
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearToastMsg{} })

	case clearToastMsg:
		m.toast = ""
		return m, nil

	case tickMsg:
		if !m.thinking {
			return m, nil // stop ticking when the call finishes
		}
		m.spinner++
		return m, spinnerTick()
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
			m.persistDaily(m.dailyDraftDate, m.dailyDraft)
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

// --- mouse selection & clipboard ---

type copiedMsg struct {
	n   int
	err error
}
type clearToastMsg struct{}
type tickMsg struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerTick drives the thinking animation/elapsed timer.
func spinnerTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func copyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(text)
		return copiedMsg{n: len([]rune(text)), err: err}
	}
}

// handleMouse forwards wheel events to the viewport and turns left click-drag
// into a line selection that copies to the clipboard on release. The selection
// is anchored in content-line coordinates, so scrolling mid-drag preserves it.
func (m *chatModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown, tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		if m.selecting {
			m.dragged = true // scrolling mid-drag extends the selection
			m.selActive = true
			m.selEL, m.selEC = m.contentLineAt(msg.Y), msg.X
			m.setContent()
		}
		return m, cmd
	case tea.MouseButtonRight:
		// Right click copies the current selection (this is the trigger — not release).
		if msg.Action == tea.MouseActionPress && m.selActive {
			text := m.selectedText()
			m.selActive = false
			m.setContent()
			if strings.TrimSpace(text) == "" {
				return m, nil
			}
			return m, copyCmd(text)
		}
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			// Anchor but don't highlight yet — a plain click (no drag) does nothing.
			m.selecting = true
			m.dragged = false
			m.selActive = false
			m.selSL, m.selSC = m.contentLineAt(msg.Y), msg.X
			m.selEL, m.selEC = m.selSL, m.selSC
		}
	case tea.MouseActionMotion:
		if m.selecting {
			m.dragged = true
			m.selActive = true
			m.selEL, m.selEC = m.contentLineAt(msg.Y), msg.X
			m.setContent()
		}
	case tea.MouseActionRelease:
		if m.selecting {
			m.selecting = false
			if !m.dragged {
				m.selActive = false // plain click → nothing
				return m, nil
			}
			m.selEL, m.selEC = m.contentLineAt(msg.Y), msg.X
			m.setContent() // keep the highlight; copy waits for right-click
		}
	}
	return m, nil
}

// contentLineAt maps a screen row to an absolute content-line index. The
// viewport starts at screen row 1 (row 0 is the header).
func (m *chatModel) contentLineAt(y int) int {
	line := m.vp.YOffset + (y - 1)
	if line < 0 {
		line = 0
	}
	if n := m.vp.TotalLineCount(); n > 0 && line > n-1 {
		line = n - 1
	}
	return line
}

// orderedSel returns the selection endpoints normalized so start ≤ end, with
// line indices clamped to the available content.
func (m *chatModel) orderedSel() (sl, sc, el, ec int) {
	sl, sc, el, ec = m.selSL, m.selSC, m.selEL, m.selEC
	if el < sl || (el == sl && ec < sc) {
		sl, sc, el, ec = el, ec, sl, sc
	}
	n := len(m.contentLines)
	if sl < 0 {
		sl = 0
	}
	if el > n-1 {
		el = n - 1
	}
	return sl, sc, el, ec
}

func clampCol(c, n int) int {
	if c < 0 {
		return 0
	}
	if c > n {
		return n
	}
	return c
}

// selectedText extracts the character range spanning the selection.
func (m *chatModel) selectedText() string {
	if len(m.contentLines) == 0 {
		return ""
	}
	sl, sc, el, ec := m.orderedSel()
	if sl == el {
		r := []rune(m.contentLines[sl])
		a, b := clampCol(sc, len(r)), clampCol(ec+1, len(r))
		if a >= b {
			return ""
		}
		return string(r[a:b])
	}
	first := []rune(m.contentLines[sl])
	parts := []string{string(first[clampCol(sc, len(first)):])}
	for i := sl + 1; i < el; i++ {
		parts = append(parts, m.contentLines[i])
	}
	last := []rune(m.contentLines[el])
	parts = append(parts, string(last[:clampCol(ec+1, len(last))]))
	return strings.Join(parts, "\n")
}

// highlightLine renders a plain line with runes [a,b) shown reversed.
func highlightLine(line string, a, b int) string {
	r := []rune(line)
	a, b = clampCol(a, len(r)), clampCol(b, len(r))
	if a > b {
		a = b
	}
	return string(r[:a]) + lipgloss.NewStyle().Reverse(true).Render(string(r[a:b])) + string(r[b:])
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func ansiStrip(s string) string { return ansiRe.ReplaceAllString(s, "") }

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
	m.thinkStart = time.Now()
	m.layout()
	return m, tea.Batch(sendCmd(m.deps.Agent, val), spinnerTick())
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
	{"/todo", "/todo [all|<status>] [hoy|ayer] — list tasks grouped by status"},
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
	{"/daily", "/daily [date] [instr] · edit|send [date] — build/edit/send a digest"},
	{"/dailies", "list stored dailies"},
	{"/resume", "resume the most recent conversation"},
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

	case "/todo", "/todos":
		m.showTodo(ctx, fields)

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
		return m.handleDaily(ctx, fields)

	case "/dailies":
		m.listDailies(ctx)

	case "/resume":
		m.resumeLast(ctx)

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

// todoFlags are the first-argument options for /todo; todoDayFlags the second.
var todoFlags = []string{"all", "backlog", "unstarted", "started", "completed", "cancelled"}
var todoDayFlags = []string{"hoy", "ayer"}

// showTodo lists tasks. Bare: in-progress (any day) plus today's todo/done.
// "all" lists everything; a status flag lists that status; an optional day flag
// (hoy/ayer/YYYY-MM-DD) narrows to that calendar day.
func (m *chatModel) showTodo(ctx context.Context, fields []string) {
	var tasks []domain.Task
	var err error
	title := "todo"

	switch {
	case len(fields) == 1:
		title = "todo · en progreso + hoy"
		tasks, err = m.defaultTodo(ctx)
	case fields[1] == "all":
		f := store.Filter{}
		if d, ok := dayArg(fields[2:]); ok {
			f.Day = d
			title = "todo · all · " + fields[2]
		} else {
			title = "todo · all"
		}
		tasks, err = m.deps.Store.List(ctx, f)
	default:
		status := domain.Status(fields[1])
		if !status.Valid() {
			m.add("err", "unknown flag "+fields[1]+" — use: all or a status ("+strings.Join(todoFlags[1:], ", ")+")")
			return
		}
		f := store.Filter{Status: status}
		title = "todo · " + fields[1]
		if d, ok := dayArg(fields[2:]); ok {
			f.Day = d
			title += " · " + fields[2]
		}
		tasks, err = m.deps.Store.List(ctx, f)
	}
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.renderTodo(title, tasks)
}

// defaultTodo is the bare /todo set: every in-progress task plus today's todo
// and done tasks.
func (m *chatModel) defaultTodo(ctx context.Context) ([]domain.Task, error) {
	today := time.Now()
	var out []domain.Task
	for _, f := range []store.Filter{
		{Status: domain.StatusStarted},
		{Status: domain.StatusUnstarted, Day: today},
		{Status: domain.StatusCompleted, Day: today},
	} {
		ts, err := m.deps.Store.List(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, ts...)
	}
	return out, nil
}

// dayArg parses an optional day flag, reporting whether one was present.
func dayArg(rest []string) (time.Time, bool) {
	if len(rest) > 0 {
		if d, ok := parseDay(rest[0]); ok {
			return d, true
		}
	}
	return time.Time{}, false
}

// renderTodo prints tasks grouped by status in workflow order.
func (m *chatModel) renderTodo(title string, tasks []domain.Task) {
	if len(tasks) == 0 {
		m.add("sys", "no tasks.")
		return
	}
	typeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	order := []domain.Status{
		domain.StatusStarted, domain.StatusUnstarted, domain.StatusBacklog,
		domain.StatusCompleted, domain.StatusCancelled,
	}
	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("%s · %d", title, len(tasks))) + "\n")
	for _, st := range order {
		var lines []string
		for _, t := range tasks {
			if t.Status != st {
				continue
			}
			line := fmt.Sprintf("%s  %s  %s",
				idStyle.Render(fmt.Sprintf("%3d", t.ID)),
				typeStyle.Render(fmt.Sprintf("%-6s", t.Type)),
				trunc(t.Title, 44))
			if t.WorkItemSeq > 0 {
				line += helpStyle.Render(fmt.Sprintf("  #%d", t.WorkItemSeq))
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}
		glyph := statusStyleFor(st).Render(statusGlyph(st)) // per-row state marker, visible on scroll
		b.WriteString("\n" + statusStyleFor(st).Render(statusGlyph(st)+" "+m.statusLabel(st)) + "\n")
		for _, ln := range lines {
			b.WriteString(glyph + " " + ln + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("/todo all · /todo <status> [hoy|ayer] · /task <id>"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

// statusStyleFor maps a task status to a color so the list scans at a glance.
func statusStyleFor(s domain.Status) lipgloss.Style {
	var c lipgloss.Color
	switch s {
	case domain.StatusStarted:
		c = "214" // orange
	case domain.StatusUnstarted:
		c = "39" // blue
	case domain.StatusCompleted:
		c = "42" // green
	case domain.StatusCancelled:
		c = "203" // red
	case domain.StatusBacklog:
		c = "245" // gray
	default:
		c = "252"
	}
	return lipgloss.NewStyle().Foreground(c)
}

// statusGlyph is the minimalist per-status marker.
func statusGlyph(s domain.Status) string {
	switch s {
	case domain.StatusBacklog:
		return "?"
	case domain.StatusUnstarted:
		return "○"
	case domain.StatusStarted:
		return "▸"
	case domain.StatusCompleted:
		return "●"
	case domain.StatusCancelled:
		return "✗"
	default:
		return "•"
	}
}

// statusLabel renders a status with its configured default Plane state in
// parentheses, e.g. "started (In Progress)".
func (m *chatModel) statusLabel(s domain.Status) string {
	if m.deps.Cfg != nil {
		if id := m.deps.Cfg.Plane.StateDefaults[string(s)]; id != "" {
			for _, ps := range m.deps.Cfg.Plane.States {
				if ps.ID == id {
					return string(s) + " (" + ps.Name + ")"
				}
			}
		}
	}
	return string(s)
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

// dailyPrompt is the "skill" that shapes the daily: it turns the day's tasks
// into a professional Spanish narrative with the fixed Trabajo/Bloqueos/Notas
// layout and the +/#/>> markers.
const dailyPrompt = `Sos un asistente que redacta el "daily" de trabajo de un desarrollador a partir de sus tareas del día.
Escribí en español neutro-profesional, en prosa nominalizada (ej: "Identificación de anomalías en la ejecución de CRONs...", "Validación del proceso de migración...").
No copies los títulos tal cual: reformulálos como acciones concretas y claras. No inventes tareas que no estén en la lista.

Devolvé EXACTAMENTE este formato, sin texto adicional ni encabezados markdown:

Daily:  <FECHA>

Trabajo:
  + <una línea por cada tarea trabajada, hecha o en progreso>

Bloqueos:
  # <una línea por cada bloqueo>

Notas:
  >> <observaciones o recomendaciones técnicas relevantes>

Reglas:
- Usá <FECHA> tal como te la paso.
- Prefijos exactos: "  + ", "  # ", "  >> ".
- Si una sección no tiene contenido, omitila por completo (incluyendo su título).`

// handleDaily routes the /daily verbs: "edit"/"send" (optional date), otherwise
// the first token is an optional date and the rest an optional LLM instruction.
func (m *chatModel) handleDaily(ctx context.Context, fields []string) tea.Cmd {
	if len(fields) == 1 {
		return m.generateDailyCmd(ctx, time.Now(), "")
	}
	switch fields[1] {
	case "edit":
		m.editDaily(ctx, dailyDayArg(fields[2:]))
		return nil
	case "send":
		m.sendDaily(ctx, dailyDayArg(fields[2:]))
		return nil
	default:
		day, ok := parseDay(fields[1])
		if !ok {
			m.add("err", "usage: /daily [today|yesterday|YYYY-MM-DD] [instruction] · /daily edit|send [date]")
			return nil
		}
		return m.generateDailyCmd(ctx, day, strings.Join(fields[2:], " "))
	}
}

// parseDay resolves today/yesterday (es/en) or an explicit YYYY-MM-DD date.
func parseDay(tok string) (time.Time, bool) {
	switch strings.ToLower(tok) {
	case "today", "hoy":
		return time.Now(), true
	case "yesterday", "ayer":
		return time.Now().AddDate(0, 0, -1), true
	}
	if t, err := time.Parse("2006-01-02", tok); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// dailyDayArg reads an optional date argument, defaulting to today.
func dailyDayArg(rest []string) time.Time {
	if len(rest) > 0 {
		if d, ok := parseDay(rest[0]); ok {
			return d
		}
	}
	return time.Now()
}

// generateDailyCmd drafts the digest for a day asynchronously, feeding the model
// the day's tasks, any previously stored/edited draft, and an optional
// instruction so prior modifications are respected.
func (m *chatModel) generateDailyCmd(ctx context.Context, day time.Time, instruction string) tea.Cmd {
	// Prefer the activity log (a task surfaces on every day it was worked on);
	// fall back to the last-touched filter when it's unavailable.
	var tasks []domain.Task
	var err error
	if m.deps.Activity != nil {
		tasks, err = m.deps.Activity.TasksWithActivityOn(ctx, day)
	} else {
		tasks, err = m.deps.Store.List(ctx, store.Filter{Day: day})
	}
	if err != nil {
		m.add("err", err.Error())
		return nil
	}
	date := dailyDate(day)
	dateKey := day.Format("2006-01-02")
	prior := ""
	if m.deps.Dailies != nil {
		if d, err := m.deps.Dailies.GetDaily(ctx, dateKey); err == nil {
			prior = d.Content
		}
	}
	m.thinking = true
	m.thinkStart = time.Now()
	m.add("sys", "generating daily for "+date+"…")
	m.layout()
	userMsg := serializeTasksForDaily(date, tasks, prior, instruction)
	return tea.Batch(dailyCmd(m.deps.Agent, dateKey, userMsg, buildDaily(date, tasks)), spinnerTick())
}

func dailyCmd(a *agent.Agent, dateKey, userMsg, fallback string) tea.Cmd {
	return func() tea.Msg {
		out, err := a.Oneshot(context.Background(), dailyPrompt, userMsg)
		return dailyMsg{dateKey: dateKey, text: out, fallback: fallback, err: err}
	}
}

// serializeTasksForDaily renders the day's tasks, the prior draft, and any edit
// request as material for the model.
func serializeTasksForDaily(date string, tasks []domain.Task, prior, instruction string) string {
	var b strings.Builder
	b.WriteString("FECHA: " + date + "\n\nTareas del día:\n")
	if len(tasks) == 0 {
		b.WriteString("(ninguna)\n")
	}
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("- [%s] estado=%s: %s", t.Type, t.Status, t.Title))
		if o := strings.TrimSpace(t.Details.Objective); o != "" {
			b.WriteString(" | objetivo: " + o)
		}
		if n := strings.TrimSpace(t.Details.TechNotes); n != "" {
			b.WriteString(" | nota: " + n)
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(prior) != "" {
		b.WriteString("\nDaily previo (respetá las ediciones ya hechas salvo que se indique lo contrario):\n" + prior + "\n")
	}
	if strings.TrimSpace(instruction) != "" {
		b.WriteString("\nModificación solicitada: " + instruction + "\n")
	}
	return b.String()
}

// dailyDate formats a date as "2006-01-02 MON" with a Spanish month abbrev.
func dailyDate(t time.Time) string {
	months := []string{"ENE", "FEB", "MAR", "ABR", "MAY", "JUN", "JUL", "AGO", "SEP", "OCT", "NOV", "DIC"}
	return t.Format("2006-01-02") + " " + months[int(t.Month())-1]
}

// draftFor returns the current in-memory draft for dateKey, or the stored one.
func (m *chatModel) draftFor(ctx context.Context, dateKey string) string {
	if dateKey == m.dailyDraftDate && strings.TrimSpace(m.dailyDraft) != "" {
		return m.dailyDraft
	}
	if m.deps.Dailies != nil {
		if d, err := m.deps.Dailies.GetDaily(ctx, dateKey); err == nil {
			return d.Content
		}
	}
	return ""
}

func (m *chatModel) persistDaily(dateKey, content string) {
	if m.deps.Dailies != nil && strings.TrimSpace(content) != "" {
		_ = m.deps.Dailies.SaveDaily(context.Background(), dateKey, content)
	}
}

// editDaily loads a date's draft into the textarea for inline editing.
func (m *chatModel) editDaily(ctx context.Context, day time.Time) {
	dateKey := day.Format("2006-01-02")
	draft := m.draftFor(ctx, dateKey)
	if strings.TrimSpace(draft) == "" {
		m.add("err", "no daily for "+dateKey+" — run /daily "+dateKey+" first")
		return
	}
	m.dailyDraft = draft
	m.dailyDraftDate = dateKey
	m.ta.SetValue(draft)
	m.dailyEditing = true
	m.suggestions = nil
	m.layout()
}

// sendDaily delivers a date's draft to Telegram, degrading with a clear warning
// when the integration is not configured.
func (m *chatModel) sendDaily(ctx context.Context, day time.Time) {
	dateKey := day.Format("2006-01-02")
	content := m.draftFor(ctx, dateKey)
	if strings.TrimSpace(content) == "" {
		m.add("err", "no daily for "+dateKey+" — run /daily "+dateKey+" first")
		return
	}
	if m.deps.Telegram == nil || !m.deps.Telegram.Configured() {
		m.add("err", "can't send: Telegram not configured (set bot token + chat id in config)")
		return
	}
	if err := m.deps.Telegram.Send(ctx, content); err != nil {
		m.add("err", err.Error())
		return
	}
	m.add("sys", "daily "+dateKey+" sent to Telegram ✓")
}

// listDailies shows the stored digests.
func (m *chatModel) listDailies(ctx context.Context) {
	if m.deps.Dailies == nil {
		m.add("err", "daily store not available")
		return
	}
	ds, err := m.deps.Dailies.ListDailies(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(ds) == 0 {
		m.add("sys", "no dailies stored yet. run /daily")
		return
	}
	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("dailies · %d", len(ds))) + "\n\n")
	for _, d := range ds {
		b.WriteString(fmt.Sprintf("  %s   %s\n", d.Date, helpStyle.Render(d.UpdatedAt.Local().Format("15:04"))))
	}
	b.WriteString("\n" + helpStyle.Render("/daily <date> regenerate · /daily edit <date> · /daily send <date>"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

// resumeLast loads the most recently updated conversation.
func (m *chatModel) resumeLast(ctx context.Context) {
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
	m.loadConversation(ctx, strconv.FormatInt(convs[0].ID, 10))
}

// buildDaily is the deterministic fallback digest (used when the LLM call
// fails): work items (+), blocks (#) and notes (>>) under the fixed layout.
func buildDaily(date string, tasks []domain.Task) string {
	var b strings.Builder
	b.WriteString("Daily:  " + date + "\n")
	var work, notes []string
	for _, t := range tasks {
		if t.Status == domain.StatusCancelled {
			continue
		}
		code := ""
		if t.WorkItemSeq > 0 {
			code = fmt.Sprintf("#%d ", t.WorkItemSeq)
		}
		work = append(work, fmt.Sprintf("[%s] %s%s", t.Type, code, t.Title))
		if n := strings.TrimSpace(t.Details.TechNotes); n != "" {
			notes = append(notes, n)
		}
	}
	section := func(title, prefix string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString("\n" + title + ":\n")
		for _, it := range items {
			b.WriteString("  " + prefix + " " + it + "\n")
		}
	}
	// The deterministic fallback fills Trabajo and Notas; Bloqueos is left to the
	// LLM daily (there is no "blocked" status — that lives in context).
	section("Trabajo", "+", work)
	section("Notas", ">>", notes)
	if len(tasks) == 0 {
		b.WriteString("\n(sin actividad registrada hoy)")
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
	if m.deps.Activity != nil {
		if acts, err := m.deps.Activity.ActivityForTask(ctx, t.ID); err == nil && len(acts) > 0 {
			b.WriteString("\n\n" + head.Render("Historial") + "\n")
			for _, a := range acts {
				b.WriteString(fmt.Sprintf("- %s  %s\n", a.At.Local().Format("2006-01-02 15:04"), a.Note))
			}
		}
	}
	m.add("raw", strings.TrimRight(b.String(), "\n"))
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
	case fields[0] == "/todo":
		switch {
		case completingArg(fields, endsSpace): // first arg: all|<status>
			prefix := argPrefix(fields)
			for _, f := range todoFlags {
				if strings.HasPrefix(f, prefix) {
					out = append(out, suggestion{"/todo " + f, "list " + f})
				}
			}
		case (len(fields) == 2 && endsSpace) || (len(fields) == 3 && !endsSpace): // second arg: day
			prefix := ""
			if len(fields) == 3 {
				prefix = fields[2]
			}
			for _, d := range todoDayFlags {
				if strings.HasPrefix(d, prefix) {
					out = append(out, suggestion{"/todo " + fields[1] + " " + d, "day: " + d})
				}
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
		case "alert":
			blocks = append(blocks, body.Inherit(errStyle).Render("⚠ "+e.text))
		case "warn":
			blocks = append(blocks, body.Inherit(warnStyle).Render("⚠ "+e.text))
		case "raw":
			blocks = append(blocks, e.text) // pre-styled/wrapped, passthrough
		default:
			blocks = append(blocks, body.Inherit(sysStyle).Render(e.text))
		}
	}
	content := strings.Join(blocks, "\n\n")
	styled := strings.Split(content, "\n")
	m.contentLines = strings.Split(ansiStrip(content), "\n") // same line count

	if m.selActive && len(styled) > 0 {
		sl, sc, el, ec := m.orderedSel()
		for i := sl; i <= el && i < len(styled); i++ {
			r := []rune(m.contentLines[i])
			a, b := 0, len(r)
			if i == sl {
				a = clampCol(sc, len(r))
			}
			if i == el {
				b = clampCol(ec+1, len(r))
			}
			styled[i] = highlightLine(m.contentLines[i], a, b)
		}
		m.vp.SetContent(strings.Join(styled, "\n"))
		return
	}
	m.vp.SetContent(content)
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
	if m.toast != "" {
		return toastStyle.Width(m.width).Render(m.toast)
	}
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
		frame := spinnerFrames[m.spinner%len(spinnerFrames)]
		return thinkStyle.Render(fmt.Sprintf("%s thinking… %ds", frame, int(time.Since(m.thinkStart).Seconds())))
	}
	return helpStyle.Render("enter send · alt+enter newline · pgup/pgdn/wheel scroll · drag select · right-click copy · esc cancel · ctrl+l clear · ctrl+c quit")
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
