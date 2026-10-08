import { beforeEach, describe, expect, it, vi } from "vitest"

const calls = vi.hoisted(() => ({
	mode: "",
	revealedID: "",
	selectedID: "",
	commitResult: true,
	committed: false,
	busy: false,
	topics: {} as Record<string, string>,
	opened: [] as { id: string; newTab: boolean | undefined }[],
}))

vi.mock("../stores/shell.svelte", () => ({
	shell: {
		setMode: async (m: string) => {
			calls.mode = m
		},
	},
}))

vi.mock("../stores/base.svelte", () => ({
	base: {
		get busy() {
			return calls.busy
		},
		commitEdit: async () => {
			calls.committed = true
			return calls.commitResult
		},
		reveal: (id: string) => {
			calls.revealedID = id
		},
		get selectedTopicID() {
			return calls.selectedID
		},
		forest: [],
	},
}))

vi.mock("../stores/tabs.svelte", () => ({
	tabs: {
		open: async (id: string, opts: { newTab?: boolean } = {}) => {
			calls.opened.push({ id, newTab: opts.newTab })
			calls.selectedID = id
		},
	},
}))

vi.mock("./tree", () => ({
	topicOf: (_forest: unknown, id: string) => calls.topics[id] ?? null,
}))

import { jumpToBlock, newTabGesture, openTopic } from "./navigate"

beforeEach(() => {
	calls.mode = ""
	calls.revealedID = ""
	calls.selectedID = ""
	calls.commitResult = true
	calls.committed = false
	calls.busy = false
	calls.topics = { b1: "t1", b2: "t2" }
	calls.opened = []
})

describe("jumpToBlock", () => {
	it("switches to tree mode and reveals the block", async () => {
		await jumpToBlock("b1")

		expect(calls.mode).toBe("tree")
		expect(calls.revealedID).toBe("b1")
	})

	it("does not reveal when the commit it re-checks after the mode switch failed", async () => {
		calls.commitResult = false

		await jumpToBlock("b1")

		expect(calls.mode).toBe("tree")
		expect(calls.revealedID).toBe("")
	})

	it("does not commit or jump while a write is in flight", async () => {
		calls.busy = true

		await jumpToBlock("b1")

		expect(calls.committed).toBe(false)
		expect(calls.revealedID).toBe("")
		expect(calls.mode).toBe("")
	})
})

describe("jumpToBlock across topics", () => {
	it("opens the block's topic before revealing it", async () => {
		calls.selectedID = "t1"

		await jumpToBlock("b2")

		expect(calls.opened).toEqual([{ id: "t2", newTab: undefined }])
		expect(calls.revealedID).toBe("b2")
	})

	it("opens the block's topic in a new tab when asked to", async () => {
		calls.selectedID = "t1"

		await jumpToBlock("b2", { newTab: true })

		expect(calls.opened).toEqual([{ id: "t2", newTab: true }])
		expect(calls.revealedID).toBe("b2")
	})

	it("stays in the tab for a block of the topic on screen", async () => {
		calls.selectedID = "t1"

		await jumpToBlock("b1", { newTab: true })

		expect(calls.opened).toEqual([])
		expect(calls.revealedID).toBe("b1")
	})

	it("only reveals a block the forest does not hold yet", async () => {
		await jumpToBlock("unknown", { newTab: true })

		expect(calls.opened).toEqual([])
		expect(calls.revealedID).toBe("unknown")
	})
})

describe("openTopic", () => {
	it("switches to tree mode and opens the topic", () => {
		openTopic("t1")

		expect(calls.mode).toBe("tree")
		expect(calls.opened).toEqual([{ id: "t1", newTab: undefined }])
	})

	it("passes a new-tab request on", () => {
		openTopic("t1", { newTab: true })

		expect(calls.opened).toEqual([{ id: "t1", newTab: true }])
	})
})

describe("newTabGesture", () => {
	function click(init: Partial<MouseEvent> = {}): MouseEvent {
		return { metaKey: false, ctrlKey: false, shiftKey: false, button: 0, ...init } as MouseEvent
	}

	it("is a click with ⌘ or Ctrl held, or with the middle button", () => {
		expect(newTabGesture(click())).toBe(false)
		expect(newTabGesture(click({ shiftKey: true }))).toBe(false)
		expect(newTabGesture(click({ metaKey: true }))).toBe(true)
		expect(newTabGesture(click({ ctrlKey: true }))).toBe(true)
		expect(newTabGesture(click({ button: 1 }))).toBe(true)
	})
})
