package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/llm"
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
		Tools:      reg,
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
