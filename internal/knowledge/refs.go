package knowledge

import (
	"iter"
	"strings"
)

// RefKind distinguishes the two reference forms found in block content.
type RefKind string

// The reference kinds.
const (
	RefTopic RefKind = "topic" // [[Topic]], resolved by normalized name
	RefBlock RefKind = "block" // ((ULID|label)), resolved by target id
)

// Ref is one outgoing reference parsed out of a block's content. TargetKey is
// the normalized topic name for RefTopic and the upper-cased target ULID for
// RefBlock; the block reference's label is a reading aid inside the text and
// is deliberately not carried here.
type Ref struct {
	Kind      RefKind
	TargetKey string
}

const ulidLen = 26

// crockford is the Crockford base32 alphabet, which excludes I, L, O and U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ParseRefs extracts every reference from a block's content, in source order
// and deduplicated. Text inside fenced code blocks and inline code spans is
// skipped: the base is largely about code, where both bracket forms occur
// literally.
func ParseRefs(content string) []Ref {
	var out []Ref
	seen := make(map[Ref]bool)
	add := func(r Ref) {
		if r.TargetKey == "" || seen[r] {
			return
		}
		seen[r] = true
		out = append(out, r)
	}
	for line, fenced := range fencedLines(content) {
		if fenced {
			continue
		}
		scanLine(stripInlineCode(line), add)
	}
	return out
}

// scanLine reports every reference on one line, code already removed.
func scanLine(line string, add func(Ref)) {
	// Not a range loop: a matched reference advances i past its whole span,
	// which a range variable would discard.
	for i := 0; i < len(line); i++ {
		if body, end, ok := topicRefAt(line, i); ok {
			add(Ref{Kind: RefTopic, TargetKey: NormalizeTitle(body)})
			i = end - 1
			continue
		}
		if target, end, ok := blockRefAt(line, i); ok {
			add(Ref{Kind: RefBlock, TargetKey: target})
			i = end - 1
		}
	}
}

// topicRefAt reports the [[Topic]] reference starting at i: its raw body and
// the offset just past the closing brackets. An unterminated pair is plain
// text. Nested brackets mean the outer pair is not a reference either; the
// inner one is found by continuing the scan from just past "[[".
//
// Both the reader (scanLine) and the rewriter (rewriteLine) go through here,
// so what counts as a topic reference is defined once.
func topicRefAt(line string, i int) (body string, end int, ok bool) {
	if i+1 >= len(line) || line[i] != '[' || line[i+1] != '[' {
		return "", 0, false
	}
	body, end, ok = until(line, i+2, "]]")
	if !ok || strings.ContainsAny(body, "[]") {
		return "", 0, false
	}
	return body, end, true
}

// blockRefAt reports the ((ULID|label)) reference starting at i: its
// upper-cased target id and the offset just past the closing parens. A body
// whose target is not a ULID is plain text, and the scan continues inside it.
//
// The label is free text — it may hold brackets and pipes — so the span is
// skipped whole by reader and rewriter alike: a [[Topic]] written inside a
// label is neither indexed nor rewritten.
func blockRefAt(line string, i int) (target string, end int, ok bool) {
	if i+1 >= len(line) || line[i] != '(' || line[i+1] != '(' {
		return "", 0, false
	}
	body, end, ok := until(line, i+2, "))")
	if !ok {
		return "", 0, false
	}
	target = body
	if pipe := strings.IndexByte(body, '|'); pipe >= 0 {
		target = body[:pipe]
	}
	target = strings.ToUpper(strings.TrimSpace(target))
	if !validULID(target) {
		return "", 0, false
	}
	return target, end, true
}

// RewriteTopicRef replaces every [[...]] whose normalized name equals oldKey
// with newName, leaving the rest of the content untouched. It is how a topic
// rename keeps the text true.
//
// It reads references exactly as ParseRefs does — same fences, same inline
// code, same brackets, block-reference labels skipped whole — so it rewrites
// the references ParseRefs indexed and nothing else. A [[Topic]] the reader
// treats as code has no backlink to keep true, and editing it would silently
// change a code sample the author wrote.
//
// A newName that cannot be written as a reference — one carrying a bracket,
// which would split the [[...]] into a reference plus stray text — is refused:
// the content comes back unchanged rather than mangled.
func RewriteTopicRef(content, oldKey, newName string) string {
	if oldKey == "" || strings.ContainsAny(newName, "[]") {
		return content
	}
	var b strings.Builder
	first := true
	for line, fenced := range fencedLines(content) {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		if fenced {
			b.WriteString(line)
			continue
		}
		b.WriteString(rewriteLine(line, oldKey, newName))
	}
	return b.String()
}

// rewriteLine rewrites matching topic references on one line outside a fence.
//
// References are located in the blanked line stripInlineCode produces — the
// reader's own view of the line — while every byte written out comes from the
// original. Blanking preserves offsets, so the two index alike, and code spans
// carry no reference to find and are therefore copied through verbatim. Sharing
// the function rather than a notion of a span is what keeps reader and rewriter
// from drifting.
func rewriteLine(line, oldKey, newName string) string {
	scan := stripInlineCode(line)
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if body, end, ok := topicRefAt(scan, i); ok {
			if NormalizeTitle(body) == oldKey {
				b.WriteString("[[" + newName + "]]")
			} else {
				b.WriteString(line[i:end])
			}
			i = end - 1
			continue
		}
		if _, end, ok := blockRefAt(scan, i); ok {
			b.WriteString(line[i:end])
			i = end - 1
			continue
		}
		b.WriteByte(line[i])
	}
	return b.String()
}

