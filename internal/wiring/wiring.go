// Package wiring is the single owner of adapter construction: it knows how to
// build the Plane and Telegram clients from a config.Config, so no caller has
// to restate that mapping. Both the command entry point and the configuration
// TUI build their adapters through here.
//
// Scope, honestly: this removes the duplicated construction knowledge, it does
// NOT make internal/tui port-pure. The configuration TUI genuinely needs
// Client.ListStates, whose result type is plane.State, so internal/tui still
// depends on internal/plane transitively through this package. Hiding that
// behind locally defined mirror types would buy an import-graph cosmetic at the
// price of a translation layer and a second type to keep in sync — a worse
// trade. The dependency is real; it is left visible.
package wiring

import (
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/plane"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/telegram"
)

// PlaneClient builds the raw Plane client — what callers that only read from
// Plane need (e.g. fetching the project's workflow states).
func PlaneClient(cfg config.Config) *plane.Client {
	return plane.New(plane.Config{
		BaseURL:       cfg.Plane.BaseURL,
		Token:         cfg.Plane.APIToken,
		WorkspaceSlug: cfg.Plane.WorkspaceSlug,
		ProjectID:     cfg.Plane.ProjectID,
	})
}

// PlaneSyncer builds the Plane syncer over the local store, with the configured
// state defaults and default estimate already applied.
func PlaneSyncer(cfg config.Config, st store.TaskStore) *plane.Syncer {
	s := plane.NewSyncer(PlaneClient(cfg), st, cfg.Plane.StateDefaults)
	s.SetEstimate(cfg.Plane.DefaultEstimate)
	return s
}

// TelegramClient builds the Telegram client used to deliver daily digests and
// test notifications.
func TelegramClient(cfg config.Config) *telegram.Client {
	return telegram.New(cfg.Telegram.BotToken, cfg.Telegram.ChatID, cfg.Telegram.ThreadID)
}
