package tui

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/memory"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/tools"
)

type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }
func (stubProvider) Chat(context.Context, []llm.Message, []llm.Tool) (llm.Response, error) {
	return llm.Response{Content: "ok"}, nil
}

func newTestModel(t *testing.T) (*chatModel, store.TaskStore) {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg := tools.New(st)
	ag := agent.New(stubProvider{}, reg, "")
	cfg := config.Default()
	deps := ChatDeps{
		Cfg:        &cfg,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Agent:      ag,
		Store:      st,
		Convos:     st,
		Tools:      reg,
		Memory:     memory.Noop{},
		Build:      func(config.Config, string) (llm.Provider, error) { return stubProvider{}, nil },
	}
	m := newChatModel(deps)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // make it ready
	return m, st
}

func TestPresetNewCreatesTask(t *testing.T) {
	m, st := newTestModel(t)

	m.ta.SetValue("/new FEAT Login screen")
	m.submit()

	tasks, err := st.List(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after /new, got %d", len(tasks))
	}
	if tasks[0].Title != "Login screen" || tasks[0].Type != "FEAT" {
		t.Fatalf("bad task: %+v", tasks[0])
	}
}

func TestModelSwitchPersists(t *testing.T) {
	m, _ := newTestModel(t)

	m.ta.SetValue("/model claude")
	m.submit()

	if m.deps.Cfg.ActiveProvider != "claude" {
		t.Fatalf("active provider not switched: %q", m.deps.Cfg.ActiveProvider)
	}
	reloaded, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ActiveProvider != "claude" {
		t.Fatalf("switch not persisted to disk: %q", reloaded.ActiveProvider)
	}
}

func TestKeySavesToConfig(t *testing.T) {
	m, _ := newTestModel(t)

	m.ta.SetValue("/key claude sk-test-123")
	m.submit()

	reloaded, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Providers["claude"].APIKey != "sk-test-123" {
		t.Fatalf("key not saved: %+v", reloaded.Providers["claude"])
	}
}

func TestConversationSaveAndLoad(t *testing.T) {
	m, _ := newTestModel(t)
	m.deps.Agent.SetHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi there"},
	})
	m.add("you", "hello")
	m.add("planner", "hi there")

	m.ta.SetValue("/save my chat")
	m.submit()
	if m.convID == 0 {
		t.Fatal("convID not set after /save")
	}
	savedID := m.convID

	m.ta.SetValue("/newchat")
	m.submit()
	if m.convID != 0 || m.deps.Agent.HistoryLen() != 0 {
		t.Fatalf("newchat should reset conv+history, got id=%d len=%d", m.convID, m.deps.Agent.HistoryLen())
	}

	m.ta.SetValue("/load " + strconv.FormatInt(savedID, 10))
	m.submit()
	if m.convID != savedID {
		t.Fatalf("load did not set convID: %d", m.convID)
	}
	if m.deps.Agent.HistoryLen() != 2 {
		t.Fatalf("history not restored: %d", m.deps.Agent.HistoryLen())
	}
	found := false
	for _, e := range m.entries {
		if e.role == "you" && e.text == "hello" {
			found = true
		}
	}
	if !found {
		t.Fatal("entries not rebuilt from loaded conversation")
	}
}

func TestKeyFlowClosesMenu(t *testing.T) {
	m, _ := newTestModel(t)
	m.ta.SetValue("/key ")
	m.suggestions = computeSuggestions(m.ta.Value(), m.providerNames())
	if len(m.suggestions) == 0 {
		t.Fatal("expected provider suggestions after '/key '")
	}
	for i, s := range m.suggestions {
		if strings.Contains(s.full, "kimi") {
			m.selected = i
		}
	}
	m.acceptSuggestion()
	if m.ta.Value() != "/key kimi " {
		t.Fatalf("expected '/key kimi ', got %q", m.ta.Value())
	}
	if len(m.suggestions) != 0 {
		t.Fatalf("menu should close after choosing provider, got %v", m.suggestions)
	}
	// Typing the key must not re-open the menu.
	m.ta.SetValue("/key kimi sk-secret")
	if s := computeSuggestions(m.ta.Value(), m.providerNames()); len(s) != 0 {
		t.Fatalf("menu re-opened while typing key: %v", s)
	}
}

func TestTaskDetail(t *testing.T) {
	m, st := newTestModel(t)
	m.ta.SetValue("/new FEAT Login screen")
	m.submit()
	tasks, err := st.List(context.Background(), store.Filter{})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("setup failed: err=%v tasks=%d", err, len(tasks))
	}
	m.ta.SetValue("/task " + strconv.FormatInt(tasks[0].ID, 10))
	m.submit()
	last := m.entries[len(m.entries)-1]
	if last.role != "raw" {
		t.Fatalf("expected raw detail entry, got role %q", last.role)
	}
	if !strings.Contains(last.text, "Login screen") || !strings.Contains(last.text, "Estado") {
		t.Fatalf("detail missing expected content: %q", last.text)
	}
}

func TestClearResetsConversation(t *testing.T) {
	m, _ := newTestModel(t)
	m.add("you", "hello")
	m.ta.SetValue("/clear")
	m.submit()
	// after /clear the only entries are the cmd echo + the "cleared" system note
	for _, e := range m.entries {
		if e.role == "you" {
			t.Fatal("conversation not cleared")
		}
	}
}
