# planner

Personal planning agent. You tell it what you're doing, planning, postponing, or
finishing during the day; an LLM keeps a local task board in sync using tools.
Multi-provider (OpenAI, Moonshot, Kimi, Claude, or any OpenAI-compatible host
like Ollama) so you can develop cheaply and plug Claude in later.

## Architecture (ports & adapters)

```
cmd/planner            entrypoint: chat REPL · tui · config
internal/domain        Task, TaskType, Status (our semantic layer over Plane states)
internal/llm           Provider port + adapters (openai/moonshot/kimi/claude/custom)
internal/store         TaskStore port + SQLite adapter (modernc, pure Go)
internal/tools         LLM tool set → deterministic ops on the store
internal/agent         tool-use loop
internal/config        JSON config (providers, db path, Plane settings)
internal/tui           Bubbletea board view
```

The core depends only on the `Provider` and `TaskStore` ports, so swapping the
LLM or storage is an adapter change, not a rewrite.

## Run

```sh
go build ./...
go run ./cmd/planner config    # writes ~/.config/planner/config.json
go run ./cmd/planner           # chat agent (defaults to a local Ollama provider)
go run ./cmd/planner tui       # read-only task board
```

In chat: `/todos`, `/model <name>`, `/help`, `/quit`.

By default the active provider is `ollama` (`http://localhost:11434/v1`) so you
burn no paid tokens while iterating. Set an API key for `openai`/`kimi`/`claude`
in the config file and `/model claude` to switch.

## Test

```sh
go test ./...
```

Adapters are tested against `httptest` servers (no live API calls); the store
and tools are tested against a temp SQLite file.

## Not yet (next slices)

- Plane sync (push-only + manual "pull states") using `WorkItemID`.
- Daily generation from tasks touched today, with the 3 blocks + history.
- Telegram bot delivery.
- Config editing from the TUI.
