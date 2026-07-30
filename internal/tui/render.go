package tui

// Styles, viewport content, and the View layout.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("57")).Padding(0, 1)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).Background(lipgloss.Color("237"))
	youLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	botLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	sysStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	sugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	selStyle  = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62"))
	thinkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	confirmStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160")).Padding(0, 1)
	toastStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("28")).Padding(0, 1)
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("108"))
	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	userBubbleBG = lipgloss.Color("236")
)

func (m *chatModel) add(role, text string) {
	m.entries = append(m.entries, entry{role: role, text: text})
	m.setContent()
	m.vp.GotoBottom() // new content jumps to the bottom…
}

// setContent rebuilds the viewport body WITHOUT moving the scroll position, so
// scrolling up survives keystrokes and relayouts.
func (m *chatModel) setContent() {
	w := m.vp.Width
	if w < 10 {
		w = 10
	}
	body := lipgloss.NewStyle().Width(w)
	bubble := lipgloss.NewStyle().Background(userBubbleBG).Foreground(lipgloss.Color("252")).Padding(0, 1).Width(w)
	cmdBubble := lipgloss.NewStyle().Background(userBubbleBG).Foreground(lipgloss.Color("213")).Padding(0, 1).Width(w)
	blocks := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		switch e.role {
		case "you":
			blocks = append(blocks, youLabel.Render("› you")+"\n"+bubble.Render(e.text))
		case "planner":
			blocks = append(blocks, botLabel.Render("planner")+"\n"+body.Render(e.text))
		case "cmd":
			blocks = append(blocks, cmdBubble.Render(e.text))
		case "tool":
			blocks = append(blocks, toolStyle.Render(e.text))
		case "err":
			blocks = append(blocks, body.Inherit(errStyle).Render("error: "+e.text))
		case "alert":
			blocks = append(blocks, body.Inherit(errStyle).Render("⚠ "+e.text))
		case "warn":
			blocks = append(blocks, body.Inherit(warnStyle).Render("⚠ "+e.text))
		case "daily":
			// Model-authored markup: paint it and wrap it. Unlike "raw" below,
			// nothing has pre-formatted this text to the viewport width.
			blocks = append(blocks, body.Render(renderMarkup(e.text)))
		case "raw":
			blocks = append(blocks, e.text) // pre-styled/wrapped, passthrough
		default:
			blocks = append(blocks, body.Inherit(sysStyle).Render(e.text))
		}
	}
	content := strings.Join(blocks, "\n\n")
	styled := strings.Split(content, "\n")
	m.contentLines = strings.Split(ansiStrip(content), "\n") // same line count

	if m.selActive && len(styled) > 0 {
		sl, sc, el, ec := m.orderedSel()
		for i := sl; i <= el && i < len(styled); i++ {
			r := []rune(m.contentLines[i])
			a, b := 0, len(r)
			if i == sl {
				a = clampCol(sc, len(r))
			}
			if i == el {
				b = clampCol(ec+1, len(r))
			}
			styled[i] = highlightLine(m.contentLines[i], a, b)
		}
		m.vp.SetContent(strings.Join(styled, "\n"))
		return
	}
	m.vp.SetContent(content)
}

func (m *chatModel) layout() {
	if !m.ready {
		return
	}
	m.ta.SetWidth(m.width - 1)
	const inputH = 3 // fixed input height (dynamic growth was reverted earlier)
	m.ta.SetHeight(inputH)
	// leaves room for: header + divider + input + help + status + margin
	vpH := m.height - len(m.suggestions) - inputH - 5
	if vpH < 3 {
		vpH = 3
	}
	m.vp.Width = m.width
	m.vp.Height = vpH
	m.setContent() // keep scroll position on resize / keystroke
}

func (m *chatModel) View() string {
	if !m.ready {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render("planner"))
	b.WriteString("\n")
	b.WriteString(m.vp.View())
	b.WriteString("\n")
	// Separator between the conversation and the input.
	b.WriteString(dividerStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")
	if len(m.suggestions) > 0 {
		b.WriteString(m.renderSuggestions())
		b.WriteString("\n")
	}
	// Wrap the input in a full-width background so the panel is uniform.
	// b.WriteString(lipgloss.NewStyle().Width(m.width).Background(inputBG).Render(m.ta.View()))
	b.WriteString(lipgloss.NewStyle().Width(m.width).Render(m.ta.View()))
	b.WriteString("\n")
	b.WriteString(m.footer())
	b.WriteString("\n")
	b.WriteString(m.statusBar())
	return b.String()
}

func (m *chatModel) footer() string {
	if m.toast != "" {
		return toastStyle.Width(m.width).Render(m.toast)
	}
	if m.confirm != nil {
		return confirmStyle.Width(m.width).Render("⚠ " + m.confirm.prompt + "    y = confirm · n/esc = cancel")
	}
	if m.dailyEditing {
		return thinkStyle.Render("editing daily · enter = save draft · alt+enter = newline · esc = cancel")
	}
	if m.quitArmed {
		return thinkStyle.Render("press ctrl+c again to quit")
	}
	if m.thinking {
		frame := spinnerFrames[m.spinner%len(spinnerFrames)]
		label := m.busyLabel
		if label == "" {
			label = "thinking"
		}
		return thinkStyle.Render(fmt.Sprintf("%s %s… %ds", frame, label, int(time.Since(m.thinkStart).Seconds())))
	}
	return helpStyle.Render("enter send · alt+enter newline · pgup/pgdn/wheel scroll · drag select · right-click copy · esc cancel · ctrl+l clear · ctrl+c quit")
}

func (m *chatModel) statusBar() string {
	model := "-"
	if pc, ok := m.deps.Cfg.Providers[m.deps.Cfg.ActiveProvider]; ok && pc.Model != "" {
		model = pc.Model
	}
	mem := "none"
	if m.deps.Memory != nil {
		mem = m.deps.Memory.Name()
	}
	chat := "new"
	if m.convID != 0 {
		chat = fmt.Sprintf("#%d", m.convID)
	}
	info := fmt.Sprintf(" %s · %s · ctx:%dmsg · mem:%s · chat:%s",
		m.deps.Agent.Provider(), model, m.deps.Agent.HistoryLen(), mem, chat)
	return statusStyle.Width(m.width).Render(info)
}

func (m *chatModel) renderSuggestions() string {
	lines := make([]string, 0, len(m.suggestions))
	for i, s := range m.suggestions {
		line := fmt.Sprintf(" %-20s %s", s.full, s.desc)
		if i == m.selected {
			line = selStyle.Render(line)
		} else {
			line = sugStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
