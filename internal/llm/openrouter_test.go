package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleSendsExtraHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	p.SetHeader("X-Title", "planner")
	if _, err := p.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got.Get("X-Title") != "planner" {
		t.Fatalf("extra header not sent: %q", got.Get("X-Title"))
	}
	// The extras must not displace what the dialect itself requires.
	if got.Get("Authorization") != "Bearer k" {
		t.Fatalf("auth header lost: %q", got.Get("Authorization"))
	}
}

func TestBuildOpenRouter(t *testing.T) {
	p, err := Build(ProviderConfig{Kind: "openrouter", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "openrouter" {
		t.Fatalf("provider name %q, want openrouter", p.Name())
	}
	o, ok := p.(*OpenAICompatible)
	if !ok {
		t.Fatalf("openrouter should reuse the OpenAI dialect, got %T", p)
	}
	if o.baseURL != OpenRouterBaseURL {
		t.Fatalf("base url %q, want %q", o.baseURL, OpenRouterBaseURL)
	}
	if o.model == "" {
		t.Fatal("an empty model must fall back to a working default")
	}
}
