package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/daily"
	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

func TestBuildDaily(t *testing.T) {
	date := daily.Date(time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC))
	if date != "2026-02-02 FEB" {
		t.Fatalf("date header: %q", date)
	}
	tasks := []domain.Task{
		{Type: domain.TypeFeat, Title: "Lazy loading", Status: domain.StatusStarted, WorkItemSeq: 343},
		{Type: domain.TypeFix, Title: "Migración Sensei2", Status: domain.StatusUnstarted},
		{Type: domain.TypeFeat, Title: "DNS", Status: domain.StatusCompleted,
			Details: domain.TaskDetails{TechNotes: "Usar VPN por restricción de IP"}},
		{Type: domain.TypeFix, Title: "Descartada", Status: domain.StatusCancelled},
	}
	out := daily.Build(date, tasks)
	for _, want := range []string{
		"**Daily:**  2026-02-02 FEB",
		"**Trabajo:**",
		"  - [FEAT] #343 Lazy loading",
		"  - [FIX] Migración Sensei2",
		"**Notas:**",
		"  >> Usar VPN por restricción de IP",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("daily missing %q\n got:\n%s", want, out)
		}
	}
	// cancelled tasks are excluded from the fallback digest.
	if strings.Contains(out, "Descartada") {
		t.Fatalf("cancelled task should not appear:\n%s", out)
	}
}

func TestParseDay(t *testing.T) {
	if _, ok := parseDay("hoy"); !ok {
		t.Fatal("hoy should parse")
	}
	y, ok := parseDay("ayer")
	if !ok {
		t.Fatal("ayer should parse")
	}
	if got := y.Format("2006-01-02"); got != time.Now().AddDate(0, 0, -1).Format("2006-01-02") {
		t.Fatalf("ayer = %s", got)
	}
	d, ok := parseDay("2026-02-02")
	if !ok || d.Format("2006-01-02") != "2026-02-02" {
		t.Fatalf("explicit date parse failed: %v %v", d, ok)
	}
	if _, ok := parseDay("mañana-quizá"); ok {
		t.Fatal("garbage should not parse")
	}
}

// The store derives its 24h window from day.Location() (see sqlite.go), so
// parseDay must hand back Local times on every branch. time.Parse returns UTC,
// which silently shifted the window by the zone offset and made "hoy" and
// today's explicit date select different tasks.
func TestParseDayUsesLocalLocation(t *testing.T) {
	today, ok := parseDay("hoy")
	if !ok {
		t.Fatal("hoy should parse")
	}
	explicit, ok := parseDay(time.Now().Format("2006-01-02"))
	if !ok {
		t.Fatal("today's explicit date should parse")
	}

	// time.Local and time.UTC are distinct Locations even on a UTC machine, so
	// this assertion has teeth regardless of where the tests run.
	if explicit.Location() != time.Local {
		t.Fatalf("explicit date must be Local, got %v", explicit.Location())
	}

	// The window the store actually computes.
	startOf := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
	}
	if !startOf(today).Equal(startOf(explicit)) {
		t.Fatalf("%q and today's explicit date must select the same window:\n hoy:  %v\n date: %v",
			"hoy", startOf(today), startOf(explicit))
	}
}

func TestDailyShow(t *testing.T) {
	m, st := newTestModel(t)
	dailies := st.(store.DailyStore)
	m.deps.Dailies = dailies
	ctx := context.Background()

	const content = "**Daily:**  2026-07-07 JUL\n\n**Trabajo:**\n  - deploy a `producción`"
	if err := dailies.SaveDaily(ctx, "2026-07-07", content); err != nil {
		t.Fatal(err)
	}

	// The entry keeps the source markup; rendering happens at display time.
	m.handleDaily(ctx, []string{"/daily", "show", "2026-07-07"})
	if !hasRoleContaining(m.entries, "daily", "deploy a `producción`") {
		t.Fatalf("show should keep the stored daily verbatim; entries=%+v", m.entries)
	}

	// A missing daily errors read-only; it must NOT regenerate.
	m.entries = nil
	m.handleDaily(ctx, []string{"/daily", "show", "2020-01-01"})
	if !hasRole(m.entries, "err") {
		t.Fatalf("show of a missing daily should error; entries=%+v", m.entries)
	}
	if hasRole(m.entries, "daily") {
		t.Fatalf("show of a missing daily should not print content; entries=%+v", m.entries)
	}
}

