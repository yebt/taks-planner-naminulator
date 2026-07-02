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

func TestClaudeChatToolUse(t *testing.T) {
	var req claudeRequest
	var apiKey, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		w.Write([]byte(`{"content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"tu_1","name":"create_task","input":{"title":"x"}}]}`))
	}))
	defer srv.Close()

	c := NewClaude("sekret", "claude-test").WithBaseURL(srv.URL)
	resp, err := c.Chat(context.Background(),
		[]Message{
			{Role: RoleSystem, Content: "sys"},
			{Role: RoleUser, Content: "hi"},
		},
		[]Tool{{Name: "create_task", Description: "d", Parameters: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("bad content: %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "create_task" {
		t.Fatalf("bad tool calls: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Arguments != `{"title":"x"}` {
		t.Fatalf("bad args: %q", resp.ToolCalls[0].Arguments)
	}
	if apiKey != "sekret" || version != claudeAPIVersion {
		t.Fatalf("bad headers: key=%q version=%q", apiKey, version)
	}
	if req.System != "sys" {
		t.Fatalf("system not extracted: %q", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("bad messages: %+v", req.Messages)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools not sent")
	}
}

func TestClaudeChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	defer srv.Close()

	c := NewClaude("k", "m").WithBaseURL(srv.URL)
	_, err := c.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error on 400")
	}
}