// until returns the text between start and the next occurrence of closer,
// plus the offset just past closer. A missing terminator reports !ok.
func until(line string, start int, closer string) (body string, end int, ok bool) {
	rest := line[start:]
	j := strings.Index(rest, closer)
	if j < 0 {
		return "", 0, false
	}
	return rest[:j], start + j + len(closer), true
}

// fencedLines splits content into lines, reporting for each whether it is code:
// a fence delimiter itself or a line inside one. An unterminated fence swallows
// the rest of the content, matching how a markdown renderer reads it.
//
// The single definition of a code fence: the reader drops what it reports,
// the rewriter passes it through untouched, and neither can drift from the
// other. What matters more is that both agree with markdown-it, which is what
// actually renders the block: a reference the reader sees as code must be code
// to the index too, or a backlink points into a sample and a rename edits it.
func fencedLines(content string) iter.Seq2[string, bool] {
	return func(yield func(string, bool) bool) {
		var char byte
		open := 0
		for line := range strings.SplitSeq(content, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case open > 0:
				if closesFence(trimmed, char, open) {
					open = 0
				}
			default:
				c, n, ok := opensFence(trimmed)
				if !ok {
					if !yield(line, false) {
						return
					}
					continue
				}
				char, open = c, n
			}
			if !yield(line, true) {
				return
			}
		}
	}
}

// opensFence reports the fence a line opens: its delimiter character and the
// length of the run, following CommonMark. A run of at least three backticks or
// tildes opens one; the text after the run is the info string, which for a
// backtick fence may not itself contain a backtick, so a line of prose carrying
// a code span never opens a block.
func opensFence(trimmed string) (char byte, n int, ok bool) {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	char = trimmed[0]
	n = runLen(trimmed, char)
	if n < 3 {
		return 0, 0, false
	}
	if char == '`' && strings.ContainsRune(trimmed[n:], '`') {
		return 0, 0, false
	}
	return char, n, true
}

// closesFence reports whether a line closes the open fence: the same character,
// a run at least as long as the opening one, and nothing after it. CommonMark
// allows no info string on a closing fence, so ```go inside a fence is content.
func closesFence(trimmed string, char byte, open int) bool {
	if trimmed == "" || trimmed[0] != char {
		return false
	}
	n := runLen(trimmed, char)
	return n >= open && n == len(trimmed)
}

// runLen returns the length of the run of char starting s.
func runLen(s string, char byte) int {
	i := 0
	for i < len(s) && s[i] == char {
		i++
	}
	return i
}

// stripInlineCode replaces every closed backtick span with spaces, preserving
// offsets so the caller can scan the line as-is.
//
// The single definition of an inline code span: the reader (scanLine) scans
// what it returns, the rewriter (rewriteLine) scans it too and writes from the
// original line, and neither can drift from the other.
//
// Spans are matched by backtick run, as CommonMark and therefore markdown-it
// do: a run of n backticks opens a span that the next run of exactly n closes,
// and a run with no matching closer is literal text the scan continues past.
// Counting runs is what makes a reference inside a two-backtick span code to
// the reader and to the index alike — the single-backtick reading indexed it
// and left a backlink pointing into a code sample nobody could see.
//
// The grammar stops there. It has no escape mechanism, so a backslash before
// [[ escapes the bracket for markdown's renderer but not here, and \[[Zig]]
// still indexes: matching markdown's escaping too would mean a second markdown
// implementation in two languages, and the reference forms are rare enough
// characters that quoting them in code — which is handled — is how they
// actually turn up.
func stripInlineCode(line string) string {
	b := []byte(line)
	i := 0
	for i < len(b) {
		if b[i] != '`' {
			i++
			continue
		}
		open := i
		i = runEnd(b, i)
		if closeEnd, ok := matchingRun(b, i, i-open); ok {
			for k := open; k < closeEnd; k++ {
				b[k] = ' '
			}
			i = closeEnd
		}
	}
	return string(b)
}

// runEnd returns the offset just past the backtick run starting at i.
func runEnd(b []byte, i int) int {
	for i < len(b) && b[i] == '`' {
		i++
	}
	return i
}

// matchingRun finds the next backtick run of exactly n after from, returning
// the offset just past it.
func matchingRun(b []byte, from, n int) (end int, ok bool) {
	for i := from; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		run := runEnd(b, i)
		if run-i == n {
			return run, true
		}
		i = run
	}
	return 0, false
}

// validULID reports whether s is 26 Crockford base32 characters. The domain
// imports nothing but the standard library, so the check is inlined here
// rather than taken from internal/ulid.
func validULID(s string) bool {
	if len(s) != ulidLen {
		return false
	}
	for i := range len(s) {
		if !strings.ContainsRune(crockford, rune(s[i])) {
			return false
		}
	}
	return true
}

// NormalizeTitle reduces a topic's content to the key references resolve
// against: its first line, without leading markdown heading or bullet
// markers, with inner whitespace collapsed and casefolded. The same function
// produces blocks.title_key and the target key of a [[Topic]] reference —
// they must not drift apart or resolution fails silently.
func NormalizeTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(TopicName(s)), " "))
}

// TopicName is the text a [[Topic]] reference to this content should carry:
// its first line without the markdown markers NormalizeTitle also drops, with
// the author's casing and spacing kept. Writing a reference from anything else
// — FirstLine, say — would put a marker inside the brackets, so what the
// reader sees and what the reference resolves by would disagree.
func TopicName(s string) string {
	return strings.TrimLeft(FirstLine(s), "#>-*+ \t")
}

// FirstLine is a block's display title: its first line, trimmed. Unlike
// NormalizeTitle it preserves the author's casing and punctuation, because it
// is shown to a reader rather than matched against anything.
func FirstLine(s string) string {
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[:nl]
	}
	return strings.TrimSpace(s)
}
