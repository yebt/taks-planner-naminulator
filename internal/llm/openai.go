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

// OpenAIBaseURL is the default OpenAI endpoint.
const OpenAIBaseURL = "https://api.openai.com/v1"

// OpenAICompatible talks to any OpenAI /chat/completions API. OpenAI, Moonshot,
// Kimi and Ollama all speak this dialect — only the base URL/model/key differ.
type OpenAICompatible struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewOpenAICompatible builds an adapter for an arbitrary OpenAI-compatible host
// (use this for Ollama or any self-hosted gateway).
func NewOpenAICompatible(name, baseURL, apiKey, model string) *OpenAICompatible {
	return &OpenAICompatible{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// NewOpenAI builds the plain OpenAI adapter.
func NewOpenAI(apiKey, model string) *OpenAICompatible {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return NewOpenAICompatible("openai", OpenAIBaseURL, apiKey, model)
}

func (o *OpenAICompatible) Name() string { return o.name }

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiFunctionCall `json:"function"`
}

type oaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
	Tools    []oaiTool    `json:"tools,omitempty"`
}

type oaiResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat implements Provider.
func (o *OpenAICompatible) Chat(ctx context.Context, msgs []Message, tools []Tool) (Response, error) {
	req := oaiRequest{Model: o.model}
	for _, m := range msgs {
		om := oaiMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == RoleTool {
			om.ToolCallID = m.ToolCallID
			om.Name = m.Name
		}
		for _, tc := range m.ToolCalls {
			args := tc.Arguments
			if args == "" {
				args = "{}"
			}
			om.ToolCalls = append(om.ToolCalls, oaiToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: oaiFunctionCall{Name: tc.Name, Arguments: args},
			})
		}
		req.Messages = append(req.Messages, om)
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, oaiTool{
			Type:     "function",
			Function: oaiFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("%s: status %d: %s", o.name, resp.StatusCode, string(data))
	}

	var out oaiResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Response{}, fmt.Errorf("%s: decode: %w", o.name, err)
	}
	if out.Error != nil {
		return Response{}, fmt.Errorf("%s: %s", o.name, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return Response{}, fmt.Errorf("%s: empty choices", o.name)
	}
	msg := out.Choices[0].Message
	r := Response{Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return r, nil
}
