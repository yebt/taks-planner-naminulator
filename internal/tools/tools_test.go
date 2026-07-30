package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

type fakeTelegram struct {
	sent       string
	configured bool
}

func (f *fakeTelegram) Configured() bool { return f.configured }
func (f *fakeTelegram) Send(_ context.Context, text string) error {
	f.sent = text
	return nil
}

// fakeSyncer stands in for Plane: it counts pushes and returns a fixed result.
type fakeSyncer struct {
	configured bool
	err        error
	pushes     int
}

func (f *fakeSyncer) Configured() bool { return f.configured }
func (f *fakeSyncer) Push(_ context.Context, _ *domain.Task) error {
	f.pushes++
	return f.err
}
func (f *fakeSyncer) PullStates(_ context.Context) (int, error) { return 0, nil }

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

func TestDailyTools(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := New(st)
	r.SetActivity(st)
	r.SetDailies(st)
	tg := &fakeTelegram{configured: true}
	r.SetTelegram(tg)

	if _, err := r.Dispatch(ctx, "save_daily", `{"date":"2026-07-07","content":"Daily:  2026-07-07\n\nTrabajo:\n  + x"}`); err != nil {
		t.Fatal(err)
	}
	out, err := r.Dispatch(ctx, "get_daily", `{"date":"2026-07-07"}`)
	if err != nil || !strings.Contains(out, "Trabajo") {
		t.Fatalf("get_daily: %s (%v)", out, err)
	}
	if _, err := r.Dispatch(ctx, "send_daily", `{"date":"2026-07-07"}`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tg.sent, "Trabajo") {
		t.Fatalf("telegram did not receive the daily: %q", tg.sent)
	}
	if _, err := r.Dispatch(ctx, "send_daily", `{"date":"1999-01-01"}`); err == nil {
		t.Fatal("sending a missing daily should error")
	}

	// telegram unconfigured → send errors
	r2 := New(st)
	r2.SetDailies(st)
	r2.SetTelegram(&fakeTelegram{configured: false})
	if _, err := r2.Dispatch(ctx, "send_daily", `{"date":"2026-07-07"}`); err == nil {
		t.Fatal("send with unconfigured telegram should error")
	}

	// list_day_tasks includes tasks worked today (create logs activity)
	_, _ = r.Dispatch(ctx, "create_task", `{"type":"feat","title":"Worked today"}`)
	lt, err := r.Dispatch(ctx, "list_day_tasks", `{"day":"today"}`)
	if err != nil {
		t.Fatal(err)
	}
	var views []taskView
	_ = json.Unmarshal([]byte(lt), &views)
	if len(views) == 0 {
		t.Fatalf("list_day_tasks should include today's task: %s", lt)
	}
}

// newDailyReg builds a registry with activity, dailies and a configured
// Telegram sender, so the day-scoped tools are all reachable.
func newDailyReg(t *testing.T) (*Registry, *fakeTelegram) {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := New(st)
	r.SetActivity(st)
	r.SetDailies(st)
	tg := &fakeTelegram{configured: true}
	r.SetTelegram(tg)
	return r, tg
}

// dayToolNames are the tools that accept optional arguments; a truncated or
// otherwise malformed payload must never be silently defaulted away.
var dayToolNames = []string{"send_daily", "get_daily", "list_day_tasks", "list_tasks"}

func TestDispatchRejectsMalformedArgs(t *testing.T) {
	ctx := context.Background()
	r, tg := newDailyReg(t)
	// a daily exists for today, so only the bad JSON can stop send_daily
	if _, err := r.Dispatch(ctx, "save_daily", `{"content":"Daily de hoy"}`); err != nil {
		t.Fatal(err)
	}

	for _, name := range dayToolNames {
		if _, err := r.Dispatch(ctx, name, "{not json"); err == nil {
			t.Fatalf("%s: malformed arguments should error", name)
		}
	}
	if tg.sent != "" {
		t.Fatalf("malformed send_daily must not deliver anything, got %q", tg.sent)
	}
}

func TestDispatchAcceptsEmptyArgs(t *testing.T) {
	ctx := context.Background()
	r, _ := newDailyReg(t)
	if _, err := r.Dispatch(ctx, "save_daily", `{"content":"Daily de hoy"}`); err != nil {
		t.Fatal(err)
	}

	for _, args := range []string{"", "{}"} {
		for _, name := range dayToolNames {
			if _, err := r.Dispatch(ctx, name, args); err != nil {
				t.Fatalf("%s with args %q should still work: %v", name, args, err)
			}
		}
	}
}

