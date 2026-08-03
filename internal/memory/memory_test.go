package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// hungRunner never returns on its own: only ctx cancellation frees it. This is
// the engram-backend-is-wedged case.
func hungRunner(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// engramCalls are the two operations that shell out, so both must be bounded.
var engramCalls = []struct {
	name string
	call func(*Engram, context.Context) error
}{
	{"save", func(e *Engram, ctx context.Context) error { return e.Save(ctx, "Title", "Body") }},
	{"recall", func(e *Engram, ctx context.Context) error {
		_, err := e.Recall(ctx, "query", 1)
		return err
	}},
}

// A hung engram must surface as a timeout rather than blocking the caller: the
// TUI calls this with context.Background(), so the adapter is the only thing
// standing between a wedged CLI and a frozen prompt.
func TestEngramTimesOutOnHungCLI(t *testing.T) {
	for _, tt := range engramCalls {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engram{bin: "engram", run: hungRunner, timeout: 20 * time.Millisecond}

			start := time.Now()
			err := tt.call(e, context.Background())
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("a hung engram must fail, not succeed")
			}
			if !strings.Contains(err.Error(), "timed out") {
				t.Fatalf("the error should name the timeout, got: %v", err)
			}
			if elapsed > time.Second {
				t.Fatalf("call did not return promptly: %s", elapsed)
			}
		})
	}
}

// The caller's own cancellation still wins, and must not be mislabelled as our
// deadline — the two mean different things to whoever reads the error.
func TestEngramReportsCallerCancellationAsSuch(t *testing.T) {
	for _, tt := range engramCalls {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engram{bin: "engram", run: hungRunner, timeout: time.Minute}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := tt.call(e, ctx)
			if err == nil {
				t.Fatal("a cancelled context should fail")
			}
			if strings.Contains(err.Error(), "timed out") {
				t.Fatalf("caller cancellation must not be reported as a timeout: %v", err)
			}
		})
	}
}

// A zero timeout means the default, so an Engram built directly (as the tests
// above and any future caller do) is bounded rather than unbounded.
func TestEngramZeroTimeoutFallsBackToDefault(t *testing.T) {
	e := &Engram{bin: "engram", run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ok"), nil
	}}
	if got := e.timeoutOrDefault(); got != defaultTimeout {
		t.Fatalf("zero timeout should fall back to %s, got %s", defaultTimeout, got)
	}
	if err := e.Save(context.Background(), "Title", "Body"); err != nil {
		t.Fatalf("a healthy call should still succeed: %v", err)
	}
}

func TestEngramSaveArgs(t *testing.T) {
	var gotArgs []string
	e := &Engram{bin: "engram", project: "proj", run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("Saved memory"), nil
	}}
	if err := e.Save(context.Background(), "Title", "Body"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"save", "Title", "Body", "--type note", "--project proj"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
}

func TestEngramRecallArgsAndOutput(t *testing.T) {
	var gotArgs []string
	e := &Engram{bin: "engram", run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("Found 1 memories:\n[1] result\n"), nil
	}}
	out, err := e.Recall(context.Background(), "login", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Found 1 memories") {
		t.Fatalf("unexpected output: %q", out)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "search login") || !strings.Contains(joined, "--limit 3") {
		t.Fatalf("bad recall args: %q", joined)
	}
	if strings.Contains(joined, "--project") {
		t.Fatalf("no project set, should not pass --project: %q", joined)
	}
}

func TestNoop(t *testing.T) {
	var n Noop
	if n.Available() {
		t.Fatal("noop should be unavailable")
	}
	if err := n.Save(context.Background(), "a", "b"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if _, err := n.Recall(context.Background(), "q", 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
