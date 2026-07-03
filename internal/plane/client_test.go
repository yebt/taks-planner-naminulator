package plane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(url string) *Client {
	return New(Config{BaseURL: url, Token: "tok", WorkspaceSlug: "acme", ProjectID: "proj"})
}

func TestConfigured(t *testing.T) {
	if !testClient("http://x").Configured() {
		t.Fatal("should be configured")
	}
	if New(Config{BaseURL: "x"}).Configured() {
		t.Fatal("missing fields should be unconfigured")
	}
}

func TestCreateIssue(t *testing.T) {
	var body map[string]any
	var key, path, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, path, method = r.Header.Get("X-API-Key"), r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Write([]byte(`{"id":"issue-123"}`))
	}))
	defer srv.Close()

	id, err := testClient(srv.URL).CreateIssue(context.Background(), IssueInput{
		Name: "FEAT - x", Description: "do it", StartDate: "2026-06-01", TargetDate: "2026-06-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "issue-123" {
		t.Fatalf("id: %q", id)
	}
	if key != "tok" || method != http.MethodPost {
		t.Fatalf("auth/method: %q %q", key, method)
	}
	if !strings.HasSuffix(path, "/api/v1/workspaces/acme/projects/proj/issues/") {
		t.Fatalf("path: %q", path)
	}
	if body["name"] != "FEAT - x" || body["start_date"] != "2026-06-01" || body["target_date"] != "2026-06-02" {
		t.Fatalf("body: %+v", body)
	}
	if _, ok := body["description_html"]; !ok {
		t.Fatalf("description not wrapped: %+v", body)
	}
}

func TestListStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"id":"s1","name":"In Progress","group":"started"}]}`))
	}))
	defer srv.Close()
	states, err := testClient(srv.URL).ListStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Name != "In Progress" || states[0].ID != "s1" {
		t.Fatalf("states: %+v", states)
	}
}

func TestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`unauthorized`))
	}))
	defer srv.Close()
	if _, err := testClient(srv.URL).CreateIssue(context.Background(), IssueInput{Name: "x"}); err == nil {
		t.Fatal("expected error on 401")
	}
}
