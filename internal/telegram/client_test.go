package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// The bot token is part of the request URL, so a transport failure must never
// surface it: the error is rendered straight into the TUI.
func TestSendTransportErrorHidesToken(t *testing.T) {
	const token = "123456789:AAHs-this-is-a-secret-bot-token"
	c := New(token, "chat", "")
	c.api = "http://127.0.0.1:1" // nothing listening -> connection refused
	err := c.Send(context.Background(), "x")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("bot token leaked into the error: %v", err)
	}
}

func TestSendUnconfigured(t *testing.T) {
	if err := New("", "", "").Send(context.Background(), "x"); err == nil {
		t.Fatal("unconfigured send should error")
	}
}

// recorder is a fake Bot API that records every sendMessage body. failAt is the
// 1-based request number that should answer ok:false; 0 means never fail.
type recorder struct {
	mu     sync.Mutex
	bodies []map[string]any
	failAt int
}

func (rec *recorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, body)
		n := len(rec.bodies)
		rec.mu.Unlock()
		if n == rec.failAt {
			w.Write([]byte(`{"ok":false,"description":"message is too long"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// texts returns the text field of every recorded request, in order.
func (rec *recorder) texts(t *testing.T) []string {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]string, 0, len(rec.bodies))
	for i, b := range rec.bodies {
		s, ok := b["text"].(string)
		if !ok {
			t.Fatalf("request %d has no text field: %+v", i+1, b)
		}
		out = append(out, s)
	}
	return out
}

// longDaily builds a digest well past the 4096-char limit. Every line is unique
// so a dropped or duplicated one is visible, and each carries markup so the
// rendered size differs from the source size.
func longDaily(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "**Task %03d** — revisar el `deploy-%03d` & cerrar el ticket\n\n", i, i)
	}
	return strings.TrimRight(b.String(), "\n")
}

// nonEmptyLines is the content-preservation view: blank-line padding at chunk
// boundaries is presentation, the sequence of real lines is the content.
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// A daily over the limit must arrive in several messages, each within the
// limit, with every original line delivered exactly once and in order.
func TestSendSplitsLongDaily(t *testing.T) {
	rec := &recorder{}
	srv := rec.server(t)

	c := New("secret-token", "-100123", "42")
	c.api = srv.URL
	daily := longDaily(200)
	if len(toHTML(daily)) <= maxMessageChars {
		t.Fatalf("fixture is not over the limit: %d", len(toHTML(daily)))
	}
	if err := c.Send(context.Background(), daily); err != nil {
		t.Fatal(err)
	}

	got := rec.texts(t)
	if len(got) < 2 {
		t.Fatalf("expected several requests, got %d", len(got))
	}
	for i, chunk := range got {
		if len(chunk) > maxMessageChars {
			t.Errorf("chunk %d is %d chars, over the %d limit", i+1, len(chunk), maxMessageChars)
		}
		if strings.TrimSpace(chunk) == "" {
			t.Errorf("chunk %d is empty", i+1)
		}
	}

	// Every chunk keeps parse_mode and the thread routing.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i, b := range rec.bodies {
		if b["parse_mode"] != "HTML" {
			t.Errorf("chunk %d parse_mode = %v, want HTML", i+1, b["parse_mode"])
		}
		if b["message_thread_id"] != float64(42) {
			t.Errorf("chunk %d thread id = %v, want 42", i+1, b["message_thread_id"])
		}
		if b["chat_id"] != "-100123" {
			t.Errorf("chunk %d chat_id = %v", i+1, b["chat_id"])
		}
	}

	// Nothing dropped, nothing duplicated: the received lines are the source
	// lines rendered, in the same order.
	want := nonEmptyLines(daily)
	have := nonEmptyLines(strings.Join(got, "\n"))
	if len(have) != len(want) {
		t.Fatalf("delivered %d lines, want %d", len(have), len(want))
	}
	for i := range want {
		if have[i] != toHTML(want[i]) {
			t.Fatalf("line %d = %q, want %q", i, have[i], toHTML(want[i]))
		}
	}
}

// No-regression guard: a message that fits is still exactly one request.
func TestSendShortMessageIsOneRequest(t *testing.T) {
	rec := &recorder{}
	srv := rec.server(t)

	c := New("tok", "chat", "")
	c.api = srv.URL
	if err := c.Send(context.Background(), "**Trabajo:**\n\n  - [FEAT] #343 (hoy)"); err != nil {
		t.Fatal(err)
	}
	got := rec.texts(t)
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	if got[0] != "<b>Trabajo:</b>\n\n  - [FEAT] #343 (hoy)" {
		t.Fatalf("text was altered: %q", got[0])
	}
}

// A failure partway through must name the chunk: silently reporting a generic
// error after half the daily was delivered is worse than not sending.
func TestSendChunkFailureNamesChunk(t *testing.T) {
	rec := &recorder{failAt: 2}
	srv := rec.server(t)

	c := New("tok", "chat", "")
	c.api = srv.URL
	err := c.Send(context.Background(), longDaily(200))
	if err == nil {
		t.Fatal("expected an error")
	}
	total := len(splitForTelegram(longDaily(200), maxMessageChars))
	want := fmt.Sprintf("chunk 2 of %d", total)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q should mention %q", err, want)
	}
	if !strings.Contains(err.Error(), "message is too long") {
		t.Fatalf("error %q should keep the API description", err)
	}
	if n := len(rec.texts(t)); n != 2 {
		t.Fatalf("send should stop at the failing chunk, made %d requests", n)
	}
}

// Infinite-loop guard: a single line with no break points must still terminate
// and be delivered whole.
func TestSendSplitsUnbrokenLine(t *testing.T) {
	rec := &recorder{}
	srv := rec.server(t)

	c := New("tok", "chat", "")
	c.api = srv.URL
	line := strings.Repeat("a", 5*maxMessageChars)
	if err := c.Send(context.Background(), line); err != nil {
		t.Fatal(err)
	}
	got := rec.texts(t)
	if len(got) < 5 {
		t.Fatalf("expected at least 5 requests, got %d", len(got))
	}
	for i, chunk := range got {
		if len(chunk) > maxMessageChars {
			t.Errorf("chunk %d is %d chars, over the limit", i+1, len(chunk))
		}
	}
	if joined := strings.Join(got, ""); joined != line {
		t.Fatalf("hard split lost content: got %d chars, want %d", len(joined), len(line))
	}
}

// splitForTelegram is the unit under the chunking: exercise its boundaries
// directly rather than only through the HTTP path.
func TestSplitForTelegram(t *testing.T) {
	t.Run("text that fits is untouched", func(t *testing.T) {
		in := "**a**\n\nb"
		got := splitForTelegram(in, maxMessageChars)
		if len(got) != 1 || got[0] != in {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("prefers paragraph boundaries", func(t *testing.T) {
		// Two paragraphs of 6 chars; a limit of 8 cannot hold both.
		got := splitForTelegram("aaaaaa\n\nbbbbbb", 8)
		if len(got) != 2 || got[0] != "aaaaaa" || got[1] != "bbbbbb" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("falls back to single newlines", func(t *testing.T) {
		got := splitForTelegram("aaaaaa\nbbbbbb", 8)
		if len(got) != 2 || got[0] != "aaaaaa" || got[1] != "bbbbbb" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("sizes by rendered HTML, not source", func(t *testing.T) {
		// "**ab**" is 6 source chars but renders to 11.
		got := splitForTelegram("**ab**\n\n**cd**", 12)
		if len(got) != 2 {
			t.Fatalf("got %q", got)
		}
		for _, chunk := range got {
			if len(toHTML(chunk)) > 12 {
				t.Fatalf("chunk %q renders to %d chars", chunk, len(toHTML(chunk)))
			}
		}
	})

	t.Run("terminates on a rune wider than the limit", func(t *testing.T) {
		// A limit of 1 cannot hold "&amp;": progress is forced anyway.
		got := splitForTelegram("&&&", 1)
		if len(got) != 3 {
			t.Fatalf("got %q", got)
		}
		if strings.Join(got, "") != "&&&" {
			t.Fatalf("content lost: %q", got)
		}
	})

	t.Run("never yields an empty chunk", func(t *testing.T) {
		for _, chunk := range splitForTelegram("aaaaaa\n\n\n\n\nbbbbbb", 8) {
			if strings.TrimSpace(chunk) == "" {
				t.Fatal("empty chunk would be rejected by Telegram")
			}
		}
	})
}
