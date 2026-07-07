// Package tools exposes the task operations to the LLM as callable tools and
// executes the calls against the store. This is the deterministic bridge:
// the model decides *what*, this package decides *how* and validates.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/memory"
	"github.com/webcloster-dev/planner/internal/store"
)

// Syncer pushes a task to Plane and refreshes states. Implemented by internal/plane.
type Syncer interface {
	Configured() bool
	Push(ctx context.Context, t *domain.Task) error
	PullStates(ctx context.Context) (int, error)
}

// Registry wires the tool set to a task store, long-term memory, and Plane sync.
type Registry struct {
	store    store.TaskStore
	mem      memory.Memory
	sync     Syncer
	activity store.ActivityStore
	ctxStore store.ContextStore
}

// New builds a tool registry over a store.
func New(s store.TaskStore) *Registry { return &Registry{store: s} }

// SetMemory enables the recall/remember tools when a memory backend is present.
func (r *Registry) SetMemory(m memory.Memory) { r.mem = m }

// SetSyncer enables automatic push to Plane after a task mutation.
func (r *Registry) SetSyncer(s Syncer) { r.sync = s }

// SetActivity enables per-task activity logging after each mutation.
func (r *Registry) SetActivity(a store.ActivityStore) { r.activity = a }

// SetContext enables the project/person context tools.
func (r *Registry) SetContext(c store.ContextStore) { r.ctxStore = c }

func (r *Registry) ctxEnabled() bool { return r.ctxStore != nil }

// logActivity best-effort records a task interaction (never fails the op).
func (r *Registry) logActivity(ctx context.Context, taskID int64, kind, note string) {
	if r.activity != nil {
		_ = r.activity.LogActivity(ctx, taskID, kind, note)
	}
}

func (r *Registry) memEnabled() bool { return r.mem != nil && r.mem.Available() }

// pushSync best-effort pushes a task to Plane (local-first: sync errors don't
// fail the local operation; the returned work_item reflects success).
func (r *Registry) pushSync(ctx context.Context, t *domain.Task) {
	if r.sync != nil && r.sync.Configured() {
		_ = r.sync.Push(ctx, t)
	}
}

