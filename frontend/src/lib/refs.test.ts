import { describe, expect, it } from "vitest"
import fixture from "../../../internal/knowledge/testdata/refs.json"
import { normalizeTitle, parseRefs } from "./refs"

type Case = {
	name: string
	content: string
	refs: { kind: string; targetKey: string }[]
}

// The fixture is shared with the Go parser (internal/knowledge/refs_test.go).
// Both implementations must agree on it, or references resolve differently
// depending on which side looked at the text.
const cases: Case[] = fixture

describe("parseRefs", () => {
	it("has a non-empty fixture", () => {
		expect(cases.length).toBeGreaterThan(0)
	})

	for (const tc of cases) {
		it(tc.name, () => {
			expect(parseRefs(tc.content)).toEqual(tc.refs)
		})
	}
})

describe("normalizeTitle", () => {
	it.each([
		["Local-first", "local-first"],
		["  Local-first  ", "local-first"],
		["Local-first\n\nBody.", "local-first"],
		["## Local-first", "local-first"],
		["Local\tfirst   design", "local first design"],
		["   \n\n", ""],
		["###", ""],
	])("normalizes %j", (input, want) => {
		expect(normalizeTitle(input)).toBe(want)
	})
})
