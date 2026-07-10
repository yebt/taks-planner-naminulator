package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

func TestBuildDaily(t *testing.T) {
	date := dailyDate(time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC))
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
	out := buildDaily(date, tasks)
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

func TestDailyShow(t *testing.T) {
	m, st := newTestModel(t)
	dailies := st.(store.DailyStore)
	m.deps.Dailies = dailies
	ctx := context.Background()

	const content = "**Daily:**  2026-07-07 JUL\n\n**Trabajo:**\n  - deploy a `producción`"
	if err := dailies.SaveDaily(ctx, "2026-07-07", content); err != nil {
		t.Fatal(err)
	}

	m.handleDaily(ctx, []string{"/daily", "show", "2026-07-07"})
	if !hasRawContaining(m.entries, "deploy a `producción`") {
		t.Fatalf("show should print the stored daily verbatim; entries=%+v", m.entries)
	}

	// A missing daily errors read-only; it must NOT regenerate.
	m.entries = nil
	m.handleDaily(ctx, []string{"/daily", "show", "2020-01-01"})
	if !hasRole(m.entries, "err") {
		t.Fatalf("show of a missing daily should error; entries=%+v", m.entries)
	}
	if hasRole(m.entries, "raw") {
		t.Fatalf("show of a missing daily should not print content; entries=%+v", m.entries)
	}
}

func hasRawContaining(entries []entry, want string) bool {
	for _, e := range entries {
		if e.role == "raw" && strings.Contains(e.text, want) {
			return true
		}
	}
	return false
}

func TestBuildDailyEmpty(t *testing.T) {
	out := buildDaily("2026-02-02 FEB", nil)
	if !strings.Contains(out, "sin actividad") {
		t.Fatalf("empty daily should note no activity, got: %s", out)
	}
}
