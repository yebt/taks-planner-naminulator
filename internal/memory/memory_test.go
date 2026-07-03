package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
