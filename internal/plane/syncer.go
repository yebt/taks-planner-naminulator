package plane

import (
	"context"
	"fmt"
	"strings"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

// Syncer pushes local tasks to Plane and pulls their states back. Push-only for
// content; states are the only field brought back (and only on demand).
type Syncer struct {
	client        *Client
	store         store.TaskStore
	stateDefaults map[string]string // group -> default state name (reserved for state mapping)
	states        []State
	loaded        bool
}

// NewSyncer builds a syncer over a Plane client and the local store.
func NewSyncer(client *Client, st store.TaskStore, stateDefaults map[string]string) *Syncer {
	return &Syncer{client: client, store: st, stateDefaults: stateDefaults}
}

// Configured reports whether Plane is usable.
func (s *Syncer) Configured() bool { return s.client.Configured() }

func (s *Syncer) ensureStates(ctx context.Context) {
	if s.loaded {
		return
	}
	if st, err := s.client.ListStates(ctx); err == nil {
		s.states = st
	}
	s.loaded = true
}

func (s *Syncer) stateIDByName(ctx context.Context, name string) string {
	if name == "" {
		return ""
	}
	s.ensureStates(ctx)
	for _, st := range s.states {
		if strings.EqualFold(st.Name, name) {
			return st.ID
		}
	}
	return ""
}

func (s *Syncer) stateNameByID(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	s.ensureStates(ctx)
	for _, st := range s.states {
		if st.ID == id {
			return st.Name
		}
	}
	return ""
}

func (s *Syncer) issueInput(ctx context.Context, t *domain.Task) IssueInput {
	in := IssueInput{
		Name:        fmt.Sprintf("%s - %s", t.Type, t.Title),
		Description: t.Description,
		StartDate:   t.StartDate,
		TargetDate:  t.DueDate,
	}
	if t.State != "" {
		in.StateID = s.stateIDByName(ctx, t.State)
	}
	return in
}

// Push creates (or updates) the work item for t and persists work_item_id.
func (s *Syncer) Push(ctx context.Context, t *domain.Task) error {
	if !s.Configured() {
		return nil
	}
	in := s.issueInput(ctx, t)
	if t.WorkItemID == "" {
		id, err := s.client.CreateIssue(ctx, in)
		if err != nil {
			return err
		}
		t.WorkItemID = id
	} else if err := s.client.UpdateIssue(ctx, t.WorkItemID, in); err != nil {
		return err
	}
	return s.store.Update(ctx, *t)
}

// PullStates refreshes local state names from Plane for synced tasks. Returns
// how many local tasks changed.
func (s *Syncer) PullStates(ctx context.Context) (int, error) {
	if !s.Configured() {
		return 0, fmt.Errorf("plane not configured")
	}
	tasks, err := s.store.List(ctx, store.Filter{})
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, t := range tasks {
		if t.WorkItemID == "" {
			continue
		}
		stateID, err := s.client.IssueStateID(ctx, t.WorkItemID)
		if err != nil {
			return updated, err
		}
		name := s.stateNameByID(ctx, stateID)
		if name != "" && name != t.State {
			t.State = name
			if err := s.store.Update(ctx, t); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}
