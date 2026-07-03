package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

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
	if created.ID == 0 || created.Type != "FEAT" || created.Status != "todo" {
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
		`{"id":`+itoa(created.ID)+`,"status":"in_progress"}`)
	if err != nil {
		t.Fatal(err)
	}
	var updated taskView
	_ = json.Unmarshal([]byte(statusOut), &updated)
	if updated.Status != "in_progress" {
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
	if len(defs) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(defs))
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
