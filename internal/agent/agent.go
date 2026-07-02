// Package agent runs the tool-use loop: send the conversation to the provider,
// execute any requested tools against the registry, feed results back, repeat
// until the model returns a plain answer.
package agent

import (
	"context"
	"fmt"

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

// Provider returns the active provider name.
func (a *Agent) Provider() string { return a.provider.Name() }

// Send runs one user turn to completion, executing tools as needed, and returns
// the model's final text.
func (a *Agent) Send(ctx context.Context, input string) (string, error) {
	a.messages = append(a.messages, llm.Message{Role: llm.RoleUser, Content: input})
	defs := a.tools.Definitions()

	for step := 0; step < a.maxSteps; step++ {
		resp, err := a.provider.Chat(ctx, a.messages, defs)
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
