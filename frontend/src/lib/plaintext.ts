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
	return src
		.replace(/^[ \t]*(?:#{1,6}[ \t]+|[-*+][ \t]+|\d+\.[ \t]+|>[ \t]*)/gm, "")
		.replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
		.replace(/\*\*([^*]+)\*\*/g, "$1")
		.replace(/(?<![\w_])__([^_]+)__(?![\w_])/g, "$1")
		.replace(/\*(\S|\S[^*\n]*\S)\*/g, "$1")
		.replace(/(?<![\w_])_(\S|\S[^_\n]*\S)_(?![\w_])/g, "$1")
		.replace(/`([^`\n]+)`/g, "$1")
		.replace(/\s+/g, " ")
		.trim()
}
