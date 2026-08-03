package main

import (
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/daily"
)

// The daily format used to be restated in the agent system prompt, in the
// /daily one-shot prompt and in the fallback builder, and the three drifted.
// internal/daily now owns it; this guard makes a future re-fork fail loudly
// here instead of silently producing two different dailies.
func TestSystemPromptEmbedsDailyFormatSpec(t *testing.T) {
	if !strings.Contains(systemPrompt, daily.FormatSpec) {
		t.Fatal("systemPrompt must embed daily.FormatSpec verbatim, not restate the daily format")
	}
}
