package knowledge

import "strings"

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
	for _, line := range codeFreeLines(content) {
		scanLine(line, add)
	}
	return out
}

// scanLine reports every reference on one code-free line.
func scanLine(line string, add func(Ref)) {
	for i := 0; i < len(line)-1; i++ {
		switch {
		case line[i] == '[' && line[i+1] == '[':
			body, end, ok := until(line, i+2, "]]")
			if !ok {
				continue
			}
			// Nested brackets mean the outer pair is not a reference; the
			// inner one is found by continuing the scan from just past "[[".
			if !strings.ContainsAny(body, "[]") {
				add(Ref{Kind: RefTopic, TargetKey: NormalizeTitle(body)})
				i = end - 1
			}
		case line[i] == '(' && line[i+1] == '(':
			body, end, ok := until(line, i+2, "))")
			if !ok {
				continue
			}
			target := body
			if pipe := strings.IndexByte(body, '|'); pipe >= 0 {
				target = body[:pipe]
			}
			target = strings.ToUpper(strings.TrimSpace(target))
			if validULID(target) {
				add(Ref{Kind: RefBlock, TargetKey: target})
				i = end - 1
			}
		}
	}
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

// codeFreeLines splits content into lines with code removed: lines inside a
// fenced block are dropped entirely, and inline code spans are blanked out of
// the lines that survive. An unterminated fence swallows the rest of the
// content, matching how a markdown renderer reads it.
func codeFreeLines(content string) []string {
	var out []string
	fence := ""
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = "```"
			continue
		}
		if strings.HasPrefix(trimmed, "~~~") {
			fence = "~~~"
			continue
		}
		out = append(out, stripInlineCode(line))
	}
	return out
}

// stripInlineCode replaces the contents of every closed backtick span with
// spaces, preserving offsets so the caller can scan the line as-is.
func stripInlineCode(line string) string {
	open := strings.IndexByte(line, '`')
	if open < 0 {
		return line
	}
	b := []byte(line)
	for open >= 0 && open < len(b)-1 {
		closeIdx := strings.IndexByte(string(b[open+1:]), '`')
		if closeIdx < 0 {
			break
		}
		closeIdx += open + 1
		for i := open; i <= closeIdx; i++ {
			b[i] = ' '
		}
		next := strings.IndexByte(string(b[closeIdx+1:]), '`')
		if next < 0 {
			break
		}
		open = closeIdx + 1 + next
	}
	return string(b)
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
	line := s
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "#>-*+ \t")
	return strings.ToLower(strings.Join(strings.Fields(line), " "))
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
