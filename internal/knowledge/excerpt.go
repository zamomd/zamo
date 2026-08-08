package knowledge

import "strings"

// BacklinkExcerpt returns the paragraph of source that carries a reference to
// the target: the sentence that made the link, rather than the whole block a
// document-sized source would otherwise put into a backlink panel.
//
// It matches the reference, not the target's name, because those are not the
// same question. A block reference resolves by id and its label is free text —
// often nothing at all — and a topic reference resolves by normalized title,
// so [[go   contexts]] finds "Go Contexts" while a search for that literal
// name finds nothing. titleKey is the target's title_key and is empty for a
// block that is not a topic root, which is exactly when no topic reference can
// name it.
//
// A source whose reference this cannot see falls back to its opening
// paragraph; content that is entirely blank yields an empty string.
func BacklinkExcerpt(source string, target ID, titleKey string) string {
	want := Ref{Kind: RefBlock, TargetKey: strings.ToUpper(string(target))}
	paragraphs := splitParagraphs(source)
	for _, p := range paragraphs {
		for _, ref := range ParseRefs(p) {
			if ref == want || (titleKey != "" && ref.Kind == RefTopic && ref.TargetKey == titleKey) {
				return p
			}
		}
	}
	if len(paragraphs) == 0 {
		return ""
	}
	return paragraphs[0]
}

// splitParagraphs cuts content on blank lines and drops the empty pieces, so
// each result is one paragraph with no surrounding whitespace.
func splitParagraphs(content string) []string {
	var out []string
	var buf []string
	flush := func() {
		if len(buf) == 0 {
			return
		}
		if p := strings.TrimSpace(strings.Join(buf, "\n")); p != "" {
			out = append(out, p)
		}
		buf = nil
	}
	for line := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return out
}
