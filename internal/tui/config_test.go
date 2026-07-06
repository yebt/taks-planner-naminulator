package tui

import (
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/config"
)

func TestProviderFieldsDrillIn(t *testing.T) {
	cfg := config.Default()
	m := &configModel{cfg: &cfg}
	fields := m.providerFields()
	// two general settings + one row per provider
	if len(fields) != 2+len(cfg.Providers) {
		t.Fatalf("provider list field count = %d", len(fields))
	}
	// the last row is a provider (an action); running it drills into the detail
	if f := fields[len(fields)-1]; f.action == nil {
		t.Fatal("expected provider rows to be actions")
	} else {
		f.action()
	}
	if m.provider == "" {
		t.Fatal("drilling in should set m.provider")
	}
	if len(m.fields) != 3 { // model, api key, activate
		t.Fatalf("provider detail should have 3 fields, got %d", len(m.fields))
	}
}

func TestProviderDetailActivate(t *testing.T) {
	cfg := config.Default() // active = ollama
	m := &configModel{cfg: &cfg}
	var activate func() string
	for _, f := range m.providerDetailFields("openai") {
		if f.action != nil {
			activate = f.action
		}
	}
	if activate == nil {
		t.Fatal("no activate action in provider detail")
	}
	activate()
	if cfg.ActiveProvider != "openai" {
		t.Fatalf("activate did not switch provider: %q", cfg.ActiveProvider)
	}
}

func TestProviderRowLabel(t *testing.T) {
	cfg := config.Default() // ollama active (custom, key set)
	if !strings.Contains(providerRowLabel(&cfg, "ollama"), "▸") {
		t.Fatal("active provider should show the ▸ marker")
	}
	if !strings.Contains(providerRowLabel(&cfg, "openai"), "○") {
		t.Fatal("keyless provider should show the ○ marker")
	}
	pc := cfg.Providers["openai"]
	pc.APIKey = "sk-test"
	cfg.Providers["openai"] = pc
	if !strings.Contains(providerRowLabel(&cfg, "openai"), "●") {
		t.Fatal("provider with a key should show the ● marker")
	}
}
