package wiring

import (
	"testing"

	"github.com/webcloster-dev/planner/internal/config"
)

// populated returns a config with every field the adapters require, so the
// constructors can be checked without touching the network.
func populated() config.Config {
	return config.Config{
		Plane: config.PlaneConfig{
			BaseURL:         "https://plane.example",
			APIToken:        "token",
			WorkspaceSlug:   "acme",
			ProjectID:       "project-uuid",
			StateDefaults:   map[string]string{"started": "state-uuid"},
			DefaultEstimate: "3",
		},
		Telegram: config.TelegramConfig{
			BotToken: "bot-token",
			ChatID:   "12345",
			ThreadID: "7",
		},
	}
}

func TestPlaneClientConfigured(t *testing.T) {
	if PlaneClient(config.Config{}).Configured() {
		t.Fatal("zero config should build an unconfigured Plane client")
	}
	if !PlaneClient(populated()).Configured() {
		t.Fatal("populated config should build a configured Plane client")
	}
}

func TestPlaneSyncerConfigured(t *testing.T) {
	if PlaneSyncer(config.Config{}, nil).Configured() {
		t.Fatal("zero config should build an unconfigured Plane syncer")
	}
	if !PlaneSyncer(populated(), nil).Configured() {
		t.Fatal("populated config should build a configured Plane syncer")
	}
}

func TestTelegramClientConfigured(t *testing.T) {
	if TelegramClient(config.Config{}).Configured() {
		t.Fatal("zero config should build an unconfigured Telegram client")
	}
	if !TelegramClient(populated()).Configured() {
		t.Fatal("populated config should build a configured Telegram client")
	}
}
