package tui

// Model state, messages, and the Bubbletea Init/Update loop.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/config"
)

type entry struct {
	role string // you | planner | sys | cmd | err
	text string
}

// suggestion is a menu entry. A "+project"/"@person" full is a mention (it
// completes only the word under the cursor); anything else is a slash command
// (it replaces the whole input). See isMention.
type suggestion struct{ full, desc string }

// isMention reports whether a suggestion completes a +project / @person token.
func isMention(s suggestion) bool {
	return strings.HasPrefix(s.full, "+") || strings.HasPrefix(s.full, "@")
}

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
	thinkStart     time.Time       // when the current call began (for the elapsed timer)
	busyLabel      string          // what is running, shown next to the spinner
	spinner        int             // animation frame
	quitArmed      bool            // first ctrl+c clears; second quits
	confirm        *pendingConfirm // non-nil while awaiting y/n
	statePick      *statePicker    // non-nil while picking a Plane state
	config         *configModel    // non-nil while the configuration modal is open
	dailyDraft     string          // last generated/edited daily digest
	dailyDraftDate string          // YYYY-MM-DD the current draft belongs to
	dailyEditing   bool            // true while editing the daily in the textarea
	history        []string        // submitted inputs, for ↑/↓ recall
	histPos        int             // -1 = not navigating
	histDraft      string          // what was being typed when history navigation began
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

	// mention autocomplete cache (+project / @person)
	mentionProjects []string
	mentionPeople   []string
	mentionLoaded   bool
}

type replyMsg struct {
	text string
	err  error
}

// dailyMsg carries the result of an async daily generation. fallback is the
// deterministic digest used when the model call fails or returns nothing, and
// prior is the daily already stored for that day — kept so a failed generation
// can never overwrite text the user edited by hand.
type dailyMsg struct {
	dateKey  string
	text     string
	fallback string
	prior    string
	err      error
}

// Command timeouts. Update() is the only thing draining the Bubbletea event
// queue, so blocking work must run off it AND be bounded: an unbounded call on
// a background goroutine leaks instead of freezing, which is better but still
// wrong. These are deliberately generous — the job is to guarantee an end, not
// to second-guess a slow network.
const (
	memoryOpTimeout   = 30 * time.Second
	telegramOpTimeout = 30 * time.Second
	planeOpTimeout    = 5 * time.Minute
	storeOpTimeout    = 10 * time.Second
)

// asyncMsg carries the entries produced by a command that ran off the event
// loop, to be appended when it lands.
type asyncMsg struct{ entries []entry }

// busy runs fn on its own goroutine under a bounded context and shows the
// spinner labelled label while it runs. fn must not touch model state: it runs
// concurrently with Update. Resolve anything you need from the model before
// calling busy and capture it in the closure.
func (m *chatModel) busy(label string, timeout time.Duration, fn func(context.Context) []entry) tea.Cmd {
	m.thinking = true
	m.thinkStart = time.Now()
	m.busyLabel = label
	run := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return asyncMsg{entries: fn(ctx)}
	}
	return tea.Batch(run, spinnerTick())
}

// errEntry and sysEntry keep the async closures terse.
func errEntry(err error) []entry { return []entry{{role: "err", text: err.Error()}} }
func sysEntry(s string) []entry  { return []entry{{role: "sys", text: s}} }

func (m *chatModel) Init() tea.Cmd { return textarea.Blink }

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The configuration modal takes the screen and the keyboard while it is
	// open. Its own closing message is handled below rather than here, so the
	// chat — not the config screen — decides what happens on the way back.
	if m.config != nil {
		if _, ok := msg.(configClosedMsg); !ok {
			_, cmd := m.config.Update(msg)
			if sz, ok := msg.(tea.WindowSizeMsg); ok {
				// Keep the chat's own geometry current, or it would repaint at
				// the old size the moment the modal closes.
				m.width, m.height = sz.Width, sz.Height
				m.layout()
			}
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		return m, nil

	case configClosedMsg:
		m.closeConfig()
		return m, nil

	case replyMsg:
		m.thinking = false
		if msg.err != nil {
			m.add("err", msg.err.Error())
		} else {
			m.renderToolEvents()
			m.mentionLoaded = false // the agent may have created a project/person
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
			why := "the model returned nothing"
			if msg.err != nil {
				why = msg.err.Error()
			}
			// A stored daily is the user's own edited text and the deterministic
			// fallback cannot reconstruct it, so a failed generation must leave it
			// alone. The fallback is only useful when there is nothing to lose.
			if prior := strings.TrimSpace(msg.prior); prior != "" {
				m.dailyDraft = prior
				m.dailyDraftDate = msg.dateKey
				m.add("err", "daily generation failed ("+why+") — the stored daily is untouched")
				m.add("daily", prior)
				m.add("sys", "daily ("+msg.dateKey+") unchanged — /daily edit to tweak, /daily send to deliver.")
				m.layout()
				return m, nil
			}
			text = msg.fallback
			m.add("sys", "daily generation failed ("+why+"), using basic format")
		}
		m.dailyDraft = text
		m.dailyDraftDate = msg.dateKey
		m.add("daily", text)
		if err := m.persistDaily(msg.dateKey, text); err != nil {
			m.add("err", "daily generated but NOT saved: "+err.Error())
		} else {
			m.add("sys", "daily ("+msg.dateKey+") ready — /daily edit to tweak, /daily send to deliver.")
		}
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

	case asyncMsg:
		m.thinking = false
		m.busyLabel = ""
		for _, e := range msg.entries {
			m.add(e.role, e.text)
		}
		m.layout()
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
