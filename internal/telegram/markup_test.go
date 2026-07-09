package telegram

import "testing"

func TestToHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"**Trabajo:**", "<b>Trabajo:</b>"},
		{"__nota__", "<i>nota</i>"},
		{"ref a `+liquida`", "ref a <code>+liquida</code>"},
		// daily punctuation stays literal, only < > & are escaped.
		{"  - [FEAT] #343 (hoy)", "  - [FEAT] #343 (hoy)"},
		{"  >> usar VPN", "  &gt;&gt; usar VPN"},
		{"a < b & c > d", "a &lt; b &amp; c &gt; d"},
		// markers inside a code span are left untouched.
		{"`**x**`", "<code>**x**</code>"},
	}
	for _, c := range cases {
		if got := toHTML(c.in); got != c.want {
			t.Errorf("toHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
