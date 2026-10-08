// @vitest-environment jsdom
import { flushSync } from "svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import type { TreeNodeData } from "../lib/tree"
import { base } from "./base.svelte"
import { readTabs, tabs } from "./tabs.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [] },
}))

const KEY = "zamo.tabs"

function node(id: string, parent: string, children: TreeNodeData[] = []): TreeNodeData {
	return {
		block: {
			ID: id,
			ParentID: parent,
			Position: 0,
			Content: id,
			CreatedBy: Author.AuthorOwner,
			Mode: Mode.ModeOutline,
			CreatedAt: "",
			UpdatedAt: "",
			Unreviewed: false,
		},
		children,
	}
}

function topics(...ids: string[]): TreeNodeData[] {
	return ids.map((id) => node(id, "", [node(`${id}-child`, id)]))
}

function open(): string[] {
	return tabs.tabs.map((t) => t.topicID)
}

// The base as it is after its first load, showing topic a in one tab.
beforeEach(() => {
	localStorage.clear()
	tabs.load()
	base.busy = false
	base.editingID = null
	base.focusID = null
	base.forest = topics("a", "b", "c", "d")
	base.selectedTopicID = "a"
	base.loaded = true
	tabs.reconcile()
})

describe("opening a topic", () => {
	it("replaces the active tab's topic on a plain open", async () => {
		await tabs.open("b")
		tabs.reconcile()

		expect(open()).toEqual(["b"])
		expect(base.selectedTopicID).toBe("b")
	})

	it("adds a tab right of the active one when asked for a new tab", async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })
		await tabs.activate(tabs.tabs[0].id)
		await tabs.open("d", { newTab: true })

		expect(open()).toEqual(["a", "d", "b", "c"])
		expect(tabs.active?.topicID).toBe("d")
		expect(base.selectedTopicID).toBe("d")
	})

	it("activates the tab that already holds the topic, new tab or not", async () => {
		await tabs.open("b", { newTab: true })

		await tabs.open("a")
		expect(tabs.active?.topicID).toBe("a")

		await tabs.open("b", { newTab: true })
		expect(tabs.active?.topicID).toBe("b")
		expect(open()).toEqual(["a", "b"])
	})

	it("opens one topic in a new tab after nextOpensNew, then goes back to replacing", async () => {
		tabs.nextOpensNew = true

		await tabs.open("b")
		expect(open()).toEqual(["a", "b"])

		await tabs.open("c")
		tabs.reconcile()
		expect(open()).toEqual(["a", "c"])
	})

	it("does nothing while the base is busy", async () => {
		base.busy = true

		await tabs.open("b", { newTab: true })

		expect(open()).toEqual(["a"])
		expect(base.selectedTopicID).toBe("a")
	})

	it("does nothing when the open edit cannot be committed", async () => {
		const commit = vi.spyOn(base, "commitEdit").mockResolvedValue(false)

		await tabs.open("b", { newTab: true })
		commit.mockRestore()

		expect(open()).toEqual(["a"])
	})

	it("drops the new tab when its topic turns out not to exist", async () => {
		await tabs.open("gone", { newTab: true })

		expect(open()).toEqual(["a"])
		expect(tabs.active?.topicID).toBe("a")
	})
})

describe("switching tabs", () => {
	it("brings a tab back focused where it was left", async () => {
		await base.focus("a-child")
		tabs.reconcile()
		await tabs.open("b", { newTab: true })
		expect(base.focusID).toBeNull()

		await tabs.activate(tabs.tabs[0].id)

		expect(base.selectedTopicID).toBe("a")
		expect(base.focusID).toBe("a-child")
	})

	it("comes back unfocused when the focused block is gone", async () => {
		await base.focus("a-child")
		tabs.reconcile()
		await tabs.open("b", { newTab: true })
		base.forest[0].children = []

		await tabs.activate(tabs.tabs[0].id)

		expect(base.focusID).toBeNull()
		expect(tabs.tabs[0].focusID).toBeNull()
	})

	it("steps forward and back, wrapping at the ends", async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })

		await tabs.next()
		expect(tabs.active?.topicID).toBe("a")

		await tabs.previous()
		expect(tabs.active?.topicID).toBe("c")
	})

	it("activates by position, the ninth being the last", async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })

		await tabs.at(0)
		expect(tabs.active?.topicID).toBe("a")

		await tabs.at(8)
		expect(tabs.active?.topicID).toBe("c")

		await tabs.at(5)
		expect(tabs.active?.topicID).toBe("c")
	})
})

describe("closing a tab", () => {
	beforeEach(async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })
	})

	it("activates the right neighbour", async () => {
		await tabs.activate(tabs.tabs[1].id)

		await tabs.close(tabs.tabs[1].id)

		expect(open()).toEqual(["a", "c"])
		expect(base.selectedTopicID).toBe("c")
	})

	it("activates the left neighbour when the last tab closes", async () => {
		await tabs.close(tabs.tabs[2].id)

		expect(open()).toEqual(["a", "b"])
		expect(tabs.active?.topicID).toBe("b")
		expect(base.selectedTopicID).toBe("b")
	})

	it("leaves the active tab alone when another one closes", async () => {
		await tabs.close(tabs.tabs[0].id)

		expect(open()).toEqual(["b", "c"])
		expect(base.selectedTopicID).toBe("c")
	})

	it("keeps the only tab", async () => {
		await tabs.close(tabs.tabs[0].id)
		await tabs.close(tabs.tabs[0].id)

		await tabs.close(tabs.tabs[0].id)

		expect(open()).toEqual(["c"])
	})

	it("keeps the active tab while the base is busy", async () => {
		base.busy = true

		await tabs.close(tabs.tabs[2].id)

		expect(open()).toEqual(["a", "b", "c"])
	})
})

