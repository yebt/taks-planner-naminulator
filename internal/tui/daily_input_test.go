package tui

// What the daily generation actually hands the model, and the /dailies listing.

import (
	"context"
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
)

func TestSerializeTasksForDaily(t *testing.T) {
	const date = "2026-02-02 FEB"

	worked := []domain.Task{
		{Type: domain.TypeFeat, Title: "Lazy loading", Status: domain.StatusStarted,
			Details: domain.TaskDetails{Objective: "bajar el TTI del listado"}},
		{Type: domain.TypeFix, Title: "Migración Sensei2", Status: domain.StatusCompleted,
			Details: domain.TaskDetails{TechNotes: "usar VPN por restricción de IP"}},
	}

	cases := []struct {
		name        string
		tasks       []domain.Task
		prior       string
		instruction string
		want        []string
		notWant     []string
	}{
		{
			name:  "the date and every task with its details reach the model",
			tasks: worked,
			want: []string{
				date,
				"Lazy loading", string(domain.StatusStarted), "bajar el TTI del listado",
				"Migración Sensei2", string(domain.StatusCompleted), "usar VPN por restricción de IP",
			},
			notWant: []string{"ninguna"},
		},
		{
			name:  "a prior draft is carried so the model respects earlier edits",
			tasks: worked,
			prior: "**Daily:**  lo que escribí a mano",
			want:  []string{"lo que escribí a mano", "Lazy loading"},
		},
		{
			name:        "the requested modification is carried alongside the draft",
			tasks:       worked,
			prior:       "**Daily:**  lo que escribí a mano",
			instruction: "sacá la parte de DNS y agregá el deploy",
			want:        []string{"sacá la parte de DNS y agregá el deploy", "lo que escribí a mano"},
		},
		{
			name:  "a day with no tasks is stated, not silently omitted",
			tasks: nil,
			want:  []string{date, "ninguna"},
		},
		{
			name:        "a blank draft and a blank instruction add no empty sections",
			tasks:       worked,
			prior:       "   ",
			instruction: "  ",
			notWant:     []string{"Daily previo", "Modificación solicitada"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := serializeTasksForDaily(date, tc.tasks, tc.prior, tc.instruction)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Fatalf("the model input is missing %q:\n%s", want, out)
				}
			}
			for _, unwanted := range tc.notWant {
				if strings.Contains(out, unwanted) {
					t.Fatalf("the model input should not contain %q:\n%s", unwanted, out)
				}
			}
		})
	}
}

func TestListDailies(t *testing.T) {
	ctx := context.Background()

	t.Run("nothing stored yet says so and points at how to build one", func(t *testing.T) {
		m, st := newTestModel(t)
		dailyStore(t, m, st)
		m.entries = nil

		m.listDailies(ctx)

		got := lastEntry(t, m)
		if got.role == "err" {
			t.Fatalf("an empty list is not an error: %q", got.text)
		}
		if !strings.Contains(got.text, "/daily") {
			t.Fatalf("an empty list should tell the user how to fill it: %q", got.text)
		}
	})

	t.Run("every stored digest is listed by its date", func(t *testing.T) {
		m, st := newTestModel(t)
		dailies := dailyStore(t, m, st)
		for _, d := range []string{"2026-07-07", "2026-07-08", "2026-07-09"} {
			if err := dailies.SaveDaily(ctx, d, "**Daily:** "+d); err != nil {
				t.Fatal(err)
			}
		}
		m.entries = nil

		m.listDailies(ctx)

		got := lastEntry(t, m)
		for _, d := range []string{"2026-07-07", "2026-07-08", "2026-07-09"} {
			if !strings.Contains(got.text, d) {
				t.Fatalf("the listing dropped %q:\n%s", d, got.text)
			}
		}
	})

	t.Run("without a daily store the listing says so instead of panicking", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.deps.Dailies = nil
		m.entries = nil

		m.listDailies(ctx)

		if !hasRole(m.entries, "err") {
			t.Fatalf("an unavailable daily store must be reported; entries=%+v", m.entries)
		}
	})
}
