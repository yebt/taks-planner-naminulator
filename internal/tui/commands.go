package tui

// Slash-command catalogue and dispatch.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/store"
)

var baseCommands = []suggestion{
	{"/help", "show commands"},
	{"/todo", "/todo [all|<status>] [hoy|ayer] — list tasks grouped by status"},
	{"/task", "/task <id> — show a task in full"},
	{"/new", "/new <TYPE> <title> — create a task (no LLM)"},
	{"/status", "/status <id> <status> — change a task status"},
	{"/state", "/state <id> — pick a Plane state from the real list"},
	{"/drop", "/drop <id> [sync] — delete a task (sync also removes it in Plane)"},
	{"/model", "switch LLM provider"},
	{"/fav", "/fav [save|del] <name> — save/switch a provider+model favorite"},
	{"/key", "/key <provider> <apikey> — set & save an API key"},
	{"/save", "save this conversation"},
	{"/chats", "list saved conversations"},
	{"/load", "/load <id> — restore a conversation"},
	{"/newchat", "start a fresh conversation"},
	{"/recall", "/recall <query> — search long-term memory"},
	{"/remember", "/remember <note> — save to long-term memory"},
	{"/sync", "push local tasks to Plane"},
	{"/pull", "pull states from Plane"},
	{"/daily", "/daily [date] [instr] · show|edit|send [date] — build/show/edit/send a digest"},
	{"/dailies", "list stored dailies"},
	{"/projects", "list projects (+slug)"},
	{"/project", "/project <slug> · new <slug> [desc] · <slug> note [kind] <text>"},
	{"/people", "list people (@nick)"},
	{"/person", "/person <nick> · new <nick> [role] · <nick> note [kind] <text>"},
	{"/resume", "resume the most recent conversation"},
	{"/clear", "clear the conversation"},
	{"/quit", "exit"},
}

var needsArg = map[string]bool{
	"/new": true, "/status": true, "/model": true, "/key": true, "/fav": true,
	"/load": true, "/recall": true, "/remember": true, "/task": true, "/drop": true, "/state": true,
	"/project": true, "/person": true,
}

// secretCommands carry a credential in their arguments, so the line must not be
// echoed verbatim or kept in the recallable input history. The config TUI
// already masks these; this is the same protection for the chat command.
var secretCommands = map[string]bool{"/key": true}

// carriesSecret reports whether a command line has a credential typed into it.
// A bare "/key" or "/key <provider>" has nothing sensitive yet.
func carriesSecret(val string) bool {
	fields := strings.Fields(val)
	return len(fields) > 2 && secretCommands[fields[0]]
}

// redactCommand returns the form safe to show on screen: the command and its
// non-secret arguments, with the credential replaced.
func redactCommand(val string) string {
	if !carriesSecret(val) {
		return val
	}
	fields := strings.Fields(val)
	return strings.Join(fields[:2], " ") + " ••••••"
}

