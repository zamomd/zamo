// plainText reduces markdown to the text a reader would see, on one line. It
// exists for the places that show a block's content as a single ellipsised
// label — the sidebar's rows, the zoom-in breadcrumbs — where markdown's
// punctuation is noise and renderMarkdown's block elements would fight the
// layout.
//
// This is deliberately not markdown-it: that module (see markdown.ts) is the
// one place markdown becomes HTML, and its output is trusted by {@html}.
// Nothing here produces HTML, so nothing here can widen that surface.
//
// The subset handled is the subset the expansion prompt emits: headings,
// emphasis, inline code, links, list markers and blockquotes. Anything else
// passes through as written — a label showing one stray delimiter is a better
// failure than a label that has swallowed a character of content.
export function plainText(src: string): string {
	return (
		src
			// Leading block markers, per line: heading hashes, list bullets,
			// ordered-list numbers, blockquote angles.
			.replace(/^[ \t]*(?:#{1,6}[ \t]+|[-*+][ \t]+|\d+\.[ \t]+|>[ \t]*)/gm, "")
			// Links and images: keep the text, drop the target.
			.replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
			// Emphasis, only with a closing delimiter. Bold must run first: it has
			// to consume ** before the single-* rule sees it. Each delimiter must
			// hug non-whitespace on both inner sides — a delimiter next to
			// whitespace is punctuation, not emphasis. `_` additionally must not
			// sit between word characters, because snake_case is content and
			// CommonMark's intraword-underscore rule leaves it untouched.
			.replace(/\*\*([^*]+)\*\*/g, "$1")
			.replace(/(?<![\w_])__([^_]+)__(?![\w_])/g, "$1")
			.replace(/\*(\S|\S[^*\n]*\S)\*/g, "$1")
			.replace(/(?<![\w_])_(\S|\S[^_\n]*\S)_(?![\w_])/g, "$1")
			.replace(/`([^`\n]+)`/g, "$1")
			// One line: newlines become spaces, then runs of blank space collapse.
			.replace(/\s+/g, " ")
			.trim()
	);
}
