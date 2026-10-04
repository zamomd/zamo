// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
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

beforeEach(() => {
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
