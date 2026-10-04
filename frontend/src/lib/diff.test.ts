import { describe, expect, it } from "vitest"
import { type DiffLine, diffLines } from "./diff"

function text(line: DiffLine): string {
	return line.parts.map((p) => p.text).join("")
}

function changed(line: DiffLine): string[] {
	return line.parts.filter((p) => p.changed).map((p) => p.text)
}

describe("diffLines", () => {
	it("keeps unchanged lines and marks whole added and removed ones", () => {
		const lines = diffLines("a\nb\nc", "a\nc\nd")
		expect(lines.map((l) => [l.kind, text(l)])).toEqual([
			["same", "a"],
			["del", "b"],
			["same", "c"],
			["add", "d"],
		])
		expect(lines.some((l) => l.intraline)).toBe(false)
	})

	it("singles out the changed words of a rewritten line", () => {
		const [del, add] = diffLines("the quick brown fox", "the quick red fox")
		expect([del.kind, text(del), changed(del)]).toEqual(["del", "the quick brown fox", ["brown"]])
		expect([add.kind, text(add), changed(add)]).toEqual(["add", "the quick red fox", ["red"]])
		expect(del.intraline && add.intraline).toBe(true)
	})

	it("leaves a rewrite with nothing in common as whole lines", () => {
		const lines = diffLines("alpha", "omega")
		expect(lines.map((l) => [l.kind, text(l), l.intraline])).toEqual([
			["del", "alpha", false],
			["add", "omega", false],
		])
	})

	it("keeps every line of a rewritten run, on both sides", () => {
		const lines = diffLines("one two\nthree four", "one 2\nthree four five\nsix")
		expect(lines.filter((l) => l.kind === "del").map(text)).toEqual(["one two", "three four"])
		expect(lines.filter((l) => l.kind === "add").map(text)).toEqual(["one 2", "three four five", "six"])
	})

	it("reads a first version as one added line per line", () => {
		const lines = diffLines("", "x\ny")
		expect(lines.filter((l) => l.kind === "add").map(text)).toEqual(["x", "y"])
	})
})
