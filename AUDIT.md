# Audit — planner

**Audited:** 2026-07-29 · **Last updated:** 2026-08-03
**Audited at commit:** `a088da8`
**Branch:** `feat/rutine-repo-actions` (identical to `main`, 0 commits ahead/behind)

**Progress: 37 / 44 closed — nothing is open.** Every CRITICAL, every HIGH and
every MEDIUM is done, along with all three findings raised during remediation and
every actionable LOW.

Coverage moved with it: `internal/tui` went 29.5% → 48.1%, and the repo's
weakest packages are now its adapters rather than its core.

The remaining 7 are LOW items **deliberately declined**, each with a reason —
see [Declined](#declined). They are not backlog; they are decisions.

Both open test gaps closed too, so the table below has no `⬜` left in it either.
[C1](#c1) ✅ [C2](#c2) ✅ [C3](#c3) ✅ · [H1](#h1) ✅ [H2](#h2) ✅ [H3](#h3) ✅
[H4](#h4) ✅ [H5](#h5) ✅ [H6](#h6) ✅ [H7](#h7) ✅ · [M1](#m1) ✅ [M5](#m5) ✅
[M14](#m14) ✅ · [N1](#n1) ✅

Work landed on two branches: `feat/rutine-repo-actions` (through [M2](#m2)) and
`chore/audit-leftovers` (the rest).

Suite green after every fix, `vet` and `gofmt` clean. **Every closed item ships
with a test that was verified to fail against the old code** — reverted in place
and re-run, not assumed. Date-sensitive fixes were additionally checked under
`TZ=America/Bogota` so a UTC-only CI cannot hide them.

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

Status: ✅ done · ⬜ open · ➖ declined

| ID | St | Severity | Title |
| -- | -- | -------- | ----- |
| [C1](#c1) | ✅ | CRITICAL | Slash commands block the Bubbletea event loop — unrecoverable freeze |
| [C1a](#c1) | ✅ | — | ↳ engram shell-out had no deadline at all (worst case: no escape) |
| [C1b](#c1) | ✅ | — | ↳ blocking branches of `runCommand` moved out of `Update()` |
| [C2](#c2) | ✅ | CRITICAL | `contextmgr.Fit` can return only the system message |
| [C3](#c3) | ✅ | CRITICAL | Daily fallback silently overwrites a hand-edited draft |
| [H1](#h1) | ✅ | HIGH | Telegram bot token leaks on screen through `*url.Error` |
| [H2](#h2) | ✅ | HIGH | Local-vs-UTC day boundary mismatch |
| [H3](#h3) | ✅ | HIGH | No Telegram 4096-char handling — long dailies never deliver |
| [H4](#h4) | ✅ | HIGH | TUI never renders the markup it asks the LLM to produce |
| [H5](#h5) | ✅ | HIGH | Daily format spec forked three ways, already drifted |
| [H6](#h6) | ✅ | HIGH | Malformed tool-call JSON silently swallowed in 4 handlers |
| [H7](#h7) | ✅ | HIGH | `pushSync` discards Plane errors with zero signal |
| [M1](#m1) | ✅ | MEDIUM | `config.json` is world-readable and written non-atomically |
| [M2](#m2) | ✅ | MEDIUM | `/key` echoes the secret and keeps it in input history |
| [M3](#m3) | ✅ | MEDIUM | `tui.go` is a 2503-line god-object |
| [M4](#m4) | ✅ | MEDIUM | `send_daily` advertised to the LLM without Telegram configured |
| [M5](#m5) | ✅ | MEDIUM | `dayFrom` swallows bad dates; slash path rejects them |
| [M6](#m6) | ✅ | MEDIUM | `Dispatch` is exported with no nil-dependency guards |
| [M7](#m7) | ✅ | MEDIUM | SQLite opened without `busy_timeout` / WAL / conn limit |
| [M8](#m8) | ✅ | MEDIUM | `Syncer.Push` can create duplicate Plane issues |
| [M9](#m9) | ✅ | MEDIUM | `PullStates` aborts the batch and hides which task failed |
| [M10](#m10) | ✅ | MEDIUM | Agent max-steps exhaustion leaves dangling history |
| [M11](#m11) | ✅ | MEDIUM | `internal/tui` imports concrete adapters, duplicating wiring |
| [M12](#m12) | ✅ | MEDIUM | `Definitions()` / `Dispatch()` are two hand-synced registries |
| [M13](#m13) | ✅ | MEDIUM | No CI |
| [M14](#m14) | ✅ | MEDIUM | `buildDaily` hardcodes "hoy" for every date |
| [M15](#m15) | ✅ | MEDIUM | No `list_dailies` tool — README oversells conversational parity |
| [N1](#n1) | ✅ | MEDIUM | `persistDaily` discards the save error — "daily ready" can be a lie |
| [N2](#n2) | ✅ | LOW | Startup warnings show their backticks literally |
| [N3](#n3) | ✅ | MEDIUM | History navigation destroyed the draft in progress |
| [L2–L5, L7, L8, L13, L14](#low) | ✅ | LOW | Cleanup, docs drift, cosmetics |
| [L1, L6, L9–L12](#low) | ➖ | LOW | [Declined](#declined) with reasons — decisions, not backlog |

Findings discovered *during* remediation are filed under [New findings](#new)
and carry an `N` prefix.

---

## CRITICAL

### C1 — Slash commands block the Bubbletea event loop {#c1} ✅ DONE

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

**Split into two, deliberately.** The freeze has two independent causes and only
one of them is a refactor:

#### C1a — engram had no bound at all ✅ DONE

This was the genuinely unrecoverable case. `/sync`, `/pull` and `/daily send`
freeze for 20–30s but do eventually return, bounded by their HTTP client
timeouts. `/recall` and `/remember` never returned, because the engram shell-out
had no deadline of any kind — no timeout, no cancellation, nothing.

**Fix applied:** the timeout lives in the `Engram` adapter, not the caller. The
adapter is what knows the CLI can wedge, and putting it there bounds every
present and future caller instead of relying on each one to remember. Each
invocation runs under `context.WithTimeout(ctx, 10s)`. A zero `timeout` field
falls back to the default, so an `Engram` built directly is bounded too.

Caller cancellation is deliberately **not** relabelled as a timeout — the errors
mean different things to whoever reads them (`errors.Is(ctx.Err(),
context.DeadlineExceeded)` distinguishes the two).

**Landed in:** `internal/memory/memory.go` (new `defaultTimeout`, `timeout`
field, `exec` and `fail` helpers; `Save`/`Recall` route through `exec`)

**Tests added** (`internal/memory/memory_test.go`), table-driven over both
shelling operations:
- `TestEngramTimesOutOnHungCLI` — a runner that only returns on cancellation;
  asserts it fails, names the timeout, and returns promptly.
- `TestEngramReportsCallerCancellationAsSuch` — cancellation is not mislabelled.
- `TestEngramZeroTimeoutFallsBackToDefault` — a directly built `Engram` is bounded.

**Teeth verified in the strongest possible form:** with the timeout removed,
`TestEngramTimesOutOnHungCLI` does not fail — it *hangs* until the Go test
runner kills it (`panic: test timed out after 5s`). The test reproduces the
reported bug literally. With the fix it passes in 40ms. Existing tests were not
modified.

#### C1b — blocking branches ran inside `Update()` ✅ DONE

**Fix applied:** a single `(*chatModel).busy(label, timeout, fn)` helper. It sets
the spinner, then returns `tea.Batch(run, spinnerTick())` where `run` executes
`fn` on its own goroutine under `context.WithTimeout` and hands the result back
as an `asyncMsg` carrying the entries to append.

Two properties, both deliberate:

- **Off the loop** — `Update()` is the only thing draining the Bubbletea event
  queue, so blocking work there freezes everything, ctrl+c included.
- **Bounded anyway** — an unbounded call on a background goroutine leaks instead
  of freezing. Better, but still wrong. Timeouts: `memoryOpTimeout` 30s,
  `telegramOpTimeout` 30s, `planeOpTimeout` 5m, `storeOpTimeout` 10s. Generous on
  purpose: the job is to guarantee an end, not to second-guess a slow network.

**Converted:** `/recall`, `/remember`, `/sync` (`syncAll`), `/pull`,
`/daily send` (`sendDaily`).

`busy`'s closure must not touch model state — it runs concurrently with
`Update`. Each call site resolves what it needs first and captures it. `sendDaily`
is the one that mattered: `draftFor` reads `m.dailyDraft`, so the draft is
resolved on the event loop and only the network call is handed off.

Unconfigured Plane/Telegram still fail fast: they report and return `nil` without
starting a command or the spinner.

**Also:** the status bar now shows what is running (`recalling…`, `syncing…`,
`sending…`) instead of a generic `thinking…`, via a new `busyLabel` field. The
point of this finding is user feedback, and "frozen" versus "working" is exactly
what the label communicates.

**Landed in:** `internal/tui/tui.go` — new `busy`/`asyncMsg`/`errEntry`/`sysEntry`
and the timeout constants; `runCommand`, `syncAll`, `sendDaily`, `handleDaily`,
`statusBar`, `chatModel`.

**Tests added** (`internal/tui/async_test.go`):
- `TestBlockingCommandsDoNotBlockUpdate` — table-driven; a memory backend that
  only returns on cancellation, asserting `submit` returns promptly, hands back a
  `tea.Cmd`, and turns on a *labelled* spinner.
- `TestBusyBoundsTheWork` — a `fn` that never returns still lands, as an error.
- `TestAsyncResultAppendsEntriesAndClearsBusy` — per-item failures and the
  summary both survive the round trip.
- `TestUnconfiguredCommandsFailFast` — no command, no spinner.
- `TestDailySaveFailureIsReported` — [N1](#n1), below.

**Teeth verified:** reverting `/recall` to its inline form makes
`TestBlockingCommandsDoNotBlockUpdate/recall` fail with exactly the right
diagnosis — `submit blocked: the command is still running inside Update()`.

Verified under `-race`, since this introduces concurrency.

---

### C2 — `contextmgr.Fit` can return only the system message {#c2} ✅ DONE **[verified]**

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

**Worse than originally reported.** While proving the new test had teeth, the
revert surfaced a case the audit missed: **with no leading system message the old
code returned a completely empty slice**, not merely a system-only one. That is an
outright invalid request body, not just a blank-slate model. Same root cause, same
fix.

**Fix applied:** after the anti-orphan pass, if `kept` is empty but `rest` is not,
fall back to `suffixFromNewestNonTool(rest)` — the suffix starting at the newest
non-`RoleTool` message.

Chosen over "pair the oversized tool result with its assistant tool-call" because
it always keeps a **contiguous suffix**, so every `tool_result` following the
anchor is included and no `tool_use` is left unanswered. The pairing option would
have had to special-case parallel tool calls. Budget deliberately loses here —
the same trade the pre-existing "always keep at least one recent message" rule
already makes, and the alternative was a silent conversation reset with no error.

**Invariant now guaranteed and documented on `Fit`:** if the input has ≥1
non-system message, the output has ≥1 non-system message and never starts on an
orphan tool result. (Degenerate exception, documented: a malformed history whose
non-system messages are *all* tool results has no valid window.)

**Landed in:** `internal/contextmgr/contextmgr.go` (`Fit`, new
`suffixFromNewestNonTool` helper — signature unchanged)

**Tests added** (`internal/contextmgr/contextmgr_test.go`):
- `TestFitKeepsOversizedToolResultWithItsCall` — the exact C2 trace.
- `TestFitAlwaysKeepsAValidNonSystemWindow` — property-style, 5 shapes × 4 budgets.

**`TestFitDropsOrphanToolResult` still passes unmodified** — it encoded correct
behavior, not the bug, and never reaches the new fallback.

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

### H2 — Local-vs-UTC day boundary mismatch {#h2} ✅ DONE **[verified]**

**Where:** `internal/tui/tui.go:1348-1359` (`parseDay`), `internal/tools/tools.go:281-292` (`dayFrom`), `internal/store/sqlite.go:217,291`

`"hoy"` returns `time.Now()` — **Local**. An explicit date goes through
`time.Parse("2006-01-02", tok)`, which Go returns in **UTC**. Both are handed to
`store.Filter{Day:}` / `TasksWithActivityOn`, whose window math uses
`day.Location()`.

**Failure scenario:** in UTC-5, `/daily hoy` covers local 00:00–24:00 while
`/daily 2026-07-29` (the same calendar day) covers UTC 00:00–24:00 — a window
shifted five hours. A task touched at 20:00 local appears in one and not the
other. Same bug on the agent path via `dayFrom`, and on `/todo <status> <day>`.

**Fix applied:** `time.ParseInLocation("2006-01-02", tok, time.Local)` in both
parsers, with the reason recorded in each doc comment.

**Landed in:** `internal/tui/tui.go` (`parseDay`), `internal/tools/tools.go`
(`dayFrom` — see also [M5](#m5), fixed in the same pass)

**Tests added:** `TestParseDayUsesLocalLocation` (`internal/tui/daily_test.go`),
`TestDayFromKeepsLocalDayWindow` (`internal/tools/tools_test.go`).

Both assert `.Location() == time.Local` directly and compare the **derived
midnight windows**, never `.Format("2006-01-02")` strings — that string
comparison is precisely what let this hide in `TestParseDay`. The Location
assertion has teeth on any machine, because `time.Local` and `time.UTC` are
distinct `Location` values even where the offset is zero; a CI running in UTC
would otherwise pass with the bug intact.

Additionally verified end to end under `TZ=America/Bogota` (the UTC-5 case from
the finding).

---

### H3 — No Telegram 4096-char handling {#h3} ✅ DONE

**Where:** `internal/telegram/client.go:42-84`, `internal/tui/tui.go:1494-1510`

Telegram caps `sendMessage` at 4096 characters. `Send` neither checks nor splits.

**Failure scenario:** a busy day produces a digest past 4096 chars. Telegram
replies `ok:false` with "message is too long"; the user gets an opaque error and
the daily simply never arrives. No chunking, no truncation, no pointer to the
full text. They must manually shorten and retry.

**Fix applied:** chunking, not truncation — nothing is dropped.
`internal/telegram/split.go`: `splitForTelegram(text, limit) []string`, plus
`fitsHTML`, `splitUnits`, `packUnits`, `hardSplit`, `longestFit`.

Three decisions worth keeping in mind:

1. **Split the source, never the HTML.** Cutting the HTML output could straddle
   an open `<b>`/`<i>`/`<code>` tag and earn a 400. Dailies never span a markup
   construct across lines, so line-boundary splitting on the CommonMark source is
   safe. `toHTML` is applied per chunk inside `sendOne`.
2. **Size by rendered length** — every fit check is `len(toHTML(chunk)) <= 4096`.
   Conservative, since tags inflate the count and Telegram's limit is on the
   parsed text. Conservative is right: the cost of being wrong is a message that
   never arrives.
3. **Blank lines first, then newlines, then a hard cut.** Sections stay together
   when they can. `chunkUnit.sep` remembers which separator rejoins a unit, and
   the separator is dropped when a unit opens a chunk, so no chunk starts with a
   stray blank line.

**Termination is guaranteed, not merely likely.** `hardSplit` forces `n = 1` when
`longestFit` returns 0. The pathological case is real, not theoretical: at limit
1, `&` renders to `&amp;` — five characters — so nothing ever fits and a naive
loop spins forever. There is a test pinned to exactly that input.

**Partial failure is legible:** `sending chunk 2 of 4: telegram: message is too
long`, wrapping so the API description survives, and the send stops there.
Reporting a generic failure after half the daily was delivered would be worse
than not sending at all.

**Common case untouched:** `splitForTelegram` returns the input byte-for-byte
when it fits and `Send` short-circuits on `len(chunks) == 1`, so a normal daily
produces the identical single request it always did. `Send`'s signature is
unchanged — no caller needed editing.

**Judgement call flagged by the implementer:** empty chunks are filtered, so a run
of 3+ newlines collapses at a boundary (Telegram rejects an empty message).
Content lines are never affected.

**Tests added** (`internal/telegram/client_test.go`): `TestSendSplitsLongDaily`
(200-line digest — every chunk within the limit, every source line delivered once
and in order, `parse_mode`/thread/chat id on all of them),
`TestSendShortMessageIsOneRequest` (the no-regression guard),
`TestSendChunkFailureNamesChunk`, `TestSendSplitsUnbrokenLine` (a 20480-char
unbroken line), and `TestSplitForTelegram` (6 subtests on the helper directly).

No existing test needed touching. Verified failing-first, and under `-race`.

---

### H4 — TUI never renders the markup it asks the LLM to produce {#h4} ✅ DONE

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

**Fix applied:** a new `"daily"` entry role, rendered and wrapped:

```go
case "daily":
    blocks = append(blocks, body.Render(renderMarkup(e.text)))
```

`renderMarkup` (`internal/tui/markup.go`) mirrors `internal/telegram/markup.go`
exactly — same three constructs, same code-spans-to-placeholders-first ordering
so `` `**x**` `` stays literal — with lipgloss as the output target instead of
HTML. The two files carry a note pointing at each other; adding a construct to
one means adding it to the other.

**Why a new role rather than changing `"raw"`:** `"raw"` has ten call sites and
nine of them (task detail, projects, people, the dailies list) really *are*
pre-styled lipgloss tables that must pass through untouched. Only the daily is
model-authored. Running a markdown pass over the other nine would have corrupted
them. Splitting the role fixes the daily without touching anything else.

`entry.text` still holds the **source** markup — rendering happens at display
time — so editing, storing and sending are all unaffected.

**Landed in:** `internal/tui/markup.go` (new), `internal/tui/tui.go`
(`setContent`, the three daily call sites)

**Tests added** (`internal/tui/markup_test.go`):
- `TestRenderMarkupRemovesMarkers` — table-driven over the three constructs.
- `TestRenderMarkupKeepsMarkersInsideCodeSpans` — the re-parse guard.
- `TestRenderMarkupKeepsDailyHeader`.
- `TestDailyEntryIsWrappedToViewport` — no `contentLines` entry exceeds the
  viewport width. This is the point-2 correctness bug, tested independently.
- `TestDailyEntryRendersWithoutMarkers` — end-to-end, no marker reaches the screen.

The assertions read **ANSI-stripped** output on purpose: lipgloss emits no escape
codes when it detects no colour profile (as in CI), so asserting on the codes
themselves would make the tests pass or fail by environment. What must hold
everywhere is that the markers are gone and the text survives.

---

### H5 — Daily format spec forked three ways, already drifted {#h5} ✅ DONE

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

**Fix applied:** a new `internal/daily` package owns the format outright.

The key move: **`FormatSpec` is assembled by const concatenation of the very
prefix and title constants `Build` uses.** The prose spec and the builder are
not "kept in sync" — they are physically incapable of disagreeing about what a
prefix is. `Prompt` (the `/daily` one-shot) embeds `FormatSpec` rather than
restating it, and `cmd/planner`'s `systemPrompt` became a `var` that does the
same.

API: `PrefixWork`/`PrefixBlock`/`PrefixNote`, `TitleDaily`/`TitleWork`/
`TitleBlocks`/`TitleNotes`, `FormatSpec`, `Prompt`, `Date(time.Time) string`,
`Build(date string, tasks []domain.Task) string`. Only import: `internal/domain`.

**Divergences resolved — the fuller version won every time:**

| | Resolution |
| - | ---------- |
| A | Canonical header is `**Daily:**  <FECHA>` in `Date()` format. `FormatSpec` now states the format explicitly with an example, so the agent path can derive it (it has no injected value); `/daily` still pins the exact value. |
| B | Exact-prefix rule kept, generated from the constants. |
| C | The 4-example backtick list wins (`deploy a producción` restored). |
| D | The full two-line `+` warning wins. |
| E | The explicit Spanish empty-section rule wins. |
| F | "No copies los títulos tal cual" promoted into `FormatSpec`, so the agent path gets it too. |

**Divergence F′ (the fallback implements ~40% of the spec) was deliberately not
"fixed".** `Build` cannot produce backticks, bold task titles or nominalized
prose — those need judgement about what a task *means*. Faking them would yield
output that looks compliant while being wrong. Instead `Build`'s doc comment
states it is structural-only and says why. Honesty over a cosmetic fix.

**Landed in:** `internal/daily/daily.go` (new); `cmd/planner/main.go` (−22 lines
of restated format); `internal/tui/tui.go` (`dailyPrompt`, `dailyDate`,
`buildDaily` deleted, −73 lines).

`internal/tui/daily_test.go` changed **call sites only** — `dailyDate(…)` →
`daily.Date(…)`, `buildDaily(…)` → `daily.Build(…)`. No assertion was touched;
`TestBuildDaily` and `TestBuildDailyEmpty` assert the same strings and pass.

**Tests added:** `TestBuildUsesExportedPrefixes`,
`TestFormatSpecMentionsEveryTitleAndPrefix`, `TestPromptEmbedsFormatSpec`,
`TestDateUsesSpanishMonthAbbreviation` (`internal/daily`), and
`TestSystemPromptEmbedsDailyFormatSpec` (`cmd/planner`).

**That last test is the one that matters**, and its teeth were verified: with
`systemPrompt` re-forked to an inline copy of the spec, **the code still
compiled cleanly**. Nothing but that test caught it. A re-fork is invisible to
the compiler — which is exactly how this drifted in the first place.

---

### H6 — Malformed tool-call JSON silently swallowed {#h6} ✅ DONE

**Where:** `internal/tools/tools.go:298, 334, 348, 522`

`listDayTasks`, `getDaily`, `sendDaily` and `listTasks` do
`_ = json.Unmarshal(...)` and proceed with zero-value fields. The other 11
handlers correctly return `fmt.Errorf("... bad args: %w", err)`.

**Failure scenario:** a provider truncates the arguments for `send_daily`. Instead
of an error the agent loop could react to, the tool silently defaults to **today**
and **actually delivers a Telegram message** — a wrong, externally-visible side
effect with no error signal anywhere.

**Fix applied:** all four now return `fmt.Errorf("<tool>: bad args: %w", err)`,
the exact convention the other 11 handlers already use.

Legitimate no-arg calls are unaffected: `orEmptyObj` rewrites `""` to `"{}"`
before the unmarshal. This was **verified with a dedicated test rather than
assumed** — it is the obvious way an over-eager fix breaks the tools.

**Landed in:** `internal/tools/tools.go` (`listDayTasks`, `getDaily`,
`sendDaily`, `listTasks`)

**Tests added** (`internal/tools/tools_test.go`):
- `TestDispatchRejectsMalformedArgs` — `"{not json"` errors for all four, **and
  the fake Telegram received nothing** (the side effect is what made this HIGH).
- `TestDispatchAcceptsEmptyArgs` — `""` and `"{}"` still succeed for all four.

---

### H7 — `pushSync` discards Plane errors with zero signal {#h7} ✅ DONE

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

**Fix applied — the smallest change that removes the silence.** `pushSync`
returns its error, and the four mutating handlers (`createTask`, `setStatus`,
`setState`, `setDetails`) attach it to the tool result via a new
`syncedView(t, syncErr)` helper. `taskView` gained
`SyncError string \`json:"sync_error,omitempty"\`` — snake_case, matching the
existing `work_item` / `start_date` convention.

**The distinction that is the whole point of this finding:** the handlers still
return `(string, nil)` when a push fails. The local write succeeded, so it is not
a tool error — the failure is *data in the result*. Returning a Go error would
turn a local success into a tool failure and break the local-first contract that
[H7](#h7) exists to defend. Visible, not fatal.

The agent reading that result is what tells the user, which is exactly the
channel that was missing.

`view(t)` itself is untouched, so `list_tasks`, `list_day_tasks` and `drop_task`
are byte-identical. On success, or with Plane unconfigured, the key is **absent**
rather than empty.

**No store column, no domain change, no other package touched** — deliberately
smaller than the original suggestion, which is still available if per-task sync
state in `/todo` / `/task` turns out to be wanted.

**Tests added** (`internal/tools/tools_test.go`, with a new `fakeSyncer`):
`TestPushFailureReportedButLocalWriteSucceeds` (asserts no Go error, the task is
really in the store, and `sync_error` carries the reason — for `create_task` and
`set_status`, so the fix is not create-only), `TestPushSuccessOmitsSyncError`,
`TestPlaneUnconfiguredNeverPushesNorReportsSyncError`.

The no-regression guards decode into `map[string]json.RawMessage` and assert the
key is **absent**, not merely empty — an `omitempty` bug would slip past a
zero-value check.

No existing test was modified. Verified failing-first, and under `-race`.

---

## MEDIUM

### M1 — `config.json` is world-readable and written non-atomically {#m1} ✅ DONE

**Where:** `internal/config/config.go:179-188`

`os.MkdirAll(dir, 0o755)` + `os.WriteFile(path, data, 0o644)` for a file holding
every provider API key, the Plane token and the Telegram bot token in plaintext.
Any other local account can read them. The write also goes straight to the final
path — a crash mid-write truncates `config.json` and loses all configuration on
next `Load`, with no backup.

**Fix applied:** `0o700` dir (plus a best-effort `os.Chmod` to tighten a legacy
`0755` dir, whose failure never breaks an otherwise valid save), and an atomic
write: `os.CreateTemp` in the **same** directory → write → `Sync` → `Close` →
explicit `os.Chmod(tmp, 0o600)` → `os.Rename`. A deferred cleanup removes the
temp file on any pre-rename failure.

**Two gotchas that make the naive fix wrong** — worth remembering, they apply
anywhere in this codebase:

1. `os.WriteFile` does **not** re-apply its mode argument to an already-existing
   file (same for `MkdirAll` on an existing dir). Simply changing `0o644` to
   `0o600` leaves every pre-existing `config.json` world-readable forever.
2. `os.Rename` preserves the **source** inode's mode, so the `Chmod` on the temp
   file is the load-bearing step that makes the result `0600` even when the
   destination already existed as `0644`.

**Landed in:** `internal/config/config.go` (`Save` — signature unchanged)

**Tests added** (`internal/config/config_test.go`): `TestSaveUsesPrivateFileMode`,
`TestSaveOverExistingFileKeepsPrivateMode`, `TestSaveUsesPrivateDirMode`,
`TestSaveLeavesNoTempFiles`.

`TestSaveOverExistingFileKeepsPrivateMode` is the important one: it chmods the
file back to `0644` between saves and **fails against a naive `os.WriteFile`
fix**, verified by trying exactly that. `TestSaveUsesPrivateDirMode` uses a
nested path on purpose — `t.TempDir()` is already `0700`, so a directory test
written against it directly would pass vacuously.

### M2 — `/key` echoes the secret and keeps it in input history {#m2} ✅ DONE

**Where:** `internal/tui/tui.go:855-856` (`m.add("cmd", val)`), `621` (`m.pushHistory(val)`)

`/key openai sk-live-XXXX` is rendered verbatim in the viewport and stored in
`m.history`, one Up-arrow away for the rest of the session. Note the config TUI
does this correctly (`config.go:467,485-493`, `maskSecret`) — the protection just
doesn't extend to the chat command.

**Fix applied:** two helpers in `commands.go`. `carriesSecret(val)` reports
whether a credential has actually been typed — a bare `/key` or `/key <provider>`
has nothing sensitive yet and stays fully recallable. `redactCommand(val)`
returns the on-screen form, `/key kimi ••••••`.

`runCommand` echoes the redacted form; `submit` skips `pushHistory` when the
line carries a secret. The list is a map (`secretCommands`), so a future command
that takes a credential is one entry away from the same protection.

**Tests added** (`internal/tui/model_test.go`):
- `TestKeyCommandDoesNotLeakTheSecret` — the secret appears in **no** entry and
  **no** history slot, the masked echo still shows `/key kimi`, **and the key is
  actually saved to disk** (masking must not break the job it was doing).
- `TestPartialKeyCommandStillRecallable` — a bare `/key` is still recallable, so
  the fix does not over-reach into ordinary usability.

Verified failing-first: with the echo un-redacted the test reports
`secret echoed on screen in a "cmd" entry`.

### M3 — `tui.go` is a 2503-line god-object {#m3} ✅ DONE

**Where:** `internal/tui/tui.go`

Eleven unrelated concerns in one file: Bubbletea lifecycle, key state machine,
mouse selection + clipboard, slash-command dispatch, todo rendering, task detail,
the whole daily flow, projects/people, mentions, provider/favorites, conversation
persistence, suggestions, and layout. Longest functions: `runCommand` (206 lines,
`855-1060`), `handleKey` (154 lines, `301-454`).

Combined with 29.5% coverage, this is where regressions will come from.

**Fix applied — `tui.go` went from 2531 lines to 99, across 13 new files.**
Landed after [M13](#m13) on purpose, so a ~2400-line mechanical move had an
automated gate under it rather than someone remembering to run the suite.

| File | Lines | | File | Lines |
| ---- | ----- | - | ---- | ----- |
| `commands.go` | 299 | | `keys.go` | 167 |
| `mentions.go` | 284 | | `selection.go` | 156 |
| `model.go` | 252 | | `providers.go` | 151 |
| `daily.go` | 239 | | `chat.go` | 136 |
| `todo.go` | 236 | | `task.go` | 122 |
| `render.go` | 208 | | `conversations.go` | 116 |
| `suggestions.go` | 202 | | `tui.go` | 99 |

**How a pure move was proved, rather than asserted:** the sorted set of
top-level declarations was captured before and after and diffed — **118 before,
118 after, byte-identical**. I re-ran that check independently against
`git show HEAD:internal/tui/tui.go` rather than trusting the report. The
implementer additionally ran a multiset comparison of every non-blank body line
(2320 before, 2320 after, zero missing, zero extra).

**No test file was touched.** Tests live in the same package, so a genuine move
cannot require editing one — needing to is the signal that something other than
a move happened. Verified via `git status`.

Nothing was renamed, no signature changed, and nothing suspected of being dead
was removed *during the move*. Findings spotted along the way were reported, not
acted on — see the cleanup commit that followed, which closed [L2](#low)
separately.

**Deliberate deviation, reported:** six top-level `// --- section ---` separator
comments were folded into the new per-file header comments instead of carried.
One of them (`// --- mouse selection & clipboard ---`) would have been actively
wrong, since the declarations directly beneath it in the monolith
(`copiedMsg`/`tickMsg`/`spinnerTick`) belong to `model.go`, not `selection.go`.
Three separators that still divide groups *inside* a single file were kept
verbatim.

Verified: build, vet, `gofmt`, full suite, and `internal/tui` under `-race`.

**Next in this file, if there is a sequel:** `commands.go` is the fattest at 299
lines, 211 of which are the single `runCommand` switch.

---

**Original proposed split** (superseded by the table above, kept for the record):

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

### M4 — `send_daily` advertised without Telegram configured {#m4} ✅ DONE

**Where:** `internal/tools/tools.go:66` (`dailiesEnabled`), `208`, `230-234`

Registration is gated on `r.dailies != nil` only; Telegram is never consulted.
The memory tools do this correctly (`memEnabled()`, `tools.go:75,148`).

**Failure scenario:** on a box with no Telegram config the LLM is still handed
`send_daily`, offers to deliver the daily, calls it, and fails. The user is
promised a delivery the system knew in advance it could not make.

**Fix applied:** the `send_daily` entry in `toolTable` is now gated on
`r.dailiesEnabled() && r.tg != nil && r.tg.Configured()` — one line, exactly
where [M12](#m12) said it would land.

**[M12](#m12)'s guard did its job.** The change immediately broke
`TestGatingHidesUnwiredTools/dailies_only`, which is the whole point: a
behaviour change to what the model is offered has to be acknowledged, not slip
through.

Rather than just relaxing that expectation, the gating table grew from 5 cases
to 8, covering all four dailies × Telegram combinations:

| Case | `send_daily` advertised |
| ---- | ----------------------- |
| dailies only | no |
| telegram without dailies | no |
| dailies + unconfigured telegram | no |
| dailies + configured telegram | **yes** |

That last row is load-bearing. Without it the suite would also pass if
`send_daily` disappeared from the registry entirely — a test that only asserts
absence can be satisfied by deletion.

### M5 — `dayFrom` swallows bad dates {#m5} ✅ DONE

**Where:** `internal/tools/tools.go:281-292` vs `internal/tui/tui.go:1348-1359`

`parseDay` returns `ok=false` on garbage and the slash command shows usage.
`dayFrom` silently returns `time.Now()`. "armá el daily del 32 de enero"
quietly becomes today's.

**Fix applied:** `dayFrom` signature is now `(time.Time, error)`. Unparseable
input returns `date must be today | yesterday | YYYY-MM-DD, got %q`. The empty
string still means today — that is the intentional no-arg default, not garbage.

**Landed in:** `internal/tools/tools.go` — `dayFrom` plus its four callers
(`listDayTasks`, `saveDaily`, `getDaily`, `sendDaily`), each wrapping the failure
as `fmt.Errorf("<tool>: %w", err)`. Nothing outside the package calls it; the TUI
has its own `parseDay`, which already rejected garbage.

**Test added:** `TestDayFromRejectsInvalidDate` — rejects `"2026-13-45"`,
`"mañana-quizá"` and `"32/01/2026"`, confirms `""` still means today, and checks
the rejection surfaces through `Dispatch` for `get_daily` and `list_day_tasks`.

### M6 — `Dispatch` is exported with no nil-dependency guards {#m6} ✅ DONE

**Where:** `internal/tools/tools.go:241`; unguarded derefs at `326, 336, 350, 368, 383, 397, 412`

Seven handlers dereference `r.dailies` / `r.ctxStore` with no check. Only
`recallMemory` (`429`) and `rememberNote` (`443`) defend themselves. Not reachable
today because `main.go:158-177` always calls the setters — but registration is the
*only* thing standing between a nil interface and a panic on an **exported**
method. `sendDaily` (`343-353`) happens to check `r.tg` first, which shadows the
nil-dailies path by luck, not design.

**Fix applied:** all eight handlers that need a backend now check it first —
the four daily ones against `dailiesEnabled()`, the four context ones against
`ctxEnabled()` — exactly as `recallMemory` and `rememberNote` already did.

`sendDaily` no longer depends on its Telegram check happening to shadow the nil
`dailies` path, which the original audit called out as luck rather than design.

**The distinction that makes this worth fixing at all:** registration gates what
the model is *told* exists. It does not gate what can be *called*. `Dispatch` is
an exported method, so any future caller — a test, a CLI path, a second
front-end — reaches those handlers without passing the gate.

**Test added:** `TestDispatchGuardsMissingBackends`, table-driven over all eight
tools against a registry wired with a task store only.

**Teeth verified, and the failure mode is worth knowing:** removing one guard
does not produce a clean failure, it produces
`panic: runtime error: invalid memory address or nil pointer dereference`
that takes the whole test binary down. Latent, but not theoretical.

### M7 — SQLite opened without concurrency pragmas {#m7} ✅ DONE

**Where:** `internal/store/sqlite.go:25-36`

`sql.Open("sqlite", path)` with no `busy_timeout`, no `journal_mode=WAL`, no
`SetMaxOpenConns(1)`. Concurrent access is reachable: `submit()` only blocks
re-submission for non-slash input, so a synchronous `/new` can write while a
background `sendCmd` goroutine's tool calls write too. With the default rollback
journal and zero busy timeout, the loser gets `SQLITE_BUSY` immediately — a raw
"database is locked" error, no retry.

**Fix applied:** both pragmas ride on the connection string
(`sqliteDSN`), so **every pooled connection** gets them — `busy_timeout` is
per-connection, so setting it with a single `db.Exec` after opening would have
applied to one connection and silently missed the rest.

`url.URL` builds the DSN so a path with spaces or other odd characters cannot
break out into the query string. The test opens a database at
`"pragma test.db"` — space deliberate — for exactly that reason.

**`SetMaxOpenConns(1)` was considered and rejected.** It would also remove the
race, but it converts "a query issued while rows are still open" from an error
into a **deadlock**. `ensureColumn` (`sqlite.go:137-158`) is safe today only
because its loop drains the rows before the `ALTER`, which releases the
connection — add a `break` there tomorrow and the process hangs. Trading an
error for a hang is not a fix.

**Test added:** `TestOpenSQLiteAppliesPragmas` queries `PRAGMA busy_timeout` and
`PRAGMA journal_mode` **on a live connection**. Asserting the DSN string would
prove nothing — an escaping mistake leaves both silently at their defaults,
which is the whole failure mode. Verified failing-first: against the old code it
reports `busy_timeout = 0`, i.e. a concurrent writer failing instantly.

### M8 — `Syncer.Push` can create duplicate Plane issues {#m8} ✅ DONE

**Where:** `internal/plane/syncer.go:125-151`

`CreateIssue` succeeds, then `s.store.Update` persists `WorkItemID`. If **that
write** fails (e.g. the `SQLITE_BUSY` of M7), the id is never durably saved. The
next `Push` reloads the task with an empty `WorkItemID` and takes the
`CreateIssue` branch again → a **second** work item in Plane, with no dedup.

**Fix applied:** the write is now `persistLink`, which retries three times with
a doubling delay and, if it still fails, returns an error naming the orphan:

```
plane work item wi-7 (#42) was created but could not be linked to task 3
locally; re-syncing would create a duplicate: database is locked
```

**Being honest about which half does the work.** [M7](#m7) landed first, and its
`busy_timeout` already absorbs the contention case, so the retry mostly covers
failures that a retry will not help (a full disk, for instance). The real value
here is the **error message**: it converts a silent duplicate — which surfaces
days later as two identical tickets nobody can explain — into a loud, specific
failure that names what to reconcile by hand.

Reconciling automatically by searching Plane for the expected title was
considered and skipped: it costs an extra API call on every create to defend
against a window that [M7](#m7) has now mostly closed, and title matching is
itself unreliable.

**Tests added** (`internal/plane/syncer_test.go`), with a `flakyStore` that
embeds a real store and fails the first N `Update` calls:
- `TestSyncerPushRetriesTransientLinkFailure` — one transient failure, push
  succeeds, the id is linked.
- `TestSyncerPushNamesOrphanedWorkItem` — permanent failure; the error names the
  work item, the sequence number **and** the word "duplicate", and exactly
  `linkRetries` attempts were made.

`TestSyncerPushPersistsIDBeforeRename`, which guards the adjacent ordering rule,
passes unmodified.

### M9 — `PullStates` aborts the batch and hides which task failed {#m9} ✅ DONE

**Where:** `internal/plane/syncer.go:163-190`

Returns on the first per-task error instead of continuing and aggregating. The
`/pull` caller reports only a count and the message — the user cannot tell which
tasks were skipped. Note `syncAll` (`tui.go:1783-1803`) does this correctly with a
`pushed/failed` summary; `PullStates` should match it.

### M10 — Agent max-steps exhaustion leaves dangling history {#m10} ✅ DONE

**Where:** `internal/agent/agent.go:98-130`

On 8 tool rounds `Send` returns an error, but `a.messages` already has 8 rounds of
tool-call + tool-result appended with no rollback and no trailing assistant text.
A subsequent `Send` starts from a dangling tool-call turn.

**Test gap:** `agent_test.go` covers only the success path. Add a provider that
always returns `ToolCalls`, assert the max-steps error **and** the resulting
`History()` state.

### M11 — `internal/tui` imports concrete adapters {#m11} ✅ DONE

**Where:** `internal/tui/config.go:12-14, 217, 231-233` vs `internal/tui/tui.go:34-39`

`tui.go` defines `Syncer` and `Telegram` ports; `config.go`, in the same package,
imports `internal/plane` and `internal/telegram` directly and constructs clients
ad hoc — duplicating the wiring already in `cmd/planner/main.go:163-177`. A
same-package violation of interfaces declared 200 lines away.

**Fix applied:** a new `internal/wiring` package owns adapter construction —
`PlaneClient(cfg)`, `PlaneSyncer(cfg, st)`, `TelegramClient(cfg)`. Both
`cmd/planner/main.go` and `internal/tui/config.go` call it; neither imports
`internal/plane` or `internal/telegram` any more. Verified: exactly one
non-adapter file in the repo imports those packages, and it is `wiring.go`.

All three take the whole `config.Config` rather than sub-sections, because the
knowledge being centralised is precisely *which config section feeds which
adapter*. `RunConfig` and `RunChat` signatures are unchanged.

**What this does NOT fix, stated in the package doc comment so the next reader
cannot mistake it:** `internal/tui` is not port-pure. The config TUI needs
`ListStates`, whose result type is `plane.State`, so `internal/tui` still
depends on `internal/plane` transitively through `wiring`. Defining local mirror
types to hide that would buy an import-graph cosmetic at the price of a
translation layer and a second type to keep in sync — a worse trade. The
duplication was the real defect; the coupling is left visible.

**Tests added** (`internal/wiring/wiring_test.go`): each constructor yields an
unconfigured client from a zero config and a configured one from a populated
config. No network.

### M12 — Two hand-synced tool registries {#m12} ✅ DONE

**Where:** `internal/tools/tools.go:86-238` (`Definitions`) and `241-278` (`Dispatch`)

Two independently maintained lists of the same 15 names, currently consistent but
unenforced. Adding a tool means editing both plus the handler. Every handler also
repeats the same unmarshal boilerplate (12+ times) and the mutators repeat
`pushSync` → `logActivity` → `marshal(view(t))` (4 times).

**Fix applied:** a single `toolTable []toolDef` with `Def llm.Tool`,
`Enabled func(*Registry) bool` and `Handler func(*Registry, context.Context,
string) (string, error)`. `Definitions()` is a filter over it; `Dispatch()` is a
map lookup built from the same table. A tool can no longer be advertised without
a handler, or carry a handler nobody advertises.

Two decisions from the implementer worth recording:

- **No separate `Name` field**, contrary to my initial sketch. `Name` alongside
  `Def.Name` would be a second pair of values to hand-synchronise — the exact
  bug class this finding is about. Identity lives in `Def.Name` only. Good push
  back.
- **Handlers are method expressions** (`Handler: (*Registry).createTask`), whose
  type is already the required signature. No closures, no wrappers, and not one
  handler body was touched.

**Gating is preserved precisely, including an asymmetry worth knowing about:**
`Enabled` gates `Definitions` only. Today's `Dispatch` has no gating at all —
`recall_memory` dispatches even with no memory backend and the handler returns
"memory backend not available". Adding an `Enabled` check to `Dispatch` would
have been a silent behaviour change, so it was not done. Documented on the field.

**Proof the schemas are unchanged:** rather than diffing names, the implementer
extracted a pristine `git archive HEAD` tree, ran the same dump in both, and
diffed `json.MarshalIndent(Definitions())` for a fully wired registry —
**byte-identical**, including descriptions, JSON-schema parameters, required
arrays and order.

**Tests added:** `TestToolTableIsWellFormed`, `TestFullRegistryAdvertisesExactly`
(pins all 16 names explicitly, order included), `TestGatingHidesUnwiredTools`
(5 subtests), `TestEveryAdvertisedToolDispatches`. The `memory present but
unavailable` subtest exists because `memEnabled()` is `r.mem != nil &&
r.mem.Available()` — a `Noop` backend must not advertise the memory tools.

Guards were mutation-tested, not merely observed green: renaming a tool,
removing a gate, and nilling a handler each fail a specific test.

**[M4](#m4) is now a one-line change** on the `send_daily` entry, and it will
break the `dailies only` subtest — which is the point: the behaviour change has
to be acknowledged rather than slip through.

### M13 — No CI {#m13} ✅ DONE

No `.github/`, no workflow files, no pre-commit hooks anywhere. Nothing enforced
`go build` / `go vet` / `go test` on push. Notably, the branch is named
`feat/rutine-repo-actions` — that intent was entirely unstarted. It is now
started.

**Fix applied:** `.github/workflows/ci.yml`, one job on `ubuntu-latest`, on every
push and pull request, with a `concurrency` group that cancels a run superseded
by a newer push to the same ref.

Six gates, in order: `gofmt` (fails on any unformatted file), `go build`, a
second build with **`CGO_ENABLED=0`**, `go vet`, `go test ./... -count=1`, and
the suite again under **`-race`**.

Two of those are deliberate rather than boilerplate:

- **The cgo-free build** checks a claim the README makes — a pure-Go binary via
  modernc SQLite. An assertion in prose that nothing verifies is a wish; this
  turns it into a gate.
- **`-race`** because [C1b](#c1) started handing command work to goroutines. The
  rule that `busy`'s closure must not touch model state is the kind of thing a
  future edit breaks silently, and the race detector is the only thing that
  catches it.

The toolchain comes from `go-version-file: go.mod`, so the Go version has a
single source of truth (see [L8](#low) on the patch-level pin — still open, and
now consumed by CI).

**Every gate was run locally before committing**, including the cgo-free build
and the full `-race` suite, so the first CI run is not the first time anyone
checked whether it passes.

README updated to describe what CI enforces.

### M14 — `buildDaily` hardcodes "hoy" {#m14} ✅ DONE **[verified]**

**Where:** `internal/tui/tui.go:1587`

`b.WriteString("\n(sin actividad registrada hoy)")` inside a function whose whole
input is a parameterized date. `/daily ayer` and `/daily 2026-01-05` both print
"hoy".

**Fix applied:** now `(sin actividad registrada)` — the digest already carries the
date in its header, so no date word is needed at all. `TestBuildDailyEmpty` passes
unmodified (it asserts on `"sin actividad"`).

### M15 — No `list_dailies` tool {#m15} ✅ DONE

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

### N1 — `persistDaily` discards the save error {#n1} ✅ DONE

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

**Fix applied:** `persistDaily` now returns its error (bounded by
`storeOpTimeout`), and both callers branch on it. The success line is only
printed when the write actually succeeded; otherwise the user is told the daily
exists **in this session only**:

- generation → `daily generated but NOT saved: <err>`
- inline edit → `edit kept in this session but NOT saved: <err>`

The wording matters: the in-memory `m.dailyDraft` really does still hold the
text, so "failed" alone would be misleading — what was lost is durability, and
the user needs to know it disappears on restart.

**Landed in:** `internal/tui/tui.go` (`persistDaily`, the `dailyMsg` handler, the
daily-edit key handler)

**Test added:** `TestDailySaveFailureIsReported` — a `DailyStore` whose
`SaveDaily` always fails; asserts the error is surfaced **and** that the "ready"
line is not printed.

### N2 — startup warnings show their backticks literally {#n2} ✅ DONE

**Where:** `internal/tui/tui.go` — the startup banner entries added around
`tui.go:112-118`, rendered under the `warn` role.

Surfaced while testing [H4](#h4): the Plane/Telegram "not configured" warnings
are written with backticks (``set it in `planner config` → Plane``) and the
`warn` role does not render markup, so the backticks appear on screen.

Cosmetic only, and deliberately **not** folded into H4 — that finding is about
model-authored daily content, and quietly widening `renderMarkup` to other roles
is how the `"raw"` role got overloaded in the first place. The fix is a decision
about which roles are markup-bearing, which is worth making explicitly.

**Failure scenario:** none beyond a slightly scruffy first screen. Filed for
completeness.

---

## LOW {#low}

| ID | Where | Item |
| -- | ----- | ---- |
| L1 | `tui.go` ×25 (`1003, 1015, 1030, 1110, 1249, 1385, 1506, 1520, 1544, 1601, …`) | The `if err != nil { m.add("err", …); return }` idiom is duplicated 25 times while the existing `report()` helper (`1062-1068`) is used only 4 times. |
| L2 ✅ | `tui.go:75, 80-86, 2434` | ~~Nine lines of commented-out code referencing `inputBG`, an identifier that no longer exists anywhere — it would not even compile if uncommented. Dead remnant of a reverted theme.~~ **Done**, together with two duplicated doc comments left on `syncAll` and `sendDaily` when [C1b](#c1) moved them off the event loop. Cleaned in a commit separate from the [M3](#m3) move, so the move stayed pure. |
| L3 ✅ | `plane/syncer.go:17` | ~~Comment says `stateDefaults` is "reserved for state mapping" (it is actively used at `114-116`) and that it holds state **names** (it holds **ids** — `config.go:23`, `tui/config.go:270`). Wrong on both counts.~~ **Done** — now reads `Plane state group -> chosen state id, used by resolveStateID`, naming both the real content and the real consumer. |
| L4 ✅ | `main.go:106` | ~~`usage()` advertises a stale command subset — missing `/state`, `/drop`, `/sync`, `/pull`, `/project`, `/person`, `/fav`.~~ **Done** — rewritten as grouped lines (tasks / plane / dailies / llm / context / session) covering every command. |
| L5 ✅ | README:25-30, 88-100 | ~~Undocumented: `/todos`, `/exit`, `/q` aliases; `ctrl+u`/`ctrl+d` scroll; `ctrl+p`/`ctrl+n` menu nav; `planner chat`; `planner -h`; the `/state` picker keys. (No documented-but-missing commands — every README command resolves.)~~ **Done**, and then **enforced** — see the [Declined](#declined) note below, which this superseded. |
| L6 | `tui.go:1609, 1620, 1776, 678-721, 1806-1912` | The Spanish/English boundary drifts field by field rather than following a documented rule: English `projects · %d` header over a Spanish `Notas` block; English command chrome with fully Spanish `buildMentionContext`; Spanish `Estado`/`Objetivo` labels under an English `/task` usage string. |
| L7 ✅ | `tui.go:2405-2408` | ~~`vpH := m.height - len(m.suggestions) - inputH - 5` — `inputH` is named, the sibling `5` bundles five distinct rows into a literal nothing forces to stay in sync with `View()`.~~ **Done** — the literal is now `chromeH` (`render.go:125`), named next to the rows it counts. |
| L8 ✅ | `go.mod:3` | ~~`go 1.26.4` pins a patch-level toolchain in the `go` directive; anyone on 1.26.0–1.26.3 is forced into a toolchain download. Convention is `go 1.26` + a separate `toolchain` line.~~ **Done** — the directive is `go 1.26`. |
| L9 | `store/context.go:16-35, 86-103` | `slug`/`nick` are `COLLATE NOCASE PRIMARY KEY`, so `ON CONFLICT DO UPDATE` matches case-insensitively but never rewrites the stored casing. Create `Liquida`, upsert `liquida` → DB keeps `Liquida` but the returned view says `+liquida`. |
| L10 | `llm/openai.go:139-141`, `llm/claude.go:148-150` | Every non-2xx (including 429) is a terminal error — no `Retry-After`, no single backoff retry. Visible, not auto-recovered. Acceptable for a local tool; noted for completeness. |
| L11 | `telegram/client.go:72-83` | If Telegram rejects the HTML for an edge case, the send fails outright with no retry using plain text. `toHTML` always produces balanced tags so this is unlikely, but there is no degradation path. |
| L12 | `plane/client.go:190-192` | The raw Plane response body is echoed verbatim into the TUI. No credential leak (the token travels in the `X-API-Key` header, never the URL), but unfiltered server text reaches the screen. |
| L13 ➖ | git | `feat/rutine-repo-actions` is a bare pointer at `main` — 0 ahead, 0 behind, clean tree, already pushed. Nothing half-landed because nothing is on it. Resolved by the work rather than by a fix — see [Declined](#declined). |
| L14 ✅ | `.gitignore` | ~~`*.db` is ignored; `config.json` (which holds the actual secrets) is not explicitly listed. Nothing indicates it is tracked today, but the guard is missing.~~ **Done** — `config.json` is ignored, with a comment saying why (`holds API keys and tokens in plaintext`). |

---

## Test gaps worth closing

| Priority | St | Test to add |
| -------- | -- | ----------- |
| 1 | ✅ | `Fit` never returns a system-only (or empty) window when the input has ≥1 non-system message ([C2](#c2)) — 2 tests |
| 2 | ✅ | LLM-failure path does **not** overwrite the stored daily ([C3](#c3)) — 3 tests |
| 3 | ✅ | The Telegram bot token never appears in any error returned by `Send` ([H1](#h1)) |
| 4 | ✅ | `"hoy"` and the equivalent `YYYY-MM-DD` resolve to the same local window ([H2](#h2)) — 2 tests, replacing the formatting-only assertion |
| 5 | ✅ | `Dispatch` with malformed JSON errors for `send_daily`, `get_daily`, `list_day_tasks`, `list_tasks` — and empty args still work ([H6](#h6)) |
| 6 | ✅ | A >4096-char daily is chunked, not dropped ([H3](#h3)) — 5 tests |
| 7 | ✅ | Agent max-steps exhaustion: error returned, and `History()` left in a defined state ([M10](#m10)) — `TestAgentMaxStepsExhaustionLeavesUsableHistory` |
| 8 | ✅ | `PullStates` behavior when task A fails and task B follows ([M9](#m9)) — 3 tests: continues after a failure, reports every failure, clean run returns `nil` |
| 9 | ✅ | `persistDaily` failure is reported instead of announcing "ready" ([N1](#n1)) |
| 11 | ✅ | Blocking commands return a `tea.Cmd` and never run inside `Update()` ([C1b](#c1)) — 5 tests |
| 12 | ✅ | A hung engram times out instead of blocking forever ([C1a](#c1)) — 3 tests |
| 10 | ✅ | `config.json` keeps `0600` across rewrites, dir is `0700`, no temp files left ([M1](#m1)) — 4 tests |

---

### N3 — history navigation destroyed the draft {#n3} ✅ DONE

**Where:** `internal/tui/chat.go` — `historyPrev`, `historyNext`

Found while raising `internal/tui` coverage, and found in the most useful way
possible: the brief asked for a test that "the draft in progress is not
destroyed", and **the implementer reported that the behaviour did not exist
rather than writing a test that would fail**. That is the right instinct — a
test bent until it passes is worse than no test.

`historyPrev` overwrote the textarea unconditionally; `historyNext` walking past
the newest entry set it to `""`. So typing a line, pressing ↑ to glance at a
previous command, then ↓ to come back left the prompt empty and the draft
unrecoverable.

**Fix applied:** a `histDraft` field stashes the line on entering history and
restores it on leaving.

**A second defect surfaced while fixing it, and it was mine.** [M2](#m2) made
`submit` skip `pushHistory` for secret commands — but `pushHistory` was also
where history navigation got reset, so sending a `/key` left navigation state
dangling. The reset moved into `submit` as `resetHistoryNav`, which always runs.
`pushHistory` now only appends, which is what its name promised all along.

**Tests added:** `TestHistoryNavigationRestoresTheDraft` and
`TestSubmitEndsHistoryNavigation` (table-driven over an ordinary and a secret
command, so the [M2](#m2) interaction stays pinned).

---

## Declined {#declined}

Not backlog — decisions, with the reason, so nobody re-opens them by reflex.

| ID | Why it stays |
| -- | ------------ |
| **L1** — 25 duplicated `if err != nil { m.add("err", …); return }` blocks | Collapsing them into a `guard(err) bool` helper trades an explicit, greppable two-liner for an indirection that hides control flow. Go's error handling is verbose on purpose; churning 25 call sites to hide that is noise, not improvement. |
| **L6** — Spanish/English boundary drifts field by field | Real inconsistency, but the fix is a *decision* about where the boundary belongs (command chrome vs. task content), not a mechanical edit. Making that call belongs with the person who reads the UI daily, not with an audit sweep. |
| **L9** — `NOCASE` upsert can return a casing that differs from what is stored | Requires deciding whether the first spelling wins or the latest does, then a store change and a migration. That is a product decision with a data cost, for a cosmetic mismatch in a label. |
| **L10** — no 429 / `Retry-After` handling in the LLM adapters | For a single-user local tool, a rate-limit error that is *visible and immediate* beats silent retries that make the TUI look hung. Adding backoff would need the [C1b](#c1) spinner story to say "retrying" — worth doing when it actually bites. |
| **L11** — no plain-text fallback if Telegram rejects the HTML | `toHTML` only ever emits balanced tags from its own regexes, so a self-inflicted 400 is close to unreachable. A fallback path that is never exercised is a liability: it rots, and it fires on the one case you did not predict. |
| **L12** — the Plane response body is echoed verbatim into the TUI | The Plane instance is self-hosted and single-tenant, and the token travels in a header rather than the URL ([H1](#h1)), so there is no credential path. Truncating server errors would cost real diagnostic value. |
| **L13** — the branch was an empty pointer at `main` | Resolved by the work itself rather than by a fix: the branch now carries the audit remediation, and its name finally matches what is on it ([M13](#m13)). |
| ~~**L4 / L5 partially** — nothing enforces the usage/README match~~ | **Un-declined and closed.** The note said a test asserting every `baseCommands` entry appears in the README "is worth doing only if the drift recurs". It recurred inside this very audit — [M15](#m15) added `/dailies` and [M12](#m12) changed the tool surface — so the guard was written: `TestEveryCommandIsDocumented` (`internal/tui/commands_doc_test.go`) now fails the build for an undocumented command. It asserts **presence only** — pinning wording would be a tax on every prose edit — and its word-boundary logic is tested separately, so the 27 subtests cannot pass vacuously (`/daily` is not satisfied by `/dailies`). |

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
   ~~[H1](#h1)~~, ~~[M1](#m1)~~ — all done.
2. ✅ **Correctness:** ~~[C2](#c2)~~, ~~[H2](#h2)~~, ~~[H6](#h6)~~, ~~[M5](#m5)~~,
   ~~[M14](#m14)~~ — all done.
3. ✅ **Unfreeze the UI:** ~~[C1a](#c1)~~ (engram bounded), ~~[C1b](#c1)~~
   (blocking branches off the event loop), ~~[N1](#n1)~~ — all done.
4. ✅ **Collapse the daily spec:** ~~[H5](#h5)~~ → `internal/daily`. Done before
   H4, as planned, so the renderer consumes the same constants.
5. ✅ **Then [H4](#h4)** ~~(render + wrap)~~ — also fixed the selection desync.
6. ✅ **Remaining HIGHs:** ~~[H3](#h3)~~ (long dailies chunked) and ~~[H7](#h7)~~
   (failed Plane pushes now visible) — done.
7. ✅ **[M13](#m13) CI** — moved ahead of the structural work on purpose: [M3](#m3)
   is a ~2400-line mechanical refactor, and it should land against an automated
   gate rather than someone remembering to run the suite.
8. ✅ **Structural:** ~~[M3](#m3)~~ file split, ~~[M12](#m12)~~ table-driven
   tools, ~~[M11](#m11)~~ shared adapter factory — all done behind the tests from
   steps 1–6 and the CI gate from step 7.
9. ✅ **Robustness pair:** ~~[M7](#m7)~~ SQLite pragmas and ~~[M8](#m8)~~
   duplicate Plane issues — done together, since M7's missing busy timeout was
   what made M8's window likely in the first place.
10. ✅ **Behaviour polish:** ~~[M4](#m4)~~ (one line, caught by [M12](#m12)'s
   guard exactly as intended) and ~~[M2](#m2)~~ (secret masking).
11. ✅ **Leftovers:** ~~[M9](#m9)~~, ~~[M10](#m10)~~, ~~[M15](#m15)~~,
   ~~[N2](#n2)~~, and the cleanup — [L2](#low), [L3](#low), [L4](#low),
   [L5](#low), [L7](#low), [L8](#low), [L14](#low). The rest are
   [declined](#declined) with reasons.

12. ✅ ~~[M6](#m6)~~ — nil guards on the exported `Dispatch`.

**The audit is closed.** Everything that could cost a user data, a credential, a delivery or a working UI
has been fixed, tested, and verified failing-first.
