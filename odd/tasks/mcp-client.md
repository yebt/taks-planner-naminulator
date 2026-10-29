# Feature: mcp-client

Locator: `odd/tasks/mcp-client.md` · Engram mirror: `odd/mcp-client/tasks` · Branch: `feat/mcp-client`

## Objective
Make the planner an MCP client so the chat agent can call tools from stdio MCP servers
(first target: `uvx plane-mcp-server stdio`) alongside its built-in tools.

## Problem / why
The app only acts on tasks it created itself (local SQLite). The user needs to act on any
Plane work item — e.g. backfill `start_date`/`target_date` on DONE items from their activity
history. The Plane MCP already exposes list/activity/update tools.

## Scope
- Config: `mcp_servers` (name → command, args, env, optional tool allowlist, enabled).
- `internal/mcp`: connect over stdio (official `github.com/modelcontextprotocol/go-sdk`),
  list tools, call tools, close.
- Composite dispatcher: built-in registry + MCP tools, namespaced `<server>__<tool>`.
- Wiring in `cmd/planner` (start before agent, close on exit); a server that fails to start
  is reported and skipped, never fatal.
- System-prompt hint: self-hosted Plane v1.2.0 rejects workspace-scope calls (404) — always
  pass `project_id`.
- README docs.

Out of scope: hot reload of MCP servers from `/config`; HTTP/SSE transports.

## Constraints
- Agent loop (`internal/agent`) stays unchanged; it only sees `ToolDispatcher`.
- Secrets in `env` live in the 0600 config file; never logged or rendered.
- Generated artifacts in English.

## Acceptance criteria
- With a configured server, its tools appear in `Definitions()` as `<server>__<tool>` and
  `Dispatch` routes calls to it, returning the text content.
- Allowlist limits exposed tools; a disabled or failing server does not break the chat.
- `make check` passes.

## Checks
- `go test ./...`, `go vet ./...`, `gofmt -l .` (`make check`).
- Test-first: in-memory MCP transport (SDK) for client tests; fake dispatcher for composite.

## Delivery
Strategy: `ask-on-risk` → chain strategy `stacked-to-main` (user choice).
Forecast: ~600 authored lines → two slices (T1, T2).

## Tasks
- [x] T1 — config `MCPServers` + `internal/mcp` client (connect, list, call, close) with tests.
  Route: delegated writer (2+ non-trivial files; preparation reading).
  Evidence: commit 4b70070; go-sdk v1.8.0; RED observed (compile + empty-args lookup);
  `go test -race ./internal/mcp/... ./internal/config/...` pass, vet/gofmt clean.
  Assess: unassessable → high (RDD off) → independent verifier: pass-with-notes
  (name collisions, long/`__` server names) → folded into T2.
- [x] T2 — composite dispatcher, wiring in `cmd/planner`, TUI deps, Plane prompt hint,
  README. Route: delegated writer (2+ non-trivial files); parent inline follow-up moved
  startup warnings from stderr into the chat (`ChatDeps.Warnings` → healthCheck, RED→GREEN
  `TestHealthCheckSurfacesStartupWarnings`).
  Evidence: `go test ./...`, `go vet ./...`, `gofmt -l .` clean. Real smoke test against
  Plane v1.2.0: 30 `plane__*` tools listed, `plane__state` list dispatched OK, prompt
  section 991 bytes. Startup: 23 s cold (uvx resolve), ~4 s warm.

## Follow-ups (not in scope)
- Connect MCP servers asynchronously so the chat opens immediately (~4 s warm block today).
- Consider a `tools` allowlist for plane to cut per-turn schema tokens (30 tools).
- T2 verifier (pass-with-notes, minor): SDK kills only the direct child (uvx grandchild can
  survive a hung handshake); `Close` is sequential (up to ~10 s each with a stuck server);
  `/load` restores the saved system prompt, so the MCP section reflects save time; server
  tool descriptions/schemas are not size-capped.

## Next step
Feature done on `feat/mcp-client`; push/PR is the user's decision.

## Progress / evidence
- Premise verified 2026-10-28: plane-mcp-server 0.3.5 project-scoped calls work against
  Plane v1.2.0 (`workitem list`, `state list`, `workitem_activity list`); `project list`
  fails because it uses `/projects-lite/` (404).


## Slices (stacked-to-main)
- PR 1: `feat/mcp-client-core` → `main` — 4b70070 (client + config, 471 lines).
- PR 2: `feat/mcp-client` → `feat/mcp-client-core` — 18b66f3 (dispatcher, wiring, TUI, docs).
