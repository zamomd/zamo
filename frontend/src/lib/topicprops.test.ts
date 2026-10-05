// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { base } from "../stores/base.svelte"
import TopicProps from "./TopicProps.svelte"
import { hasTag, splitNames, tagCounts, tagKey, unreferenceable } from "./topicprops"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
const backend = vi.hoisted(() => ({
	props: { Aliases: [] as string[], Tags: [] as string[] },
	set: [] as unknown[],
	topics: [] as { ID: string }[],
	setError: null as unknown,
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => backend.topics,
		TopicAliases: async () => [],
		TopicTags: async () => [],
		GetSubtree: async (id: string) => backend.topics.filter((b) => b.ID === id),
		Unreviewed: async () => ({ Blocks: [] }),
		TopicProps: async () => backend.props,
		SetTopicProps: async (id: string, aliases: string[], tags: string[]) => {
			if (backend.setError !== null) {
				throw backend.setError
			}
			backend.set.push({ id, aliases, tags })
			return { Aliases: aliases, Tags: tags }
		},
	},
}))

function topic(id: string, content: string) {
	return {
		block: {
			ID: id,
			ParentID: "",
			Position: 0,
			Content: content,
			CreatedBy: Author.AuthorOwner,
			Mode: Mode.ModeOutline,
			CreatedAt: "",
			UpdatedAt: "",
			Unreviewed: false,
		},
		children: [],
	}
}

beforeEach(() => {
	backend.props = { Aliases: [], Tags: [] }
	backend.set = []
	backend.setError = null
	base.forest = [topic("go", "Go"), topic("lang", "# Languages")]
	base.tagFilter = null
	backend.topics = base.forest.map((t) => t.block)
})

describe("splitNames", () => {
	it("trims names and drops the empty pieces", () => {
		expect(splitNames(" Golang , ,Go lang,")).toEqual(["Golang", "Go lang"])
	})
})

describe("unreferenceable", () => {
	it("is a name carrying a bracket", () => {
		expect(unreferenceable("a [b]")).toBe(true)
		expect(unreferenceable("plain name")).toBe(false)
	})
})

describe("tagKey", () => {
	it("folds the spellings of one tag", () => {
		expect(tagKey("  #To   Read ")).toBe("to read")
	})
})

describe("tagCounts", () => {
	it("counts the topics under each tag, ordered by name", () => {
		const tags = new Map([
			["go", ["Languages", "Draft"]],
			["zig", ["draft"]],
		])
		expect(tagCounts(tags)).toEqual([
			{ key: "draft", name: "Draft", topics: 2 },
			{ key: "languages", name: "Languages", topics: 1 },
		])
		expect(hasTag(tags, "zig", "draft")).toBe(true)
		expect(hasTag(tags, "zig", "languages")).toBe(false)
		expect(hasTag(tags, "unknown", "draft")).toBe(false)
	})
})

describe("TopicProps", () => {
	it("shows the aliases and the tags, and a tag narrows the sidebar to itself", async () => {
		backend.props = { Aliases: ["Golang"], Tags: ["Languages", "To Read"] }
		const { container } = render(TopicProps, { id: "go" })
		await waitFor(() => expect(container.textContent).toContain("Golang"))
		const tags = [...container.querySelectorAll(".tag")]
		expect(tags.map((t) => t.textContent?.trim())).toEqual(["#Languages", "#To Read"])

		await fireEvent.click(tags[1])
		expect(base.tagFilter).toBe("to read")
		await fireEvent.click(tags[1])
		expect(base.tagFilter).toBeNull()
	})

	it("offers to add properties to a topic that has none", async () => {
		const { container } = render(TopicProps, { id: "go" })
		await waitFor(() => expect(container.querySelector(".edit")?.textContent?.trim()).toBe("+ aliases, tags"))
	})

	it("saves what was typed as lists of names", async () => {
		backend.props = { Aliases: ["Golang"], Tags: ["Languages"] }
		const { container } = render(TopicProps, { id: "go" })
		await waitFor(() => expect(container.textContent).toContain("Golang"))
		await fireEvent.click(container.querySelector(".edit") as HTMLElement)
		const [aliases, tags] = [...container.querySelectorAll("input")]
		expect(aliases.value).toBe("Golang")
		expect(tags.value).toBe("Languages")
		await fireEvent.input(aliases, { target: { value: "Golang, Go lang" } })
		await fireEvent.submit(container.querySelector("form") as HTMLElement)
		await waitFor(() => expect(container.querySelector("form")).toBeNull())
		expect(backend.set).toEqual([{ id: "go", aliases: ["Golang", "Go lang"], tags: ["Languages"] }])
	})

	it("refuses an alias with a bracket without asking the backend, but not a tag", async () => {
		const { container } = render(TopicProps, { id: "go" })
		await waitFor(() => expect(container.querySelector(".edit")).not.toBeNull())
		await fireEvent.click(container.querySelector(".edit") as HTMLElement)
		const [aliases, tags] = [...container.querySelectorAll("input")]
		await fireEvent.input(aliases, { target: { value: "a [b]" } })
		await fireEvent.submit(container.querySelector("form") as HTMLElement)
		expect(container.querySelector(".error")?.textContent).toContain("a [b]")
		expect(backend.set).toEqual([])

		await fireEvent.input(aliases, { target: { value: "" } })
		await fireEvent.input(tags, { target: { value: "v[1]" } })
		await fireEvent.submit(container.querySelector("form") as HTMLElement)
		await waitFor(() => expect(container.querySelector("form")).toBeNull())
		expect(backend.set).toEqual([{ id: "go", aliases: [], tags: ["v[1]"] }])
	})

	it("keeps the form open and says why when the backend refuses", async () => {
		backend.setError = Object.assign(new Error("taken"), { cause: { code: "duplicate_topic" } })
		const { container } = render(TopicProps, { id: "go" })
		await waitFor(() => expect(container.querySelector(".edit")).not.toBeNull())
		await fireEvent.click(container.querySelector(".edit") as HTMLElement)
		await fireEvent.input(container.querySelector("input") as HTMLElement, { target: { value: "Languages" } })
		await fireEvent.submit(container.querySelector("form") as HTMLElement)
		await waitFor(() => expect(container.querySelector(".error")).not.toBeNull())
		expect(container.querySelector("form")).not.toBeNull()
	})
})