func (m *chatModel) runCommand(val string) tea.Cmd {
	m.add("cmd", redactCommand(val))
	fields := strings.Fields(val)
	ctx := context.Background()

	switch fields[0] {
	case "/quit", "/exit", "/q":
		return tea.Quit

	case "/help":
		var b strings.Builder
		b.WriteString("commands:\n")
		for _, c := range baseCommands {
			b.WriteString(fmt.Sprintf("  %-10s %s\n", c.full, c.desc))
		}
		b.WriteString("\nkeys: enter=send · alt+enter=newline · tab/enter=complete · ↑/↓=history · esc=close menu")
		b.WriteString("\nAPI keys: " + m.deps.ConfigPath + " (or /key).")
		m.add("sys", strings.TrimRight(b.String(), "\n"))

	case "/clear":
		m.entries = nil
		m.deps.Agent.Reset()
		m.add("sys", "conversation cleared.")

	case "/todo", "/todos":
		m.showTodo(ctx, fields)

	case "/task":
		if len(fields) < 2 {
			m.add("err", "usage: /task <id>")
			break
		}
		m.showTask(ctx, fields[1])

	case "/new":
		if len(fields) < 3 {
			m.add("err", "usage: /new <TYPE> <title>")
			break
		}
		args, _ := json.Marshal(map[string]string{"type": fields[1], "title": strings.Join(fields[2:], " ")})
		out, err := m.deps.Tools.Dispatch(ctx, "create_task", string(args))
		m.report("created: ", out, err)

	case "/status":
		if len(fields) < 3 {
			m.add("err", "usage: /status <id> <status>")
			break
		}
		if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
			m.add("err", "id must be a number")
			break
		}
		args := fmt.Sprintf(`{"id":%s,"status":%q}`, fields[1], fields[2])
		out, err := m.deps.Tools.Dispatch(ctx, "set_status", args)
		m.report("updated: ", out, err)

	case "/state":
		if len(fields) < 2 {
			m.add("err", "usage: /state <id>")
			break
		}
		id, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			m.add("err", "id must be a number")
			break
		}
		m.openStatePicker(id)

	case "/drop":
		if len(fields) < 2 {
			m.add("err", "usage: /drop <id> [sync]")
			break
		}
		id, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			m.add("err", "id must be a number")
			break
		}
		withSync := len(fields) > 2 && (fields[2] == "sync" || fields[2] == "--sync")
		if withSync {
			m.confirm = &pendingConfirm{
				prompt: fmt.Sprintf("delete #%d locally and in Plane?", id),
				action: func() { m.dropTask(context.Background(), id, true) },
			}
			break
		}
		m.dropTask(ctx, id, false)

	case "/model":
		if len(fields) < 2 {
			m.add("sys", "providers: "+strings.Join(m.providerNames(), ", ")+"\nuse: /model <name>")
			break
		}
		m.switchModel(fields[1])

	case "/fav":
		switch {
		case len(fields) == 1:
			m.listFavorites()
		case fields[1] == "save":
			m.saveFavorite(strings.Join(fields[2:], " "))
		case fields[1] == "del" || fields[1] == "drop":
			if len(fields) < 3 {
				m.add("err", "usage: /fav del <name>")
				break
			}
			m.delFavorite(strings.Join(fields[2:], " "))
		default:
			m.applyFavorite(strings.Join(fields[1:], " "))
		}

	case "/key":
		if len(fields) < 3 {
			m.add("err", "usage: /key <provider> <apikey>")
			break
		}
		m.setKey(fields[1], strings.Join(fields[2:], " "))

	case "/save":
		title := m.convTitle()
		if len(fields) > 1 {
			title = strings.Join(fields[1:], " ")
		}
		m.saveConversation(title)

	case "/chats":
		m.showChats(ctx)

	case "/load":
		if len(fields) < 2 {
			m.add("err", "usage: /load <id>")
			break
		}
		m.loadConversation(ctx, fields[1])

	case "/newchat":
		m.deps.Agent.Reset()
		m.convID = 0
		m.entries = nil
		m.add("sys", "started a new conversation.")

	case "/recall":
		if len(fields) < 2 {
			m.add("err", "usage: /recall <query>")
			break
		}
		mem, query := m.deps.Memory, strings.Join(fields[1:], " ")
		return m.busy("recalling", memoryOpTimeout, func(ctx context.Context) []entry {
			out, err := mem.Recall(ctx, query, 5)
			if err != nil {
				return errEntry(err)
			}
			return sysEntry(out)
		})

	case "/remember":
		if len(fields) < 2 {
			m.add("err", "usage: /remember <note>")
			break
		}
		mem, note := m.deps.Memory, strings.Join(fields[1:], " ")
		return m.busy("remembering", memoryOpTimeout, func(ctx context.Context) []entry {
			if err := mem.Save(ctx, trunc(note, 48), note); err != nil {
				return errEntry(err)
			}
			return sysEntry("remembered: " + trunc(note, 48))
		})

	case "/sync":
		return m.syncAll()

	case "/pull":
		if m.deps.Syncer == nil || !m.deps.Syncer.Configured() {
			m.add("err", "Plane not configured (set base_url/token/slug/project in config)")
			break
		}
		syncer := m.deps.Syncer
		return m.busy("pulling", planeOpTimeout, func(ctx context.Context) []entry {
			n, err := syncer.PullStates(ctx)
			if err != nil {
				return errEntry(err)
			}
			return sysEntry(fmt.Sprintf("pulled states: %d task(s) updated", n))
		})

	case "/daily":
		return m.handleDaily(ctx, fields)

	case "/dailies":
		m.listDailies(ctx)

	case "/projects":
		m.listProjects(ctx)

	case "/project":
		m.handleProject(ctx, fields)

	case "/people":
		m.listPeople(ctx)

	case "/person":
		m.handlePerson(ctx, fields)

	case "/resume":
		m.resumeLast(ctx)

	default:
		m.add("err", "unknown command: "+fields[0]+" (try /help)")
	}
	return nil
}

func (m *chatModel) report(prefix, out string, err error) {
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.add("sys", prefix+out)
}

// syncAll pushes every local task to Plane off the event loop, reporting
// per-task failures plus a summary when it lands.
func (m *chatModel) syncAll() tea.Cmd {
	if m.deps.Syncer == nil || !m.deps.Syncer.Configured() {
		m.add("err", "Plane not configured (set base_url/token/slug/project in config)")
		return nil
	}
	syncer, st := m.deps.Syncer, m.deps.Store
	return m.busy("syncing", planeOpTimeout, func(ctx context.Context) []entry {
		tasks, err := st.List(ctx, store.Filter{})
		if err != nil {
			return errEntry(err)
		}
		var out []entry
		pushed, failed := 0, 0
		for _, t := range tasks {
			tt := t
			if err := syncer.Push(ctx, &tt); err != nil {
				out = append(out, entry{role: "err", text: fmt.Sprintf("#%d %s: %v", t.ID, t.Label, err)})
				failed++
			} else {
				pushed++
			}
		}
		return append(out, entry{role: "sys", text: fmt.Sprintf("sync → Plane: %d pushed, %d failed", pushed, failed)})
	})
}
