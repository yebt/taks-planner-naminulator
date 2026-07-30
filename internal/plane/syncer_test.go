package plane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

func TestSyncerPushCreatesAndPersists(t *testing.T) {
	var patchedName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet: // ListStates lookup
			w.Write([]byte(`{"results":[]}`))
		case http.MethodPatch: // rename with the Plane code
			var body map[string]any
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			patchedName, _ = body["name"].(string)
			w.Write([]byte(`{}`))
		default: // create issue
			w.Write([]byte(`{"id":"wi-1","sequence_id":7}`))
		}
	}))
	defer srv.Close()

	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	created, err := st.Create(context.Background(), domain.Task{
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
	})
	if err != nil {
		t.Fatal(err)
	}

	sy := NewSyncer(testClient(srv.URL), st, nil)
	if err := sy.Push(context.Background(), &created); err != nil {
		t.Fatal(err)
	}
	if created.WorkItemID != "wi-1" || created.WorkItemSeq != 7 {
		t.Fatalf("work item not set on task: id=%q seq=%d", created.WorkItemID, created.WorkItemSeq)
	}
	if patchedName != "[FEAT] - #7 - X" {
		t.Fatalf("rename did not embed the code: %q", patchedName)
	}
	got, _ := st.Get(context.Background(), created.ID)
	if got.WorkItemID != "wi-1" || got.WorkItemSeq != 7 {
		t.Fatalf("work item not persisted: id=%q seq=%d", got.WorkItemID, got.WorkItemSeq)
	}
}

func TestSyncerPushSetsLabelAndPriority(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/labels/"):
			w.Write([]byte(`{"results":[{"id":"lbl-fix","name":"FIX"},{"id":"lbl-feat","name":"FEAT"}]}`))
		case r.Method == http.MethodGet:
			w.Write([]byte(`{"results":[]}`))
		case r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &createBody)
			w.Write([]byte(`{"id":"wi-9","sequence_id":9}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	created, _ := st.Create(context.Background(), domain.Task{
		Label: "fix-x", Type: domain.TypeFix, Title: "X", Status: domain.StatusUnstarted,
	})

	sy := NewSyncer(testClient(srv.URL), st, nil)
	if err := sy.Push(context.Background(), &created); err != nil {
		t.Fatal(err)
	}
	if createBody["priority"] != "high" { // FIX bumps priority
		t.Fatalf("priority not set from type: %v", createBody["priority"])
	}
	labels, _ := createBody["labels"].([]any)
	if len(labels) != 1 || labels[0] != "lbl-fix" {
		t.Fatalf("label not matched to type: %v", createBody["labels"])
	}
}

func TestSyncerPushMapsStatusToGroupDefault(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/labels/"):
			w.Write([]byte(`{"results":[]}`))
		case r.Method == http.MethodGet:
			w.Write([]byte(`{"results":[]}`))
		case r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &createBody)
			w.Write([]byte(`{"id":"wi-1","sequence_id":1}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	st, _ := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	// in_progress → group "started"; the configured default for that group wins.
	created, _ := st.Create(context.Background(), domain.Task{
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusStarted,
	})

	defaults := map[string]string{"started": "state-started-id"}
	sy := NewSyncer(testClient(srv.URL), st, defaults)
	if err := sy.Push(context.Background(), &created); err != nil {
		t.Fatal(err)
	}
	if createBody["state"] != "state-started-id" {
		t.Fatalf("status did not map to the group default state: %v", createBody["state"])
	}
}

func TestSyncerPushPersistsIDBeforeRename(t *testing.T) {
	// The create succeeds but the rename PATCH fails. The work item id MUST still
	// be persisted, or a retry would create a duplicate work item.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`{"results":[]}`))
		case http.MethodPatch:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`boom`))
		default: // create
			w.Write([]byte(`{"id":"wi-2","sequence_id":5}`))
		}
	}))
	defer srv.Close()

	st, _ := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	created, _ := st.Create(context.Background(), domain.Task{
		Label: "a", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
	})

	sy := NewSyncer(testClient(srv.URL), st, nil)
	if err := sy.Push(context.Background(), &created); err == nil {
		t.Fatal("expected the rename failure to surface")
	}
	got, _ := st.Get(context.Background(), created.ID)
	if got.WorkItemID != "wi-2" {
		t.Fatalf("work item id must be persisted despite rename failure, got %q", got.WorkItemID)
	}
}

