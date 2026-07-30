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

func TestFitKeepsOversizedToolResultWithItsCall(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "do the thing"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "t"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: strings.Repeat("x", 5000)},
	}
	// The tool result alone blows the budget, so the "keep at least one recent
	// message" rule leaves a lone orphan. Trimming it must not strip the window
	// down to the system message.
	out := New(60).Fit(msgs)

	if len(out) < 2 {
		t.Fatalf("window collapsed to system only: %+v", out)
	}
	if out[0].Role != llm.RoleSystem {
		t.Fatalf("system must remain first: %+v", out)
	}
	if out[1].Role == llm.RoleTool {
		t.Fatalf("window starts on an orphan tool result: %+v", out)
	}
	last := out[len(out)-1]
	if last.Role != llm.RoleTool || last.ToolCallID != "1" {
		t.Fatalf("tool result was lost: %+v", out)
	}
}

func TestFitAlwaysKeepsAValidNonSystemWindow(t *testing.T) {
	bulk := strings.Repeat("x", 5000)
	cases := []struct {
		name string
		msgs []llm.Message
	}{
		{
			name: "user only",
			msgs: []llm.Message{
				{Role: llm.RoleSystem, Content: "sys"},
				{Role: llm.RoleUser, Content: bulk},
			},
		},
		{
			name: "no system message",
			msgs: []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "t", Arguments: bulk}}},
				{Role: llm.RoleTool, ToolCallID: "1", Content: bulk},
			},
		},
		{
			name: "oversized tool result last",
			msgs: []llm.Message{
				{Role: llm.RoleSystem, Content: "sys"},
				{Role: llm.RoleUser, Content: "do the thing"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "t"}}},
				{Role: llm.RoleTool, ToolCallID: "1", Content: bulk},
			},
		},
		{
			name: "parallel tool results",
			msgs: []llm.Message{
				{Role: llm.RoleSystem, Content: "sys"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "t"}, {ID: "2", Name: "u"}}},
				{Role: llm.RoleTool, ToolCallID: "1", Content: bulk},
				{Role: llm.RoleTool, ToolCallID: "2", Content: bulk},
			},
		},
		{
			name: "tool result followed by user turn",
			msgs: []llm.Message{
				{Role: llm.RoleSystem, Content: "sys"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "t"}}},
				{Role: llm.RoleTool, ToolCallID: "1", Content: bulk},
				{Role: llm.RoleUser, Content: bulk},
			},
		},
	}

	for _, tc := range cases {
		for _, budget := range []int{1, 17, 60, 500} {
			out := New(budget).Fit(tc.msgs)

			rest := out
			if len(rest) > 0 && rest[0].Role == llm.RoleSystem {
				rest = rest[1:]
			}
			if len(rest) == 0 {
				t.Fatalf("%s (budget %d): no non-system message kept: %+v", tc.name, budget, out)
			}
			if rest[0].Role == llm.RoleTool {
				t.Fatalf("%s (budget %d): window starts on an orphan tool result: %+v", tc.name, budget, out)
			}
		}
	}
}

func TestFitEmpty(t *testing.T) {
	if got := New(100).Fit(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
