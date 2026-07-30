package tui

// Conversation save, autosave, load and resume.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/webcloster-dev/planner/internal/llm"
)

func (m *chatModel) saveConversation(title string) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	id, err := m.deps.Convos.SaveConversation(context.Background(), m.convID, title, m.deps.Agent.History())
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.convID = id
	m.add("sys", fmt.Sprintf("saved conversation #%d — %s", id, title))
}

func (m *chatModel) autosave() {
	if m.deps.Convos == nil {
		return
	}
	if id, err := m.deps.Convos.SaveConversation(context.Background(), m.convID, m.convTitle(), m.deps.Agent.History()); err == nil {
		m.convID = id
	}
}

func (m *chatModel) showChats(ctx context.Context) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	convs, err := m.deps.Convos.ListConversations(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(convs) == 0 {
		m.add("sys", "no saved conversations yet.")
		return
	}
	var b strings.Builder
	for _, c := range convs {
		b.WriteString(fmt.Sprintf("#%d  %-48s %s\n",
			c.ID, trunc(c.Title, 48), c.UpdatedAt.Local().Format("2006-01-02 15:04")))
	}
	b.WriteString("\nuse: /load <id>")
	m.add("sys", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) loadConversation(ctx context.Context, idStr string) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		m.add("err", "id must be a number")
		return
	}
	msgs, err := m.deps.Convos.LoadConversation(ctx, id)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.deps.Agent.SetHistory(msgs)
	m.convID = id
	m.entries = nil
	for _, msg := range msgs {
		switch msg.Role {
		case llm.RoleUser:
			m.entries = append(m.entries, entry{"you", msg.Content})
		case llm.RoleAssistant:
			if msg.Content != "" {
				m.entries = append(m.entries, entry{"planner", msg.Content})
			}
		}
	}
	m.add("sys", fmt.Sprintf("loaded conversation #%d (%d messages)", id, len(msgs)))
}

func (m *chatModel) convTitle() string {
	for _, e := range m.entries {
		if e.role == "you" {
			return trunc(e.text, 48)
		}
	}
	return "conversation"
}

// resumeLast loads the most recently updated conversation.
func (m *chatModel) resumeLast(ctx context.Context) {
	if m.deps.Convos == nil {
		m.add("err", "conversation store not available")
		return
	}
	convs, err := m.deps.Convos.ListConversations(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(convs) == 0 {
		m.add("sys", "no saved conversations yet.")
		return
	}
	m.loadConversation(ctx, strconv.FormatInt(convs[0].ID, 10))
}
