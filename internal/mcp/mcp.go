// Package mcp connects the planner to stdio MCP servers (official Go SDK) and
// exposes their tools in the provider-agnostic llm.Tool shape. Tool names are
// namespaced "<server>__<tool>" so they never collide with built-in tools.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/llm"
)

// Separator joins the server name and the tool name in an exposed tool name.
const Separator = "__"

// maxToolName is the provider limit for tool names (^[a-zA-Z0-9_-]{1,64}$).
const maxToolName = 64

// Client is one live session with an MCP server.
type Client struct {
	name    string
	allow   map[string]bool // empty = every tool is exposed
	session *sdk.ClientSession
	// original maps the exposed (sanitized) tool name back to the server's
	// own name, so Call works with whatever Tools handed to the model.
	mu       sync.Mutex
	original map[string]string
}

// Connect launches the server's command and opens an MCP session over its
// stdin/stdout. srv.Env is added on top of the planner's environment; its
// values never appear in errors.
func Connect(ctx context.Context, name string, srv config.MCPServer) (*Client, error) {
	if srv.Command == "" {
		return nil, fmt.Errorf("mcp server %q: no command configured", name)
	}
	cmd := exec.Command(srv.Command, srv.Args...)
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return connect(ctx, name, srv.Tools, &sdk.CommandTransport{Command: cmd})
}

// connect opens a session over any transport (tests use the in-memory one).
func connect(ctx context.Context, name string, allow []string, t sdk.Transport) (*Client, error) {
	client := sdk.NewClient(&sdk.Implementation{Name: "planner", Version: "v1"}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: connect: %w", name, err)
	}
	c := &Client{name: name, session: session, original: map[string]string{}}
	if len(allow) > 0 {
		c.allow = make(map[string]bool, len(allow))
		for _, a := range allow {
			c.allow[a] = true
		}
	}
	return c, nil
}

// Name is the configured server name (the tool-name prefix).
func (c *Client) Name() string { return c.name }

// Tools lists the server's tools (every page), keeps the allowlisted ones and
// returns them namespaced with an object input schema.
func (c *Client) Tools(ctx context.Context) ([]llm.Tool, error) {
	var out []llm.Tool
	for t, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: list tools: %w", c.name, err)
		}
		if c.allow != nil && !c.allow[t.Name] {
			continue
		}
		full := ToolName(c.name, t.Name)
		_, short, _ := SplitToolName(full)
		c.mu.Lock()
		c.original[short] = t.Name
		c.mu.Unlock()
		out = append(out, llm.Tool{
			Name:        full,
			Description: t.Description,
			Parameters:  objectSchema(t.InputSchema),
		})
	}
	return out, nil
}

// Call runs a tool by its un-prefixed name with raw JSON arguments (empty =
// no arguments) and returns the concatenated text content. A result flagged
// IsError comes back as an error carrying the server's text.
func (c *Client) Call(ctx context.Context, tool, argsJSON string) (string, error) {
	args := map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("mcp tool %s: invalid arguments: %w", ToolName(c.name, tool), err)
		}
	}
	name, err := c.resolve(ctx, tool)
	if err != nil {
		return "", err
	}
	res, err := c.session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("mcp tool %s: %w", ToolName(c.name, tool), err)
	}
	var parts []string
	for _, block := range res.Content {
		if tc, ok := block.(*sdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if res.IsError {
		if text == "" {
			text = "tool reported an error"
		}
		return "", errors.New(text)
	}
	return text, nil
}

// resolve maps an exposed tool name to the server's own name, listing the
// tools once if Call runs before Tools. Tools outside the allowlist are
// refused, so the model can't reach them by guessing a name.
func (c *Client) resolve(ctx context.Context, tool string) (string, error) {
	if orig, ok := c.lookup(tool); ok {
		return orig, nil
	}
	if _, err := c.Tools(ctx); err != nil {
		return "", err
	}
	if orig, ok := c.lookup(tool); ok {
		return orig, nil
	}
	if c.allow != nil {
		return "", fmt.Errorf("mcp tool %s: not in the allowlist", ToolName(c.name, tool))
	}
	return tool, nil // unknown: let the server report it
}

func (c *Client) lookup(tool string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	orig, ok := c.original[tool]
	return orig, ok
}

// Close ends the session (and, for stdio servers, the child process).
func (c *Client) Close() error { return c.session.Close() }

// ToolName builds the exposed "<server>__<tool>" name, replacing characters
// providers reject with '_' and truncating to their 64-char limit.
func ToolName(server, tool string) string {
	name := sanitize(server) + Separator + sanitize(tool)
	if len(name) > maxToolName {
		name = name[:maxToolName]
	}
	return name
}

// SplitToolName splits an exposed name at the first separator. ok is false
// for names that are not namespaced (built-in tools).
func SplitToolName(name string) (server, tool string, ok bool) {
	server, tool, found := strings.Cut(name, Separator)
	if !found || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		}
		return '_'
	}, s)
}

// objectSchema decodes an input schema into a map and guarantees it is an
// object schema with properties, which every provider accepts.
func objectSchema(schema any) map[string]any {
	m, ok := schema.(map[string]any)
	if !ok && schema != nil {
		// Defensive: re-decode anything that is not already a map.
		if raw, err := json.Marshal(schema); err == nil {
			_ = json.Unmarshal(raw, &m)
		}
	}
	if m == nil {
		m = map[string]any{}
	}
	if _, ok := m["type"]; !ok {
		m["type"] = "object"
	}
	if _, ok := m["properties"]; !ok {
		m["properties"] = map[string]any{}
	}
	return m
}
