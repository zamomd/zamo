import { describe, expect, it } from "vitest";
import { plainText } from "./plaintext";

describe("plainText", () => {
	// Each case is a shape the expansion prompt actually produces, so these are
	// the strings the sidebar has to survive, not invented ones.
	const cases: [string, string, string][] = [
		["heading", "# Rate limiting", "Rate limiting"],
		["deeper heading", "### Наш шлюз", "Наш шлюз"],
		["bold", "**burst** is the bucket size", "burst is the bucket size"],
		["italic", "_leaky_ bucket", "leaky bucket"],
		["inline code", "`burst` is the bucket size", "burst is the bucket size"],
		["link", "see [the RFC](https://example.com/rfc)", "see the RFC"],
		["list marker", "- token bucket", "token bucket"],
		["ordered marker", "1. token bucket", "token bucket"],
		["blockquote", "> smooths bursts", "smooths bursts"],
		["multi-line collapses to one", "first line\nsecond line", "first line second line"],
		["plain text is untouched", "burst = 40", "burst = 40"],
		["empty stays empty", "", ""],
	];
	for (const [name, src, want] of cases) {
		it(`strips ${name}`, () => {
			expect(plainText(src)).toBe(want);
		});
	}

	it("leaves a bare asterisk alone rather than eating punctuation", () => {
		// Emphasis needs a closing delimiter; a lone star is content.
		expect(plainText("2 * 3 = 6")).toBe("2 * 3 = 6");
	});
});
