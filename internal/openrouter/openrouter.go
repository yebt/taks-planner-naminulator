// Package openrouter is a read-only adapter to the OpenRouter model catalog.
//
// The conversation itself never comes through here: OpenRouter speaks the
// OpenAI dialect, so internal/llm already talks to it. This package exists for
// the one thing that dialect does not give us — knowing which models the key
// can actually reach, what they cost, and whether they support tool calling.
// Without that, choosing a model means typing an id from memory and finding out
// it was wrong at the first message.
package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// BaseURL is OpenRouter's API root, shared with the chat adapter.
const BaseURL = "https://openrouter.ai/api/v1"

// Config holds the catalog connection details.
type Config struct {
	BaseURL string // defaults to BaseURL when empty
	Token   string // optional: the catalog is public, a key only scopes it
}

// Model is one entry of the catalog, reduced to what picking a model needs.
type Model struct {
	ID            string // what goes in the request body, e.g. "openai/gpt-4o-mini"
	Name          string // human label, e.g. "OpenAI: GPT-4o-mini"
	ContextLength int
	// Tools reports tool-calling support. The agent is built on tools, so a
	// model without them is not a slower choice here — it is a broken one.
	Tools         bool
	PromptUSD     float64 // per million tokens
	CompletionUSD float64 // per million tokens
}

// Free reports whether the model costs nothing to run.
func (m Model) Free() bool { return m.PromptUSD == 0 && m.CompletionUSD == 0 }

// Client reads the OpenRouter catalog.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a catalog client, falling back to the public endpoint.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = BaseURL
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// wire mirrors the subset of /models we decode. The endpoint returns a great
// deal more; everything not listed here is deliberately dropped.
type wire struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ContextLength int    `json:"context_length"`
		Pricing       struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		SupportedParameters []string `json:"supported_parameters"`
	} `json:"data"`
}

// ListModels fetches the catalog, sorted by id so the list is stable between
// calls — the endpoint returns newest-first, which would reshuffle the picker
// under the user every time OpenRouter adds a model.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter: status %d: %s", resp.StatusCode, string(data))
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("openrouter: decode: %w", err)
	}
	out := make([]Model, 0, len(w.Data))
	for _, d := range w.Data {
		out = append(out, Model{
			ID:            d.ID,
			Name:          d.Name,
			ContextLength: d.ContextLength,
			Tools:         supports(d.SupportedParameters, "tools"),
			PromptUSD:     perMillion(d.Pricing.Prompt),
			CompletionUSD: perMillion(d.Pricing.Completion),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func supports(params []string, want string) bool {
	for _, p := range params {
		if p == want {
			return true
		}
	}
	return false
}

// perMillion converts OpenRouter's per-token price string into dollars per
// million tokens. An unparseable or absent price reads as free rather than
// failing the whole catalog over one malformed entry.
func perMillion(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v * 1e6
}
