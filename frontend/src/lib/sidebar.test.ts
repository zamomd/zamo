// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { base } from "../stores/base.svelte"
import { shell } from "../stores/shell.svelte"
import Sidebar from "./Sidebar.svelte"
import { SIDEBAR_MIN } from "./sidebarsize"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [] },
}))

const RAIL = 48

// jsdom has no layout: the sidebar's width is whatever the store last asked
// for, and the handle sits at its right edge.
function mount(startWidth: number) {
	const { container } = render(Sidebar)
	const aside = container.querySelector("aside") as HTMLElement
	const handle = container.querySelector(".resizer") as HTMLElement
	const width = () => (shell.sidebarHidden ? 0 : (shell.sidebarWidth ?? startWidth))
	Object.defineProperty(aside, "offsetWidth", { get: width })
	handle.getBoundingClientRect = () => ({ left: RAIL + width() }) as DOMRect
	handle.setPointerCapture = () => {}
	return { aside, handle }
}

async function drag(handle: HTMLElement, from: number, to: number): Promise<void> {
	await fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: from })
	await fireEvent.pointerMove(handle, { pointerId: 1, clientX: to })
	await fireEvent.pointerUp(handle, { pointerId: 1, clientX: to })
}

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

function topicTitles(container: HTMLElement): (string | undefined)[] {
	return [...container.querySelectorAll(".row.topic")].map((r) => r.textContent?.replace(/^[▤☰]\s*/u, "").trim())
}

beforeEach(() => {
	base.forest = []
	base.tags = new Map()
	base.tagFilter = null
	localStorage.clear()
	shell.sidebarWidth = null
	shell.sidebarHidden = false
	window.innerWidth = 1400
})

describe("Sidebar resizer", () => {
	it("sets the width the edge is dragged to and remembers it", async () => {
		const { aside, handle } = mount(300)
		await drag(handle, RAIL + 300, RAIL + 420)
		expect(aside.style.width).toBe("420px")
		expect(aside.hidden).toBe(false)
		expect(JSON.parse(localStorage.getItem("zamo.sidebar") ?? "")).toEqual({ width: 420, hidden: false })
	})

	it("hides when dragged to the rail and keeps the width it had", async () => {
		shell.sidebarWidth = 300
		const { aside, handle } = mount(300)
		await drag(handle, RAIL + 300, RAIL + 10)
		expect(aside.hidden).toBe(true)
		expect(shell.sidebarWidth).toBe(300)
		expect(JSON.parse(localStorage.getItem("zamo.sidebar") ?? "")).toEqual({ width: 300, hidden: true })
	})

	it("comes back when dragged out from the rail", async () => {
		shell.sidebarHidden = true
		const { aside, handle } = mount(300)
		await drag(handle, RAIL, RAIL + 250)
		expect(aside.hidden).toBe(false)
		expect(aside.style.width).toBe("250px")
	})

	it("ignores moves without a pressed pointer", async () => {
		const { aside, handle } = mount(300)
		await fireEvent.pointerMove(handle, { pointerId: 1, clientX: RAIL + 500 })
		expect(aside.style.width).toBe("")
	})

	it("toggles on Enter and steps with the arrow keys", async () => {
		shell.sidebarWidth = 300
		const { aside, handle } = mount(300)
		await fireEvent.keyDown(handle, { key: "ArrowRight" })
		expect(aside.style.width).toBe("316px")
		await fireEvent.keyDown(handle, { key: "Enter" })
		expect(aside.hidden).toBe(true)
		await fireEvent.keyDown(handle, { key: "ArrowRight" })
		expect(aside.hidden).toBe(false)
		expect(aside.style.width).toBe(`${SIDEBAR_MIN}px`)
	})
})

describe("Sidebar tag filter", () => {
	function seed(): void {
		base.forest = [topic("go", "Go"), topic("zig", "Zig"), topic("sql", "SQLite")]
		base.tags = new Map([
			["go", ["Languages", "Draft"]],
			["zig", ["languages"]],
		])
	}

	it("lists each tag once and narrows the topics to the one chosen", async () => {
		seed()
		const { container } = render(Sidebar)
		const chips = [...container.querySelectorAll(".tags .tag")] as HTMLElement[]
		expect(chips.map((c) => c.textContent?.trim())).toEqual(["#Draft", "#Languages"])
		expect(topicTitles(container)).toEqual(["Go", "Zig", "SQLite"])

		await fireEvent.click(chips[1])
		expect(topicTitles(container)).toEqual(["Go", "Zig"])
		await fireEvent.click(chips[1])
		expect(topicTitles(container)).toEqual(["Go", "Zig", "SQLite"])
	})

	it("shows every topic again once the filtered tag is gone from the base", async () => {
		seed()
		base.tagFilter = "draft"
		const { container } = render(Sidebar)
		expect(topicTitles(container)).toEqual(["Go"])

		base.tags = new Map([["zig", ["languages"]]])
		await Promise.resolve()
		expect(topicTitles(container)).toEqual(["Go", "Zig", "SQLite"])
	})
})
