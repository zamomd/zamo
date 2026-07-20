import MarkdownIt from "markdown-it";

// Block content is authored by an LLM, so it is untrusted. `html: false` makes
// markdown-it escape any raw HTML in the source and never emit input-derived
// tags, and its default link validation rejects javascript:, vbscript: and
// data: hrefs — so the rendered string is safe to inject with {@html}. That
// guarantee holds only as long as html stays off and no raw-HTML plugin is
// added here; keep this module the single place markdown becomes HTML.
const md = new MarkdownIt({
	html: false,
	linkify: true,
	breaks: true,
});

// Mark every link external. The rendered href is already scheme-checked by
// markdown-it; the app additionally intercepts the click (see TreeNode) to open
// the system browser instead of navigating the webview, and target/rel make the
// intent explicit and safe even if an anchor is ever activated another way.
// With no prior link_open rule, the renderer's default is self.renderToken, so
// setting attributes and delegating to it is the canonical override.
md.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
	tokens[idx].attrSet("target", "_blank");
	tokens[idx].attrSet("rel", "noopener noreferrer");
	return self.renderToken(tokens, idx, options);
};

// renderMarkdown turns one block's markdown source into sanitized HTML ready for
// {@html}. The raw source is never mutated — it stays the edit round-trip value.
export function renderMarkdown(src: string): string {
	return md.render(src);
}
