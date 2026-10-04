// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Block } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { base } from "../stores/base.svelte"
import TreeNode from "./TreeNode.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
const backend = vi.hoisted(() => ({
	subtree: [] as unknown[],
	writeGate: null as Promise<void> | null,
	written: [] as string[],
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => [],
		GetSubtree: async () => backend.subtree,
		Backlinks: async () => [],
		EditBlockContent: async (id: string) => {
			if (backend.writeGate !== null) {
				await backend.writeGate
			}
			backend.written.push(id)
		},
	},
}))

function block(overrides: Partial<Block> = {}): Block {
	return {
		ID: "b1",
		ParentID: "root",
		Position: 1,
		Content: "a claim",
		Meta: null,
		Mode: Mode.ModeOutline,
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
		...overrides,
	}
}

beforeEach(() => {
	base.clearEdit()
	base.forest = []
	backend.subtree = []
	backend.writeGate = null
	backend.written = []
})

describe("TreeNode", () => {
	it("renders the block's markdown", () => {
		const { container } = render(TreeNode, {
			node: { block: block({ Content: "a **bold** claim" }), children: [] },
		})
		const content = container.querySelector(".content")
		expect(content?.textContent?.trim()).toBe("a bold claim")
		expect(content?.querySelector("strong")).not.toBeNull()
	})

	it("keeps the same lanes beside the text in both states", async () => {
		const { container } = render(TreeNode, {
			node: { block: block({ Content: "a claim long enough to wrap" }), children: [] },
		})
		const rendered = container.querySelectorAll(".gutter").length
		await fireEvent.click(container.querySelector(".content") as Element)
		await waitFor(() => {
			expect(container.querySelector("textarea")).not.toBeNull()
		})
		expect(container.querySelectorAll(".gutter").length).toBe(rendered)
	})

	it("swaps the rendered row for a textarea on click", async () => {
		const { container } = render(TreeNode, {
			node: { block: block({ Content: "a claim" }), children: [] },
		})
		const content = container.querySelector(".content")
		expect(content).not.toBeNull()
		await fireEvent.click(content as Element)
		await waitFor(() => {
			expect(container.querySelector("textarea")).not.toBeNull()
		})
		expect(container.querySelector(".content")).toBeNull()
	})

	it("carries the clicked offset into the next block across an async commit", async () => {
		const root = block({ ID: "root", ParentID: "", Content: "Topic" })
		const first = block({ ID: "b1", Content: "first block" })
		const second = block({ ID: "b2", Position: 2, Content: "second block, wrapping over lines" })
		backend.subtree = [root, first, second]
		backend.written = []
		base.forest = [
			{
				block: root,
				children: [
					{ block: first, children: [] },
					{ block: second, children: [] },
				],
			},
		]
		base.selectedTopicID = "root"

		await base.startEdit(first, 0)
		base.editText = "first block, edited"

		const { container } = render(TreeNode, { node: { block: second, children: [] } })
		const content = container.querySelector(".content") as HTMLElement
		const text = content.firstChild?.firstChild as Node
		let release: () => void = () => {}
		backend.writeGate = new Promise<void>((resolve) => {
			release = resolve
		})
		stubCaretFromPoint(text, 7)

		await fireEvent.click(content)
		expect(base.editingID).toBe("b1")

		release()
		await waitFor(() => {
			expect(base.editingID).toBe("b2")
		})
		expect(backend.written).toEqual(["b1"])
		expect(base.editCaret).toBe(7)
		backend.writeGate = null
	})
})

// jsdom has no caretRangeFromPoint, so click handling needs a rendered offset stub.
function stubCaretFromPoint(node: Node, offset: number): void {
	const stub = (): Range => {
		const range = document.createRange()
		range.setStart(node, offset)
		range.collapse(true)
		return range
	}
	;(document as Document & { caretRangeFromPoint?: () => Range }).caretRangeFromPoint = stub
}
