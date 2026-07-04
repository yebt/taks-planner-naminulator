package plane

import (
	"context"
	"encoding/json"
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
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusTodo,
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
		Label: "fix-x", Type: domain.TypeFix, Title: "X", Status: domain.StatusTodo,
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
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusInProgress,
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