// Definitions returns the provider-agnostic tool schemas.
func (r *Registry) Definitions() []llm.Tool {
	defs := []llm.Tool{
		{
			Name:        "create_task",
			Description: "Create a new task in the local planner. Use when the user starts or mentions new work.",
			Parameters: obj(props{
				"type":        enumProp("Activity type", "FEAT", "FIX", "HOTFIX", "TEST", "EPIC"),
				"title":       strProp("Short task title"),
				"description": strProp("Optional longer description"),
				"label":       strProp("Optional short label; auto-generated from type+title if omitted"),
				"status":      enumProp("Initial status — one of Plane's 5 state groups (default unstarted)", statuses()...),
				"start_date":  strProp("Plane start date, YYYY-MM-DD (defaults to today if omitted)"),
				"due_date":    strProp("Plane due date, YYYY-MM-DD (defaults to start_date + 1 day)"),
				"project":     strProp("Linked project slug when the task belongs to a mentioned +project"),
			}, "type", "title"),
		},
		{
			Name:        "list_tasks",
			Description: "List local tasks, optionally filtered by status.",
			Parameters: obj(props{
				"status": enumProp("Optional status filter", statuses()...),
			}),
		},
		{
			Name:        "set_status",
			Description: "Move a task between Plane's 5 state groups (backlog, unstarted, started, completed, cancelled).",
			Parameters: obj(props{
				"id":     intProp("Task id"),
				"status": enumProp("New status — one of Plane's 5 state groups", statuses()...),
			}, "id", "status"),
		},
		{
			Name:        "set_state",
			Description: "Set the concrete Plane state name for a task (e.g. 'In Progress', 'Devuelto por Calidad') within its group.",
			Parameters: obj(props{
				"id":    intProp("Task id"),
				"state": strProp("Plane state name, e.g. 'In Progress'"),
			}, "id", "state"),
		},
		{
			Name:        "set_details",
			Description: "Enrich a task with activity-template fields. Only the fields you pass are updated.",
			Parameters: obj(props{
				"id":                  intProp("Task id"),
				"objective":           strProp("Objetivo — central purpose in 1-2 sentences"),
				"justification":       strProp("Justificación — why it matters / value"),
				"as_a":                strProp("Como — the role/user"),
				"i_want":              strProp("Quiero — the desired capability"),
				"so_that":             strProp("Para — the outcome/benefit"),
				"acceptance_criteria": arrProp("Criterios de aceptación (Dado/Cuando/Entonces)"),
				"preconditions":       arrProp("Pre-condiciones"),
				"tech_notes":          strProp("Consideraciones técnicas"),
				"start_date":          strProp("Plane start date, YYYY-MM-DD"),
				"due_date":            strProp("Plane due date (target date), YYYY-MM-DD"),
			}, "id"),
		},
		{
			Name:        "drop_task",
			Description: "Delete a task from the planner permanently.",
			Parameters:  obj(props{"id": intProp("Task id")}, "id"),
		},
	}
	if r.memEnabled() {
		defs = append(defs,
			llm.Tool{
				Name:        "recall_memory",
				Description: "Search long-term memory for relevant past notes, decisions, or context.",
				Parameters: obj(props{
					"query": strProp("What to search for"),
					"limit": intProp("Max results (default 5)"),
				}, "query"),
			},
			llm.Tool{
				Name:        "remember_note",
				Description: "Save an important fact or decision to long-term memory for later recall.",
				Parameters: obj(props{
					"title":   strProp("Short title"),
					"content": strProp("The note to remember"),
				}, "title", "content"),
			},
		)
	}
	if r.ctxEnabled() {
		defs = append(defs,
			llm.Tool{
				Name:        "upsert_project",
				Description: "Create or update a project (referenced as +slug). Pass only the fields you want to set.",
				Parameters: obj(props{
					"slug":        strProp("Project slug (the +slug identifier)"),
					"name":        strProp("Human name"),
					"description": strProp("Short summary: stack, purpose, constraints"),
				}, "slug"),
			},
			llm.Tool{
				Name:        "add_project_note",
				Description: "Append context to a project: info, a decision made, or a change that happened.",
				Parameters: obj(props{
					"slug": strProp("Project slug"),
					"kind": enumProp("Note kind", "info", "decision", "change"),
					"text": strProp("The note"),
				}, "slug", "text"),
			},
			llm.Tool{
				Name:        "upsert_person",
				Description: "Create or update a person (referenced as @nick). Pass only the fields you want to set.",
				Parameters: obj(props{
					"nick": strProp("Person nick (the @nick identifier)"),
					"name": strProp("Full name"),
					"role": strProp("Area / role, e.g. 'área comercial'"),
				}, "nick"),
			},
			llm.Tool{
				Name:        "add_person_note",
				Description: "Append context about a person: info, a decision, or a change.",
				Parameters: obj(props{
					"nick": strProp("Person nick"),
					"kind": enumProp("Note kind", "info", "decision", "change"),
					"text": strProp("The note"),
				}, "nick", "text"),
			},
		)
	}
	return defs
}

// Dispatch runs a tool by name with raw JSON arguments and returns a JSON result.
func (r *Registry) Dispatch(ctx context.Context, name, args string) (string, error) {
	switch name {
	case "create_task":
		return r.createTask(ctx, args)
	case "list_tasks":
		return r.listTasks(ctx, args)
	case "set_status":
		return r.setStatus(ctx, args)
	case "set_state":
		return r.setState(ctx, args)
	case "set_details":
		return r.setDetails(ctx, args)
	case "drop_task":
		return r.dropTask(ctx, args)
	case "recall_memory":
		return r.recallMemory(ctx, args)
	case "remember_note":
		return r.rememberNote(ctx, args)
	case "upsert_project":
		return r.upsertProject(ctx, args)
	case "add_project_note":
		return r.addProjectNote(ctx, args)
	case "upsert_person":
		return r.upsertPerson(ctx, args)
	case "add_person_note":
		return r.addPersonNote(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (r *Registry) upsertProject(ctx context.Context, args string) (string, error) {
	var in struct{ Slug, Name, Description string }
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("upsert_project: bad args: %w", err)
	}
	if strings.TrimSpace(in.Slug) == "" {
		return "", fmt.Errorf("upsert_project: slug is required")
	}
	p, err := r.ctxStore.UpsertProject(ctx, domain.Project{Slug: in.Slug, Name: in.Name, Description: in.Description})
	if err != nil {
		return "", err
	}
	return marshal(map[string]any{"label": "+" + p.Slug})
}

func (r *Registry) addProjectNote(ctx context.Context, args string) (string, error) {
	var in struct{ Slug, Kind, Text string }
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("add_project_note: bad args: %w", err)
	}
	if strings.TrimSpace(in.Slug) == "" || strings.TrimSpace(in.Text) == "" {
		return "", fmt.Errorf("add_project_note: slug and text are required")
	}
	if err := r.ctxStore.AddProjectNote(ctx, in.Slug, noteKind(in.Kind), in.Text); err != nil {
		return "", err
	}
	return marshal(map[string]any{"label": "+" + in.Slug})
}

func (r *Registry) upsertPerson(ctx context.Context, args string) (string, error) {
	var in struct{ Nick, Name, Role string }
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("upsert_person: bad args: %w", err)
	}
	if strings.TrimSpace(in.Nick) == "" {
		return "", fmt.Errorf("upsert_person: nick is required")
	}
	p, err := r.ctxStore.UpsertPerson(ctx, domain.Person{Nick: in.Nick, Name: in.Name, Role: in.Role})
	if err != nil {
		return "", err
	}
	return marshal(map[string]any{"label": "@" + p.Nick})
}

