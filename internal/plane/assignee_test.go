package plane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

// Work items created by the planner arrived in Plane unassigned: the API key
// sets created_by, but assignees stayed empty, so every task landed on nobody.
// These tests pin the two halves of the fix — resolving who owns the key, and
// sending that id on create ONLY.

func TestIssueInputOmitsAssigneesWhenUnset(t *testing.T) {
	body := IssueInput{Name: "X"}.body()
	if _, ok := body["assignees"]; ok {
		t.Fatalf("assignees must be absent, not empty: %#v", body["assignees"])
	}
}

func TestIssueInputCarriesAssignees(t *testing.T) {
	body := IssueInput{Name: "X", Assignees: []string{"user-1"}}.body()
	got, _ := body["assignees"].([]string)
	if len(got) != 1 || got[0] != "user-1" {
		t.Fatalf("assignees not sent: %#v", body["assignees"])
	}
}

func TestClientMeReturnsTheAPIKeyOwner(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"id":"user-42","display_name":"Yahir","email":"y@example.com"}`))
	}))
	defer srv.Close()

	me, err := testClient(srv.URL).Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.ID != "user-42" || me.DisplayName != "Yahir" {
		t.Fatalf("wrong user: %#v", me)
	}
	// The endpoint hangs off the API root, not the workspace/project path the
	// rest of the client uses — a base() mistake would silently 404.
	if gotPath != "/api/v1/users/me/" {
		t.Fatalf("wrong endpoint: %q", gotPath)
	}
}

// planeServer records what the client sends, so a test can assert on the exact
// create/update bodies without repeating the routing in every case.
type planeServer struct {
	srv        *httptest.Server
	createBody map[string]any
	patchBody  map[string]any
	meCalls    atomic.Int32
	meStatus   int // 0 = 200
}

func newPlaneServer(t *testing.T) *planeServer {
	t.Helper()
	p := &planeServer{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decode := func(into *map[string]any) {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, into)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/me/"):
			p.meCalls.Add(1)
			if p.meStatus != 0 {
				w.WriteHeader(p.meStatus)
				w.Write([]byte(`{"error":"nope"}`))
				return
			}
			w.Write([]byte(`{"id":"owner-1","display_name":"Owner"}`))
		case r.Method == http.MethodGet:
			w.Write([]byte(`{"results":[]}`))
		case r.Method == http.MethodPost:
			decode(&p.createBody)
			w.Write([]byte(`{"id":"wi-1","sequence_id":5}`))
		default:
			decode(&p.patchBody)
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func newTestStore(t *testing.T) store.TaskStore {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func assigneesOf(body map[string]any) []string {
	raw, ok := body["assignees"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func TestSyncerPushAssignsTheAPIKeyOwnerOnCreate(t *testing.T) {
	p := newPlaneServer(t)
	st := newTestStore(t)
	created, err := st.Create(context.Background(), domain.Task{
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
	})
	if err != nil {
		t.Fatal(err)
	}

	sy := NewSyncer(testClient(p.srv.URL), st, nil)
	if err := sy.Push(context.Background(), &created); err != nil {
		t.Fatal(err)
	}

	got := assigneesOf(p.createBody)
	if len(got) != 1 || got[0] != "owner-1" {
		t.Fatalf("work item was not assigned to the key owner: %#v", p.createBody["assignees"])
	}
}

// The rename that follows a create, and every later update, must NOT carry
// assignees. body() is shared between create and update, so sending them
// unconditionally would silently undo a reassignment made by hand in Plane.
func TestSyncerPushNeverReassignsOnUpdate(t *testing.T) {
	p := newPlaneServer(t)
	st := newTestStore(t)
	task, err := st.Create(context.Background(), domain.Task{
		Label: "feat-y", Type: domain.TypeFeat, Title: "Y", Status: domain.StatusUnstarted,
		WorkItemID: "wi-existing", WorkItemSeq: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	sy := NewSyncer(testClient(p.srv.URL), st, nil)
	if err := sy.Push(context.Background(), &task); err != nil {
		t.Fatal(err)
	}

	if _, ok := p.patchBody["assignees"]; ok {
		t.Fatalf("update must not touch assignees, got %#v", p.patchBody["assignees"])
	}
	if p.meCalls.Load() != 0 {
		t.Fatalf("an update should not even ask who owns the key, got %d calls", p.meCalls.Load())
	}
}

// Not being able to resolve the owner is a degraded result, not a failed push:
// the work item must still be created rather than lost.
func TestSyncerPushSurvivesAnOwnerLookupFailure(t *testing.T) {
	p := newPlaneServer(t)
	p.meStatus = http.StatusInternalServerError
	st := newTestStore(t)
	created, err := st.Create(context.Background(), domain.Task{
		Label: "feat-z", Type: domain.TypeFeat, Title: "Z", Status: domain.StatusUnstarted,
	})
	if err != nil {
		t.Fatal(err)
	}

	sy := NewSyncer(testClient(p.srv.URL), st, nil)
	if err := sy.Push(context.Background(), &created); err != nil {
		t.Fatalf("a failed owner lookup must not fail the push: %v", err)
	}
	if created.WorkItemID != "wi-1" {
		t.Fatalf("work item was not created: %#v", created)
	}
	if _, ok := p.createBody["assignees"]; ok {
		t.Fatalf("unresolved owner must send no assignees at all: %#v", p.createBody["assignees"])
	}
}

func TestSyncerResolvesTheOwnerOnce(t *testing.T) {
	p := newPlaneServer(t)
	st := newTestStore(t)
	sy := NewSyncer(testClient(p.srv.URL), st, nil)

	for i := 0; i < 3; i++ {
		task, err := st.Create(context.Background(), domain.Task{
			Label:  "feat-" + string(rune('a'+i)),
			Type:   domain.TypeFeat,
			Title:  "T",
			Status: domain.StatusUnstarted,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := sy.Push(context.Background(), &task); err != nil {
			t.Fatal(err)
		}
	}
	if n := p.meCalls.Load(); n != 1 {
		t.Fatalf("owner should be resolved once and cached, got %d lookups", n)
	}
}
