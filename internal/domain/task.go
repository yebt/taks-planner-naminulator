package domain

import (
	"fmt"
	"html"
	"strings"
	"time"
)

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
	WorkItemID  string // Plane work item id (uuid); empty until synced
	WorkItemSeq int    // Plane sequence id (the "#343" in LIQMSTR-343); 0 until synced
	StartDate   string // YYYY-MM-DD (Plane start_date); required by Plane work items
	DueDate     string // YYYY-MM-DD (Plane target_date)
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TouchedAt   time.Time   // last interaction — drives the daily
	Details     TaskDetails // extended activity-template fields
}

// DisplayTitle is the work-item title: "[TYPE] - #<seq> - <title>". The Plane
// code (#seq) is only present once the item has been created and its sequence
// id is known; before that the code segment is omitted.
func (t Task) DisplayTitle() string {
	if t.WorkItemSeq > 0 {
		return fmt.Sprintf("[%s] - #%d - %s", t.Type, t.WorkItemSeq, t.Title)
	}
	return fmt.Sprintf("[%s] - %s", t.Type, t.Title)
}

// RenderHTML builds the Plane work-item body (description_html) from the
// description and the activity-template fields, as rich-text HTML. Empty
// sections are skipped. Section labels follow the Spanish activity template.
func (t Task) RenderHTML() string {
	var b strings.Builder
	esc := html.EscapeString
	h := func(s string) { b.WriteString("<h2>" + esc(s) + "</h2>") }
	p := func(s string) {
		if strings.TrimSpace(s) != "" {
			b.WriteString("<p>" + esc(s) + "</p>")
		}
	}
	section := func(title, body string) {
		if strings.TrimSpace(body) != "" {
			h(title)
			p(body)
		}
	}
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		h(title)
		b.WriteString("<ul>")
		for _, it := range items {
			b.WriteString("<li>" + esc(it) + "</li>")
		}
		b.WriteString("</ul>")
	}

	d := t.Details
	if strings.TrimSpace(t.Description) != "" {
		h("Descripción funcional")
		p(t.Description)
	}
	section("Objetivo", d.Objective)
	section("Justificación", d.Justification)
	if d.AsA != "" || d.IWant != "" || d.SoThat != "" {
		h("Historia de usuario")
		b.WriteString("<p>Como " + esc(orEmpty(d.AsA)) +
			"<br/>Quiero " + esc(orEmpty(d.IWant)) +
			"<br/>Para " + esc(orEmpty(d.SoThat)) + "</p>")
	}
	list("Pre-condiciones", d.Preconditions)
	list("Criterios de aceptación", d.AcceptanceCriteria)
	section("Consideraciones técnicas", d.TechNotes)
	section("Funcionalidad relacionada", d.RelatedFeature)
	section("Ambiente", d.Environment)
	list("Pasos a reproducir", d.StepsToReproduce)
	section("Resultado actual", d.ActualResult)
	section("Resultado esperado", d.ExpectedResult)
	if len(d.Checklist) > 0 {
		h("Checklist")
		b.WriteString("<ul>")
		for _, it := range d.Checklist {
			mark := "[ ] "
			if it.Done {
				mark = "[x] "
			}
			b.WriteString("<li>" + esc(mark+it.Text) + "</li>")
		}
		b.WriteString("</ul>")
	}
	list("Anexos", d.Links)
	return b.String()
}

func orEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// ChecklistItem is one deliverable in a task's checklist.
type ChecklistItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// TaskDetails holds the extended activity-template fields
// (see docs/template-tasks.md). Everything is optional and serialized as JSON.
type TaskDetails struct {
	Objective          string   `json:"objective,omitempty"`
	Justification      string   `json:"justification,omitempty"`
	AsA                string   `json:"as_a,omitempty"`    // Como
	IWant              string   `json:"i_want,omitempty"`  // Quiero
	SoThat             string   `json:"so_that,omitempty"` // Para
	Preconditions      []string `json:"preconditions,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	// FIX / incident-specific
	RelatedFeature   string   `json:"related_feature,omitempty"`
	Environment      string   `json:"environment,omitempty"`
	StepsToReproduce []string `json:"steps_to_reproduce,omitempty"`
	ActualResult     string   `json:"actual_result,omitempty"`
	ExpectedResult   string   `json:"expected_result,omitempty"`
	// Common
	TechNotes string          `json:"tech_notes,omitempty"`
	Checklist []ChecklistItem `json:"checklist,omitempty"`
	Links     []string        `json:"links,omitempty"`
}
