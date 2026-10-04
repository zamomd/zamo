import { describe, expect, it } from "vitest"
import type { Plan, Warning } from "../../bindings/github.com/zamomd/zamo/internal/modules/vaultimport/models"
import { canApply, describeKind, groupByKind, summaryLine } from "./vaultimport"

const plan = (over: Partial<Plan> = {}): Plan => ({
	token: "t",
	dir: "/vault",
	format: "Obsidian vault",
	pages: 3,
	blocks: 12,
	links: 4,
	conflicts: null,
	warnings: null,
	...over,
})

describe("groupByKind", () => {
	it("groups warnings by kind in first-seen order with counts", () => {
		const warnings: Warning[] = [
			{ page: "A", kind: "alias", detail: "[[X|y]]" },
			{ page: "B", kind: "broken-link", detail: "[[Nowhere]]" },
			{ page: "C", kind: "alias", detail: "[[Z|w]]" },
		]
		expect(groupByKind(warnings)).toEqual([
			{ kind: "alias", count: 2, items: [warnings[0], warnings[2]] },
			{ kind: "broken-link", count: 1, items: [warnings[1]] },
		])
	})

	it("treats a null list as empty", () => {
		expect(groupByKind(null)).toEqual([])
	})
})

describe("canApply", () => {
	it("needs a plan with at least one page", () => {
		expect(canApply(null)).toBe(false)
		expect(canApply(plan({ pages: 0 }))).toBe(false)
		expect(canApply(plan())).toBe(true)
	})
})

describe("summaryLine", () => {
	it("names the format, the directory and the counts", () => {
		expect(summaryLine(plan())).toBe("Obsidian vault at /vault: 3 pages, 12 blocks, 4 links")
	})

	it("uses singular forms", () => {
		expect(summaryLine(plan({ pages: 1, blocks: 1, links: 1 }))).toBe(
			"Obsidian vault at /vault: 1 page, 1 block, 1 link",
		)
	})
})

describe("describeKind", () => {
	it("explains every kind the backend emits and falls back to the kind itself", () => {
		expect(describeKind("alias")).toBe("Link aliases dropped")
		expect(describeKind("broken-link")).toBe("Links to pages that do not exist")
		expect(describeKind("path")).toBe("Folder paths dropped from links")
		expect(describeKind("exists")).toBe("A topic of this name is already in the base")
		expect(describeKind("something-new")).toBe("something-new")
	})
})
