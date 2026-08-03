package tui

// The /daily digest: generate, show, edit, send and list.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/daily"
	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

// handleDaily routes the /daily verbs: "edit"/"send" (optional date), otherwise
// the first token is an optional date and the rest an optional LLM instruction.
func (m *chatModel) handleDaily(ctx context.Context, fields []string) tea.Cmd {
	if len(fields) == 1 {
		return m.generateDailyCmd(ctx, time.Now(), "")
	}
	switch fields[1] {
	case "show":
		m.showDaily(ctx, dailyDayArg(fields[2:]))
		return nil
	case "edit":
		m.editDaily(ctx, dailyDayArg(fields[2:]))
		return nil
	case "send":
		return m.sendDaily(ctx, dailyDayArg(fields[2:]))
	default:
		day, ok := parseDay(fields[1])
		if !ok {
			m.add("err", "usage: /daily [today|yesterday|YYYY-MM-DD] [instruction] · /daily show|edit|send [date]")
			return nil
		}
		return m.generateDailyCmd(ctx, day, strings.Join(fields[2:], " "))
	}
}

// parseDay resolves today/yesterday (es/en) or an explicit YYYY-MM-DD date.
// The date is parsed in the local zone on purpose: the store derives its 24h
// window from day.Location(), so a UTC value (what time.Parse returns) would
// shift the window by the zone offset and make "hoy" and today's explicit date
// select different tasks.
func parseDay(tok string) (time.Time, bool) {
	switch strings.ToLower(tok) {
	case "today", "hoy":
		return time.Now(), true
	case "yesterday", "ayer":
		return time.Now().AddDate(0, 0, -1), true
	}
	if t, err := time.ParseInLocation("2006-01-02", tok, time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// dailyDayArg reads an optional date argument, defaulting to today.
func dailyDayArg(rest []string) time.Time {
	if len(rest) > 0 {
		if d, ok := parseDay(rest[0]); ok {
			return d
		}
	}
	return time.Now()
}

// generateDailyCmd drafts the digest for a day asynchronously, feeding the model
// the day's tasks, any previously stored/edited draft, and an optional
// instruction so prior modifications are respected.
func (m *chatModel) generateDailyCmd(ctx context.Context, day time.Time, instruction string) tea.Cmd {
	// Prefer the activity log (a task surfaces on every day it was worked on);
	// fall back to the last-touched filter when it's unavailable.
	var tasks []domain.Task
	var err error
	if m.deps.Activity != nil {
		tasks, err = m.deps.Activity.TasksWithActivityOn(ctx, day)
	} else {
		tasks, err = m.deps.Store.List(ctx, store.Filter{Day: day})
	}
	if err != nil {
		m.add("err", err.Error())
		return nil
	}
	date := daily.Date(day)
	dateKey := day.Format("2006-01-02")
	prior := ""
	if m.deps.Dailies != nil {
		if d, err := m.deps.Dailies.GetDaily(ctx, dateKey); err == nil {
			prior = d.Content
		}
	}
	m.thinking = true
	m.thinkStart = time.Now()
	m.add("sys", "generating daily for "+date+"…")
	m.layout()
	userMsg := serializeTasksForDaily(date, tasks, prior, instruction)
	return tea.Batch(dailyCmd(m.deps.Agent, dateKey, userMsg, daily.Build(date, tasks), prior), spinnerTick())
}

func dailyCmd(a *agent.Agent, dateKey, userMsg, fallback, prior string) tea.Cmd {
	return func() tea.Msg {
		out, err := a.Oneshot(context.Background(), daily.Prompt, userMsg)
		return dailyMsg{dateKey: dateKey, text: out, fallback: fallback, prior: prior, err: err}
	}
}

// serializeTasksForDaily renders the day's tasks, the prior draft, and any edit
// request as material for the model.
func serializeTasksForDaily(date string, tasks []domain.Task, prior, instruction string) string {
	var b strings.Builder
	b.WriteString("FECHA: " + date + "\n\nTareas del día:\n")
	if len(tasks) == 0 {
		b.WriteString("(ninguna)\n")
	}
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("- [%s] estado=%s: %s", t.Type, t.Status, t.Title))
		if o := strings.TrimSpace(t.Details.Objective); o != "" {
			b.WriteString(" | objetivo: " + o)
		}
		if n := strings.TrimSpace(t.Details.TechNotes); n != "" {
			b.WriteString(" | nota: " + n)
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(prior) != "" {
		b.WriteString("\nDaily previo (respetá las ediciones ya hechas salvo que se indique lo contrario):\n" + prior + "\n")
	}
	if strings.TrimSpace(instruction) != "" {
		b.WriteString("\nModificación solicitada: " + instruction + "\n")
	}
	return b.String()
}

// draftFor returns the current in-memory draft for dateKey, or the stored one.
func (m *chatModel) draftFor(ctx context.Context, dateKey string) string {
	if dateKey == m.dailyDraftDate && strings.TrimSpace(m.dailyDraft) != "" {
		return m.dailyDraft
	}
	if m.deps.Dailies != nil {
		if d, err := m.deps.Dailies.GetDaily(ctx, dateKey); err == nil {
			return d.Content
		}
	}
	return ""
}

// persistDaily stores a digest, returning the failure rather than swallowing
// it: the caller announces the daily is ready, and that claim must not outrun
// the write.
func (m *chatModel) persistDaily(dateKey, content string) error {
	if m.deps.Dailies == nil || strings.TrimSpace(content) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	return m.deps.Dailies.SaveDaily(ctx, dateKey, content)
}

// showDaily prints a stored daily read-only, without regenerating it or
// entering edit mode.
func (m *chatModel) showDaily(ctx context.Context, day time.Time) {
	dateKey := day.Format("2006-01-02")
	content := m.draftFor(ctx, dateKey)
	if strings.TrimSpace(content) == "" {
		m.add("err", "no daily for "+dateKey+" — run /daily "+dateKey+" first")
		return
	}
	m.add("daily", content)
	m.add("sys", "daily "+dateKey+" · /daily edit "+dateKey+" to tweak · /daily send "+dateKey+" to deliver")
}

// editDaily loads a date's draft into the textarea for inline editing.
func (m *chatModel) editDaily(ctx context.Context, day time.Time) {
	dateKey := day.Format("2006-01-02")
	draft := m.draftFor(ctx, dateKey)
	if strings.TrimSpace(draft) == "" {
		m.add("err", "no daily for "+dateKey+" — run /daily "+dateKey+" first")
		return
	}
	m.dailyDraft = draft
	m.dailyDraftDate = dateKey
	m.ta.SetValue(draft)
	m.dailyEditing = true
	m.suggestions = nil
	m.layout()
}

// sendDaily delivers a stored digest to Telegram. The draft is resolved here,
// on the event loop, because it reads model state; only the network call is
// handed off.
func (m *chatModel) sendDaily(ctx context.Context, day time.Time) tea.Cmd {
	dateKey := day.Format("2006-01-02")
	content := m.draftFor(ctx, dateKey)
	if strings.TrimSpace(content) == "" {
		m.add("err", "no daily for "+dateKey+" — run /daily "+dateKey+" first")
		return nil
	}
	if m.deps.Telegram == nil || !m.deps.Telegram.Configured() {
		m.add("err", "can't send: Telegram not configured (set bot token + chat id in config)")
		return nil
	}
	tg := m.deps.Telegram
	return m.busy("sending", telegramOpTimeout, func(ctx context.Context) []entry {
		if err := tg.Send(ctx, content); err != nil {
			return errEntry(err)
		}
		return sysEntry("daily " + dateKey + " sent to Telegram ✓")
	})
}

// listDailies shows the stored digests.
func (m *chatModel) listDailies(ctx context.Context) {
	if m.deps.Dailies == nil {
		m.add("err", "daily store not available")
		return
	}
	ds, err := m.deps.Dailies.ListDailies(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(ds) == 0 {
		m.add("sys", "no dailies stored yet. run /daily")
		return
	}
	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("dailies · %d", len(ds))) + "\n\n")
	for _, d := range ds {
		b.WriteString(fmt.Sprintf("  %s   %s\n", d.Date, helpStyle.Render(d.UpdatedAt.Local().Format("15:04"))))
	}
	b.WriteString("\n" + helpStyle.Render("/daily show <date> · /daily edit <date> · /daily send <date> · /daily <date> regenerate"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}
