package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHealsMissingProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	// An older config written before groq existed: providers present but no groq.
	old := `{"active_provider":"kimi","providers":{"kimi":{"kind":"kimi","model":"k"}},"db_path":"x","context_budget":100}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Providers["groq"]; !ok {
		t.Fatal("Load should add missing default providers (groq) to an older config")
	}
	// Existing entries must be preserved, not overwritten by defaults.
	if c.Providers["kimi"].Model != "k" || c.ActiveProvider != "kimi" {
		t.Fatalf("existing config was clobbered: %+v", c.Providers["kimi"])
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveProvider != "ollama" {
		t.Fatalf("default active provider: %q", c.ActiveProvider)
	}
	if _, ok := c.Providers["claude"]; !ok {
		t.Fatal("default should include claude provider")
	}
}

func TestPlaneReadinessAndGroups(t *testing.T) {
	var p PlaneConfig
	if p.Ready() {
		t.Fatal("empty plane config should not be ready")
	}
	p = PlaneConfig{BaseURL: "u", APIToken: "t", WorkspaceSlug: "w", ProjectID: "p"}
	if !p.Ready() {
		t.Fatal("fully-filled plane config should be ready")
	}
	p.States = []PlaneState{
		{ID: "a", Name: "Por hacer", Group: "unstarted"},
		{ID: "b", Name: "En progreso", Group: "started"},
		{ID: "c", Name: "Devuelto por Calidad", Group: "completed"},
		{ID: "d", Name: "Hecho", Group: "completed"},
	}
	if got := p.StatesByGroup("completed"); len(got) != 2 {
		t.Fatalf("expected 2 completed states, got %d", len(got))
	}
	if got := p.StatesByGroup("started"); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("started group lookup failed: %+v", got)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := Default()
	in.ActiveProvider = "kimi"
	in.Plane.WorkspaceSlug = "acme"
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.ActiveProvider != "kimi" {
		t.Fatalf("active provider not persisted: %q", out.ActiveProvider)
	}
	if out.Plane.WorkspaceSlug != "acme" {
		t.Fatalf("plane slug not persisted: %q", out.Plane.WorkspaceSlug)
	}
	if out.Providers["kimi"].Kind != "kimi" {
		t.Fatalf("provider config not persisted: %+v", out.Providers["kimi"])
	}
}
