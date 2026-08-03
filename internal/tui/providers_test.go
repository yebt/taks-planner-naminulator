package tui

// The /fav listing: what the user gets back when there is nothing saved yet,
// and when there is.

import (
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/config"
)

func TestListFavorites(t *testing.T) {
	t.Run("nothing saved yet says so and points at how to save one", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.deps.Cfg.Favorites = nil
		m.entries = nil

		m.listFavorites()

		got := lastEntry(t, m)
		if got.role == "err" {
			t.Fatalf("an empty list is not an error: %q", got.text)
		}
		if !strings.Contains(got.text, "/fav save") {
			t.Fatalf("an empty list should tell the user how to fill it: %q", got.text)
		}
	})

	t.Run("every favorite is listed with the provider and model it selects", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.deps.Cfg.Favorites = []config.Favorite{
			{Name: "barato", Provider: "kimi", Model: "moonshot-v1-8k"},
			{Name: "potente", Provider: "claude", Model: "claude-opus-4"},
		}
		m.entries = nil

		m.listFavorites()

		got := lastEntry(t, m)
		for _, want := range []string{
			"barato", "kimi", "moonshot-v1-8k",
			"potente", "claude", "claude-opus-4",
		} {
			if !strings.Contains(got.text, want) {
				t.Fatalf("the listing dropped %q:\n%s", want, got.text)
			}
		}
	})
}
