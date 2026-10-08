// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [] },
}))

import { shell } from "../stores/shell.svelte"
import { tabs } from "../stores/tabs.svelte"
import { closeActiveTab, handleTabKey } from "./tabkeys"

function press(code: string, init: Partial<KeyboardEvent> = {}): KeyboardEvent {
	return { code, metaKey: true, ctrlKey: false, altKey: false, shiftKey: false, ...init } as KeyboardEvent
}

function twoTabs(): void {
	tabs.tabs = [
		{ id: "one", topicID: "a", focusID: null, scrollTop: 0 },
		{ id: "two", topicID: "b", focusID: null, scrollTop: 0 },
	]
	tabs.activeID = "two"
}

beforeEach(() => {
	vi.restoreAllMocks()
	localStorage.clear()
	tabs.load()
	shell.mode = "tree"
	shell.sidebarHidden = false
})

describe("tab shortcuts", () => {
	it("⌘T sends the next opened topic to a new tab and focuses the search, showing a hidden sidebar", () => {
		shell.sidebarHidden = true
		const before = shell.searchFocusNonce

		expect(handleTabKey(press("KeyT"))).toBe(true)

		expect(tabs.nextOpensNew).toBe(true)
		expect(shell.sidebarHidden).toBe(false)
		expect(shell.searchFocusNonce).toBe(before + 1)
	})

	it("⌘⇧] and ⌘⇧[ step through the tabs", () => {
		const next = vi.spyOn(tabs, "next").mockResolvedValue()
		const previous = vi.spyOn(tabs, "previous").mockResolvedValue()

		expect(handleTabKey(press("BracketRight", { shiftKey: true }))).toBe(true)
		expect(handleTabKey(press("BracketLeft", { shiftKey: true }))).toBe(true)

		expect(next).toHaveBeenCalledOnce()
		expect(previous).toHaveBeenCalledOnce()
	})

	it("⌘1 to ⌘9 pick a tab by position", () => {
		const at = vi.spyOn(tabs, "at").mockResolvedValue()

		expect(handleTabKey(press("Digit1"))).toBe(true)
		expect(handleTabKey(press("Digit9"))).toBe(true)

		expect(at.mock.calls).toEqual([[0], [8]])
	})

	it("Ctrl stands in for ⌘", () => {
		expect(handleTabKey(press("KeyT", { metaKey: false, ctrlKey: true }))).toBe(true)
	})

	it("leaves other keys and other modifiers alone", () => {
		const at = vi.spyOn(tabs, "at").mockResolvedValue()

		expect(handleTabKey(press("KeyT", { metaKey: false }))).toBe(false)
		expect(handleTabKey(press("KeyT", { altKey: true }))).toBe(false)
		expect(handleTabKey(press("KeyT", { shiftKey: true }))).toBe(false)
		expect(handleTabKey(press("BracketRight"))).toBe(false)
		expect(handleTabKey(press("Digit0"))).toBe(false)
		expect(handleTabKey(press("KeyK"))).toBe(false)

		expect(at).not.toHaveBeenCalled()
		expect(tabs.nextOpensNew).toBe(false)
	})

	it("does nothing away from the tree screen", () => {
		shell.mode = "graph"

		expect(handleTabKey(press("KeyT"))).toBe(false)
		expect(tabs.nextOpensNew).toBe(false)
	})
})

describe("the close key", () => {
	it("closes the active tab", () => {
		const close = vi.spyOn(tabs, "close").mockResolvedValue()
		twoTabs()

		closeActiveTab()

		expect(close).toHaveBeenCalledWith("two")
	})

	it("leaves the only tab open", async () => {
		tabs.tabs = [{ id: "one", topicID: "a", focusID: null, scrollTop: 0 }]
		tabs.activeID = "one"

		closeActiveTab()
		await Promise.resolve()

		expect(tabs.tabs).toHaveLength(1)
	})

	it("does nothing away from the tree screen", () => {
		const close = vi.spyOn(tabs, "close").mockResolvedValue()
		twoTabs()
		shell.mode = "changes"

		closeActiveTab()

		expect(close).not.toHaveBeenCalled()
	})
})
