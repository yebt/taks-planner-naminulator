package tui

// The /todo listing, status rendering, and the Plane state picker.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

// todoFlags are the first-argument options for /todo; todoDayFlags the second.
var todoFlags = []string{"all", "backlog", "unstarted", "started", "completed", "cancelled"}
var todoDayFlags = []string{"hoy", "ayer"}

// showTodo lists tasks. Bare: in-progress (any day) plus today's todo/done.
// "all" lists everything; a status flag lists that status; an optional day flag
// (hoy/ayer/YYYY-MM-DD) narrows to that calendar day.
func (m *chatModel) showTodo(ctx context.Context, fields []string) {
	var tasks []domain.Task
	var err error
	title := "todo"

	switch {
	case len(fields) == 1:
		title = "todo · en progreso + hoy"
		tasks, err = m.defaultTodo(ctx)
	case fields[1] == "all":
		f := store.Filter{}
		if d, ok := dayArg(fields[2:]); ok {
			f.Day = d
			title = "todo · all · " + fields[2]
		} else {
			title = "todo · all"
		}
		tasks, err = m.deps.Store.List(ctx, f)
	default:
		status := domain.Status(fields[1])
		if !status.Valid() {
			m.add("err", "unknown flag "+fields[1]+" — use: all or a status ("+strings.Join(todoFlags[1:], ", ")+")")
			return
		}
		f := store.Filter{Status: status}
		title = "todo · " + fields[1]
		if d, ok := dayArg(fields[2:]); ok {
			f.Day = d
			title += " · " + fields[2]
		}
		tasks, err = m.deps.Store.List(ctx, f)
	}
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.renderTodo(title, tasks)
}

// defaultTodo is the bare /todo set: every in-progress task plus today's todo
// and done tasks.
func (m *chatModel) defaultTodo(ctx context.Context) ([]domain.Task, error) {
	today := time.Now()
	var out []domain.Task
	for _, f := range []store.Filter{
		{Status: domain.StatusStarted},
		{Status: domain.StatusUnstarted, Day: today},
		{Status: domain.StatusCompleted, Day: today},
	} {
		ts, err := m.deps.Store.List(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, ts...)
	}
	return out, nil
}

// dayArg parses an optional day flag, reporting whether one was present.
func dayArg(rest []string) (time.Time, bool) {
	if len(rest) > 0 {
		if d, ok := parseDay(rest[0]); ok {
			return d, true
		}
	}
	return time.Time{}, false
}

// renderTodo prints tasks grouped by status in workflow order.
func (m *chatModel) renderTodo(title string, tasks []domain.Task) {
	if len(tasks) == 0 {
		m.add("sys", "no tasks.")
		return
	}
	typeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	order := []domain.Status{
		domain.StatusStarted, domain.StatusUnstarted, domain.StatusBacklog,
		domain.StatusCompleted, domain.StatusCancelled,
	}
	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("%s · %d", title, len(tasks))) + "\n")
	for _, st := range order {
		var lines []string
		for _, t := range tasks {
			if t.Status != st {
				continue
			}
			line := fmt.Sprintf("%s  %s  %s",
				idStyle.Render(fmt.Sprintf("%3d", t.ID)),
				typeStyle.Render(fmt.Sprintf("%-6s", t.Type)),
				trunc(t.Title, 44))
			if t.WorkItemSeq > 0 {
				line += helpStyle.Render(fmt.Sprintf("  #%d", t.WorkItemSeq))
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}
		glyph := statusStyleFor(st).Render(statusGlyph(st)) // per-row state marker, visible on scroll
		b.WriteString("\n" + statusStyleFor(st).Render(statusGlyph(st)+" "+m.statusLabel(st)) + "\n")
		for _, ln := range lines {
			b.WriteString(glyph + " " + ln + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("/todo all · /todo <status> [hoy|ayer] · /task <id>"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

// statusStyleFor maps a task status to a color so the list scans at a glance.
func statusStyleFor(s domain.Status) lipgloss.Style {
	var c lipgloss.Color
	switch s {
	case domain.StatusStarted:
		c = "214" // orange
	case domain.StatusUnstarted:
		c = "39" // blue
	case domain.StatusCompleted:
		c = "42" // green
	case domain.StatusCancelled:
		c = "203" // red
	case domain.StatusBacklog:
		c = "245" // gray
	default:
		c = "252"
	}
	return lipgloss.NewStyle().Foreground(c)
}

// statusGlyph is the minimalist per-status marker.
func statusGlyph(s domain.Status) string {
	switch s {
	case domain.StatusBacklog:
		return "?"
	case domain.StatusUnstarted:
		return "○"
	case domain.StatusStarted:
		return "▸"
	case domain.StatusCompleted:
		return "●"
	case domain.StatusCancelled:
		return "✗"
	default:
		return "•"
	}
}

// statusLabel renders a status with its configured default Plane state in
// parentheses, e.g. "started (In Progress)".
func (m *chatModel) statusLabel(s domain.Status) string {
	if m.deps.Cfg != nil {
		if id := m.deps.Cfg.Plane.StateDefaults[string(s)]; id != "" {
			for _, ps := range m.deps.Cfg.Plane.States {
				if ps.ID == id {
					return string(s) + " (" + ps.Name + ")"
				}
			}
		}
	}
	return string(s)
}

// openStatePicker shows a menu of the real Plane states (from the config cache)
// so the user selects instead of guessing. Requires a prior fetch in config.
func (m *chatModel) openStatePicker(id int64) {
	states := m.deps.Cfg.Plane.States
	if len(states) == 0 {
		m.add("err", "no states cached — run: planner config → Plane → fetch states")
		return
	}
	if _, err := m.deps.Store.Get(context.Background(), id); err != nil {
		m.add("err", err.Error())
		return
	}
	m.statePick = &statePicker{taskID: id, states: states}
	m.suggestions = m.suggestions[:0]
	for _, s := range states {
		m.suggestions = append(m.suggestions, suggestion{full: s.Name, desc: "(" + s.Group + ")"})
	}
	m.selected = 0
	m.layout()
}

// applyStatePick sets the highlighted Plane state on the task via set_state.
func (m *chatModel) applyStatePick() {
	sp := m.statePick
	m.statePick = nil
	if sp == nil || m.selected >= len(sp.states) {
		m.suggestions = nil
		return
	}
	name := sp.states[m.selected].Name
	m.suggestions = nil
	args := fmt.Sprintf(`{"id":%d,"state":%q}`, sp.taskID, name)
	out, err := m.deps.Tools.Dispatch(context.Background(), "set_state", args)
	m.report("state: ", out, err)
}

// dropTask deletes a task locally, and (when withSync) removes its Plane work
// item first — aborting the local delete if the cloud delete fails.
func (m *chatModel) dropTask(ctx context.Context, id int64, withSync bool) {
	if withSync && m.deps.Syncer != nil && m.deps.Syncer.Configured() {
		if t, err := m.deps.Store.Get(ctx, id); err == nil && t.WorkItemID != "" {
			if err := m.deps.Syncer.Delete(ctx, &t); err != nil {
				m.add("err", "Plane delete failed (task kept): "+err.Error())
				return
			}
		}
	}
	out, err := m.deps.Tools.Dispatch(ctx, "drop_task", fmt.Sprintf(`{"id":%d}`, id))
	m.report("dropped: ", out, err)
}