const curatedDaily = "**Daily:**  2026-07-07 JUL\n\n**Trabajo:**\n  - lo que escribí a mano"

// dailyStore wires a DailyStore into the model and returns it.
func dailyStore(t *testing.T, m *chatModel, st store.TaskStore) store.DailyStore {
	t.Helper()
	d, ok := st.(store.DailyStore)
	if !ok {
		t.Fatal("sqlite store should implement DailyStore")
	}
	m.deps.Dailies = d
	return d
}

// A failed generation must never overwrite a daily the user edited by hand: the
// deterministic fallback cannot reconstruct that text.
func TestDailyFailureKeepsStoredDraft(t *testing.T) {
	m, st := newTestModel(t)
	dailies := dailyStore(t, m, st)
	ctx := context.Background()

	if err := dailies.SaveDaily(ctx, "2026-07-07", curatedDaily); err != nil {
		t.Fatal(err)
	}

	m.Update(dailyMsg{
		dateKey:  "2026-07-07",
		fallback: "**Daily:**  2026-07-07 JUL\n\n**Trabajo:**\n  - [FEAT] mechanical fallback",
		prior:    curatedDaily,
		err:      errors.New("provider unreachable"),
	})

	got, err := dailies.GetDaily(ctx, "2026-07-07")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != curatedDaily {
		t.Fatalf("failed generation overwrote the stored daily:\n got: %q\nwant: %q", got.Content, curatedDaily)
	}
	if m.dailyDraft != curatedDaily {
		t.Fatalf("in-memory draft replaced: %q", m.dailyDraft)
	}
	if !hasRole(m.entries, "err") {
		t.Fatalf("the failure should be reported; entries=%+v", m.entries)
	}
}

// An empty model response with no error is still a failure — it must warn
// instead of silently replacing the draft.
func TestDailyEmptyResponseWarnsAndKeepsDraft(t *testing.T) {
	m, st := newTestModel(t)
	dailies := dailyStore(t, m, st)
	ctx := context.Background()

	if err := dailies.SaveDaily(ctx, "2026-07-07", curatedDaily); err != nil {
		t.Fatal(err)
	}

	m.Update(dailyMsg{
		dateKey:  "2026-07-07",
		text:     "   ",
		fallback: "**Daily:**  2026-07-07 JUL\n\n**Trabajo:**\n  - [FEAT] mechanical fallback",
		prior:    curatedDaily,
	})

	got, err := dailies.GetDaily(ctx, "2026-07-07")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != curatedDaily {
		t.Fatalf("empty response overwrote the stored daily: %q", got.Content)
	}
	if !hasRole(m.entries, "err") {
		t.Fatalf("an empty response must not fail silently; entries=%+v", m.entries)
	}
}

// With nothing stored there is nothing to lose, so the fallback is the useful
// outcome and should be persisted.
func TestDailyFailureWithoutPriorUsesFallback(t *testing.T) {
	m, st := newTestModel(t)
	dailies := dailyStore(t, m, st)

	const fallback = "**Daily:**  2026-07-08 JUL\n\n**Trabajo:**\n  - [FEAT] #1 algo"
	m.Update(dailyMsg{dateKey: "2026-07-08", fallback: fallback, err: errors.New("boom")})

	got, err := dailies.GetDaily(context.Background(), "2026-07-08")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != fallback {
		t.Fatalf("fallback should be stored when there is no prior draft: %q", got.Content)
	}
}

func TestBuildDailyEmpty(t *testing.T) {
	out := daily.Build("2026-02-02 FEB", nil)
	if !strings.Contains(out, "sin actividad") {
		t.Fatalf("empty daily should note no activity, got: %s", out)
	}
}
