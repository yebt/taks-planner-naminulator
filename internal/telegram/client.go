// Package telegram sends messages to a Telegram chat via the Bot API. It is the
// delivery channel for daily digests.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// Send posts a plain-text message (no parse_mode, so markdown syntax and
// brackets like "[FEAT]" are delivered literally without breaking parsing).
func (c *Client) Send(ctx context.Context, text string) error {
	if !c.Configured() {
		return fmt.Errorf("telegram not configured")
	}
	body := map[string]any{"chat_id": c.chatID, "text": text}
	if c.threadID != "" {
		if id, err := strconv.Atoi(c.threadID); err == nil {
			body["message_thread_id"] = id
		}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(c.api, "/"), c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
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
			return fmt.Errorf("telegram: %s", out.Description)
		}
		return fmt.Errorf("telegram: status %d", resp.StatusCode)
	}
	return nil
}

// Test sends a fixed message so the user can confirm token, chat, and thread
// are all correct end-to-end.
func (c *Client) Test(ctx context.Context) error {
	return c.Send(ctx, "✅ planner: test notification")
}
