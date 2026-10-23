package openrouter

import (
	"context"
	"os"
	"testing"
)

// Opt-in check against the real endpoint: the fixture is my idea of the
// payload, and that is exactly the gap that hides bugs.
func TestLiveCatalog(t *testing.T) {
	if os.Getenv("OPENROUTER_LIVE") == "" {
		t.Skip("set OPENROUTER_LIVE=1 to hit the real API")
	}
	got, err := New(Config{}).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tools, free, priced := 0, 0, 0
	for _, m := range got {
		if m.Tools {
			tools++
		}
		if m.Free() {
			free++
		} else {
			priced++
		}
	}
	t.Logf("models=%d tool-capable=%d free=%d priced=%d", len(got), tools, free, priced)
	if len(got) < 50 || tools == 0 || priced == 0 {
		t.Fatalf("the live catalog decoded implausibly: %d models, %d with tools, %d priced", len(got), tools, priced)
	}
	for _, m := range got[:3] {
		t.Logf("%-44s ctx=%-9d tools=%v $%v/$%v", m.ID, m.ContextLength, m.Tools, m.PromptUSD, m.CompletionUSD)
	}
}