func (r *Registry) addPersonNote(ctx context.Context, args string) (string, error) {
	var in struct{ Nick, Kind, Text string }
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("add_person_note: bad args: %w", err)
	}
	if strings.TrimSpace(in.Nick) == "" || strings.TrimSpace(in.Text) == "" {
		return "", fmt.Errorf("add_person_note: nick and text are required")
	}
	if err := r.ctxStore.AddPersonNote(ctx, in.Nick, noteKind(in.Kind), in.Text); err != nil {
		return "", err
	}
	return marshal(map[string]any{"label": "@" + in.Nick})
}

// noteKind defaults a blank/unknown kind to "info".
func noteKind(k string) string {
	switch k {
	case "info", "decision", "change":
		return k
	default:
		return "info"
	}
}

func (r *Registry) recallMemory(ctx context.Context, args string) (string, error) {
	if !r.memEnabled() {
		return "", fmt.Errorf("memory backend not available")
	}
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("recall_memory: bad args: %w", err)
	}
	return r.mem.Recall(ctx, in.Query, in.Limit)
}

func (r *Registry) rememberNote(ctx context.Context, args string) (string, error) {
	if !r.memEnabled() {
		return "", fmt.Errorf("memory backend not available")
	}
	var in struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("remember_note: bad args: %w", err)
	}
	if err := r.mem.Save(ctx, in.Title, in.Content); err != nil {
		return "", err
	}
	return `{"saved":true}`, nil
}

func (r *Registry) createTask(ctx context.Context, args string) (string, error) {
	var in struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Label       string `json:"label"`
		Status      string `json:"status"`
		StartDate   string `json:"start_date"`
		DueDate     string `json:"due_date"`
		Project     string `json:"project"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("create_task: bad args: %w", err)
	}
	tt := domain.TaskType(strings.ToUpper(in.Type))
	if !tt.Valid() {
		return "", fmt.Errorf("create_task: invalid type %q", in.Type)
	}
	if strings.TrimSpace(in.Title) == "" {
		return "", fmt.Errorf("create_task: title is required")
	}
	status := domain.Status(in.Status)
	if status == "" {
		status = domain.StatusUnstarted
	}
	if !status.Valid() {
		return "", fmt.Errorf("create_task: invalid status %q", in.Status)
	}
	if err := validDate("start_date", in.StartDate); err != nil {
		return "", err
	}
	if err := validDate("due_date", in.DueDate); err != nil {
		return "", err
	}
	// Default the work-item dates: start today, due one day later. The model can
	// still pass explicit dates (e.g. to space a batch of tasks across a month).
	if in.StartDate == "" {
		in.StartDate = time.Now().Format("2006-01-02")
	}
	if in.DueDate == "" {
		start, _ := time.Parse("2006-01-02", in.StartDate)
		in.DueDate = start.AddDate(0, 0, 1).Format("2006-01-02")
	}
	label := in.Label
	if label == "" {
		label = fmt.Sprintf("%s-%s", strings.ToLower(string(tt)), slug(in.Title))
	}
	t, err := r.store.Create(ctx, domain.Task{
		Label: label, Type: tt, Title: in.Title, Description: in.Description, Status: status,
		StartDate: in.StartDate, DueDate: in.DueDate, Project: strings.TrimSpace(in.Project),
	})
	if err != nil {
		return "", err
	}
	r.pushSync(ctx, &t)
	r.logActivity(ctx, t.ID, "create", "created "+t.Label)
	return marshal(view(t))
}

func (r *Registry) listTasks(ctx context.Context, args string) (string, error) {
	var in struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(orEmptyObj(args)), &in)
	tasks, err := r.store.List(ctx, store.Filter{Status: domain.Status(in.Status)})
	if err != nil {
		return "", err
	}
	views := make([]taskView, 0, len(tasks))
	for _, t := range tasks {
		views = append(views, view(t))
	}
	return marshal(views)
}

func (r *Registry) setStatus(ctx context.Context, args string) (string, error) {
	var in struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("set_status: bad args: %w", err)
	}
	status := domain.Status(in.Status)
	if !status.Valid() {
		return "", fmt.Errorf("set_status: invalid status %q", in.Status)
	}
	t, err := r.store.Get(ctx, in.ID)
	if err != nil {
		return "", err
	}
	t.Status = status
	if err := r.store.Update(ctx, t); err != nil {
		return "", err
	}
	r.pushSync(ctx, &t)
	r.logActivity(ctx, t.ID, "status", "→ "+string(status))
	return marshal(view(t))
}

func (r *Registry) setState(ctx context.Context, args string) (string, error) {
	var in struct {
		ID    int64  `json:"id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("set_state: bad args: %w", err)
	}
	t, err := r.store.Get(ctx, in.ID)
	if err != nil {
		return "", err
	}
	t.State = in.State
	if err := r.store.Update(ctx, t); err != nil {
		return "", err
	}
	r.pushSync(ctx, &t)
	r.logActivity(ctx, t.ID, "state", "state: "+in.State)
	return marshal(view(t))
}

