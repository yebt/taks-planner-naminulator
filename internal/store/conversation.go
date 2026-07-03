package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/webcloster-dev/planner/internal/llm"
)

// Conversation is a saved chat session header.
type Conversation struct {
	ID        int64
	Title     string
	UpdatedAt time.Time
}

// SaveConversation upserts a conversation and replaces its messages. Pass id=0
// to create a new one; returns the (possibly new) id.
func (s *SQLite) SaveConversation(ctx context.Context, id int64, title string, msgs []llm.Message) (int64, error) {
	now := time.Now().UTC().Unix()
	if id == 0 {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO conversations (title, updated_at) VALUES (?, ?)`, title, now)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE conversations SET title=?, updated_at=? WHERE id=?`, title, now, id); err != nil {
			return 0, err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM conv_messages WHERE conversation_id=?`, id); err != nil {
			return 0, err
		}
	}
	for i, m := range msgs {
		toolCalls, _ := json.Marshal(m.ToolCalls)
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO conv_messages (conversation_id, idx, role, content, tool_calls, tool_call_id, name)
			 VALUES (?,?,?,?,?,?,?)`,
			id, i, string(m.Role), m.Content, string(toolCalls), m.ToolCallID, m.Name); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListConversations returns saved conversations, most recent first.
func (s *SQLite) ListConversations(ctx context.Context) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, updated_at FROM conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var updated int64
		if err := rows.Scan(&c.ID, &c.Title, &updated); err != nil {
			return nil, err
		}
		c.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadConversation returns the messages of a saved conversation, in order.
func (s *SQLite) LoadConversation(ctx context.Context, id int64) ([]llm.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content, tool_calls, tool_call_id, name FROM conv_messages
		 WHERE conversation_id=? ORDER BY idx`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []llm.Message
	for rows.Next() {
		var role, content, toolCalls, toolCallID, name string
		if err := rows.Scan(&role, &content, &toolCalls, &toolCallID, &name); err != nil {
			return nil, err
		}
		m := llm.Message{Role: llm.Role(role), Content: content, ToolCallID: toolCallID, Name: name}
		if toolCalls != "" && toolCalls != "null" {
			_ = json.Unmarshal([]byte(toolCalls), &m.ToolCalls)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
