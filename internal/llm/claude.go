package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// ClaudeBaseURL is the Anthropic Messages API base.
	ClaudeBaseURL    = "https://api.anthropic.com/v1"
	claudeAPIVersion = "2023-06-01"
	claudeMaxTokens  = 4096
)

// Claude adapts the Anthropic Messages API to the Provider port. Anthropic's
// tool_use / tool_result shape is different from OpenAI, so it lives here.
type Claude struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewClaude builds the Claude adapter. Defaults to Opus 4.8.
func NewClaude(apiKey, model string) *Claude {
	if model == "" {
		model = "claude-opus-4-8"
	}
	return &Claude{
		baseURL: ClaudeBaseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// WithBaseURL overrides the endpoint (useful for tests / gateways).
func (c *Claude) WithBaseURL(u string) *Claude { c.baseURL = u; return c }

func (c *Claude) Name() string { return "claude" }

type claudeContent struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content []claudeContent `json:"content"`
}

type claudeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
	Tools     []claudeTool    `json:"tools,omitempty"`
}

type claudeResponse struct {
	Content []claudeContent `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat implements Provider.
func (c *Claude) Chat(ctx context.Context, msgs []Message, tools []Tool) (Response, error) {
	req := claudeRequest{Model: c.model, MaxTokens: claudeMaxTokens}
	for _, m := range msgs {
		switch m.Role {
		case RoleSystem:
			if req.System != "" {
				req.System += "\n\n"
			}
			req.System += m.Content
		case RoleUser:
			req.Messages = append(req.Messages, claudeMessage{
				Role:    "user",
				Content: []claudeContent{{Type: "text", Text: m.Content}},
			})
		case RoleTool:
			req.Messages = append(req.Messages, claudeMessage{
				Role:    "user",
				Content: []claudeContent{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}},
			})
		case RoleAssistant:
			var blocks []claudeContent
			if m.Content != "" {
				blocks = append(blocks, claudeContent{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				if args == "" {
					args = "{}"
				}
				blocks = append(blocks, claudeContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: json.RawMessage(args),
				})
			}
			req.Messages = append(req.Messages, claudeMessage{Role: "assistant", Content: blocks})
		}
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, claudeTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("claude: status %d: %s", resp.StatusCode, string(data))
	}

	var out claudeResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Response{}, fmt.Errorf("claude: decode: %w", err)
	}
	if out.Error != nil {
		return Response{}, fmt.Errorf("claude: %s", out.Error.Message)
	}
	var r Response
	for _, block := range out.Content {
		switch block.Type {
		case "text":
			r.Content += block.Text
		case "tool_use":
			r.ToolCalls = append(r.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
			})
		}
	}
	return r, nil
}