describe("reconciling with the base", () => {
	it("drops the tab of a deleted topic and follows the base's fallback", async () => {
		await tabs.open("b", { newTab: true })
		await tabs.open("c", { newTab: true })
		base.forest = topics("a", "b", "d")
		base.selectedTopicID = "a"

		tabs.reconcile()

		expect(open()).toEqual(["a", "b"])
		expect(tabs.active?.topicID).toBe("a")
	})

	it("hands the only tab to whatever the base fell back to", () => {
		base.forest = topics("b", "c")
		base.selectedTopicID = "b"

		tabs.reconcile()

		expect(open()).toEqual(["b"])
		expect(tabs.active?.topicID).toBe("b")
	})

	it("activates the tab holding a topic the base selected on its own", async () => {
		await tabs.open("b", { newTab: true })

		base.reveal("a-child")
		tabs.reconcile()

		expect(open()).toEqual(["a", "b"])
		expect(tabs.active?.topicID).toBe("a")
	})

	it("takes a topic no tab holds into the active tab", async () => {
		await tabs.open("b", { newTab: true })
		tabs.setScroll(300)

		base.reveal("c-child")
		tabs.reconcile()

		expect(open()).toEqual(["a", "c"])
		expect(tabs.active?.scrollTop).toBe(0)
	})

	it("mirrors the base's focus into the active tab", async () => {
		await base.focus("a-child")
		tabs.reconcile()

		expect(tabs.active?.focusID).toBe("a-child")
	})

	it("has no tabs when the base has no topic", () => {
		base.forest = []
		base.selectedTopicID = null

		tabs.reconcile()

		expect(tabs.tabs).toEqual([])
		expect(tabs.active).toBeNull()
	})

	it("leaves stored tabs alone until the base has loaded", () => {
		localStorage.setItem(KEY, JSON.stringify({ tabs: [{ topicID: "b" }, { topicID: "c" }], active: 1 }))
		tabs.load()
		base.loaded = false
		base.forest = []
		base.selectedTopicID = null

		tabs.reconcile()

		expect(open()).toEqual(["b", "c"])
	})
})

describe("stored tabs", () => {
	it("come back with the stored tab active, over the base's own first pick", async () => {
		localStorage.setItem(
			KEY,
			JSON.stringify({
				tabs: [
					{ topicID: "b", focusID: null, scrollTop: 0 },
					{ topicID: "c", focusID: "c-child", scrollTop: 120 },
				],
				active: 1,
			}),
		)
		tabs.load()

		tabs.reconcile()
		await vi.waitFor(() => expect(base.focusID).toBe("c-child"))

		expect(open()).toEqual(["b", "c"])
		expect(base.selectedTopicID).toBe("c")
		expect(tabs.active?.scrollTop).toBe(120)
	})

	it("lose the topics that no longer exist", () => {
		localStorage.setItem(KEY, JSON.stringify({ tabs: [{ topicID: "gone" }, { topicID: "a" }], active: 0 }))
		tabs.load()

		tabs.reconcile()

		expect(open()).toEqual(["a"])
		expect(tabs.active?.topicID).toBe("a")
	})

	it("round-trip through storage", async () => {
		await tabs.open("b", { newTab: true })
		tabs.setScroll(80)
		tabs.save()

		expect(readTabs(localStorage.getItem(KEY))).toEqual({
			tabs: [
				{ topicID: "a", focusID: null, scrollTop: 0 },
				{ topicID: "b", focusID: null, scrollTop: 80 },
			],
			active: 1,
		})
	})

	it("read as no tabs from nothing or from garbage", () => {
		const none = { tabs: [], active: 0 }
		expect(readTabs(null)).toEqual(none)
		expect(readTabs("not json")).toEqual(none)
		expect(readTabs("[]")).toEqual(none)
		expect(readTabs('{"tabs":"a"}')).toEqual(none)
	})

	it("skip entries that are not tabs and repeated topics", () => {
		const stored = JSON.stringify({
			tabs: [null, { topicID: 7 }, { topicID: "a", focusID: 3, scrollTop: "x" }, { topicID: "a" }, { topicID: "b" }],
			active: 4,
		})

		expect(readTabs(stored)).toEqual({
			tabs: [
				{ topicID: "a", focusID: null, scrollTop: 0 },
				{ topicID: "b", focusID: null, scrollTop: 0 },
			],
			active: 1,
		})
	})

	it("fall back to the first tab when the active position names none", () => {
		expect(readTabs('{"tabs":[{"topicID":"a"}],"active":9}').active).toBe(0)
	})

	it("survive storage that throws", () => {
		const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new Error("full")
		})

		expect(() => tabs.save()).not.toThrow()
		set.mockRestore()
	})
})

describe("the running store", () => {
	it("reconciles and stores on its own once started", async () => {
		const stop = tabs.start()
		await tabs.open("b", { newTab: true })

		base.reveal("c-child")
		flushSync()

		expect(open()).toEqual(["a", "c"])
		expect(readTabs(localStorage.getItem(KEY)).tabs.map((t) => t.topicID)).toEqual(["a", "c"])
		stop()
	})
})
