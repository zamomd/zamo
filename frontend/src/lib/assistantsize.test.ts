import { describe, expect, it } from "vitest"
import { ASSISTANT_MIN, assistantMax, assistantWidth } from "./assistantsize"

describe("assistantWidth", () => {
	it("keeps a width inside the bounds as asked", () => {
		expect(assistantWidth(400, 1200)).toBe(400)
	})

	it("never goes narrower than the minimum", () => {
		expect(assistantWidth(40, 1200)).toBe(ASSISTANT_MIN)
		expect(assistantWidth(-300, 1200)).toBe(ASSISTANT_MIN)
	})

	it("leaves the rest of the window at least half", () => {
		expect(assistantWidth(5000, 1200)).toBe(600)
		expect(assistantMax(1200)).toBe(600)
	})

	it("holds the minimum in a window too small for both", () => {
		expect(assistantWidth(400, 300)).toBe(ASSISTANT_MIN)
	})
})
