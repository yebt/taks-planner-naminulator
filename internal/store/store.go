// Package store defines the task persistence port and its SQLite adapter.
package store

import (
	"context"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/llm"
)

// Filter narrows a List query.
type Filter struct {
	Status       domain.Status // empty = any
	TouchedToday bool          // only tasks interacted with today (for the daily)
	Day          time.Time     // zero = any; else only tasks touched on this calendar day
}

// Daily is a stored daily digest for one calendar date.
type Daily struct {
	Date      string // YYYY-MM-DD
	Content   string
	UpdatedAt time.Time
}

// DailyStore persists daily digests so they can be listed, edited, and resent.
type DailyStore interface {
	SaveDaily(ctx context.Context, date, content string) error
	GetDaily(ctx context.Context, date string) (Daily, error)
	ListDailies(ctx context.Context) ([]Daily, error)
}

// Activity is one recorded interaction with a task.
type Activity struct {
	At   time.Time
	Kind string // create | status | state | details | drop
	Note string
}

// ActivityStore records per-task interactions so a task can surface in the
// digest of every day it was worked on (not only its last touched_at).
type ActivityStore interface {
	LogActivity(ctx context.Context, taskID int64, kind, note string) error
	TasksWithActivityOn(ctx context.Context, day time.Time) ([]domain.Task, error)
	ActivityForTask(ctx context.Context, taskID int64) ([]Activity, error)
}

// ContextStore persists projects and people (referenced as +slug / @nick) and
// their accumulated notes, so the agent can recall context about them.
type ContextStore interface {
	UpsertProject(ctx context.Context, p domain.Project) (domain.Project, error)
	GetProject(ctx context.Context, slug string) (domain.Project, error)
	ListProjects(ctx context.Context) ([]domain.Project, error)
	AddProjectNote(ctx context.Context, slug, kind, text string) error

	UpsertPerson(ctx context.Context, p domain.Person) (domain.Person, error)
	GetPerson(ctx context.Context, nick string) (domain.Person, error)
	ListPeople(ctx context.Context) ([]domain.Person, error)
	AddPersonNote(ctx context.Context, nick, kind, text string) error
}

// TaskStore is the persistence port.
type TaskStore interface {
	Create(ctx context.Context, t domain.Task) (domain.Task, error)
	Get(ctx context.Context, id int64) (domain.Task, error)
	List(ctx context.Context, f Filter) ([]domain.Task, error)
	Update(ctx context.Context, t domain.Task) error
	Delete(ctx context.Context, id int64) error
	Close() error
}

// ConversationStore persists and restores chat sessions.
type ConversationStore interface {
	SaveConversation(ctx context.Context, id int64, title string, msgs []llm.Message) (int64, error)
	ListConversations(ctx context.Context) ([]Conversation, error)
	LoadConversation(ctx context.Context, id int64) ([]llm.Message, error)
}
