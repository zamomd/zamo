import { describe, expect, it } from "vitest"
import { stepLabel } from "./assistantsteps"

const names = (id: string) => (id === "01GO" ? "Go contexts" : null)

describe("stepLabel", () => {
	it("says the model is thinking", () => {
		expect(stepLabel({ kind: "thinking" }, names)).toBe("thinking…")
	})

	it("quotes what a search looks for", () => {
		expect(stepLabel({ kind: "tool", tool: "search", detail: "deadline" }, names)).toBe("searching “deadline”…")
	})

	it("names the topic being read when the base knows it", () => {
		expect(stepLabel({ kind: "tool", tool: "get_subtree", detail: "01GO" }, names)).toBe("reading “Go contexts”…")
	})

	it("does not show a raw id for a block it cannot name", () => {
		expect(stepLabel({ kind: "tool", tool: "get_subtree", detail: "01GONE" }, names)).toBe("reading a topic…")
	})

	it("names whose references are followed", () => {
		expect(stepLabel({ kind: "tool", tool: "get_backlinks", detail: "01GO" }, names)).toBe(
			"following references to “Go contexts”…",
		)
		expect(stepLabel({ kind: "tool", tool: "get_backlinks", detail: "01GONE" }, names)).toBe("following references…")
	})

	it("describes the overview and the write", () => {
		expect(stepLabel({ kind: "tool", tool: "topic_map" }, names)).toBe("looking over the topics…")
		expect(stepLabel({ kind: "tool", tool: "write_blocks", detail: "sharpen the claim" }, names)).toBe(
			"writing: sharpen the claim…",
		)
		expect(stepLabel({ kind: "tool", tool: "write_blocks" }, names)).toBe("writing…")
		expect(stepLabel({ kind: "wrote" }, names)).toBe("change written")
	})

	it("falls back to working for a step it does not know", () => {
		expect(stepLabel({ kind: "tool", tool: "teleport" }, names)).toBe("working…")
		expect(stepLabel({ kind: "dreaming" }, names)).toBe("working…")
	})
})
