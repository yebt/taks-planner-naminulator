package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestClient starts an in-process SDK server with a few fake tools and
// connects a Client to it over the in-memory transport.
func newTestClient(t *testing.T, allow []string) *Client {
	t.Helper()
	ctx := context.Background()
	srv := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "v0"}, &sdk.ServerOptions{PageSize: 1})
	srv.AddTool(&sdk.Tool{
		Name:        "echo",
		Description: "Echo the text twice.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var args struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &sdk.CallToolResult{Content: []sdk.Content{
			&sdk.TextContent{Text: args.Text},
			&sdk.TextContent{Text: args.Text},
		}}, nil
	})
	srv.AddTool(&sdk.Tool{
		Name:        "fail",
		Description: "Always fails.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "boom"}}}, nil
	})
	srv.AddTool(&sdk.Tool{
		Name:        "get.item",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "item"}}}, nil
	})

	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	c, err := connect(ctx, "fake", allow, clientT)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestToolsArePrefixedAndPaginated(t *testing.T) {
	tools, err := newTestClient(t, nil).Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, tl := range tools {
		byName[tl.Name] = true
	}
	// PageSize 1 forces three pages: all must be collected.
	for _, want := range []string{"fake__echo", "fake__fail", "fake__get_item"} {
		if !byName[want] {
			t.Fatalf("missing tool %q in %v", want, byName)
		}
	}
}

func TestToolsApplyAllowlist(t *testing.T) {
	tools, err := newTestClient(t, []string{"echo"}).Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "fake__echo" {
		t.Fatalf("allowlist not applied: %+v", tools)
	}
	if tools[0].Description != "Echo the text twice." {
		t.Fatalf("description lost: %q", tools[0].Description)
	}
	props, _ := tools[0].Parameters["properties"].(map[string]any)
	if _, ok := props["text"]; !ok {
		t.Fatalf("input schema not decoded: %+v", tools[0].Parameters)
	}
}

func TestToolsGuaranteeObjectSchema(t *testing.T) {
	tools, err := newTestClient(t, []string{"fail"}).Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
	p := tools[0].Parameters
	if p["type"] != "object" {
		t.Fatalf("schema type = %v", p["type"])
	}
	if _, ok := p["properties"].(map[string]any); !ok {
		t.Fatalf("schema without properties: %+v", p)
	}
}

func TestCall(t *testing.T) {
	c := newTestClient(t, nil)
	ctx := context.Background()
	tests := []struct {
		name    string
		tool    string
		args    string
		want    string
		wantErr string
	}{
		{name: "text blocks are concatenated", tool: "echo", args: `{"text":"hi"}`, want: "hi\nhi"},
		{name: "empty args become an empty object", tool: "get_item", args: "", want: "item"},
		{name: "error result carries its text", tool: "fail", args: `{}`, wantErr: "boom"},
		{name: "invalid args JSON is rejected", tool: "echo", args: `{nope`, wantErr: "arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Call(ctx, tt.tool, tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v (out %q)", tt.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCallRefusesToolOutsideAllowlist(t *testing.T) {
	c := newTestClient(t, []string{"echo"})
	if _, err := c.Call(context.Background(), "get_item", ""); err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("want allowlist error, got %v", err)
	}
}

func TestToolName(t *testing.T) {
	tests := []struct {
		name, server, tool, want string
	}{
		{name: "plain", server: "plane", tool: "list_work_items", want: "plane__list_work_items"},
		{name: "invalid chars sanitized", server: "my server", tool: "get.item/v2", want: "my_server__get_item_v2"},
		{name: "truncated to 64", server: "s", tool: strings.Repeat("a", 80), want: "s__" + strings.Repeat("a", 61)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToolName(tt.server, tt.tool); got != tt.want {
				t.Fatalf("ToolName(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
			}
		})
	}
}

func TestSplitToolName(t *testing.T) {
	tests := []struct {
		name, in, server, tool string
		ok                     bool
	}{
		{name: "prefixed", in: "plane__list_work_items", server: "plane", tool: "list_work_items", ok: true},
		{name: "tool keeps later separators", in: "a__b__c", server: "a", tool: "b__c", ok: true},
		{name: "built-in tool", in: "create_task", ok: false},
		{name: "empty server", in: "__x", ok: false},
		{name: "empty tool", in: "x__", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, tl, ok := SplitToolName(tt.in)
			if ok != tt.ok || s != tt.server || tl != tt.tool {
				t.Fatalf("SplitToolName(%q) = %q, %q, %v", tt.in, s, tl, ok)
			}
		})
	}
}

// startClient connects a Client named name to an already populated SDK server
// over the in-memory transport.
func startClient(t *testing.T, name string, srv *sdk.Server, allow []string) *Client {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	c, err := connect(ctx, name, allow, clientT)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// textTool adds a tool that always answers with the given text.
func textTool(srv *sdk.Server, name, text string) {
	srv.AddTool(&sdk.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
		})
}

// Two server tools that sanitize to the same exposed name must not both be
// emitted: the first one listed wins, and Call reaches that same tool.
func TestToolsSkipDuplicateExposedNames(t *testing.T) {
	srv := sdk.NewServer(&sdk.Implementation{Name: "dup", Version: "v0"}, nil)
	textTool(srv, "get.item", "dotted") // lists before "get_item" ('.' < '_')
	textTool(srv, "get_item", "underscored")
	c := startClient(t, "dup", srv, nil)
	ctx := context.Background()
	tools, err := c.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "dup__get_item" {
		t.Fatalf("want one dup__get_item, got %+v", tools)
	}
	got, err := c.Call(ctx, "get_item", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "dotted" {
		t.Fatalf("Call reached %q, want the first listed tool", got)
	}
}

func TestInstructionsAreCaptured(t *testing.T) {
	srv := sdk.NewServer(&sdk.Implementation{Name: "ins", Version: "v0"}, &sdk.ServerOptions{Instructions: "Use project ids."})
	textTool(srv, "ping", "pong")
	if got := startClient(t, "ins", srv, nil).Instructions(); got != "Use project ids." {
		t.Fatalf("Instructions() = %q", got)
	}
}

func TestValidServerName(t *testing.T) {
	tests := []struct {
		name, in string
		want     bool
	}{
		{name: "plain", in: "plane", want: true},
		{name: "digits and dash", in: "plane-2", want: true},
		{name: "max length", in: strings.Repeat("a", 24), want: true},
		{name: "empty", in: "", want: false},
		{name: "too long", in: strings.Repeat("a", 25), want: false},
		{name: "underscore could fake the separator", in: "a_b", want: false},
		{name: "space", in: "my server", want: false},
		{name: "dot", in: "a.b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidServerName(tt.in); got != tt.want {
				t.Fatalf("ValidServerName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
