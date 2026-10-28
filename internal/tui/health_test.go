package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"

	"github.com/webcloster-dev/planner/internal/config"
)

func hasRole(entries []entry, role string) bool {
	for _, e := range entries {
		if e.role == role {
			return true
		}
	}
	return false
}

func TestHealthCheckAlert(t *testing.T) {
	cfg := config.Default() // ollama active = ready (custom needs only a base url)
	m := &chatModel{deps: ChatDeps{Cfg: &cfg}, vp: viewport.New(80, 20)}
	m.healthCheck()
	if hasRole(m.entries, "alert") {
		t.Fatal("a ready provider (ollama) should not raise an alert")
	}

	cfg.ActiveProvider = "openai" // has no API key → not ready
	m2 := &chatModel{deps: ChatDeps{Cfg: &cfg}, vp: viewport.New(80, 20)}
	m2.healthCheck()
	if !hasRole(m2.entries, "alert") {
		t.Fatal("expected an alert when the active provider has no key")
	}
	// Plane and Telegram absent → non-blocking warnings.
	if !hasRole(m2.entries, "warn") {
		t.Fatal("expected warnings for unconfigured Plane/Telegram")
	}
}

func TestHealthCheckSurfacesStartupWarnings(t *testing.T) {
	cfg := config.Default()
	m := &chatModel{deps: ChatDeps{Cfg: &cfg, Warnings: []string{"mcp server \"plane\": connect failed"}}, vp: viewport.New(80, 20)}
	m.healthCheck()
	for _, e := range m.entries {
		if e.role == "warn" && strings.Contains(e.text, `"plane": connect failed`) {
			return
		}
	}
	t.Fatal("expected the startup warning to be shown in the chat")
}
