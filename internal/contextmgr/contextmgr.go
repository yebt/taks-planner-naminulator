// Package contextmgr keeps an LLM conversation within a size budget before it
// is sent to the provider. It always preserves the leading system message and
// never lets the kept window start on an orphan tool result (which the API
// would reject, since a tool_result must follow its tool_use).
package contextmgr

import "github.com/webcloster-dev/planner/internal/llm"

// Manager trims a message slice to a character budget (~4 chars per token).
type Manager struct {
	Budget int
}

// New builds a manager. A non-positive budget falls back to a sane default.
func New(budget int) *Manager {
	if budget <= 0 {
		budget = 24000
	}
	return &Manager{Budget: budget}
}

// Fit returns the largest suffix of msgs that fits the budget, with the system
// message (if any) always kept at the front. The full history is never mutated.
func (m *Manager) Fit(msgs []llm.Message) []llm.Message {
	if len(msgs) == 0 {
		return msgs
	}
	var system []llm.Message
	rest := msgs
	if msgs[0].Role == llm.RoleSystem {
		system = msgs[:1]
		rest = msgs[1:]
	}

	total := 0
	for _, s := range system {
		total += size(s)
	}
	start := len(rest)
	for i := len(rest) - 1; i >= 0; i-- {
		s := size(rest[i])
		// Always keep at least one recent message even if it alone exceeds budget.
		if total+s > m.Budget && start < len(rest) {
			break
		}
		total += s
		start = i
	}
	kept := rest[start:]
	// Don't start the window on an orphan tool result.
	for len(kept) > 0 && kept[0].Role == llm.RoleTool {
		kept = kept[1:]
	}

	out := make([]llm.Message, 0, len(system)+len(kept))
	out = append(out, system...)
	out = append(out, kept...)
	return out
}

func size(m llm.Message) int {
	n := len(m.Content) + len(m.ToolCallID) + len(m.Name)
	for _, tc := range m.ToolCalls {
		n += len(tc.Name) + len(tc.Arguments)
	}
	return n + 16 // per-message overhead
}
