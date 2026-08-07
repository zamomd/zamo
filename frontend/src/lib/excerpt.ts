// excerptAround returns the blank-line-separated paragraph of content that
// carries needle. A backlink panel shows the sentence that made the reference,
// and a document source would otherwise put a whole page into the panel.
// Content without the needle falls back to its opening paragraph.
export function excerptAround(content: string, needle: string): string {
	const paragraphs = content
		.split(/\n\s*\n/)
		.map((p) => p.trim())
		.filter((p) => p !== "");
	if (paragraphs.length === 0) {
		return "";
	}
	return paragraphs.find((p) => p.includes(needle)) ?? paragraphs[0];
}
