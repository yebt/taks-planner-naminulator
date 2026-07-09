package telegram

import (
	"regexp"
	"strconv"
	"strings"
)

// The daily is authored in CommonMark-style markup (**bold**, __italic__,
// `code`) so it stays readable in the TUI and when stored. Telegram understands
// none of that natively: MarkdownV2 uses single */_ and requires escaping many
// punctuation chars (- # . + ( )) that dailies are full of. HTML mode only
// needs < > & escaped, so it is the robust target — we translate to it on send.
var (
	reCode = regexp.MustCompile("`([^`]+)`")
	reBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItal = regexp.MustCompile(`__([^_]+)__`)
)

// toHTML converts the CommonMark subset used by dailies to Telegram HTML. It
// escapes HTML-special characters first (so literal text is safe), then pulls
// code spans out to placeholders so markers inside them are never re-parsed,
// maps bold/italic, and restores the code spans.
func toHTML(s string) string {
	s = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)

	var codes []string
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, "<code>"+m[1:len(m)-1]+"</code>")
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})

	s = reBold.ReplaceAllString(s, "<b>$1</b>")
	s = reItal.ReplaceAllString(s, "<i>$1</i>")

	for i, c := range codes {
		s = strings.Replace(s, "\x00"+strconv.Itoa(i)+"\x00", c, 1)
	}
	return s
}
