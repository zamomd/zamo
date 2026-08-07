import { describe, expect, it } from "vitest";
import { excerptAround } from "./excerpt";

describe("excerptAround", () => {
	it("returns the paragraph carrying the reference", () => {
		const doc = "# Doc\n\nFirst paragraph.\n\nSee [[Go Contexts]] for more.\n\nLast paragraph.";
		expect(excerptAround(doc, "Go Contexts")).toBe("See [[Go Contexts]] for more.");
	});

	it("falls back to the opening paragraph when the reference is absent", () => {
		expect(excerptAround("# Doc\n\nFirst paragraph.\n\nSecond.", "Missing")).toBe("# Doc");
	});

	it("leaves a short block whole", () => {
		expect(excerptAround("See [[Go Contexts]].", "Go Contexts")).toBe("See [[Go Contexts]].");
	});
});
