// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { In, View } from "../bindings/github.com/zamomd/zamo/internal/settings"
import SettingsView from "./SettingsView.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))

const backend = vi.hoisted(() => ({ view: null as unknown, saved: [] as unknown[] }))
vi.mock("../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		Settings: async () => backend.view,
		SaveSettings: async (input: unknown) => {
			backend.saved.push(input)
		},
		CheckSettings: async () => {},
	},
}))
vi.mock("../bindings/github.com/zamomd/zamo/internal/modules/basebackup", () => ({
	Module: { PendingRestore: async () => "" },
}))

function view(overrides: Partial<View> = {}): View {
	return {
		aiEnabled: { value: true, origin: "default", override: null },
		provider: { value: "anthropic", origin: "gui", override: "anthropic" },
		model: { value: "claude-x", origin: "gui", override: "claude-x" },
		baseUrl: { value: "", origin: "default", override: null },
		apiKey: { set: true, origin: "config" },
		declarative: false,
		...overrides,
	}
}

function resetButtons(c: HTMLElement): HTMLButtonElement[] {
	return [...c.querySelectorAll("button")].filter((b) => b.textContent?.trim() === "reset")
}

beforeEach(() => {
	backend.view = view()
	backend.saved = []
})

describe("the settings screen", () => {
	it("offers a reset only beside the fields set on this screen", async () => {
		const { container } = render(SettingsView)
		await waitFor(() => expect(resetButtons(container)).toHaveLength(2))
		expect(container.textContent).toContain("config.toml")
	})

	it("resets one field and leaves the other overrides as stored", async () => {
		const { container } = render(SettingsView)
		await waitFor(() => expect(resetButtons(container)).toHaveLength(2))

		// An unsaved edit elsewhere must not ride along with the reset.
		const model = container.querySelector("#model") as HTMLInputElement
		await fireEvent.input(model, { target: { value: "unsaved" } })
		await fireEvent.click(resetButtons(container)[0])

		await waitFor(() => expect(backend.saved).toHaveLength(1))
		expect(backend.saved[0] as In).toEqual({
			aiEnabled: null,
			provider: null,
			model: "claude-x",
			baseUrl: null,
			apiKey: "",
			clearApiKey: false,
		})
	})

	it("says where to put the key when the screen is read-only and none is set", async () => {
		backend.view = view({ declarative: true, apiKey: { set: false, origin: "default" } })
		const { container } = render(SettingsView)
		await waitFor(() => expect(container.textContent).toContain("Set api_key_file in config.toml"))
	})
})
