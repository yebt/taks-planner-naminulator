// Package wiring is the single owner of adapter construction: it knows how to
// build the Plane and Telegram clients from a config.Config, so no caller has
// to restate that mapping. Both the command entry point and the configuration
// TUI build their adapters through here.
//
// Scope, honestly: this removes the duplicated construction knowledge, it does
// NOT make internal/tui port-pure. The configuration TUI genuinely needs
// Client.ListStates, whose result type is plane.State, so internal/tui still
// depends on internal/plane transitively through this package. Hiding that
// behind locally defined mirror types would buy an import-graph cosmetic at the
// price of a translation layer and a second type to keep in sync — a worse
// trade. The dependency is real; it is left visible.
package wiring

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/mcp"
	"github.com/webcloster-dev/planner/internal/openrouter"
	"github.com/webcloster-dev/planner/internal/plane"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/telegram"
)

// PlaneClient builds the raw Plane client — what callers that only read from
// Plane need (e.g. fetching the project's workflow states).
func PlaneClient(cfg config.Config) *plane.Client {
	return plane.New(plane.Config{
		BaseURL:       cfg.Plane.BaseURL,
		Token:         cfg.Plane.APIToken,
		WorkspaceSlug: cfg.Plane.WorkspaceSlug,
		ProjectID:     cfg.Plane.ProjectID,
	})
}

// PlaneSyncer builds the Plane syncer over the local store, with the configured
// state defaults and default estimate already applied.
func PlaneSyncer(cfg config.Config, st store.TaskStore) *plane.Syncer {
	s := plane.NewSyncer(PlaneClient(cfg), st, cfg.Plane.StateDefaults)
	s.SetEstimate(cfg.Plane.DefaultEstimate)
	return s
}

// OpenRouterClient builds the read-only OpenRouter catalog client, keyed with
// the openrouter provider's API key when one is set. The catalog is public, so
// a missing key narrows the listing rather than breaking it.
func OpenRouterClient(cfg config.Config) *openrouter.Client {
	return openrouter.New(openrouter.Config{Token: cfg.Providers["openrouter"].APIKey})
}

// TelegramClient builds the Telegram client used to deliver daily digests and
// test notifications.
func TelegramClient(cfg config.Config) *telegram.Client {
	return telegram.New(cfg.Telegram.BotToken, cfg.Telegram.ChatID, cfg.Telegram.ThreadID)
}

// mcpConnectTimeout bounds each server's start-up handshake. It is generous
// because uvx may download the server on its first run.
const mcpConnectTimeout = 30 * time.Second

// connectMCP is swapped in tests so no process is launched.
var connectMCP = mcp.Connect

// MCPClients connects every enabled, validly named MCP server concurrently.
// A server that is misnamed or fails to start is skipped and described in a
// warning — never fatal. Clients come back in name order; the caller closes
// them. Warnings never carry env values.
func MCPClients(ctx context.Context, cfg config.Config) ([]*mcp.Client, []string) {
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)

	var warnings []string
	clients := make([]*mcp.Client, len(names))
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		srv := cfg.MCPServers[name]
		if srv.Disabled {
			continue
		}
		if !mcp.ValidServerName(name) {
			warnings = append(warnings, fmt.Sprintf(
				"mcp server %q skipped: names may only use letters, digits and '-' (max 24)", name))
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, mcpConnectTimeout)
			defer cancel()
			clients[i], errs[i] = connectMCP(cctx, name, srv)
		}()
	}
	wg.Wait()

	var out []*mcp.Client
	for i, c := range clients {
		if errs[i] != nil {
			warnings = append(warnings, fmt.Sprintf("mcp server %q skipped: %v", names[i], errs[i]))
			continue
		}
		if c != nil {
			out = append(out, c)
		}
	}
	return out, warnings
}
