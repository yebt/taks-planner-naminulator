package agent

import (
	"context"
	"testing"

	"github.com/webcloster-dev/planner/internal/llm"
)

// fakeProvider returns queued responses in order.
type fakeProvider struct {
	responses []llm.Response
	calls     int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(_ context.Context, _ []llm.Message, _ []llm.Tool) (llm.Response, error) {
	r := f.responses[f.calls]
	f.calls++
	return r, nil
}

type fakeDispatcher struct{ dispatched []string }

func (d *fakeDispatcher) Definitions() []llm.Tool { return nil }
func (d *fakeDispatcher) Dispatch(_ context.Context, name, _ string) (string, error) {
	d.dispatched = append(d.dispatched, name)
	return `{"ok":true}`, nil
}

func TestAgentRunsToolThenAnswers(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "1", Name: "create_task", Arguments: `{"title":"x"}`}}},
		{Content: "done"},
	}}
	disp := &fakeDispatcher{}
	a := New(prov, disp, "system")

	out, err := a.Send(context.Background(), "make a task")
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("expected final answer 'done', got %q", out)
	}
	if len(disp.dispatched) != 1 || disp.dispatched[0] != "create_task" {
		t.Fatalf("tool not dispatched: %v", disp.dispatched)
	}
	if prov.calls != 2 {
		t.Fatalf("expected 2 provider calls, got %d", prov.calls)
	}
}

func TestAgentPlainAnswer(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{{Content: "hi"}}}
	a := New(prov, &fakeDispatcher{}, "")
	out, err := a.Send(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi" {
		t.Fatalf("got %q", out)
	}
}

func TestAgentSetProvider(t *testing.T) {
	a := New(&fakeProvider{responses: []llm.Response{{Content: "a"}}}, &fakeDispatcher{}, "")
	if a.Provider() != "fake" {
		t.Fatalf("provider name: %q", a.Provider())
	}
}
