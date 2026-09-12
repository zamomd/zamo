package vaultimport

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The link syntax shared by both formats and by Comind: [[Page]] and
// ((block)) spans. A backslash escapes the character after it, and a span is
// skipped whole once opened, so a [[Page]] written inside a block
// reference's label is text.

// spanAt reports the [[...]] or ((...)) span opening at i: its body and the
// offset just past its closer. A bracket body holding another bracket is not
// a span; an unterminated opener is text.
func spanAt(line string, i int) (open, body string, end int, ok bool) {
	if i+1 >= len(line) {
		return "", "", 0, false
	}
	switch line[i : i+2] {
	case "[[":
		body, end, ok = until(line, i+2, "]]")
		if !ok || strings.ContainsAny(body, "[]") {
			return "", "", 0, false
		}
		return "[[", body, end, true
	case "((":
		body, end, ok = until(line, i+2, "))")
		if !ok {
			return "", "", 0, false
		}
		return "((", body, end, true
	}
	return "", "", 0, false
}

// until returns the text between start and the next closer, plus the offset
// just past it.
func until(line string, start int, closer string) (body string, end int, ok bool) {
	j := strings.Index(line[start:], closer)
	if j < 0 {
		return "", 0, false
	}
	return line[start : start+j], start + j + len(closer), true
}

// rewriteLines applies rewrite to each line of content, keeping the line
// breaks as they are.
func rewriteLines(content string, rewrite func(line string) string) string {
	if !strings.ContainsAny(content, "[(#") {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = rewrite(line)
	}
	return strings.Join(lines, "\n")
}

// escaper follows backslash escaping along a line: step reports whether the
// byte at the position is active, and writes it through when it is not.
type escaper struct{ escaped bool }

func (e *escaper) active(c byte) bool {
	if c == '\\' {
		e.escaped = !e.escaped
		return false
	}
	active := !e.escaped
	e.escaped = false
	return active
}

// RewriteWikilinks reduces every [[...]] to the plain [[Page]] form Comind
// resolves: an alias after | and a heading or block anchor after # or ^ are
// dropped, and an embed ![[x]] becomes a reference. Each loss is reported
// with the original link as detail; the Page of each warning is left for the
// caller to fill. A link with no page part, such as [[#Heading]], is kept
// as written.
func RewriteWikilinks(content string) (string, []Warning) {
	var warns []Warning
	out := rewriteLines(content, func(line string) string {
		var b strings.Builder
		var esc escaper
		for i := 0; i < len(line); i++ {
			if !esc.active(line[i]) {
				b.WriteByte(line[i])
				continue
			}
			embed := line[i] == '!' && strings.HasPrefix(line[i+1:], "[[")
			start := i
			if embed {
				start++
			}
			open, body, end, ok := spanAt(line, start)
			if !ok {
				b.WriteByte(line[i])
				continue
			}
			if open == "((" {
				b.WriteString(line[i:end])
				i = end - 1
				continue
			}
			page, kinds := plainPage(body)
			if page == "" {
				b.WriteString(line[i:end])
				warns = append(warns, Warning{Kind: WarnAnchor, Detail: line[i:end]})
				i = end - 1
				continue
			}
			b.WriteString("[[" + page + "]]")
			if embed {
				warns = append(warns, Warning{Kind: WarnEmbed, Detail: line[i:end]})
			} else {
				for _, k := range kinds {
					warns = append(warns, Warning{Kind: k, Detail: line[i:end]})
				}
			}
			i = end - 1
		}
		return b.String()
	})
	return out, warns
}

// plainPage strips a wikilink body down to its page name and reports what it
// dropped, anchors before aliases.
func plainPage(body string) (string, []string) {
	var kinds []string
	page := body
	alias := false
	if pipe := strings.IndexByte(page, '|'); pipe >= 0 {
		page = page[:pipe]
		alias = true
	}
	if cut := strings.IndexAny(page, "#^"); cut >= 0 {
		page = page[:cut]
		kinds = append(kinds, WarnAnchor)
	}
	if alias {
		kinds = append(kinds, WarnAlias)
	}
	return strings.TrimSpace(page), kinds
}

