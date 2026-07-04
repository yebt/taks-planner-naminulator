package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
)

func TestBuildDaily(t *testing.T) {
	date := dailyDate(time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC))
	if date != "2026-02-02 FEB" {
		t.Fatalf("date header: %q", date)
	}
	tasks := []domain.Task{
		{Type: domain.TypeFeat, Title: "Lazy loading", Status: domain.StatusInProgress, WorkItemSeq: 343},
		{Type: domain.TypeFix, Title: "Migración Sensei2", Status: domain.StatusBlocked},
		{Type: domain.TypeFeat, Title: "DNS", Status: domain.StatusDone,
			Details: domain.TaskDetails{TechNotes: "Usar VPN por restricción de IP"}},
	}
	out := buildDaily(date, tasks)
	for _, want := range []string{
		"Daily:  2026-02-02 FEB",
		"Trabajo:",
		"  + [FEAT] #343 Lazy loading",
		"Bloqueos:",
		"  # [FIX] Migración Sensei2",
		"Notas:",
		"  >> Usar VPN por restricción de IP",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("daily missing %q\n got:\n%s", want, out)
		}
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

func TestBuildDailyEmpty(t *testing.T) {
	out := buildDaily("2026-02-02 FEB", nil)
	if !strings.Contains(out, "sin actividad") {
		t.Fatalf("empty daily should note no activity, got: %s", out)
	}
}
