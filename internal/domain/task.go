package domain

import "time"

// TaskType is the activity type from the naming scheme (docs/template-tasks.md).
type TaskType string

const (
	TypeFeat   TaskType = "FEAT"
	TypeFix    TaskType = "FIX"
	TypeHotfix TaskType = "HOTFIX"
	TypeTest   TaskType = "TEST"
	TypeEpic   TaskType = "EPIC"
)

// Valid reports whether the type is one of the known activity types.
func (t TaskType) Valid() bool {
	switch t {
	case TypeFeat, TypeFix, TypeHotfix, TypeTest, TypeEpic:
		return true
	}
	return false
}

// Status is our own semantic layer sitting on top of Plane's state groups.
// Plane groups (Backlog/Unstarted/Started/Completed/Cancelled) can hold several
// internal states, and some "Completed" states are really rejections
// (e.g. "Devuelto por Calidad") — so we never trust the group blindly.
type Status string

const (
	StatusBacklog    Status = "backlog"
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusPostponed  Status = "postponed"
	StatusBlocked    Status = "blocked"
	StatusDone       Status = "done"
	StatusRejected   Status = "rejected"
	StatusCancelled  Status = "cancelled"
)

// Valid reports whether the status is known.
func (s Status) Valid() bool {
	switch s {
	case StatusBacklog, StatusTodo, StatusInProgress, StatusPostponed,
		StatusBlocked, StatusDone, StatusRejected, StatusCancelled:
		return true
	}
	return false
}

// Task is the local record. It mirrors a Plane work item once synced
// (WorkItemID is the anchor for the push-only sync).
type Task struct {
	ID          int64
	Label       string // short label for quick scan, e.g. "feat-login"
	Type        TaskType
	Title       string
	Description string
	Status      Status
	State       string // Plane state name (e.g. "In Progress"); empty until mapped
	WorkItemID  string // Plane work item id; empty until synced
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TouchedAt   time.Time // last interaction — drives the daily
}
