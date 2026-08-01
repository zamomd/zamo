import { describe, expect, it } from "vitest";
import { renderMarkdown } from "./markdown";

const ulid = "01JT4X9K2M8P0QRSTVWXYZ2345";

describe("renderMarkdown references", () => {
	it("renders an unresolved topic reference with its written name as the fallback", () => {
		const html = renderMarkdown("See [[Local-First]].", () => null);
		expect(html).toContain('<span class="ref ref-topic ref-missing" data-target="local-first">Local-First</span>');
	});

	it("renders an unresolved bare block reference with its target ULID as the fallback", () => {
		const html = renderMarkdown(`See ((${ulid})).`, () => null);
		expect(html).toContain(`<span class="ref ref-block ref-missing" data-target="${ulid}">${ulid}</span>`);
	});

	it("renders an unresolved labelled block reference with its written label as the fallback", () => {
		const html = renderMarkdown(`See ((${ulid}|no hidden allocations)).`, () => null);
		expect(html).toContain(
			`<span class="ref ref-block ref-missing" data-target="${ulid}">no hidden allocations</span>`,
		);
	});

	it("renders a resolved topic reference with the resolver's live text, not the written name", () => {
		const html = renderMarkdown("See [[Old Name]].", (ref) =>
			ref.kind === "topic" && ref.targetKey === "old name" ? "New Name" : null,
		);
		expect(html).toContain('<span class="ref ref-topic" data-target="old name">New Name</span>');
	});

	it("renders a resolved block reference with the target's live first line, not the stale label", () => {
		const html = renderMarkdown(`See ((${ulid}|a stale label)).`, (ref) =>
			ref.kind === "block" && ref.targetKey === ulid ? "The current first line" : null,
		);
		expect(html).toContain(`<span class="ref ref-block" data-target="${ulid}">The current first line</span>`);
	});

	it("passes the resolver a Ref matching parseRefs' shape for each kind", () => {
		const seen: unknown[] = [];
		renderMarkdown(`[[Zig]] and ((${ulid}|x))`, (ref) => {
			seen.push(ref);
			return null;
		});
		expect(seen).toEqual([
			{ kind: "topic", targetKey: "zig" },
			{ kind: "block", targetKey: ulid },
		]);
	});

	it("escapes HTML in block content, never emitting an input-derived tag", () => {
		const html = renderMarkdown("<script>alert(1)</script>", () => null);
		expect(html).not.toContain("<script>");
		expect(html).toContain("&lt;script&gt;");
	});

	it("escapes a resolver's returned text the same way, since it can come from other authored content", () => {
		const html = renderMarkdown("[[Topic]]", () => "<img src=x onerror=alert(1)>");
		expect(html).not.toContain("<img");
		expect(html).toContain("&lt;img src=x onerror=alert(1)&gt;");
	});

	it("still ignores a reference written inside fenced code or a code span", () => {
		const html = renderMarkdown("`[[Zig]]` and\n```\n((not-code))\n```", () => "resolved");
		expect(html).not.toContain('class="ref');
		expect(html).toContain("[[Zig]]");
	});

	it("escapes an attribute value the same way as content", () => {
		const html = renderMarkdown('[[a" onmouseover=x]]', () => null);
		expect(html).toContain('data-target="a&quot; onmouseover=x"');
		expect(html).not.toContain('data-target="a" onmouseover=x"');
	});

	it("strips the topic's own markdown markers from the unresolved fallback, matching its key", () => {
		const html = renderMarkdown("[[## Foo]]", () => null);
		expect(html).toContain('<span class="ref ref-topic ref-missing" data-target="foo">Foo</span>');
	});
});