func TestSyncerNotConfigured(t *testing.T) {
	sy := NewSyncer(New(Config{BaseURL: "x"}), nil, nil) // missing token/slug/project
	if sy.Configured() {
		t.Fatal("should be unconfigured")
	}
	// Push is a no-op when unconfigured (doesn't touch the nil store).
	if err := sy.Push(context.Background(), &domain.Task{}); err != nil {
		t.Fatalf("unconfigured push should be a no-op, got %v", err)
	}
}

// pullServer answers the two calls a pull makes: the state catalogue, and the
// per-work-item state lookup. Work item ids listed in failing get a 500, so a
// single task can be broken while the others stay healthy.
func pullServer(t *testing.T, failing ...string) *httptest.Server {
	t.Helper()
	broken := make(map[string]bool, len(failing))
	for _, id := range failing {
		broken[id] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/states/") {
			w.Write([]byte(`{"results":[{"id":"st-doing","name":"Doing"}]}`))
			return
		}
		for id := range broken {
			if strings.Contains(r.URL.Path, "/issues/"+id+"/") {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`boom`))
				return
			}
		}
		w.Write([]byte(`{"state":"st-doing"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedSyncedTasks creates synced tasks whose list order is exactly the order of
// the labels given: List sorts by touched_at DESC, so the first label gets the
// most recent touch. Tests that care about "the FIRST task fails" depend on it.
func seedSyncedTasks(t *testing.T, st store.TaskStore, labels ...string) []domain.Task {
	t.Helper()
	out := make([]domain.Task, 0, len(labels))
	base := time.Now().UTC()
	for i, label := range labels {
		created, err := st.Create(context.Background(), domain.Task{
			Label:       label,
			Type:        domain.TypeFeat,
			Title:       strings.ToUpper(label),
			Status:      domain.StatusUnstarted,
			WorkItemID:  "wi-" + label,
			WorkItemSeq: i + 1,
			TouchedAt:   base.Add(-time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, created)
	}
	return out
}

// The regression: one task failing must not swallow the rest of the batch. The
// FIRST task in the list is the broken one, so the old "return on first error"
// left every other task unrefreshed.
func TestSyncerPullStatesContinuesAfterTaskFailure(t *testing.T) {
	srv := pullServer(t, "wi-alpha")

	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasks := seedSyncedTasks(t, st, "alpha", "bravo", "charlie")

	sy := NewSyncer(testClient(srv.URL), st, nil)
	updated, err := sy.PullStates(context.Background())
	if err == nil {
		t.Fatal("the failing task must surface an error")
	}
	if updated != 2 {
		t.Fatalf("the two healthy tasks must still be updated, got %d", updated)
	}
	for _, tk := range tasks[1:] {
		got, gErr := st.Get(context.Background(), tk.ID)
		if gErr != nil {
			t.Fatal(gErr)
		}
		if got.State != "Doing" {
			t.Fatalf("task %q was skipped after the earlier failure, state=%q", tk.Label, got.State)
		}
	}
	// The error must point at the task the user has to look at, and only that one.
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("error must name the failing task: %v", err)
	}
	for _, ok := range []string{"bravo", "charlie"} {
		if strings.Contains(err.Error(), ok) {
			t.Fatalf("error must not name the task %q that succeeded: %v", ok, err)
		}
	}
	failed := tasks[0]
	if got, _ := st.Get(context.Background(), failed.ID); got.State != "" {
		t.Fatalf("the failing task must stay unrefreshed, got state %q", got.State)
	}
}

// A pull where nothing goes wrong must report no error at all: an aggregate
// error built from an empty failure list would make /pull cry wolf.
func TestSyncerPullStatesCleanRunReturnsNilError(t *testing.T) {
	srv := pullServer(t)

	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedSyncedTasks(t, st, "alpha", "bravo", "charlie")
	// An unsynced task is skipped entirely — it must not count as a failure.
	if _, err := st.Create(context.Background(), domain.Task{
		Label: "local-only", Type: domain.TypeFeat, Title: "L", Status: domain.StatusUnstarted,
	}); err != nil {
		t.Fatal(err)
	}

	sy := NewSyncer(testClient(srv.URL), st, nil)
	updated, err := sy.PullStates(context.Background())
	if err != nil {
		t.Fatalf("a clean pull must not report a failure, got %v", err)
	}
	if updated != 3 {
		t.Fatalf("expected 3 tasks updated, got %d", updated)
	}
}

// Everything failing is still a full pass over the batch: nothing is updated
// and every task is named, so the user can see the whole damage at once.
func TestSyncerPullStatesReportsEveryFailure(t *testing.T) {
	srv := pullServer(t, "wi-alpha", "wi-bravo", "wi-charlie")

	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasks := seedSyncedTasks(t, st, "alpha", "bravo", "charlie")

	sy := NewSyncer(testClient(srv.URL), st, nil)
	updated, err := sy.PullStates(context.Background())
	if err == nil {
		t.Fatal("expected the failures to surface")
	}
	if updated != 0 {
		t.Fatalf("nothing could be updated, got %d", updated)
	}
	for _, tk := range tasks {
		if !strings.Contains(err.Error(), tk.Label) {
			t.Fatalf("error must name every failing task, %q missing from: %v", tk.Label, err)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("#%d", tk.ID)) {
			t.Fatalf("error must carry the task id #%d: %v", tk.ID, err)
		}
	}
}

// flakyStore wraps a real store and fails the first failUpdates calls to
// Update, so a transient local write failure can be exercised.
type flakyStore struct {
	store.TaskStore
	failUpdates int
	updates     int
}

func (f *flakyStore) Update(ctx context.Context, t domain.Task) error {
	f.updates++
	if f.updates <= f.failUpdates {
		return errors.New("database is locked")
	}
	return f.TaskStore.Update(ctx, t)
}

// A transient failure to record the new work item must be retried: the item
// already exists in Plane, and losing its id makes the next push duplicate it.
func TestSyncerPushRetriesTransientLinkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`{"results":[]}`))
		case http.MethodPatch:
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{"id":"wi-9","sequence_id":11}`))
		}
	}))
	defer srv.Close()

	real, _ := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	defer real.Close()
	created, _ := real.Create(context.Background(), domain.Task{
		Label: "a", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
	})
	st := &flakyStore{TaskStore: real, failUpdates: 1}

	sy := NewSyncer(testClient(srv.URL), st, nil)
	if err := sy.Push(context.Background(), &created); err != nil {
		t.Fatalf("a transient link failure should be retried, got %v", err)
	}
	got, _ := real.Get(context.Background(), created.ID)
	if got.WorkItemID != "wi-9" {
		t.Fatalf("work item id not linked after retry, got %q", got.WorkItemID)
	}
}

// When the link cannot be written at all, the error must name the orphaned work
// item — a silent failure here becomes a duplicate work item on the next push.
func TestSyncerPushNamesOrphanedWorkItem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`{"results":[]}`))
		default:
			w.Write([]byte(`{"id":"wi-7","sequence_id":42}`))
		}
	}))
	defer srv.Close()

	real, _ := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	defer real.Close()
	created, _ := real.Create(context.Background(), domain.Task{
		Label: "a", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
	})
	st := &flakyStore{TaskStore: real, failUpdates: 99}

	sy := NewSyncer(testClient(srv.URL), st, nil)
	err := sy.Push(context.Background(), &created)
	if err == nil {
		t.Fatal("a permanent link failure must surface")
	}
	for _, want := range []string{"wi-7", "#42", "duplicate"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must name the orphan and the risk, %q missing from: %v", want, err)
		}
	}
	if st.updates != linkRetries {
		t.Fatalf("expected %d attempts, got %d", linkRetries, st.updates)
	}
}
