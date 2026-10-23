package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// catalog is a trimmed copy of a real /models payload: two models, one of them
// tool-capable and priced, one free and tool-less, returned out of order.
const catalog = `{"data":[
 {"id":"z-ai/glm-chat","name":"Z: GLM Chat","context_length":32768,
  "pricing":{"prompt":"0","completion":"0"},
  "supported_parameters":["max_tokens","temperature"]},
 {"id":"openai/gpt-4o-mini","name":"OpenAI: GPT-4o-mini","context_length":128000,
  "pricing":{"prompt":"0.00000015","completion":"0.0000006"},
  "supported_parameters":["tools","tool_choice","temperature"]}
]}`

func serve(t *testing.T, status int, body string, seen *http.Header) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = r.Header.Clone()
		}
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL})
}

func TestListModelsDecodesTheCatalog(t *testing.T) {
	got, err := serve(t, 200, catalog, nil).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d models, want 2", len(got))
	}
	// Sorted by id, so the payload's order does not leak into the picker.
	if got[0].ID != "openai/gpt-4o-mini" {
		t.Fatalf("models are not sorted by id: %q first", got[0].ID)
	}
	m := got[0]
	if m.Name != "OpenAI: GPT-4o-mini" || m.ContextLength != 128000 {
		t.Fatalf("bad model: %+v", m)
	}
	if !m.Tools {
		t.Fatal("a model advertising \"tools\" must be marked tool-capable")
	}
	// Pricing arrives as USD per token; per million is the readable unit.
	if m.PromptUSD != 0.15 || m.CompletionUSD != 0.6 {
		t.Fatalf("prices are not per million tokens: %+v", m)
	}
	if got[1].Tools {
		t.Fatal("a model without \"tools\" must not be marked tool-capable")
	}
}

func TestListModelsSendsTheKeyOnlyWhenThereIsOne(t *testing.T) {
	var h http.Header
	c := serve(t, 200, catalog, &h)
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Authorization"); got != "" {
		t.Fatalf("no key configured, yet it sent %q", got)
	}

	c.cfg.Token = "secret"
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("bad auth header: %q", got)
	}
}

func TestListModelsReportsHTTPFailures(t *testing.T) {
	if _, err := serve(t, 401, `{"error":{"message":"no"}}`, nil).ListModels(context.Background()); err == nil {
		t.Fatal("a 401 must be an error, not an empty catalog")
	}
}

func TestNewFallsBackToTheRealEndpoint(t *testing.T) {
	if got := New(Config{}).cfg.BaseURL; got != BaseURL {
		t.Fatalf("empty base url became %q, want %q", got, BaseURL)
	}
}
