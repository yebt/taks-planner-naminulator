// Package telegram sends messages to a Telegram chat via the Bot API. It is the
// delivery channel for daily digests.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client posts messages to a chat through the Telegram Bot API.
type Client struct {
	token    string
	chatID   string
	threadID string
	api      string // base URL, overridable in tests
	http     *http.Client
}

// New builds a Telegram client. threadID is optional (forum topic id).
func New(token, chatID, threadID string) *Client {
	return &Client{
		token:    strings.TrimSpace(token),
		chatID:   strings.TrimSpace(chatID),
		threadID: strings.TrimSpace(threadID),
		api:      "https://api.telegram.org",
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

// Configured reports whether the client can send (token + chat present).
func (c *Client) Configured() bool { return c.token != "" && c.chatID != "" }

// Send posts a message, translating the CommonMark markup used by dailies
// (**bold**, __italic__, `code`) to Telegram HTML so it renders formatted.
//
// Text past Telegram's 4096-character limit is delivered as several messages in
// order. A message that fits — the overwhelming majority — still travels as
// exactly one request.
func (c *Client) Send(ctx context.Context, text string) error {
	if !c.Configured() {
		return fmt.Errorf("telegram not configured")
	}
	chunks := splitForTelegram(text, maxMessageChars)
	if len(chunks) == 1 {
		return c.sendOne(ctx, chunks[0])
	}
	for i, chunk := range chunks {
		if err := c.sendOne(ctx, chunk); err != nil {
			// Earlier chunks are already delivered, so a generic failure would
			// hide a half-sent daily. Say exactly where the send stopped.
			return fmt.Errorf("sending chunk %d of %d: %w", i+1, len(chunks), err)
		}
	}
	return nil
}

// sendOne posts a single message that is already known to fit. Every chunk goes
// through here, so parse_mode, thread routing and token redaction apply to all
// of them alike.
func (c *Client) sendOne(ctx context.Context, text string) error {
	body := map[string]any{
		"chat_id":    c.chatID,
		"text":       toHTML(text),
		"parse_mode": "HTML",
	}
	if c.threadID != "" {
		if id, err := strconv.Atoi(c.threadID); err == nil {
			body["message_thread_id"] = id
		}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(c.api, "/"), c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// net/http wraps transport failures in *url.Error, whose Error() embeds
		// the request URL — and the URL carries the bot token. Keep only the
		// cause, then redact as a backstop, so the token can never reach a log
		// or the screen on a DNS blip, timeout or refused connection.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram: request failed: %s", c.redact(err.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &out)
	if !out.OK {
		if out.Description != "" {
			return fmt.Errorf("telegram: %s", c.redact(out.Description))
		}
		return fmt.Errorf("telegram: status %d", resp.StatusCode)
	}
	return nil
}

// redact removes the bot token from any text that may reach a log or the
// screen. Very short tokens are left alone: they cannot be real credentials and
// blind replacement would mangle unrelated text.
func (c *Client) redact(s string) string {
	if len(c.token) < 8 {
		return s
	}
	return strings.ReplaceAll(s, c.token, "[redacted]")
}

// Test sends a fixed message so the user can confirm token, chat, and thread
// are all correct end-to-end.
func (c *Client) Test(ctx context.Context) error {
	return c.Send(ctx, "✅ planner: test notification")
}
