// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Block, SearchHit } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { base } from "../stores/base.svelte"
import RefAutocomplete from "./RefAutocomplete.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
const backend = vi.hoisted(() => ({ hits: [] as unknown[], topics: [] as unknown[] }))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => backend.topics,
		Search: async () => backend.hits,
	},
}))

function hit(id: string, content: string, snippet: string): SearchHit {
	const block: Block = {
		ID: id,
		ParentID: "root",
		Position: 1,
		Content: content,
		CreatedBy: Author.AuthorOwner,
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
	backend.topics = []
	base.aliases = new Map()
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

describe("the topic reference popup", () => {
	const topicTrigger = { trigger: { kind: "topic" as const, query: "gola", start: 0 }, target: null }

	it("offers a topic through its alias, and not a new topic of that name", async () => {
		backend.topics = [hit("go", "Go", "").Block].map((b) => ({ ...b, ParentID: "" }))
		base.aliases = new Map([["golang", { topicID: "go", alias: "Golang" }]])
		base.editText = "[[gola"

		const { container } = render(RefAutocomplete, { props: topicTrigger })

		await waitFor(() => {
			expect(container.querySelector("ul button")).not.toBeNull()
		})
		const labels = [...container.querySelectorAll("ul button")].map((b) => b.textContent?.trim())
		expect(labels[0]).toBe("Golang")
		expect(labels).toHaveLength(2)

		const exact = render(RefAutocomplete, {
			props: { trigger: { kind: "topic" as const, query: "golang", start: 0 }, target: null },
		})
		await waitFor(() => {
			expect(exact.container.querySelector("ul button")).not.toBeNull()
		})
		expect(exact.container.querySelectorAll("ul button")).toHaveLength(1)
	})
})
