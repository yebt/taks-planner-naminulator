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
	estimate      string            // default estimate_point for new work items ("" = unset)
	states        []State
	labels        []Label
	loaded        bool
	labelsLoaded  bool
}

// NewSyncer builds a syncer over a Plane client and the local store.
func NewSyncer(client *Client, st store.TaskStore, stateDefaults map[string]string) *Syncer {
	return &Syncer{client: client, store: st, stateDefaults: stateDefaults}
}

// SetEstimate sets the default estimate_point applied to new work items.
func (s *Syncer) SetEstimate(e string) { s.estimate = e }

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

// labelIDForType returns the id of the Plane label whose name matches the task
// type (e.g. type FEAT → the "FEAT" label), or "" when there is no such label.
func (s *Syncer) labelIDForType(ctx context.Context, t domain.TaskType) string {
	if !s.labelsLoaded {
		if ls, err := s.client.ListLabels(ctx); err == nil {
			s.labels = ls
		}
		s.labelsLoaded = true
	}
	for _, l := range s.labels {
		if strings.EqualFold(l.Name, string(t)) {
			return l.ID
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
		Name:       t.DisplayTitle(), // "[TYPE] - #<seq> - <title>"
		HTML:       t.RenderHTML(),   // template-expanded rich-text body
		StartDate:  t.StartDate,
		TargetDate: t.DueDate,
		Priority:   t.PlanePriority(), // medium, or bumped for FIX/HOTFIX
		Estimate:   s.estimate,        // default estimate_point (opt-in via config)
	}
	if id := s.labelIDForType(ctx, t.Type); id != "" {
		in.Labels = []string{id} // tag with the label matching the task type
	}
	in.StateID = s.resolveStateID(ctx, t)
	return in
}

// resolveStateID decides which Plane state a task lands on: an explicit state
// name (set via /state) wins; otherwise the task's semantic status maps to a
// group and we use the configured default state id for that group.
func (s *Syncer) resolveStateID(ctx context.Context, t *domain.Task) string {
	if t.State != "" {
		if id := s.stateIDByName(ctx, t.State); id != "" {
			return id
		}
	}
	if s.stateDefaults != nil {
		if id := s.stateDefaults[t.Status.PlaneGroup()]; id != "" {
			return id
		}
	}
	return ""
}

// Push creates (or updates) the work item for t and persists work_item_id and
// work_item_seq. On first create, the item is renamed once the Plane sequence
// number is known, so the title carries the "#<seq>" code.
func (s *Syncer) Push(ctx context.Context, t *domain.Task) error {
	if !s.Configured() {
		return nil
	}
	if t.WorkItemID == "" {
		ref, err := s.client.CreateIssue(ctx, s.issueInput(ctx, t))
		if err != nil {
			return err
		}
		t.WorkItemID = ref.ID
		t.WorkItemSeq = ref.Seq
		// Rename now that the code exists so the title reads "[TYPE] - #343 - …".
		if ref.Seq > 0 {
			if err := s.client.UpdateIssue(ctx, ref.ID, IssueInput{Name: t.DisplayTitle()}); err != nil {
				return err
			}
		}
	} else if err := s.client.UpdateIssue(ctx, t.WorkItemID, s.issueInput(ctx, t)); err != nil {
		return err
	}
	return s.store.Update(ctx, *t)
}

// Delete removes the work item in Plane. No-op when the task was never synced.
func (s *Syncer) Delete(ctx context.Context, t *domain.Task) error {
	if !s.Configured() || t.WorkItemID == "" {
		return nil
	}
	return s.client.DeleteIssue(ctx, t.WorkItemID)
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
