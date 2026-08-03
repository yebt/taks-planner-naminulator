package tui

// Secret masking in the config TUI. The config screen is the one place every
// API key is on display, so the mask is the only thing standing between a
// shoulder-surfer (or a screenshot) and a live credential.

import (
	"strings"
	"testing"
)

// leaks reports whether masked contains any recognisable run of the secret.
func leaks(masked, secret string) bool {
	const window = 3
	for i := 0; i+window <= len(secret); i++ {
		if strings.Contains(masked, secret[i:i+window]) {
			return true
		}
	}
	return false
}

func TestMaskSecret(t *testing.T) {
	t.Run("an unset key reads as unset rather than blank", func(t *testing.T) {
		if got := maskSecret(""); got != "(unset)" {
			t.Fatalf("maskSecret(%q) = %q, want %q", "", got, "(unset)")
		}
	})

	cases := []struct {
		name   string
		secret string
	}{
		{"a short key is hidden but marked as set", "abc"},
		{"a real key is hidden but marked as set", "sk-live-1234567890abcdef"},
		{"a key made of punctuation is hidden too", "!!!-???-###"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskSecret(tc.secret)
			if !strings.Contains(got, "(set)") {
				t.Fatalf("maskSecret(%q) = %q, should say the key is set", tc.secret, got)
			}
			if strings.Contains(got, tc.secret) || leaks(got, tc.secret) {
				t.Fatalf("the mask leaked part of the value: %q", got)
			}
		})
	}
}

// The mask must not double as a length hint for a real credential.
func TestMaskSecretDoesNotRevealKeyLength(t *testing.T) {
	short := maskSecret("sk-live-1234567")
	long := maskSecret("sk-live-1234567890abcdefghijklmnopqrstuvwxyz")
	if short != long {
		t.Fatalf("the mask leaks how long the key is: %q vs %q", short, long)
	}
}

// A key stored in the config must never be echoed back by the config view.
func TestConfigViewNeverShowsTheKey(t *testing.T) {
	// A key with no substring in common with the chrome around it, so the leak
	// check below cannot fire on the labels.
	const secret = "sk-zzqqxx-99714"

	m, _ := newTestModel(t)
	cm := &configModel{cfg: m.deps.Cfg, path: m.deps.ConfigPath}
	pc := cm.cfg.Providers["kimi"]
	pc.APIKey = secret
	cm.cfg.Providers["kimi"] = pc

	cm.enterSection(0)
	cm.fields = cm.providerDetailFields("kimi")

	out := cm.View()
	if strings.Contains(out, secret) || leaks(out, secret) {
		t.Fatalf("the provider detail rendered the raw key:\n%s", out)
	}
}
