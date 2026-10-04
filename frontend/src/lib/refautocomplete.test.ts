// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Block, SearchHit } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { base } from "../stores/base.svelte"
import RefAutocomplete from "./RefAutocomplete.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
const backend = vi.hoisted(() => ({ hits: [] as unknown[] }))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => [],
		Search: async () => backend.hits,
	},
}))

function hit(id: string, content: string, snippet: string): SearchHit {
	const block: Block = {
		ID: id,
		ParentID: "root",
		Position: 1,
		Content: content,
		Meta: null,
		Mode: Mode.ModeOutline,
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
	}
	return { Block: block, Snippet: snippet }
}

const blockTrigger = { trigger: { kind: "block" as const, query: "claim", start: 0 }, target: null }

beforeEach(() => {
	base.clearEdit()
	backend.hits = []
})

describe("the block reference popup", () => {
	it("lists the block behind each search hit", async () => {
		backend.hits = [hit("b7", "a claim worth citing", "…a **claim** worth…")]
		base.editText = "((claim"

		const { container } = render(RefAutocomplete, { props: blockTrigger })

		await waitFor(() => {
			expect(container.querySelector("ul button")).not.toBeNull()
		})
		expect(container.querySelector("ul button")?.textContent?.trim()).toBe("a claim worth citing")
	})

	it("inserts the hit's own block id", async () => {
		backend.hits = [hit("b7", "a claim worth citing", "…a **claim** worth…")]
		base.editText = "((claim"

		const { container } = render(RefAutocomplete, { props: blockTrigger })

		await waitFor(() => {
			expect(container.querySelector("ul button")).not.toBeNull()
		})
		await fireEvent.click(container.querySelector("ul button") as Element)
		expect(base.editText).toBe("((b7|a claim worth citing))")
	})
})
