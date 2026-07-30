package agent

import (
	"context"
	"strings"
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

// loopingProvider keeps asking for the same tool forever, so the agent burns
// through maxSteps. Setting answer makes it return a plain reply instead, which
// lets a test continue the conversation after exhaustion.
type loopingProvider struct {
	calls  int
	answer string
}

func (p *loopingProvider) Name() string { return "looping" }

func (p *loopingProvider) Chat(_ context.Context, _ []llm.Message, _ []llm.Tool) (llm.Response, error) {
	p.calls++
	if p.answer != "" {
		return llm.Response{Content: p.answer}, nil
	}
	return llm.Response{ToolCalls: []llm.ToolCall{
		{ID: "call", Name: "list_tasks", Arguments: `{}`},
	}}, nil
}

func TestAgentMaxStepsExhaustionLeavesUsableHistory(t *testing.T) {
	prov := &loopingProvider{}
	a := New(prov, &fakeDispatcher{}, "system")

	out, err := a.Send(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("expected max steps error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded max steps") || !strings.Contains(err.Error(), "8") {
		t.Fatalf("error should mention the step limit, got %q", err)
	}
	if out != "" {
		t.Fatalf("expected empty output on exhaustion, got %q", out)
	}
	if prov.calls != a.maxSteps {
		t.Fatalf("expected exactly %d provider calls, got %d", a.maxSteps, prov.calls)
	}

	// The real fix: the conversation must not be left dangling on tool results.
	hist := a.History()
	if len(hist) == 0 {
		t.Fatal("history is empty")
	}
	last := hist[len(hist)-1]
	if last.Role != llm.RoleAssistant {
		t.Fatalf("history must end on an assistant turn, ended on %q (content %q)", last.Role, last.Content)
	}
	if len(last.ToolCalls) != 0 {
		t.Fatalf("closing assistant message must not request more tools: %v", last.ToolCalls)
	}
	if last.Content == "" {
		t.Fatal("closing assistant message must carry text explaining the cut-off")
	}

	// And the conversation stays usable: a follow-up turn still works.
	prov.answer = "recovered"
	got, err := a.Send(context.Background(), "what happened?")
	if err != nil {
		t.Fatalf("follow-up Send failed: %v", err)
	}
	if got != "recovered" {
		t.Fatalf("follow-up answer: %q", got)
	}
	hist = a.History()
	if hist[len(hist)-1].Role != llm.RoleAssistant || hist[len(hist)-1].Content != "recovered" {
		t.Fatalf("follow-up did not land as the final assistant turn: %+v", hist[len(hist)-1])
	}
	// The user message of the follow-up must sit right after an assistant turn.
	userIdx := len(hist) - 2
	if hist[userIdx].Role != llm.RoleUser {
		t.Fatalf("expected user message at %d, got %q", userIdx, hist[userIdx].Role)
	}
	if hist[userIdx-1].Role != llm.RoleAssistant {
		t.Fatalf("follow-up user message follows %q, want assistant", hist[userIdx-1].Role)
	}
}

func TestAgentSetProvider(t *testing.T) {
	a := New(&fakeProvider{responses: []llm.Response{{Content: "a"}}}, &fakeDispatcher{}, "")
	if a.Provider() != "fake" {
		t.Fatalf("provider name: %q", a.Provider())
	}
}
