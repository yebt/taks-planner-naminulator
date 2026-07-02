package config

import (
	"path/filepath"
	"testing"
)

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