// RewriteTags turns Logseq tags into topic references: #tag becomes [[tag]]
// and #[[multi word]] becomes [[multi word]]. The rules are mldoc's: a tag
// opens at the start of a line, after whitespace, or after one of the
// punctuation marks a tag may trail; it needs a character other than
// whitespace or # after the #; it runs to the next whitespace; and trailing
// punctuation of that same set is not part of the name. That leaves
// headings, URL fragments and words such as C# alone.
func RewriteTags(content string) string {
	return rewriteLines(content, func(line string) string {
		var b strings.Builder
		var esc escaper
		for i := 0; i < len(line); i++ {
			if !esc.active(line[i]) {
				b.WriteByte(line[i])
				continue
			}
			if _, _, end, ok := spanAt(line, i); ok {
				b.WriteString(line[i:end])
				i = end - 1
				continue
			}
			if line[i] != '#' || !tagOpensAt(line, i) {
				b.WriteByte(line[i])
				continue
			}
			if _, body, end, ok := spanAt(line, i+1); ok {
				b.WriteString("[[" + body + "]]")
				i = end - 1
				continue
			}
			end := tagEnd(line, i+1)
			name := strings.TrimRight(line[i+1:end], tagTrail)
			if name == "" {
				b.WriteByte(line[i])
				continue
			}
			b.WriteString("[[" + name + "]]")
			i = i + len(name)
		}
		return b.String()
	})
}

// tagTrail is the punctuation a tag may follow directly and never ends in.
const tagTrail = `,;.!?'":#`

// tagOpensAt reports whether the # at i starts a tag.
func tagOpensAt(line string, i int) bool {
	if i > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:i])
		if !unicode.IsSpace(r) && !strings.ContainsRune(tagTrail, r) {
			return false
		}
	}
	if i+1 >= len(line) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(line[i+1:])
	return !unicode.IsSpace(r) && r != '#'
}

// tagEnd returns the offset just past the tag text starting at start: it
// runs to the next whitespace.
func tagEnd(line string, start int) int {
	for j, r := range line[start:] {
		if unicode.IsSpace(r) {
			return start + j
		}
	}
	return len(line)
}

// RewriteLinkTargets rewrites the target of every plain [[target]] span
// through resolve; a span resolve declines is left as it was.
func RewriteLinkTargets(content string, resolve func(target string) (string, bool)) string {
	return rewriteLines(content, func(line string) string {
		var b strings.Builder
		var esc escaper
		for i := 0; i < len(line); i++ {
			if !esc.active(line[i]) {
				b.WriteByte(line[i])
				continue
			}
			open, body, end, ok := spanAt(line, i)
			if !ok {
				b.WriteByte(line[i])
				continue
			}
			if open == "[[" {
				if target, ok := resolve(body); ok {
					b.WriteString("[[" + target + "]]")
					i = end - 1
					continue
				}
			}
			b.WriteString(line[i:end])
			i = end - 1
		}
		return b.String()
	})
}

// RewriteBlockRefs rewrites every ((target)) and ((target|label)) span
// through resolve, which receives the trimmed target and the label as
// written (empty when there is none) and answers with the new target and
// label. A span resolve declines is left as it was.
func RewriteBlockRefs(content string, resolve func(target, label string) (string, string, bool)) string {
	return rewriteLines(content, func(line string) string {
		var b strings.Builder
		var esc escaper
		for i := 0; i < len(line); i++ {
			if !esc.active(line[i]) {
				b.WriteByte(line[i])
				continue
			}
			open, body, end, ok := spanAt(line, i)
			if !ok {
				b.WriteByte(line[i])
				continue
			}
			if open == "((" {
				target, label, _ := strings.Cut(body, "|")
				if target, label, ok := resolve(strings.TrimSpace(target), label); ok {
					if label != "" {
						label = "|" + label
					}
					b.WriteString("((" + target + label + "))")
					i = end - 1
					continue
				}
			}
			b.WriteString(line[i:end])
			i = end - 1
		}
		return b.String()
	})
}
