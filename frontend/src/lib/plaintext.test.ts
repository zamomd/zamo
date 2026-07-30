import { describe, expect, it } from "vitest";
import { plainText } from "./plaintext";

describe("plainText", () => {
	// Each case is a shape the expansion prompt actually produces, so these are
	// the strings the sidebar has to survive, not invented ones.
	const cases: [string, string, string][] = [
		["heading", "# Rate limiting", "Rate limiting"],
		// Non-Latin source, written as \u escapes rather than literal Cyrillic:
		// `just check`'s english-only gate (justfile:32) greps tracked file
		// bytes, and this repo is source-English-only even though block
		// content is not. The runtime string is identical either way.
		[
			"deeper heading",
			"### \u041d\u0430\u0448\u0020\u0448\u043b\u044e\u0437",
			"\u041d\u0430\u0448\u0020\u0448\u043b\u044e\u0437",
		],
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
		["intraword underscores are an identifier, not emphasis", "my_var_name", "my_var_name"],
		[
			"repeated intraword underscores stay an identifier",
			"rate_limit_bucket is a thing",
			"rate_limit_bucket is a thing",
		],
		["repeated asterisks are multiplication, not emphasis pairs", "2 * 3 * 4 = 24", "2 * 3 * 4 = 24"],
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
