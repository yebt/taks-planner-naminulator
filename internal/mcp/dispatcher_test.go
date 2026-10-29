package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/webcloster-dev/planner/internal/llm"
)

// fakeBase stands in for the built-in tool registry.
type fakeBase struct{ names []string }

func (f fakeBase) Definitions() []llm.Tool {
	out := make([]llm.Tool, len(f.names))
	for i, n := range f.names {
		out[i] = llm.Tool{Name: n, Description: "built-in"}
	}
	return out
}

func (f fakeBase) Dispatch(_ context.Context, name, _ string) (string, error) {
	return "base:" + name, nil
}

func TestDispatcherDefinitionsMergeAndBaseWins(t *testing.T) {
	base := fakeBase{names: []string{"create_task", "fake__echo"}}
	d, warns := NewDispatcher(context.Background(), base, []*Client{newTestClient(t, nil)})
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	count := map[string]int{}
	for _, tl := range d.Definitions() {
		count[tl.Name]++
		if tl.Name == "fake__echo" && tl.Description != "built-in" {
			t.Fatalf("a conflicting MCP tool replaced the built-in one: %+v", tl)
		}
	}
	for _, want := range []string{"create_task", "fake__echo", "fake__fail", "fake__get_item"} {
		if count[want] != 1 {
			t.Fatalf("%s listed %d times in %v", want, count[want], count)
		}
	}
}

func TestDispatcherRoutes(t *testing.T) {
	base := fakeBase{names: []string{"create_task", "fake__echo"}}
	d, _ := NewDispatcher(context.Background(), base, []*Client{newTestClient(t, nil)})
	tests := []struct {
		name, tool, args, want string
	}{
		{name: "mcp tool goes to its server", tool: "fake__get_item", want: "item"},
		{name: "built-in goes to the base", tool: "create_task", want: "base:create_task"},
		{name: "base wins a name conflict", tool: "fake__echo", args: `{"text":"x"}`, want: "base:fake__echo"},
		{name: "unlisted prefixed name goes to the base", tool: "fake__nope", want: "base:fake__nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.Dispatch(context.Background(), tt.tool, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Dispatch(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

// A server whose listing fails is reported and left out; the others still work.
func TestDispatcherSkipsServerThatCannotList(t *testing.T) {
	broken := newTestClient(t, nil)
	broken.name = "broken"
	broken.Close()
	ok := newTestClient(t, []string{"echo"})
	d, warns := NewDispatcher(context.Background(), fakeBase{}, []*Client{broken, ok})
	if len(warns) != 1 || !strings.Contains(warns[0], "broken") {
		t.Fatalf("want one warning about broken, got %v", warns)
	}
	defs := d.Definitions()
	if len(defs) != 1 || defs[0].Name != "fake__echo" {
		t.Fatalf("want only fake__echo, got %+v", defs)
	}
	if strings.Contains(d.PromptSection(), "broken") {
		t.Fatalf("prompt mentions a server with no tools: %q", d.PromptSection())
	}
}

func TestPromptSection(t *testing.T) {
	t.Run("empty without servers", func(t *testing.T) {
		d, _ := NewDispatcher(context.Background(), fakeBase{}, nil)
		if got := d.PromptSection(); got != "" {
			t.Fatalf("want empty section, got %q", got)
		}
	})
	t.Run("lists prefix, hint and truncated instructions", func(t *testing.T) {
		long := strings.Repeat("é", 1500)
		srv := sdk.NewServer(&sdk.Implementation{Name: "plane", Version: "v0"}, &sdk.ServerOptions{Instructions: long})
		textTool(srv, "workitem", "ok")
		c := startClient(t, "plane", srv, nil)
		c.hint = "always pass project_id"
		d, _ := NewDispatcher(context.Background(), fakeBase{}, []*Client{c})
		got := d.PromptSection()
		for _, want := range []string{"plane", "plane__", "always pass project_id"} {
			if !strings.Contains(got, want) {
				t.Fatalf("section lacks %q:\n%s", want, got)
			}
		}
		if n := strings.Count(got, "é"); n != maxInstructions {
			t.Fatalf("instructions kept %d runes, want %d", n, maxInstructions)
		}
	})
}
