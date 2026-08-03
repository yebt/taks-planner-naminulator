package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/store"
)

// blockingMemory never returns on its own, like a wedged engram backend.
type blockingMemory struct{}

func (blockingMemory) Available() bool { return true }
func (blockingMemory) Name() string    { return "blocking" }

func (blockingMemory) Save(ctx context.Context, _, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingMemory) Recall(ctx context.Context, _ string, _ int) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func hasRoleContaining(entries []entry, role, want string) bool {
	for _, e := range entries {
		if e.role == role && strings.Contains(e.text, want) {
			return true
		}
	}
	return false
}

// Update() is the only thing draining the Bubbletea event queue. A slow
// dependency called inside it freezes the entire TUI — including ctrl+c, which
// is queued but never processed. Every blocking command must hand its work back
// as a tea.Cmd instead.
func TestBlockingCommandsDoNotBlockUpdate(t *testing.T) {
	commands := []struct {
		name  string
		input string
	}{
		{"recall", "/recall anything"},
		{"remember", "/remember something worth keeping"},
	}

	for _, tt := range commands {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t)
			m.deps.Memory = blockingMemory{}
			m.ta.SetValue(tt.input)

			type result struct{ cmd tea.Cmd }
			done := make(chan result, 1)
			go func() {
				_, cmd := m.submit()
				done <- result{cmd}
			}()

			var got result
			select {
			case got = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("submit blocked: the command is still running inside Update()")
			}

			if got.cmd == nil {
				t.Fatal("a blocking command must return a tea.Cmd so the work runs off the event loop")
			}
			if !m.thinking {
				t.Fatal("the spinner should be running while the command is in flight")
			}
			if m.busyLabel == "" {
				t.Fatal("the status bar should name what is running, not just say thinking")
			}
		})
	}
}

// The work handed off must be bounded, otherwise a wedged dependency leaks a
// goroutine forever instead of freezing the UI — better, but still wrong.
func TestBusyBoundsTheWork(t *testing.T) {
	m, _ := newTestModel(t)

	cmd := m.busy("testing", 20*time.Millisecond, func(ctx context.Context) []entry {
		<-ctx.Done() // never returns on its own
		return errEntry(ctx.Err())
	})

	start := time.Now()
	drive(m, cmd)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("busy did not bound the work: took %s", elapsed)
	}
	if !hasRole(m.entries, "err") {
		t.Fatalf("a timed-out command should report an error; entries=%+v", m.entries)
	}
	if m.thinking || m.busyLabel != "" {
		t.Fatalf("busy state should clear when the work lands: thinking=%v label=%q", m.thinking, m.busyLabel)
	}
}

// The entries produced off the loop are appended when the result lands.
func TestAsyncResultAppendsEntriesAndClearsBusy(t *testing.T) {
	m, _ := newTestModel(t)
	m.thinking = true
	m.busyLabel = "syncing"

	m.Update(asyncMsg{entries: []entry{
		{role: "err", text: "#3 feat-login: plane unreachable"},
		{role: "sys", text: "sync → Plane: 2 pushed, 1 failed"},
	}})

	if m.thinking || m.busyLabel != "" {
		t.Fatalf("busy state not cleared: thinking=%v label=%q", m.thinking, m.busyLabel)
	}
	if !hasRoleContaining(m.entries, "err", "plane unreachable") {
		t.Fatalf("per-item failures should survive; entries=%+v", m.entries)
	}
	if !hasRoleContaining(m.entries, "sys", "2 pushed, 1 failed") {
		t.Fatalf("summary missing; entries=%+v", m.entries)
	}
}

// syncAll and sendDaily refuse up front when their dependency is unconfigured,
// and must not spin up a command (or the spinner) to say so.
func TestUnconfiguredCommandsFailFast(t *testing.T) {
	m, _ := newTestModel(t)

	if cmd := m.syncAll(); cmd != nil {
		t.Fatal("sync with no Plane configured should not start a command")
	}
	if !hasRole(m.entries, "err") {
		t.Fatalf("sync should explain it is not configured; entries=%+v", m.entries)
	}
	if m.thinking {
		t.Fatal("a fail-fast command must not leave the spinner running")
	}
}

// failingDailies accepts reads but cannot write, e.g. a full disk or a locked
// database.
type failingDailies struct{ saved bool }

func (f *failingDailies) SaveDaily(context.Context, string, string) error {
	return errors.New("database is locked")
}
func (f *failingDailies) GetDaily(context.Context, string) (store.Daily, error) {
	return store.Daily{}, errors.New("no daily")
}
func (f *failingDailies) ListDailies(context.Context) ([]store.Daily, error) { return nil, nil }

// N1: persistDaily used to discard its error while the caller announced the
// daily was ready. A write that failed must never be reported as success.
func TestDailySaveFailureIsReported(t *testing.T) {
	m, _ := newTestModel(t)
	m.deps.Dailies = &failingDailies{}

	m.Update(dailyMsg{dateKey: "2026-07-30", text: "**Daily:**  2026-07-30 JUL"})

	if !hasRoleContaining(m.entries, "err", "NOT saved") {
		t.Fatalf("a failed save must be reported, not announced as ready; entries=%+v", m.entries)
	}
	if hasRoleContaining(m.entries, "sys", "ready") {
		t.Fatalf("must not claim the daily is ready when the write failed; entries=%+v", m.entries)
	}
}
