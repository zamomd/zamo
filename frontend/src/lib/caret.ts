import { decodeHTML, decodeHTMLStrict } from "entities"
import { type RefResolver, renderMarkdown } from "./markdown"

const entityAtStart = /^&(?:#[xX][\dA-Fa-f]+|#\d+|[A-Za-z][A-Za-z\d]+);/

// renderedText mirrors the rendered container's textContent. Tag stripping is
// safe because Markdown input HTML is disabled. resolve must be the same
// resolver the caller rendered raw's DOM with: this text is later diffed
// character-by-character against that DOM's textContent (see sourceOffset),
// and a different resolver produces a different string for any reference
// that resolves, throwing that diff off.
export function renderedText(raw: string, resolve: RefResolver): string {
	return decodeHTML(renderMarkdown(raw, resolve).replace(/<[^>]*>/g, ""))
}

// sourceOffset maps a rendered-text offset to its Markdown source offset.
// Unmappable positions fall back to the end of the source. resolve must match
// the one the offset was measured against in the DOM — see renderedText.
export function sourceOffset(raw: string, renderedOffset: number, resolve: RefResolver): number {
	const rendered = renderedText(raw, resolve)
	const target = Math.max(renderedOffset, 0)
	let r = 0
	let s = 0
	while (s < raw.length && (r < target || (r < rendered.length && raw[s] !== rendered[r]))) {
		const entity = raw.slice(s).match(entityAtStart)?.[0]
		if (entity !== undefined) {
			const decoded = decodeHTMLStrict(entity)
			if (decoded !== entity && rendered.startsWith(decoded, r)) {
				if (r === target) {
					break
				}
				r += decoded.length
				s += entity.length
				continue
			}
		}
		if (raw[s] === "\\" && raw[s + 1] === rendered[r]) {
			if (r === target) {
				break
			}
			r++
			s += 2
			continue
		}
		if (r < rendered.length && raw[s] === rendered[r]) {
			r++
			s++
			continue
		}
		if (r < target && rendered[r] === "\n" && raw[s] !== "\n") {
			r++
			continue
		}
		s++
	}
	return r < target ? raw.length : s
}

// lineCount counts the caret's lines, not the rendered ones: a trailing newline
// opens a real line the caret can sit on, so "a\n" is two.
export function lineCount(text: string): number {
	return text.split("\n").length
}

// lineIndexOf is the zero-based line the offset sits on.
export function lineIndexOf(text: string, offset: number): number {
	let line = 0
	for (let i = 0; i < offset && i < text.length; i++) {
		if (text[i] === "\n") {
			line++
		}
	}
	return line
}

// caretColumn counts characters from the start of the caret's own line. It is
// what survives an arrow jump between blocks.
export function caretColumn(text: string, offset: number): number {
	const before = text.slice(0, offset)
	const nl = before.lastIndexOf("\n")
	return nl === -1 ? before.length : before.length - nl - 1
}

// offsetAtLine is caretColumn's inverse, clamped on both axes. Clamping is the
// normal case, not the error case: a column carried from a long line routinely
// lands past the end of a short one.
export function offsetAtLine(text: string, line: number, column: number): number {
	const lines = text.split("\n")
	const index = Math.min(Math.max(line, 0), lines.length - 1)
	let offset = 0
	for (let i = 0; i < index; i++) {
		offset += lines[i].length + 1 // + the newline itself
	}
	return offset + Math.min(Math.max(column, 0), lines[index].length)
}

// renderedOffsetFromPoint returns an offset into the container's textContent.
export function renderedOffsetFromPoint(container: HTMLElement, x: number, y: number): number | null {
	const fromPoint = (
		document as Document & {
			caretRangeFromPoint?: (x: number, y: number) => Range | null
		}
	).caretRangeFromPoint
	const range = fromPoint?.call(document, x, y)
	if (!range || !container.contains(range.startContainer)) {
		return null
	}
	const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)
	let offset = 0
	let node = walker.nextNode()
	while (node !== null) {
		if (node === range.startContainer) {
			return offset + range.startOffset
		}
		offset += node.textContent?.length ?? 0
		node = walker.nextNode()
	}
	return null
}
