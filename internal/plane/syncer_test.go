package plane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

func TestSyncerPushCreatesAndPersists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // ListStates lookup
			w.Write([]byte(`{"results":[]}`))
			return
		}
		w.Write([]byte(`{"id":"wi-1"}`)) // create issue
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
	if created.WorkItemID != "wi-1" {
		t.Fatalf("work item id not set on task: %q", created.WorkItemID)
	}
	got, _ := st.Get(context.Background(), created.ID)
	if got.WorkItemID != "wi-1" {
		t.Fatalf("work item id not persisted: %q", got.WorkItemID)
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
