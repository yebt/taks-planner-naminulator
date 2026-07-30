package telegram

import "strings"

// maxMessageChars is Telegram's sendMessage text limit. A busy day produces a
// digest well past it, and the API answers ok:false ("message is too long"),
// so the daily has to be delivered in several messages instead.
const maxMessageChars = 4096

// fitsHTML reports whether the rendered form of source markup stays within
// limit. Sizing by the rendered HTML overestimates what Telegram counts (it
// measures the parsed text, not the tags), and that is deliberate: the cost of
// underestimating is a message that never arrives.
func fitsHTML(s string, limit int) bool { return len(toHTML(s)) <= limit }

// splitForTelegram cuts source markup into chunks whose rendered HTML fits
// within limit. It splits the SOURCE, never the HTML: cutting the rendered
// output could leave an open <b>/<i>/<code> tag on one side of the boundary and
// earn a 400 from Telegram. Dailies never spread a markup construct across
// lines, so line boundaries are safe cuts on the source.
//
// Text that already fits comes back as a single chunk, byte for byte, so the
// common case still travels as exactly one request.
func splitForTelegram(text string, limit int) []string {
	if limit <= 0 {
		limit = maxMessageChars
	}
	if fitsHTML(text, limit) {
		return []string{text}
	}
	chunks := packUnits(splitUnits(text, limit), limit)
	if len(chunks) == 0 {
		// Only reachable if the text is nothing but separators; sending it
		// unchanged keeps the previous behaviour rather than sending nothing.
		return []string{text}
	}
	return chunks
}

// chunkUnit is an indivisible piece of the source together with the separator
// that must precede it when it is packed behind another unit in the same chunk.
// The separator is dropped when the unit opens a chunk, so chunks never start
// with stray blank lines.
type chunkUnit struct {
	sep  string
	text string
}

// splitUnits breaks the source into units that are each known to fit. Blank
// lines come first so whole sections stay together; a paragraph that is too big
// on its own falls back to single newlines, and a single line that is still too
// long falls back to a hard cut.
func splitUnits(text string, limit int) []chunkUnit {
	var units []chunkUnit
	for i, para := range strings.Split(text, "\n\n") {
		sep := "\n\n"
		if i == 0 {
			sep = ""
		}
		if fitsHTML(para, limit) {
			units = append(units, chunkUnit{sep: sep, text: para})
			continue
		}
		for j, line := range strings.Split(para, "\n") {
			lineSep := "\n"
			if j == 0 {
				lineSep = sep
			}
			if fitsHTML(line, limit) {
				units = append(units, chunkUnit{sep: lineSep, text: line})
				continue
			}
			for k, piece := range hardSplit(line, limit) {
				pieceSep := ""
				if k == 0 {
					pieceSep = lineSep
				}
				units = append(units, chunkUnit{sep: pieceSep, text: piece})
			}
		}
	}
	return units
}

// packUnits greedily fills chunks with consecutive units, opening a new chunk
// as soon as the next unit would push the rendered size over the limit. Units
// that carry no content are skipped: a run of three or more newlines would
// otherwise open a chunk with an empty body, which Telegram rejects.
func packUnits(units []chunkUnit, limit int) []string {
	var chunks []string
	var cur string
	open := false
	for _, u := range units {
		if strings.TrimSpace(u.text) == "" {
			continue
		}
		if !open {
			cur, open = u.text, true
			continue
		}
		if candidate := cur + u.sep + u.text; fitsHTML(candidate, limit) {
			cur = candidate
			continue
		}
		chunks = append(chunks, cur)
		cur = u.text
	}
	if open {
		chunks = append(chunks, cur)
	}
	return chunks
}

// hardSplit is the last resort for a single line longer than the limit: it cuts
// mid-line on rune boundaries. Each iteration consumes at least one rune, so it
// terminates even on pathological input where a lone rune renders longer than
// the limit — the kind of loop that hangs in production and never in tests.
func hardSplit(s string, limit int) []string {
	var out []string
	runes := []rune(s)
	for len(runes) > 0 {
		n := longestFit(runes, limit)
		if n < 1 {
			n = 1
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

// longestFit returns the length in runes of the longest prefix that still fits.
// toHTML only ever grows its input (escaping and tags both add characters), so
// the rendered size is non-decreasing in the prefix length and a binary search
// is sound; the trailing shrink is a cheap guard in case some future markup
// rule breaks that assumption.
func longestFit(runes []rune, limit int) int {
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fitsHTML(string(runes[:mid]), limit) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	for lo > 1 && !fitsHTML(string(runes[:lo]), limit) {
		lo--
	}
	return lo
}
