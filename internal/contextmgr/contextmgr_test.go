package contextmgr

import (
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/llm"
)

func TestFitKeepsSystemAndRecent(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: strings.Repeat("a", 100)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("b", 100)},
		{Role: llm.RoleUser, Content: strings.Repeat("c", 100)},
	}
	m := New(200) // only room for ~1-2 recent messages plus system
	out := m.Fit(msgs)

	if out[0].Role != llm.RoleSystem {
		t.Fatalf("system not preserved: %+v", out[0])
	}
	if len(out) >= len(msgs) {
		t.Fatalf("expected trimming, kept %d of %d", len(out), len(msgs))
	}
	last := out[len(out)-1]
	if last.Content != strings.Repeat("c", 100) {
		t.Fatalf("most recent message not kept: %q", last.Content)
	}
}

func TestFitDropsOrphanToolResult(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "t"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: strings.Repeat("x", 500)},
		{Role: llm.RoleUser, Content: "ok"},
	}
	// Tight budget so the window would start on the tool result; it must be dropped.
	out := New(60).Fit(msgs)
	for _, msg := range out {
		if msg.Role == llm.RoleTool {
			t.Fatalf("orphan tool result was kept: %+v", out)
		}
	}
	if out[0].Role != llm.RoleSystem {
		t.Fatal("system must remain first")
	}
}

func TestFitEmpty(t *testing.T) {
	if got := New(100).Fit(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
