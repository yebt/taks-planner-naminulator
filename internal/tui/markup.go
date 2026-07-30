package tui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Dailies are authored in a CommonMark subset (**bold**, __italic__, `code`)
// so they stay readable when stored and when sent onward. Telegram gets an HTML
// translation (internal/telegram/markup.go); the TUI gets this one. Same three
// constructs, same code-spans-first ordering, different output target — if you
// add a construct to one, add it to the other.
var (
	mdCodeRe = regexp.MustCompile("`([^`]+)`")
	mdBoldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdItalRe = regexp.MustCompile(`__([^_]+)__`)
)

var (
	mdBold = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))
	mdItal = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245"))
	mdCode = lipgloss.NewStyle().Foreground(lipgloss.Color("180"))
)

// renderMarkup paints the CommonMark subset with lipgloss so the markers stop
// showing up literally on screen. Code spans are pulled out to placeholders
// first so bold/italic markers inside them are never re-parsed: `**x**` must
// render as literal **x**, not as bold x.
func renderMarkup(s string) string {
	var codes []string
	s = mdCodeRe.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, mdCode.Render(m[1:len(m)-1]))
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})

	s = mdBoldRe.ReplaceAllStringFunc(s, func(m string) string {
		return mdBold.Render(m[2 : len(m)-2])
	})
	s = mdItalRe.ReplaceAllStringFunc(s, func(m string) string {
		return mdItal.Render(m[2 : len(m)-2])
	})

	for i, c := range codes {
		s = strings.Replace(s, "\x00"+strconv.Itoa(i)+"\x00", c, 1)
	}
	return s
}
