// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { base } from "../stores/base.svelte"
import { tabs } from "../stores/tabs.svelte"
import TabStrip from "./TabStrip.svelte"
import type { TreeNodeData } from "./tree"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [] },
}))

function topic(id: string, content: string, unreviewed = false): TreeNodeData {
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
			Unreviewed: unreviewed,
		},
		children: [],
	}
}

function tabTitles(container: HTMLElement): (string | undefined)[] {
	return [...container.querySelectorAll('[role="tab"]')].map((t) => t.textContent?.trim())
}

function selected(container: HTMLElement): string | undefined {
	return container.querySelector('[role="tab"][aria-selected="true"]')?.textContent?.trim()
}

beforeEach(async () => {
	localStorage.clear()
	tabs.load()
	base.busy = false
	base.editingID = null
	base.focusID = null
	base.forest = [topic("a", "Alpha"), topic("b", "**Beta**", true), topic("c", "Gamma")]
	base.selectedTopicID = "a"
	base.loaded = true
	tabs.reconcile()
})

describe("the tab strip", () => {
	it("is absent with one tab", () => {
		const { container } = render(TabStrip)

		expect(container.querySelector('[role="tablist"]')).toBeNull()
	})

	it("shows each open topic by its plain title, the active one selected", async () => {
		await tabs.open("b", { newTab: true })
		const { container } = render(TabStrip)

		expect(tabTitles(container)).toEqual(["Alpha", "Beta"])
		expect(selected(container)).toBe("Beta")
	})

	it("marks a topic that has unreviewed changes", async () => {
		await tabs.open("b", { newTab: true })
		const { container } = render(TabStrip)
		const [alpha, beta] = container.querySelectorAll('[role="tab"]')

		expect(alpha.querySelector(".dot")).toBeNull()
		expect(beta.querySelector(".dot")).not.toBeNull()
	})

	it("switches on a click", async () => {
		await tabs.open("b", { newTab: true })
		const { container, getByRole } = render(TabStrip)

		await fireEvent.click(getByRole("tab", { name: "Alpha" }))
		await vi.waitFor(() => expect(selected(container)).toBe("Alpha"))

		expect(base.selectedTopicID).toBe("a")
	})

	it("closes a tab from its button and on a middle click", async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })
		const { container, getByRole } = render(TabStrip)

		await fireEvent.click(getByRole("button", { name: "close Alpha" }))
		await vi.waitFor(() => expect(tabTitles(container)).toEqual(["Beta", "Gamma"]))

		await fireEvent(getByRole("tab", { name: /Beta/ }), new MouseEvent("auxclick", { bubbles: true, button: 1 }))
		await vi.waitFor(() => expect(container.querySelector('[role="tablist"]')).toBeNull())
	})

	it("moves along the tabs with the arrow keys, wrapping", async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })
		const { container, getByRole } = render(TabStrip)

		await fireEvent.keyDown(getByRole("tab", { name: "Gamma" }), { key: "ArrowRight" })
		await vi.waitFor(() => expect(selected(container)).toBe("Alpha"))

		await fireEvent.keyDown(getByRole("tab", { name: "Alpha" }), { key: "ArrowLeft" })
		await vi.waitFor(() => expect(selected(container)).toBe("Gamma"))
		expect(document.activeElement?.textContent?.trim()).toBe("Gamma")
	})

	it("is inert while the base is busy", async () => {
		await tabs.open("b", { newTab: true })
		base.busy = true
		const { container } = render(TabStrip)

		for (const button of container.querySelectorAll("button")) {
			expect(button.disabled).toBe(true)
		}
	})
})
