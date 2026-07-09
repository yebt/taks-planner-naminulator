package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigured(t *testing.T) {
	if New("tok", "chat", "").Configured() != true {
		t.Fatal("token+chat should be configured")
	}
	if New("tok", "", "").Configured() {
		t.Fatal("missing chat should be unconfigured")
	}
}

func TestSend(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	c := New("secret-token", "-100123", "42")
	c.api = srv.URL
	if err := c.Send(context.Background(), "hola"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/botsecret-token/sendMessage") {
		t.Fatalf("path: %q", path)
	}
	if body["chat_id"] != "-100123" || body["text"] != "hola" {
		t.Fatalf("body: %+v", body)
	}
	if body["parse_mode"] != "HTML" {
		t.Fatalf("parse_mode should be HTML, got %v", body["parse_mode"])
	}
	if body["message_thread_id"] != float64(42) { // numeric thread id
		t.Fatalf("thread id not numeric: %v", body["message_thread_id"])
	}
}

func TestSendAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()
	c := New("tok", "chat", "")
	c.api = srv.URL
	err := c.Send(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected api error, got %v", err)
	}
}

func TestSendUnconfigured(t *testing.T) {
	if err := New("", "", "").Send(context.Background(), "x"); err == nil {
		t.Fatal("unconfigured send should error")
	}
}