func TestDayFromKeepsLocalDayWindow(t *testing.T) {
	today, err := dayFrom("hoy")
	if err != nil {
		t.Fatal(err)
	}
	if today.Location() != time.Local {
		t.Fatalf("hoy resolved outside the local zone: %s", today.Location())
	}
	// the store slices its window with day.Location(), so an explicit date must
	// land in the same zone as "hoy" or the two cover different hours
	explicit, err := dayFrom(today.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Location() != time.Local {
		t.Fatalf("explicit date resolved outside the local zone: %s", explicit.Location())
	}
	ty, tm, td := today.Date()
	ey, em, ed := explicit.Date()
	if ty != ey || tm != em || td != ed {
		t.Fatalf("hoy and its explicit date disagree: %s vs %s", today, explicit)
	}
	start := time.Date(ty, tm, td, 0, 0, 0, 0, today.Location())
	if got := time.Date(ey, em, ed, 0, 0, 0, 0, explicit.Location()); !got.Equal(start) {
		t.Fatalf("day windows differ: %s vs %s", start, got)
	}
}

func TestDayFromRejectsInvalidDate(t *testing.T) {
	for _, in := range []string{"2026-13-45", "mañana-quizá", "32/01/2026"} {
		if _, err := dayFrom(in); err == nil {
			t.Fatalf("dayFrom(%q) should reject an invalid date", in)
		}
	}
	// an absent date is the intentional "today" default, not an error
	if _, err := dayFrom(""); err != nil {
		t.Fatalf("empty date should default to today: %v", err)
	}

	ctx := context.Background()
	r, _ := newDailyReg(t)
	if _, err := r.Dispatch(ctx, "get_daily", `{"date":"2026-13-45"}`); err == nil {
		t.Fatal("get_daily should reject an invalid date instead of reading today")
	}
	if _, err := r.Dispatch(ctx, "list_day_tasks", `{"day":"mañana-quizá"}`); err == nil {
		t.Fatal("list_day_tasks should reject an invalid day instead of listing today")
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

// rawKeys decodes a tool result as a raw JSON object so a test can assert that
// a key is absent, not merely empty.
func rawKeys(t *testing.T, out string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result is not a JSON object: %s (%v)", out, err)
	}
	return m
}

// newSyncReg builds a registry wired to the given syncer.
func newSyncReg(t *testing.T, s Syncer) *Registry {
	t.Helper()
	r := newReg(t)
	r.SetSyncer(s)
	return r
}

func TestPushFailureReportedButLocalWriteSucceeds(t *testing.T) {
	ctx := context.Background()
	sy := &fakeSyncer{configured: true, err: errors.New("plane: 401 unauthorized")}
	r := newSyncReg(t, sy)

	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"Login Screen"}`)
	if err != nil {
		t.Fatalf("a failed Plane push must not fail create_task: %v", err)
	}
	if sy.pushes != 1 {
		t.Fatalf("expected 1 push attempt, got %d", sy.pushes)
	}
	var created taskView
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatalf("task should still be created locally: %s", out)
	}
	if _, ok := rawKeys(t, out)["sync_error"]; !ok {
		t.Fatalf("create_task result should carry sync_error: %s", out)
	}
	if !strings.Contains(created.SyncError, "401 unauthorized") {
		t.Fatalf("sync_error should carry the reason, got %q", created.SyncError)
	}
	// the task is really in the store, not just in the response
	if _, err := r.store.Get(ctx, created.ID); err != nil {
		t.Fatalf("task not persisted after a failed push: %v", err)
	}

	// not create-only: a mutating tool reports the same way
	statusOut, err := r.Dispatch(ctx, "set_status", `{"id":`+itoa(created.ID)+`,"status":"started"}`)
	if err != nil {
		t.Fatalf("a failed Plane push must not fail set_status: %v", err)
	}
	if _, ok := rawKeys(t, statusOut)["sync_error"]; !ok {
		t.Fatalf("set_status result should carry sync_error: %s", statusOut)
	}
	var updated taskView
	_ = json.Unmarshal([]byte(statusOut), &updated)
	if updated.Status != "started" {
		t.Fatalf("status should still be applied locally: %+v", updated)
	}
	if !strings.Contains(updated.SyncError, "401 unauthorized") {
		t.Fatalf("sync_error should carry the reason, got %q", updated.SyncError)
	}
	tk, err := r.store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(tk.Status) != "started" {
		t.Fatalf("status not persisted after a failed push: %+v", tk)
	}
}

func TestPushSuccessOmitsSyncError(t *testing.T) {
	ctx := context.Background()
	sy := &fakeSyncer{configured: true}
	r := newSyncReg(t, sy)

	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sy.pushes != 1 {
		t.Fatalf("expected 1 push attempt, got %d", sy.pushes)
	}
	if _, ok := rawKeys(t, out)["sync_error"]; ok {
		t.Fatalf("a successful push must not add sync_error: %s", out)
	}
	var created taskView
	_ = json.Unmarshal([]byte(out), &created)
	statusOut, err := r.Dispatch(ctx, "set_status", `{"id":`+itoa(created.ID)+`,"status":"started"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rawKeys(t, statusOut)["sync_error"]; ok {
		t.Fatalf("a successful push must not add sync_error: %s", statusOut)
	}
}

func TestPlaneUnconfiguredNeverPushesNorReportsSyncError(t *testing.T) {
	ctx := context.Background()
	// an unconfigured syncer: Push must never be attempted
	sy := &fakeSyncer{configured: false, err: errors.New("should not be called")}
	r := newSyncReg(t, sy)

	out, err := r.Dispatch(ctx, "create_task", `{"type":"feat","title":"X"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sy.pushes != 0 {
		t.Fatalf("unconfigured Plane must not be pushed to, got %d pushes", sy.pushes)
	}
	if _, ok := rawKeys(t, out)["sync_error"]; ok {
		t.Fatalf("unconfigured Plane must not add sync_error: %s", out)
	}

	// no syncer at all behaves the same way
	plain := newReg(t)
	plainOut, err := plain.Dispatch(ctx, "create_task", `{"type":"feat","title":"X"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rawKeys(t, plainOut)["sync_error"]; ok {
		t.Fatalf("no syncer must not add sync_error: %s", plainOut)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
