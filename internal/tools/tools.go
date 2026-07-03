// Package tools exposes the task operations to the LLM as callable tools and
// executes the calls against the store. This is the deterministic bridge:
// the model decides *what*, this package decides *how* and validates.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/memory"
	"github.com/webcloster-dev/planner/internal/store"
)

// Registry wires the tool set to a task store and (optionally) long-term memory.
type Registry struct {
	store store.TaskStore
	mem   memory.Memory
}

// New builds a tool registry over a store.
func New(s store.TaskStore) *Registry { return &Registry{store: s} }

// SetMemory enables the recall/remember tools when a memory backend is present.
func (r *Registry) SetMemory(m memory.Memory) { r.mem = m }

func (r *Registry) memEnabled() bool { return r.mem != nil && r.mem.Available() }

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
				"status":      enumProp("Initial status", statuses()...),
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
			Description: "Change a task's status (e.g. postpone, block, complete, reject).",
			Parameters: obj(props{
				"id":     intProp("Task id"),
				"status": enumProp("New status", statuses()...),
			}, "id", "status"),
		},
		{
			Name:        "set_state",
			Description: "Set the Plane state name for a task (the concrete workflow state).",
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
			}, "id"),
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
	case "recall_memory":
		return r.recallMemory(ctx, args)
	case "remember_note":
		return r.rememberNote(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
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
		status = domain.StatusTodo
	}
	if !status.Valid() {
		return "", fmt.Errorf("create_task: invalid status %q", in.Status)
	}
	label := in.Label
	if label == "" {
		label = fmt.Sprintf("%s-%s", strings.ToLower(string(tt)), slug(in.Title))
	}
	t, err := r.store.Create(ctx, domain.Task{
		Label: label, Type: tt, Title: in.Title, Description: in.Description, Status: status,
	})
	if err != nil {
		return "", err
	}
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
	}
	if err := json.Unmarshal([]byte(orEmptyObj(args)), &in); err != nil {
		return "", fmt.Errorf("set_details: bad args: %w", err)
	}
	t, err := r.store.Get(ctx, in.ID)
	if err != nil {
		return "", err
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
	return marshal(view(t))
}

// --- helpers ---

type taskView struct {
	ID       int64  `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	State    string `json:"state,omitempty"`
	WorkItem string `json:"work_item,omitempty"` // Plane id; empty = not synced yet
}

func view(t domain.Task) taskView {
	return taskView{
		ID: t.ID, Label: t.Label, Type: string(t.Type), Title: t.Title,
		Status: string(t.Status), State: t.State, WorkItem: t.WorkItemID,
	}
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
		string(domain.StatusBacklog), string(domain.StatusTodo), string(domain.StatusInProgress),
		string(domain.StatusPostponed), string(domain.StatusBlocked), string(domain.StatusDone),
		string(domain.StatusRejected), string(domain.StatusCancelled),
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
