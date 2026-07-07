package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/store"
)

func newReg(t *testing.T) *Registry {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestCreateAndListAndStatus(t *testing.T) {
	ctx := context.Background()
	r := newReg(t)

	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"Login Screen"}`)
	if err != nil {
		t.Fatal(err)
	}
	var created taskView
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Type != "FEAT" || created.Status != "unstarted" {
		t.Fatalf("bad create result: %+v", created)
	}
	if created.Label != "feat-login-screen" {
		t.Fatalf("bad auto label: %q", created.Label)
	}

	listOut, err := r.Dispatch(ctx, "list_tasks", "")
	if err != nil {
		t.Fatal(err)
	}
	var list []taskView
	if err := json.Unmarshal([]byte(listOut), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 task, got %d", len(list))
	}

	statusOut, err := r.Dispatch(ctx, "set_status",
		`{"id":`+itoa(created.ID)+`,"status":"started"}`)
	if err != nil {
		t.Fatal(err)
	}
	var updated taskView
	_ = json.Unmarshal([]byte(statusOut), &updated)
	if updated.Status != "started" {
		t.Fatalf("status not updated: %+v", updated)
	}
}

func TestSetDetails(t *testing.T) {
	ctx := context.Background()
	r := newReg(t)
	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"Login"}`)
	if err != nil {
		t.Fatal(err)
	}
	var created taskView
	_ = json.Unmarshal([]byte(out), &created)

	_, err = r.Dispatch(ctx, "set_details", `{"id":`+itoa(created.ID)+`,"objective":"Let users log in","as_a":"user","acceptance_criteria":["Dado X Cuando Y Entonces Z"]}`)
	if err != nil {
		t.Fatal(err)
	}
	// verify persistence through the store
	tk, err := r.store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Details.Objective != "Let users log in" || tk.Details.AsA != "user" {
		t.Fatalf("details not persisted: %+v", tk.Details)
	}
	if len(tk.Details.AcceptanceCriteria) != 1 {
		t.Fatalf("acceptance criteria not persisted: %+v", tk.Details.AcceptanceCriteria)
	}
}

func TestDropTask(t *testing.T) {
	ctx := context.Background()
	r := newReg(t)
	out, _ := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X"}`)
	var created taskView
	_ = json.Unmarshal([]byte(out), &created)

	dropped, err := r.Dispatch(ctx, "drop_task", `{"id":`+itoa(created.ID)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	var dv taskView
	_ = json.Unmarshal([]byte(dropped), &dv)
	if dv.Label != created.Label {
		t.Fatalf("drop should report the removed task: %+v", dv)
	}

	list, _ := r.Dispatch(ctx, "list_tasks", "")
	var tasks []taskView
	_ = json.Unmarshal([]byte(list), &tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after drop, got %d", len(tasks))
	}
	if _, err := r.Dispatch(ctx, "drop_task", `{"id":`+itoa(created.ID)+`}`); err == nil {
		t.Fatal("expected error dropping a missing task")
	}
}

func TestCreateWithDates(t *testing.T) {
	ctx := context.Background()
	r := newReg(t)
	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X","start_date":"2026-06-01","due_date":"2026-06-02"}`)
	if err != nil {
		t.Fatal(err)
	}
	var v taskView
	_ = json.Unmarshal([]byte(out), &v)
	if v.StartDate != "2026-06-01" || v.DueDate != "2026-06-02" {
		t.Fatalf("dates not set: %+v", v)
	}

	if _, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"Y","due_date":"06-2026"}`); err == nil {
		t.Fatal("expected error for invalid date format")
	}
}

func TestCreateDefaultsDates(t *testing.T) {
	ctx := context.Background()
	r := newReg(t)
	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X"}`)
	if err != nil {
		t.Fatal(err)
	}
	var v taskView
	_ = json.Unmarshal([]byte(out), &v)
	if v.StartDate == "" || v.DueDate == "" {
		t.Fatalf("dates should default: %+v", v)
	}
	start, err := time.Parse("2006-01-02", v.StartDate)
	if err != nil {
		t.Fatalf("bad start date: %q", v.StartDate)
	}
	if got := start.AddDate(0, 0, 1).Format("2006-01-02"); got != v.DueDate {
		t.Fatalf("due should be start+1 day: start=%s due=%s", v.StartDate, v.DueDate)
	}
}

func TestActivityLoggedOnMutations(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := New(st)
	r.SetActivity(st)

	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X"}`)
	if err != nil {
		t.Fatal(err)
	}
	var v taskView
	_ = json.Unmarshal([]byte(out), &v)
	if _, err := r.Dispatch(ctx, "set_status", `{"id":`+itoa(v.ID)+`,"status":"started"}`); err != nil {
		t.Fatal(err)
	}

	acts, err := st.ActivityForTask(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 || acts[0].Kind != "create" || acts[1].Kind != "status" {
		t.Fatalf("expected create+status activity, got %+v", acts)
	}
}

func TestContextTools(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := New(st)
	r.SetContext(st)

	if n := len(r.Definitions()); n != 10 { // 6 base + 4 context
		t.Fatalf("expected 10 tools with context enabled, got %d", n)
	}

	if _, err := r.Dispatch(ctx, "upsert_project", `{"slug":"liquida","description":"PHP app"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(ctx, "add_project_note", `{"slug":"LIQUIDA","kind":"decision","text":"migró a PHP 8.3"}`); err != nil {
		t.Fatal(err)
	}
	p, err := st.GetProject(ctx, "liquida")
	if err != nil {
		t.Fatal(err)
	}
	if p.Description != "PHP app" || len(p.Notes) != 1 || p.Notes[0].Kind != "decision" {
		t.Fatalf("project not persisted via tools: %+v", p)
	}
	if _, err := r.Dispatch(ctx, "upsert_project", `{"description":"x"}`); err == nil {
		t.Fatal("expected error for missing slug")
	}

	// person; a blank/omitted kind defaults to "info"
	if _, err := r.Dispatch(ctx, "upsert_person", `{"nick":"kari","role":"comercial"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(ctx, "add_person_note", `{"nick":"kari","text":"pidió cambios"}`); err != nil {
		t.Fatal(err)
	}
	per, _ := st.GetPerson(ctx, "kari")
	if len(per.Notes) != 1 || per.Notes[0].Kind != "info" {
		t.Fatalf("person note default kind wrong: %+v", per.Notes)
	}
}

func TestCreateWithProject(t *testing.T) {
	ctx := context.Background()
	r := newReg(t)
	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X","project":"liquida"}`)
	if err != nil {
		t.Fatal(err)
	}
	var v taskView
	_ = json.Unmarshal([]byte(out), &v)
	if v.Project != "liquida" {
		t.Fatalf("project link not set: %+v", v)
	}
	tk, _ := r.store.Get(ctx, v.ID)
	if tk.Project != "liquida" {
		t.Fatalf("project not persisted: %+v", tk)
	}
}

func TestCreateInvalidType(t *testing.T) {
	r := newReg(t)
	if _, err := r.Dispatch(context.Background(), "create_task", `{"type":"nope","title":"x"}`); err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestUnknownTool(t *testing.T) {
	r := newReg(t)
	if _, err := r.Dispatch(context.Background(), "frobnicate", "{}"); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDefinitionsShape(t *testing.T) {
	r := newReg(t)
	defs := r.Definitions()
	if len(defs) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(defs))
	}
	for _, d := range defs {
		if d.Parameters["type"] != "object" {
			t.Fatalf("tool %s params not an object schema", d.Name)
		}
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
