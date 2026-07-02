package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIChatToolCall(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"create_task","arguments":"{\"title\":\"x\"}"}}]}}]}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "secret", "m1")
	resp, err := p.Chat(context.Background(),
		[]Message{{Role: RoleUser, Content: "hi"}},
		[]Tool{{Name: "create_task", Description: "d", Parameters: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "create_task" {
		t.Fatalf("bad tool calls: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Arguments != `{"title":"x"}` {
		t.Fatalf("bad arguments: %q", resp.ToolCalls[0].Arguments)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("bad auth header: %q", gotAuth)
	}
	if gotBody["model"] != "m1" {
		t.Fatalf("model not sent: %v", gotBody["model"])
	}
	if _, ok := gotBody["tools"]; !ok {
		t.Fatalf("tools not sent")
	}
}

func TestOpenAIChatText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello there"}}]}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	resp, err := p.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello there" || len(resp.ToolCalls) != 0 {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func TestOpenAIChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	_, err := p.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error on 401")
	}
}
