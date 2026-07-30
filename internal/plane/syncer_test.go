package plane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
