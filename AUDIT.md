# Audit — planner

**Audited:** 2026-07-29 · **Last updated:** 2026-07-30
**Audited at commit:** `a088da8`
**Branch:** `feat/rutine-repo-actions` (identical to `main`, 0 commits ahead/behind)

**Progress: 2 / 41 closed** — [C3](#c3) ✅ · [H1](#h1) ✅ · 1 new finding filed
([N1](#n1)). Suite green after every fix; each closed item ships with a test that
was verified to fail against the old behavior.

## Baseline

| Check | Result |
| ----- | ------ |
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `gofmt -l .` | clean |
| `go test ./...` | all green |
| `go mod tidy` | no diff |
| TODO/FIXME/XXX markers | none |

Nothing is broken. Everything below is latent risk, drift, or an unfinished decision.

### Coverage by package

| Package | Coverage |
| ------- | -------- |
| `internal/contextmgr` | 90.3% |
| `internal/telegram` | 87.2% |
| `internal/tools` | 72.0% |
| `internal/store` | 69.0% |
| `internal/domain` | 65.6% |
| `internal/config` | 65.3% |
| `internal/plane` | 55.2% |
| `internal/memory` | 55.6% |
| `internal/llm` | 53.3% |
| `internal/agent` | 51.2% |
| **`internal/tui`** | **29.5%** ← largest file (2503 lines), least covered |
| `cmd/planner` | 0.0% |

> Line numbers are as of `a088da8`. Findings marked **[verified]** were re-read
> and confirmed by hand, not only reported by an automated lens.

---

## Severity index

Status: ✅ done · 🔧 in progress · ⬜ open

| ID | St | Severity | Title |
| -- | -- | -------- | ----- |
| [C1](#c1) | ⬜ | CRITICAL | Slash commands block the Bubbletea event loop — unrecoverable freeze |
| [C2](#c2) | ⬜ | CRITICAL | `contextmgr.Fit` can return only the system message |
| [C3](#c3) | ✅ | CRITICAL | Daily fallback silently overwrites a hand-edited draft |
| [H1](#h1) | ✅ | HIGH | Telegram bot token leaks on screen through `*url.Error` |
| [H2](#h2) | ⬜ | HIGH | Local-vs-UTC day boundary mismatch |
| [H3](#h3) | ⬜ | HIGH | No Telegram 4096-char handling — long dailies never deliver |
| [H4](#h4) | ⬜ | HIGH | TUI never renders the markup it asks the LLM to produce |
| [H5](#h5) | ⬜ | HIGH | Daily format spec forked three ways, already drifted |
| [H6](#h6) | ⬜ | HIGH | Malformed tool-call JSON silently swallowed in 4 handlers |
| [H7](#h7) | ⬜ | HIGH | `pushSync` discards Plane errors with zero signal |
| [M1](#m1) | ⬜ | MEDIUM | `config.json` is world-readable and written non-atomically |
| [M2](#m2) | ⬜ | MEDIUM | `/key` echoes the secret and keeps it in input history |
| [M3](#m3) | ⬜ | MEDIUM | `tui.go` is a 2503-line god-object |
| [M4](#m4) | ⬜ | MEDIUM | `send_daily` advertised to the LLM without Telegram configured |
| [M5](#m5) | ⬜ | MEDIUM | `dayFrom` swallows bad dates; slash path rejects them |
| [M6](#m6) | ⬜ | MEDIUM | `Dispatch` is exported with no nil-dependency guards |
| [M7](#m7) | ⬜ | MEDIUM | SQLite opened without `busy_timeout` / WAL / conn limit |
| [M8](#m8) | ⬜ | MEDIUM | `Syncer.Push` can create duplicate Plane issues |
| [M9](#m9) | ⬜ | MEDIUM | `PullStates` aborts the batch and hides which task failed |
| [M10](#m10) | ⬜ | MEDIUM | Agent max-steps exhaustion leaves dangling history |
| [M11](#m11) | ⬜ | MEDIUM | `internal/tui` imports concrete adapters, duplicating wiring |
| [M12](#m12) | ⬜ | MEDIUM | `Definitions()` / `Dispatch()` are two hand-synced registries |
| [M13](#m13) | ⬜ | MEDIUM | No CI |
| [M14](#m14) | ⬜ | MEDIUM | `buildDaily` hardcodes "hoy" for every date |
| [M15](#m15) | ⬜ | MEDIUM | No `list_dailies` tool — README oversells conversational parity |
| [N1](#n1) | ⬜ | MEDIUM | `persistDaily` discards the save error — "daily ready" can be a lie |
| [L1–L14](#low) | ⬜ | LOW | Cleanup, docs drift, cosmetics |

Findings discovered *during* remediation are filed under [New findings](#new)
and carry an `N` prefix.

---

## CRITICAL

### C1 — Slash commands block the Bubbletea event loop {#c1}

**Where:** `internal/tui/tui.go:616-643` (`submit`), `855-1041` (`runCommand`), `internal/memory/memory.go:38-40`

`submit` calls `m.runCommand(val)` **inline inside `Update()`** instead of
returning a `tea.Cmd`. `runCommand` uses `context.Background()` — no deadline —
and performs blocking I/O for `/recall`, `/remember`, `/sync`, `/pull` and
`/daily send`. There is not a single `context.WithTimeout` in the repo.

The non-slash chat path does this correctly (`sendCmd`, `tui.go:725-730`), which
makes the asymmetry accidental rather than deliberate.

**Failure scenario:** the user types `/remember <note>` while the `engram`
backend is unresponsive. `Update()` blocks inside `CombinedOutput()` forever.
Bubbletea dequeues the next message only after `Update()` returns, so the queued
**Ctrl+C is never processed**. The TUI is frozen with no spinner (`m.thinking` is
only set on the chat path) and no escape — recovery requires killing the process
from another terminal. `/sync`, `/pull` and `/daily send` freeze the same way for
20–30s, bounded only by their HTTP client timeouts.

**Fix:** move every blocking branch of `runCommand` into a `tea.Cmd`, give each a
`context.WithTimeout`, and set `m.thinking` so the user sees a spinner. Add a
timeout to `memory.defaultRun` specifically — it has no bound of any kind.

---

### C2 — `contextmgr.Fit` can return only the system message {#c2} **[verified]**

**Where:** `internal/contextmgr/contextmgr.go:39-53`

Trace with `[system, user, assistant(tool_call), tool(oversized)]`:

1. `start = len(rest) = 3`.
2. `i=2` (the oversized tool result): the guard is
   `total+s > Budget && start < len(rest)` — `start(3) < 3` is **false**, so the
   message is kept regardless of size. `start = 2`.
3. `i=1` (the assistant tool-call): budget already blown and `start(2) < 3` is
   true → `break`.
4. `kept = [tool]`.
5. The anti-orphan pass strips a leading `RoleTool` → `kept = []`.
6. Returns `[system]`.

**Failure scenario:** one large `list_tasks` dump mid-tool-loop and the next
provider call carries the system prompt and **nothing else** — no user request,
no tool result. The model answers from a blank slate; from its point of view the
conversation silently reset. No error is raised anywhere.

**Fix:** after the orphan pass, if `kept` is empty but `rest` is not, fall back to
the newest non-`RoleTool` message (or truncate the oversized tool result rather
than dropping its context). Whatever the policy, make it explicit.

**Test gap:** `TestFitDropsOrphanToolResult` only covers the case where a small
trailing user message survives. Add: *`Fit` never returns a system-only window
when the input has ≥1 non-system message.*

---

### C3 — Daily fallback silently overwrites a hand-edited draft {#c3} ✅ DONE **[verified]**

**Where:** `internal/tui/tui.go:1390-1401` (`generateDailyCmd`), `252-263` (`dailyMsg` handler)

`generateDailyCmd` carefully loads the stored draft into `prior` and feeds it to
the LLM so previous edits survive — but the fallback is built as
`buildDaily(date, tasks)`, which **never receives `prior`**. Then:

```go
case dailyMsg:
    text := strings.TrimSpace(msg.text)
    if msg.err != nil || text == "" {
        text = msg.fallback
        if msg.err != nil {
            m.add("sys", "LLM daily failed, using basic format: "+msg.err.Error())
        }
    }
    m.persistDaily(msg.dateKey, text)   // ← unconditional overwrite
```

**Failure scenario:** the user spends ten minutes curating a daily via
`/daily edit`, then runs `/daily 2026-07-29 "agregá el deploy"` to refine it. The
provider is down. The curated content is replaced by the mechanical fallback and
**written to the store**, destroying the edits.

**Worse sub-case:** when the LLM returns an empty string with `err == nil`, the
draft is overwritten with the fallback and **no message is shown at all** — the
`sys` warning is inside `if msg.err != nil`. Silent data loss.

**Fix applied:** `dailyMsg` now carries `prior`, threaded through `dailyCmd` from
`generateDailyCmd`. On the failure path the handler branches:

- **A stored daily exists** → it is kept verbatim, **nothing is persisted**, and
  the failure is reported as an `err` entry ("the stored daily is untouched").
- **Nothing stored** → there is nothing to lose, so the fallback is the useful
  outcome and is persisted as before.

The empty-response case (`err == nil`, `text == ""`) now takes the same branch
and always reports, closing the silent sub-case.

**Landed in:** `internal/tui/tui.go` (`dailyMsg` struct, `dailyMsg` handler,
`generateDailyCmd`, `dailyCmd`)

**Tests added** (`internal/tui/daily_test.go`):
- `TestDailyFailureKeepsStoredDraft` — LLM error with a curated draft stored:
  store content unchanged, in-memory draft unchanged, failure reported.
- `TestDailyEmptyResponseWarnsAndKeepsDraft` — empty response, no error: store
  unchanged **and** an `err` entry is emitted (the silent sub-case).
- `TestDailyFailureWithoutPriorUsesFallback` — no prior: fallback *is* persisted,
  so the fix doesn't over-correct into uselessness.

---

## HIGH

### H1 — Telegram bot token leaks on screen through `*url.Error` {#h1} ✅ DONE **[verified]**

**Where:** `internal/telegram/client.go:60-69`, `internal/tui/tui.go:1505-1508`, `2366`

```go
url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(c.api, "/"), c.token)
req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, ...)
resp, err := c.http.Do(req)
if err != nil {
    return err          // ← *url.Error, whose Error() contains the full URL
}
```

`sendDaily` renders that string with `m.add("err", err.Error())`.

**Failure scenario:** run `/daily send` with no network. Go's `net/http` returns a
`*url.Error` whose `Error()` embeds the request URL — i.e. the **bot token in
cleartext** — printed to the terminal and retained in `m.entries` until `/clear`.
A DNS blip, a TLS failure or a timeout does the same. This is a live credential.

**Confirmed empirically** before fixing — a throwaway program reproducing the old
path against a refused connection returned:

```
Post "http://127.0.0.1:1/bot123456789:AAHs-this-is-a-secret-bot-token/sendMessage":
  dial tcp 127.0.0.1:1: connect: connection refused
```

**Fix applied:** two layers, so neither alone has to be perfect.

1. **Unwrap** — `errors.As` extracts the `*url.Error` cause and only `ue.Err` is
   reported, so the URL never enters the string in the first place.
2. **Redact** — `(*Client).redact` replaces the token with `[redacted]` as a
   backstop, applied to the transport error *and* to the API `description`.
   Tokens shorter than 8 chars are left alone: they cannot be real credentials
   and blind replacement would mangle unrelated text.

The result is formatted with `%s`, not `%w`, so no `errors.Unwrap` path can
resurrect the original message.

**Also fixed in passing:** a local variable named `url` shadowed the `net/url`
package — renamed to `endpoint`.

**Landed in:** `internal/telegram/client.go` (`Send`, new `redact` method)

**Test added:** `TestSendTransportErrorHidesToken` (`internal/telegram/client_test.go`)
— points the client at a dead port and asserts the token appears nowhere in the
returned error. Verified to fail against the old behavior.

---

### H2 — Local-vs-UTC day boundary mismatch {#h2} **[verified]**

**Where:** `internal/tui/tui.go:1348-1359` (`parseDay`), `internal/tools/tools.go:281-292` (`dayFrom`), `internal/store/sqlite.go:217,291`

`"hoy"` returns `time.Now()` — **Local**. An explicit date goes through
`time.Parse("2006-01-02", tok)`, which Go returns in **UTC**. Both are handed to
`store.Filter{Day:}` / `TasksWithActivityOn`, whose window math uses
`day.Location()`.

**Failure scenario:** in UTC-5, `/daily hoy` covers local 00:00–24:00 while
`/daily 2026-07-29` (the same calendar day) covers UTC 00:00–24:00 — a window
shifted five hours. A task touched at 20:00 local appears in one and not the
other. Same bug on the agent path via `dayFrom`, and on `/todo <status> <day>`.

**Fix:** `time.ParseInLocation("2006-01-02", tok, time.Local)` in both parsers.

**Test gap:** `TestParseDay` only compares `.Format("2006-01-02")` strings, which
passes regardless of Location — it asserts formatting trivia, not behavior. Add:
*two tasks straddling the offset boundary return the same set via `"hoy"` and via
the equivalent explicit date.*

---

### H3 — No Telegram 4096-char handling {#h3}

**Where:** `internal/telegram/client.go:42-84`, `internal/tui/tui.go:1494-1510`

Telegram caps `sendMessage` at 4096 characters. `Send` neither checks nor splits.

**Failure scenario:** a busy day produces a digest past 4096 chars. Telegram
replies `ok:false` with "message is too long"; the user gets an opaque error and
the daily simply never arrives. No chunking, no truncation, no pointer to the
full text. They must manually shorten and retry.

**Fix:** split on paragraph boundaries into ≤4096-char chunks and send
sequentially, or truncate with an explicit `…(truncated)` marker. Test with a
5000-char daily.

---

### H4 — TUI never renders the markup it asks the LLM to produce {#h4}

**Where:** `internal/tui/tui.go:2371-2372` (`setContent`, `case "raw"`), `263`, `1472`; `go.mod` (no glamour)

The daily is authored in a CommonMark subset (`**bold**`, `__italic__`,
`` `code` ``) and converted to HTML **only** on the Telegram hop
(`telegram/markup.go`). The TUI displays it via `m.add("raw", ...)` and:

```go
case "raw":
    blocks = append(blocks, e.text) // pre-styled/wrapped, passthrough
```

Two consequences:

1. The user literally sees `**Daily:**  2026-07-29`, `**Trabajo:**`,
   `` `migración de DNS` ``, `__nota__`. The whole markup contract we just built
   is noise in the one UI they actually look at.
2. The "pre-wrapped" assumption is **false** for LLM output. No `Render(w)` width
   wrapping is applied, so long lines overflow the viewport and desync the
   character-granular selection mapping (`contentLines`, `tui.go:2379`) — click-drag
   selection silently selects the wrong text.

**Fix:** render the CommonMark subset with lipgloss at display time (mirroring
`markup.go`'s regex set), and wrap `raw` blocks to the viewport width. Point 2 is
a correctness bug independent of point 1 and should be fixed either way.

---

### H5 — Daily format spec forked three ways, already drifted {#h5}

**Where:** `cmd/planner/main.go:39-60` (`systemPrompt`), `internal/tui/tui.go:1300-1319` (`dailyPrompt`), `internal/tui/tui.go:1556-1590` (`buildDaily`)

Three independent owners of one specification. What matches: the prefixes
`"  - "`, `"  # "`, `"  >> "`, the bold section titles, and the bold/italic rules
(verbatim identical between the two prompts). What has **already drifted**:

| # | Divergence |
| - | ---------- |
| A | **Date header.** `main.go:39` → `**Daily:**  <YYYY-MM-DD>`. `tui.go:1300` → `**Daily:**  <FECHA>` where FECHA is `dailyDate()` = `2026-07-29 JUL`. Same feature, two visible output formats depending on which path wrote it. |
| B | **Exact-prefix rule** (`tui.go:1313`) exists only in `dailyPrompt`; in `systemPrompt` the prefixes appear only implicitly in the sample block. |
| C | **Backtick examples**: `main.go:55` has 4, `tui.go:1316` has 3 (drops `deploy a producción`). |
| D | **The `+` warning** is truncated in `tui.go:1315` vs `main.go:52-53`. |
| E | **Empty-section rule**: English one-liner in `main.go:59`, explicit Spanish rule in `tui.go:1319`. |
| F | **The fallback implements ~40% of the spec.** `buildDaily` emits section titles and prefixes only — zero backticks, zero bold titles, zero italic notes, and never a `Bloqueos` section. It also emits raw `[FEAT] #12 Título`, directly contradicting `tui.go:1296` ("no copies los títulos tal cual"). |

**Why it matters:** this is not hypothetical drift — C and D prove hand-copying
already failed. Any future format change requires remembering three files across
two packages.

**Fix:** one `internal/daily` package owning the spec: the prefix constants, the
prompt text (composed, not duplicated), and the fallback builder. Both `main.go`
and `tui.go` consume it. This collapses A–F into a single source of truth.

---

### H6 — Malformed tool-call JSON silently swallowed {#h6}

**Where:** `internal/tools/tools.go:298, 334, 348, 522`

`listDayTasks`, `getDaily`, `sendDaily` and `listTasks` do
`_ = json.Unmarshal(...)` and proceed with zero-value fields. The other 11
handlers correctly return `fmt.Errorf("... bad args: %w", err)`.

**Failure scenario:** a provider truncates the arguments for `send_daily`. Instead
of an error the agent loop could react to, the tool silently defaults to **today**
and **actually delivers a Telegram message** — a wrong, externally-visible side
effect with no error signal anywhere.

**Fix:** return the unmarshal error in all four, matching the other handlers.

**Test gap:** no test calls `Dispatch` with malformed JSON. Add
`Dispatch(ctx, "send_daily", "{not json")` → expect an error, for all four.

---

### H7 — `pushSync` discards Plane errors with zero signal {#h7}

**Where:** `internal/tools/tools.go:77-83`

```go
func (r *Registry) pushSync(ctx context.Context, t *domain.Task) {
    if r.sync != nil && r.sync.Configured() {
        _ = r.sync.Push(ctx, t)
    }
}
```

Local-first is the right policy — a failed push must not fail the local write.
But "don't fail" has been implemented as "don't mention", and there is no log
line, no counter, no status marker.

**Failure scenario:** the Plane token expires. Every task created through the
agent or `/new` reports success and lands locally; none reach Plane. The user
finds out days later when a ticket is missing.

**Fix:** record the push outcome on the task (a `sync_error` column or a
`last_sync_at`) and surface a discreet indicator in `/todo` / `/task`, plus a
startup or per-N-failures warning. Keep the operation non-fatal.

---

## MEDIUM

### M1 — `config.json` is world-readable and written non-atomically {#m1}

**Where:** `internal/config/config.go:179-188`

`os.MkdirAll(dir, 0o755)` + `os.WriteFile(path, data, 0o644)` for a file holding
every provider API key, the Plane token and the Telegram bot token in plaintext.
Any other local account can read them. The write also goes straight to the final
path — a crash mid-write truncates `config.json` and loses all configuration on
next `Load`, with no backup.

**Fix:** `0o700` dir, `0o600` file, and write via temp-file + `os.Rename`.

### M2 — `/key` echoes the secret and keeps it in input history {#m2}

**Where:** `internal/tui/tui.go:855-856` (`m.add("cmd", val)`), `621` (`m.pushHistory(val)`)

`/key openai sk-live-XXXX` is rendered verbatim in the viewport and stored in
`m.history`, one Up-arrow away for the rest of the session. Note the config TUI
does this correctly (`config.go:467,485-493`, `maskSecret`) — the protection just
doesn't extend to the chat command.

**Fix:** mask the echo and skip `pushHistory` for `/key`.

### M3 — `tui.go` is a 2503-line god-object {#m3}

**Where:** `internal/tui/tui.go`

Eleven unrelated concerns in one file: Bubbletea lifecycle, key state machine,
mouse selection + clipboard, slash-command dispatch, todo rendering, task detail,
the whole daily flow, projects/people, mentions, provider/favorites, conversation
persistence, suggestions, and layout. Longest functions: `runCommand` (206 lines,
`855-1060`), `handleKey` (154 lines, `301-454`).

Combined with 29.5% coverage, this is where regressions will come from.

**Proposed split** (same package, pure file-boundary move, no behavior change):

| New file | From `tui.go` | Contents |
| -------- | ------------- | -------- |
| `tui.go` (trimmed) | 1-98, 143-299, 2400-2441 | `ChatDeps`, ports, `chatModel`, `RunChat`, `Init`/`Update`/`View` |
| `handlekey.go` | 301-454 | key state machine |
| `selection.go` | 456-614 | mouse selection, clipboard, ANSI strip |
| `commands.go` | 819-1069 | `runCommand`, `baseCommands`, `needsArg`, `report` |
| `todo.go` | 1070-1238 | `showTodo`, `renderTodo`, status styling, state picker, `dropTask` |
| `task_detail.go` | 1806-1912 | `showTask`, `writeDetails` |
| `daily.go` | 1291-1590 | whole daily flow |
| `context.go` | 645-723, 1592-1780 | mentions, projects/people |
| `suggestions.go` | 150-158, 2131-2334 | `computeSuggestions`, `acceptSuggestion` |
| `providers.go` | 1914-2043 | model/favorites/key management |
| `conversations.go` | 1536-1552, 2045-2129 | save/load/resume/autosave |
| `render.go` | 117-141, 2336-2399, 2443-2503 | styles, `add`, `setContent`, `footer`, `statusBar` |

### M4 — `send_daily` advertised without Telegram configured {#m4}

**Where:** `internal/tools/tools.go:66` (`dailiesEnabled`), `208`, `230-234`

Registration is gated on `r.dailies != nil` only; Telegram is never consulted.
The memory tools do this correctly (`memEnabled()`, `tools.go:75,148`).

**Failure scenario:** on a box with no Telegram config the LLM is still handed
`send_daily`, offers to deliver the daily, calls it, and fails. The user is
promised a delivery the system knew in advance it could not make.

**Fix:** gate registration on `r.tg != nil && r.tg.Configured()`.

### M5 — `dayFrom` swallows bad dates {#m5}

**Where:** `internal/tools/tools.go:281-292` vs `internal/tui/tui.go:1348-1359`

`parseDay` returns `ok=false` on garbage and the slash command shows usage.
`dayFrom` silently returns `time.Now()`. "armá el daily del 32 de enero"
quietly becomes today's.

**Fix:** return an error and let the agent loop see it.

### M6 — `Dispatch` is exported with no nil-dependency guards {#m6}

**Where:** `internal/tools/tools.go:241`; unguarded derefs at `326, 336, 350, 368, 383, 397, 412`

Seven handlers dereference `r.dailies` / `r.ctxStore` with no check. Only
`recallMemory` (`429`) and `rememberNote` (`443`) defend themselves. Not reachable
today because `main.go:158-177` always calls the setters — but registration is the
*only* thing standing between a nil interface and a panic on an **exported**
method. `sendDaily` (`343-353`) happens to check `r.tg` first, which shadows the
nil-dailies path by luck, not design.

**Fix:** guard each handler the way the memory tools do.

### M7 — SQLite opened without concurrency pragmas {#m7}

**Where:** `internal/store/sqlite.go:25-36`

`sql.Open("sqlite", path)` with no `busy_timeout`, no `journal_mode=WAL`, no
`SetMaxOpenConns(1)`. Concurrent access is reachable: `submit()` only blocks
re-submission for non-slash input, so a synchronous `/new` can write while a
background `sendCmd` goroutine's tool calls write too. With the default rollback
journal and zero busy timeout, the loser gets `SQLITE_BUSY` immediately — a raw
"database is locked" error, no retry.

**Fix:** `PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`, and consider
`SetMaxOpenConns(1)`.

### M8 — `Syncer.Push` can create duplicate Plane issues {#m8}

**Where:** `internal/plane/syncer.go:125-151`

`CreateIssue` succeeds, then `s.store.Update` persists `WorkItemID`. If **that
write** fails (e.g. the `SQLITE_BUSY` of M7), the id is never durably saved. The
next `Push` reloads the task with an empty `WorkItemID` and takes the
`CreateIssue` branch again → a **second** work item in Plane, with no dedup.

**Fix:** on `store.Update` failure after a successful create, retry the local
write before returning; or reconcile by searching Plane for the expected title.

### M9 — `PullStates` aborts the batch and hides which task failed {#m9}

**Where:** `internal/plane/syncer.go:163-190`

Returns on the first per-task error instead of continuing and aggregating. The
`/pull` caller reports only a count and the message — the user cannot tell which
tasks were skipped. Note `syncAll` (`tui.go:1783-1803`) does this correctly with a
`pushed/failed` summary; `PullStates` should match it.

### M10 — Agent max-steps exhaustion leaves dangling history {#m10}

**Where:** `internal/agent/agent.go:98-130`

On 8 tool rounds `Send` returns an error, but `a.messages` already has 8 rounds of
tool-call + tool-result appended with no rollback and no trailing assistant text.
A subsequent `Send` starts from a dangling tool-call turn.

**Test gap:** `agent_test.go` covers only the success path. Add a provider that
always returns `ToolCalls`, assert the max-steps error **and** the resulting
`History()` state.

### M11 — `internal/tui` imports concrete adapters {#m11}

**Where:** `internal/tui/config.go:12-14, 217, 231-233` vs `internal/tui/tui.go:34-39`

`tui.go` defines `Syncer` and `Telegram` ports; `config.go`, in the same package,
imports `internal/plane` and `internal/telegram` directly and constructs clients
ad hoc — duplicating the wiring already in `cmd/planner/main.go:163-177`. A
same-package violation of interfaces declared 200 lines away.

**Fix:** move client construction into a shared factory both `main.go` and the
config TUI call.

### M12 — Two hand-synced tool registries {#m12}

**Where:** `internal/tools/tools.go:86-238` (`Definitions`) and `241-278` (`Dispatch`)

Two independently maintained lists of the same 15 names, currently consistent but
unenforced. Adding a tool means editing both plus the handler. Every handler also
repeats the same unmarshal boilerplate (12+ times) and the mutators repeat
`pushSync` → `logActivity` → `marshal(view(t))` (4 times).

**Fix:** one table `[]struct{Name; Schema; Handler}` generating both.

### M13 — No CI {#m13}

No `.github/`, no workflow files, no pre-commit hooks anywhere. Nothing enforces
`go build` / `go vet` / `go test` on push. Notably, the branch is named
`feat/rutine-repo-actions` — that intent is entirely unstarted.

### M14 — `buildDaily` hardcodes "hoy" {#m14} **[verified]**

**Where:** `internal/tui/tui.go:1587`

`b.WriteString("\n(sin actividad registrada hoy)")` inside a function whose whole
input is a parameterized date. `/daily ayer` and `/daily 2026-01-05` both print
"hoy".

### M15 — No `list_dailies` tool {#m15}

**Where:** `internal/tools/tools.go:208-238` vs `internal/tui/tui.go:1038`

The agent has `list_day_tasks`, `save_daily`, `get_daily`, `send_daily` — but no
equivalent of `/dailies`. "mostrame los dailies" is unanswerable in conversation,
though README:145-148 sells the conversational path as equivalent.
Also `renderToolEvents` (`tui.go:748-774`) special-cases `save_daily`/`send_daily`
but not `get_daily`/`list_day_tasks`, which fall through to a generic label.

---

## New findings {#new}

Discovered while remediating, not in the original sweep. Filed here to be worked
afterwards, not folded silently into the fix that surfaced them.

### N1 — `persistDaily` discards the save error {#n1}

**Where:** `internal/tui/tui.go:1457-1461`

```go
func (m *chatModel) persistDaily(dateKey, content string) {
	if m.deps.Dailies != nil && strings.TrimSpace(content) != "" {
		_ = m.deps.Dailies.SaveDaily(context.Background(), dateKey, content)
	}
}
```

Surfaced while fixing [C3](#c3): the write outcome is thrown away, and the caller
unconditionally announces `daily (<date>) ready`.

**Failure scenario:** the DB is locked ([M7](#m7)) or the disk is full. `SaveDaily`
fails, nothing is stored, and the TUI still says the daily is ready and offers
`/daily send`. The user later runs `/daily show` and finds nothing — or worse,
`/daily edit` and silently starts from an empty draft. The in-memory
`m.dailyDraft` masks the loss for the rest of the session, so it only becomes
visible after a restart.

Note the same call uses `context.Background()` — it is also part of [C1](#c1)'s
missing-timeout surface.

**Fix:** return the error and let the caller report it; only claim "ready" once
the write succeeded.

---

## LOW {#low}

| ID | Where | Item |
| -- | ----- | ---- |
| L1 | `tui.go` ×25 (`1003, 1015, 1030, 1110, 1249, 1385, 1506, 1520, 1544, 1601, …`) | The `if err != nil { m.add("err", …); return }` idiom is duplicated 25 times while the existing `report()` helper (`1062-1068`) is used only 4 times. |
| L2 | `tui.go:75, 80-86, 2434` | Nine lines of commented-out code referencing `inputBG`, an identifier that no longer exists anywhere — it would not even compile if uncommented. Dead remnant of a reverted theme. |
| L3 | `plane/syncer.go:17` | Comment says `stateDefaults` is "reserved for state mapping" (it is actively used at `114-116`) and that it holds state **names** (it holds **ids** — `config.go:23`, `tui/config.go:270`). Wrong on both counts. |
| L4 | `main.go:106` | `usage()` advertises a stale command subset — missing `/state`, `/drop`, `/sync`, `/pull`, `/project`, `/person`, `/fav`. |
| L5 | README:25-30, 88-100 | Undocumented: `/todos`, `/exit`, `/q` aliases; `ctrl+u`/`ctrl+d` scroll; `ctrl+p`/`ctrl+n` menu nav; `planner chat`; `planner -h`; the `/state` picker keys. (No documented-but-missing commands — every README command resolves.) |
| L6 | `tui.go:1609, 1620, 1776, 678-721, 1806-1912` | The Spanish/English boundary drifts field by field rather than following a documented rule: English `projects · %d` header over a Spanish `Notas` block; English command chrome with fully Spanish `buildMentionContext`; Spanish `Estado`/`Objetivo` labels under an English `/task` usage string. |
| L7 | `tui.go:2405-2408` | `vpH := m.height - len(m.suggestions) - inputH - 5` — `inputH` is named, the sibling `5` bundles five distinct rows into a literal nothing forces to stay in sync with `View()`. |
| L8 | `go.mod:3` | `go 1.26.4` pins a patch-level toolchain in the `go` directive; anyone on 1.26.0–1.26.3 is forced into a toolchain download. Convention is `go 1.26` + a separate `toolchain` line. |
| L9 | `store/context.go:16-35, 86-103` | `slug`/`nick` are `COLLATE NOCASE PRIMARY KEY`, so `ON CONFLICT DO UPDATE` matches case-insensitively but never rewrites the stored casing. Create `Liquida`, upsert `liquida` → DB keeps `Liquida` but the returned view says `+liquida`. |
| L10 | `llm/openai.go:139-141`, `llm/claude.go:148-150` | Every non-2xx (including 429) is a terminal error — no `Retry-After`, no single backoff retry. Visible, not auto-recovered. Acceptable for a local tool; noted for completeness. |
| L11 | `telegram/client.go:72-83` | If Telegram rejects the HTML for an edge case, the send fails outright with no retry using plain text. `toHTML` always produces balanced tags so this is unlikely, but there is no degradation path. |
| L12 | `plane/client.go:190-192` | The raw Plane response body is echoed verbatim into the TUI. No credential leak (the token travels in the `X-API-Key` header, never the URL), but unfiltered server text reaches the screen. |
| L13 | git | `feat/rutine-repo-actions` is a bare pointer at `main` — 0 ahead, 0 behind, clean tree, already pushed. Nothing half-landed because nothing is on it. |
| L14 | `.gitignore` | `*.db` is ignored; `config.json` (which holds the actual secrets) is not explicitly listed. Nothing indicates it is tracked today, but the guard is missing. |

---

## Test gaps worth closing

| Priority | St | Test to add |
| -------- | -- | ----------- |
| 1 | ⬜ | `Fit` never returns a system-only window when the input has ≥1 non-system message ([C2](#c2)) |
| 2 | ✅ | LLM-failure path does **not** overwrite the stored daily ([C3](#c3)) — 3 tests |
| 3 | ✅ | The Telegram bot token never appears in any error returned by `Send` ([H1](#h1)) |
| 4 | ⬜ | `"hoy"` and the equivalent `YYYY-MM-DD` return the same task set across the offset boundary ([H2](#h2)) — the current `TestParseDay` asserts formatting, not behavior |
| 5 | ⬜ | `Dispatch` with malformed JSON returns an error for `send_daily`, `get_daily`, `list_day_tasks`, `list_tasks` ([H6](#h6)) |
| 6 | ⬜ | A >4096-char daily is chunked or truncated, not dropped ([H3](#h3)) |
| 7 | ⬜ | Agent max-steps exhaustion: error returned, and `History()` left in a defined state ([M10](#m10)) |
| 8 | ⬜ | `PullStates` behavior when task A fails and task B follows ([M9](#m9)) |
| 9 | ⬜ | `persistDaily` failure is reported instead of announcing "ready" ([N1](#n1)) |

---

## Verified clean (negative results)

Recorded so future audits don't re-litigate these:

- **SQL injection:** every query with user/task data uses `?` placeholders. The
  only concatenated SQL (`sqlite.go:136-159` `ensureColumn`, `context.go:155-171`
  `addNote`) interpolates hardcoded internal constants only.
- **Migrations:** no `DROP TABLE` anywhere. `migrate()` is `CREATE IF NOT EXISTS`
  + additive `ALTER TABLE` + one idempotent legacy-status remap. No upgrade path
  loses data.
- **Command injection:** `memory.go:38-40` uses `exec.CommandContext(ctx, name,
  args...)` — discrete argv, no shell. LLM-controlled content cannot inject.
- **TLS:** no `InsecureSkipVerify`, no custom `TLSClientConfig`. Default
  verification everywhere.
- **Stored XSS into Plane:** `domain.RenderHTML()` escapes every field via
  `html.EscapeString` before composing `description_html`.
- **Plane token handling:** header-only (`client.go:179`), never in the URL — so
  it cannot leak the way the Telegram token does ([H1](#h1)).
- **Context propagation in LLM adapters:** `http.NewRequestWithContext` in all
  three providers; 120s timeouts; non-2xx bodies read into the error; malformed
  JSON wrapped; `len(out.Choices)==0` guarded against index panics.
- **`domain/task.go`:** no dependency on `store`, `llm`, `plane` or `tui` — the
  domain layer itself is intact. `Valid()`, `PlaneGroup()`, `DisplayTitle()` and
  `RenderHTML()` are correctly covered including the empty-task edge case.
- **`Syncer.Push` id-before-rename ordering** is deliberate and tested
  (`TestSyncerPushPersistsIDBeforeRename`).
- **`go mod tidy`** produces no diff; all five direct requires are imported and
  every import has a require. No `vendor/`, no `replace`, no retracted versions.
- **No TODO/FIXME/XXX/HACK markers and no `panic(` calls** anywhere in the repo.
- **README command coverage:** every documented slash command resolves to a real
  dispatch case (see [L5](#low) for the reverse direction).
- **`planner config` secret display** masks correctly (`config.go:467,485-493`).

---

## Suggested order of attack

1. ✅ **Stop the bleeding — data loss and credential leak:** ~~[C3](#c3)~~,
   ~~[H1](#h1)~~ done. **[M1](#m1) still open** (config file perms + atomic write).
2. **Correctness:** [C2](#c2), [H2](#h2), [H6](#h6), [M14](#m14). Also small, and
   [C2](#c2)/[H2](#h2) are the two most likely to be silently wrong right now.
3. **Unfreeze the UI:** [C1](#c1). Larger — it touches every blocking branch of
   `runCommand` — but it is the worst user-facing failure.
4. **Collapse the daily spec:** [H5](#h5) → one `internal/daily` package. Do this
   *before* [H4](#h4), since the renderer should consume the same constants.
5. **Then [H4](#h4)** (render + wrap), which also fixes the selection desync.
6. **Structural:** [M3](#m3) file split, [M12](#m12) table-driven tools,
   [M11](#m11) shared adapter factory. Pure refactors — land them behind the
   tests added in steps 1–2, never before.
7. **[M13](#m13) CI last**, so it locks in everything above.
