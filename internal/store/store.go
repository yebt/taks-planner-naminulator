// Package store defines the task persistence port and its SQLite adapter.
package store

import (
	"context"

	"github.com/webcloster-dev/planner/internal/domain"
)

// Filter narrows a List query.
type Filter struct {
	Status       domain.Status // empty = any
	TouchedToday bool          // only tasks interacted with today (for the daily)
}

// TaskStore is the persistence port.
type TaskStore interface {
	Create(ctx context.Context, t domain.Task) (domain.Task, error)
	Get(ctx context.Context, id int64) (domain.Task, error)
	List(ctx context.Context, f Filter) ([]domain.Task, error)
	Update(ctx context.Context, t domain.Task) error
	Close() error
}
