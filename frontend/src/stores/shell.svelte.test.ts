// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { TopicMap: async () => [], GetSubtree: async () => [] },
}))

import { readAssistantPrefs, shell } from "./shell.svelte"

const KEY = "zamo.assistant"

beforeEach(() => {
	localStorage.clear()
	shell.assistantOpen = false
	shell.assistantWidth = null
})

describe("the assistant panel's preferences", () => {
	it("reads what was stored", () => {
		expect(readAssistantPrefs('{"width":420,"open":true}')).toEqual({ width: 420, open: true })
	})

	it("comes up closed at the default width from nothing or from garbage", () => {
		const closed = { width: null, open: false }
		expect(readAssistantPrefs(null)).toEqual(closed)
		expect(readAssistantPrefs("not json")).toEqual(closed)
		expect(readAssistantPrefs('"a string"')).toEqual(closed)
		expect(readAssistantPrefs('{"width":"wide","open":"yes"}')).toEqual(closed)
	})

	it("remembers a toggle", () => {
		shell.toggleAssistant()

		expect(shell.assistantOpen).toBe(true)
		expect(readAssistantPrefs(localStorage.getItem(KEY))).toEqual({ width: null, open: true })

		shell.toggleAssistant()

		expect(readAssistantPrefs(localStorage.getItem(KEY)).open).toBe(false)
	})

	it("remembers a width", () => {
		shell.assistantWidth = 380
		shell.saveAssistant()

		expect(readAssistantPrefs(localStorage.getItem(KEY)).width).toBe(380)
	})
})
