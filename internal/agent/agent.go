// Package agent runs the tool-use loop: send the conversation to the provider,
// execute any requested tools against the registry, feed results back, repeat
// until the model returns a plain answer.
package agent

import (
	"context"
	"fmt"

	"github.com/webcloster-dev/planner/internal/contextmgr"
	"github.com/webcloster-dev/planner/internal/llm"
)

// ToolDispatcher is the subset of the tools registry the agent needs.
type ToolDispatcher interface {
	Definitions() []llm.Tool
	Dispatch(ctx context.Context, name, args string) (string, error)
}

// Agent holds a conversation plus the active provider and tool set.
type Agent struct {
	provider llm.Provider
	tools    ToolDispatcher
	messages []llm.Message
	maxSteps int
	window   *contextmgr.Manager
}

// New builds an agent with an optional system prompt.
func New(p llm.Provider, tools ToolDispatcher, systemPrompt string) *Agent {
	a := &Agent{provider: p, tools: tools, maxSteps: 8}
	if systemPrompt != "" {
		a.messages = append(a.messages, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	return a
}

// SetProvider swaps the active provider (used by the /model command). The
// conversation history is preserved.
func (a *Agent) SetProvider(p llm.Provider) { a.provider = p }

// Reset clears the conversation, keeping the system prompt (used by /clear).
func (a *Agent) Reset() {
	if len(a.messages) > 0 && a.messages[0].Role == llm.RoleSystem {
		a.messages = a.messages[:1:1]
		return
	}
	a.messages = nil
}

// SetWindow installs a context manager used to trim history before each call.
func (a *Agent) SetWindow(w *contextmgr.Manager) { a.window = w }

// History returns the full conversation (for persistence).
func (a *Agent) History() []llm.Message { return a.messages }

// HistoryLen reports how many messages are in the conversation.
func (a *Agent) HistoryLen() int { return len(a.messages) }

// SetHistory replaces the conversation (used when loading a saved chat).
func (a *Agent) SetHistory(msgs []llm.Message) { a.messages = msgs }

// Provider returns the active provider name.
func (a *Agent) Provider() string { return a.provider.Name() }

// Send runs one user turn to completion, executing tools as needed, and returns
// the model's final text.
func (a *Agent) Send(ctx context.Context, input string) (string, error) {
	a.messages = append(a.messages, llm.Message{Role: llm.RoleUser, Content: input})
	defs := a.tools.Definitions()

	for step := 0; step < a.maxSteps; step++ {
		send := a.messages
		if a.window != nil {
			send = a.window.Fit(a.messages)
		}
		resp, err := a.provider.Chat(ctx, send, defs)
		if err != nil {
			return "", err
		}
		if len(resp.ToolCalls) == 0 {
			a.messages = append(a.messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
			return resp.Content, nil
		}
		a.messages = append(a.messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		for _, tc := range resp.ToolCalls {
			result, err := a.tools.Dispatch(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf(`{"error":%q}`, err.Error())
			}
			a.messages = append(a.messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    result,
			})
		}
	}
	return "", fmt.Errorf("agent: exceeded max steps (%d)", a.maxSteps)
}
