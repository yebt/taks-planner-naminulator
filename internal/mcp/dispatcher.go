package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/webcloster-dev/planner/internal/llm"
)

// maxInstructions caps how many runes of a server's own instructions reach the
// system prompt, so a verbose server can't crowd out the planner's rules.
const maxInstructions = 1000

// Base is the built-in tool set the dispatcher wraps (*tools.Registry).
type Base interface {
	Definitions() []llm.Tool
	Dispatch(ctx context.Context, name, argsJSON string) (string, error)
}

// route is where an exposed MCP tool name lands: a client and the un-prefixed
// name its Call expects.
type route struct {
	client *Client
	tool   string
}

// Dispatcher presents the built-in tools plus every connected server's tools
// as one tool set (it satisfies agent.ToolDispatcher). MCP tools are listed
// once at construction; built-in names win any conflict.
type Dispatcher struct {
	base    Base
	defs    []llm.Tool // MCP tools only; built-ins are asked for live
	routes  map[string]route
	servers []*Client // servers that expose at least one tool, in order
}

// NewDispatcher lists each client's tools and builds the routing table. A
// client whose listing fails is left out and reported as a warning — never an
// error, so one broken server can't take the chat down.
func NewDispatcher(ctx context.Context, base Base, clients []*Client) (*Dispatcher, []string) {
	d := &Dispatcher{base: base, routes: map[string]route{}}
	taken := map[string]bool{}
	for _, t := range base.Definitions() {
		taken[t.Name] = true
	}
	var warnings []string
	for _, c := range clients {
		tools, err := c.Tools(ctx)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		prefix := sanitize(c.Name()) + Separator
		exposed := 0
		for _, t := range tools {
			if taken[t.Name] {
				continue
			}
			taken[t.Name] = true
			d.routes[t.Name] = route{client: c, tool: strings.TrimPrefix(t.Name, prefix)}
			d.defs = append(d.defs, t)
			exposed++
		}
		if exposed > 0 {
			d.servers = append(d.servers, c)
		}
	}
	return d, warnings
}

// Definitions is the built-in tool set followed by the MCP tools.
func (d *Dispatcher) Definitions() []llm.Tool {
	base := d.base.Definitions()
	out := make([]llm.Tool, 0, len(base)+len(d.defs))
	return append(append(out, base...), d.defs...)
}

// Dispatch routes a listed MCP tool to its server; every other name goes to
// the built-in set, which reports unknown tools itself.
func (d *Dispatcher) Dispatch(ctx context.Context, name, argsJSON string) (string, error) {
	if r, ok := d.routes[name]; ok {
		return r.client.Call(ctx, r.tool, argsJSON)
	}
	return d.base.Dispatch(ctx, name, argsJSON)
}

// PromptSection is a block for the system prompt describing the connected
// servers (prefix, configured hint, server instructions), or "" when there
// are none.
func (d *Dispatcher) PromptSection() string {
	if len(d.servers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nExternal tools (MCP servers):\n")
	for _, c := range d.servers {
		fmt.Fprintf(&b, "- %s: tools prefixed %q.\n", c.Name(), sanitize(c.Name())+Separator)
		if h := strings.TrimSpace(c.Hint()); h != "" {
			fmt.Fprintf(&b, "  Hint: %s\n", h)
		}
		if ins := strings.TrimSpace(c.Instructions()); ins != "" {
			fmt.Fprintf(&b, "  Server instructions: %s\n", truncateRunes(ins, maxInstructions))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