func (r *Registry) setDetails(ctx context.Context, args string) (string, error) {
	var in struct {
		ID                 int64    `json:"id"`
		Objective          string   `json:"objective"`
		Justification      string   `json:"justification"`
		AsA                string   `json:"as_a"`
		IWant              string   `json:"i_want"`
		SoThat             string   `json:"so_that"`
		AcceptanceCriteria []string `json:"acceptance_criteria"`
		Preconditions      []string `json:"preconditions"`
		TechNotes          string   `json:"tech_notes"`
		StartDate          string   `json:"start_date"`
		DueDate            string   `json:"due_date"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("set_details: bad args: %w", err)
	}
	if err := validDate("start_date", in.StartDate); err != nil {
		return "", err
	}
	if err := validDate("due_date", in.DueDate); err != nil {
		return "", err
	}
	t, err := r.store.Get(ctx, in.ID)
	if err != nil {
		return "", err
	}
	if in.StartDate != "" {
		t.StartDate = in.StartDate
	}
	if in.DueDate != "" {
		t.DueDate = in.DueDate
	}
	d := &t.Details
	if in.Objective != "" {
		d.Objective = in.Objective
	}
	if in.Justification != "" {
		d.Justification = in.Justification
	}
	if in.AsA != "" {
		d.AsA = in.AsA
	}
	if in.IWant != "" {
		d.IWant = in.IWant
	}
	if in.SoThat != "" {
		d.SoThat = in.SoThat
	}
	if len(in.AcceptanceCriteria) > 0 {
		d.AcceptanceCriteria = in.AcceptanceCriteria
	}
	if len(in.Preconditions) > 0 {
		d.Preconditions = in.Preconditions
	}
	if in.TechNotes != "" {
		d.TechNotes = in.TechNotes
	}
	if err := r.store.Update(ctx, t); err != nil {
		return "", err
	}
	r.pushSync(ctx, &t)
	r.logActivity(ctx, t.ID, "details", "details updated")
	return marshal(view(t))
}

func (r *Registry) dropTask(ctx context.Context, args string) (string, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("drop_task: bad args: %w", err)
	}
	// fetch first so the result can report which task was removed
	t, err := r.store.Get(ctx, in.ID)
	if err != nil {
		return "", err
	}
	if err := r.store.Delete(ctx, in.ID); err != nil {
		return "", err
	}
	r.logActivity(ctx, in.ID, "drop", "dropped "+t.Label)
	return marshal(view(t))
}

// --- helpers ---

type taskView struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	State     string `json:"state,omitempty"`
	WorkItem  string `json:"work_item,omitempty"` // Plane id; empty = not synced yet
	StartDate string `json:"start_date,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
	Project   string `json:"project,omitempty"`
}

func view(t domain.Task) taskView {
	return taskView{
		ID: t.ID, Label: t.Label, Type: string(t.Type), Title: t.Title,
		Status: string(t.Status), State: t.State, WorkItem: t.WorkItemID,
		StartDate: t.StartDate, DueDate: t.DueDate, Project: t.Project,
	}
}

// validDate checks an optional YYYY-MM-DD date.
func validDate(field, v string) error {
	if v == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return fmt.Errorf("%s must be YYYY-MM-DD, got %q", field, v)
	}
	return nil
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func orEmptyObj(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	if out == "" {
		out = "task"
	}
	return out
}

func statuses() []string {
	return []string{
		string(domain.StatusBacklog), string(domain.StatusUnstarted), string(domain.StatusStarted),
		string(domain.StatusCompleted), string(domain.StatusCancelled),
	}
}

// --- tiny JSON-schema builders ---

type props map[string]any

func obj(p props, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": map[string]any(p)}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func enumProp(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

func arrProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
