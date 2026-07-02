// Package llm defines the LLM provider port and the adapters that implement it.
// The core never depends on a concrete provider — it talks to Provider, so we
// can develop against a cheap/local model and swap Claude in later.
package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a request from the model to run one of our tools.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // raw JSON object
}

// Message is a provider-agnostic conversation entry. Each adapter translates
// these into its own wire format.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall // set on assistant messages that request tools
	ToolCallID string     // set on RoleTool: which call this result answers
	Name       string     // set on RoleTool: the tool name
}

// Tool is a provider-agnostic tool definition (JSON-schema parameters).
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Response is what a provider returns for one Chat turn.
type Response struct {
	Content   string
	ToolCalls []ToolCall
}

// Provider is the port every LLM adapter implements.
type Provider interface {
	Name() string
	Chat(ctx context.Context, msgs []Message, tools []Tool) (Response, error)
}
