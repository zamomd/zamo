import { beforeEach, describe, expect, it, vi } from "vitest"

const settingsFixture = vi.hoisted(() => ({
	aiEnabled: true,
	error: null as Error | null,
	calls: 0,
}))

vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		Settings: async () => {
			settingsFixture.calls++
			if (settingsFixture.error !== null) {
				throw settingsFixture.error
			}
			return {
				aiEnabled: { value: settingsFixture.aiEnabled, origin: "default", override: null },
				provider: { value: "anthropic", origin: "config", override: null },
				model: { value: "", origin: "default", override: null },
				baseUrl: { value: "", origin: "default", override: null },
				apiKey: { set: false, origin: "default" },
				declarative: false,
			}
		},
	},
}))

const baseFixture = vi.hoisted(() => ({ cleared: 0 }))
vi.mock("./base.svelte", () => ({
	base: {
		clearScope: () => {
			baseFixture.cleared++
		},
	},
}))

import { ai } from "./ai.svelte"

describe("ai policy store", () => {
	beforeEach(() => {
		settingsFixture.aiEnabled = true
		settingsFixture.error = null
		settingsFixture.calls = 0
		baseFixture.cleared = 0
		ai.set(false)
	})

	it("starts disabled until the backend supplies the policy", () => {
		expect(ai.enabled).toBe(false)
	})

	it("takes the policy from the backend on load", async () => {
		settingsFixture.aiEnabled = false
		await ai.load()
		expect(ai.enabled).toBe(false)
	})

	it("accepts the policy from a settings save", () => {
		ai.set(false)
		expect(ai.enabled).toBe(false)
		ai.set(true)
		expect(ai.enabled).toBe(true)
	})

	it("keeps the last known policy when loading fails", async () => {
		settingsFixture.error = new Error("boom")
		await ai.load()
		expect(ai.enabled).toBe(false)
	})

	it("clears the AI command target when AI goes off", () => {
		baseFixture.cleared = 0
		ai.set(false)
		expect(baseFixture.cleared).toBe(1)
	})

	it("does not clear the target when AI stays on", () => {
		baseFixture.cleared = 0
		ai.set(true)
		expect(baseFixture.cleared).toBe(0)
	})
})
