package wiring

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/mcp"
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

// The openrouter entry may be absent (a config written before it existed, or a
// zero value in a test), so the lookup must survive a nil provider map.
func TestOpenRouterClientSurvivesAConfigWithoutProviders(t *testing.T) {
	if OpenRouterClient(config.Config{}) == nil {
		t.Fatal("a zero config should still build a catalog client")
	}
}

func TestMCPClientsSkipsAndReports(t *testing.T) {
	var mu sync.Mutex
	var dialed []string
	orig := connectMCP
	t.Cleanup(func() { connectMCP = orig })
	connectMCP = func(_ context.Context, name string, _ config.MCPServer) (*mcp.Client, error) {
		mu.Lock()
		dialed = append(dialed, name)
		mu.Unlock()
		return nil, errors.New("exec: not found")
	}
	cfg := config.Config{MCPServers: map[string]config.MCPServer{
		"plane":    {Command: "uvx", Env: map[string]string{"PLANE_API_KEY": "s3cret"}},
		"off":      {Command: "x", Disabled: true},
		"bad_name": {Command: "x"},
	}}
	clients, warns := MCPClients(context.Background(), cfg)
	if len(clients) != 0 {
		t.Fatalf("no server connected, got %d clients", len(clients))
	}
	if len(dialed) != 1 || dialed[0] != "plane" {
		t.Fatalf("only the enabled, validly named server should be dialed, got %v", dialed)
	}
	all := strings.Join(warns, "\n")
	if len(warns) != 2 || !strings.Contains(all, "bad_name") || !strings.Contains(all, "plane") {
		t.Fatalf("want warnings for bad_name and plane, got %v", warns)
	}
	if strings.Contains(all, "s3cret") {
		t.Fatalf("a warning leaked an env value: %v", warns)
	}
}

func TestMCPClientsEmptyConfig(t *testing.T) {
	clients, warns := MCPClients(context.Background(), config.Config{})
	if len(clients) != 0 || len(warns) != 0 {
		t.Fatalf("want nothing, got %v %v", clients, warns)
	}
}
