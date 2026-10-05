// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Author, Mode } from "../bindings/github.com/zamomd/zamo/internal/knowledge"
import type { View } from "../bindings/github.com/zamomd/zamo/internal/settings"
import ModeRail from "./lib/ModeRail.svelte"
import TreeNode from "./lib/TreeNode.svelte"
import SettingsView from "./SettingsView.svelte"
import { ai } from "./stores/ai.svelte"
import { base } from "./stores/base.svelte"
import { shell } from "./stores/shell.svelte"
import TopicView from "./TopicView.svelte"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))

const backend = vi.hoisted(() => ({ aiEnabled: true }))
vi.mock("../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => [],
		GetSubtree: async () => [],
		Settings: async () => settingsView(backend.aiEnabled),
		SaveSettings: async () => {},
		CheckSettings: async () => {},
	},
}))

function settingsView(aiEnabled: boolean): View {
	const field = <T>(value: T) => ({ value, origin: "default", override: null })
	return {
		aiEnabled: field(aiEnabled),
		provider: field(""),
		model: field(""),
		baseUrl: field(""),
		apiKey: { set: false, origin: "default" },
		declarative: false,
	}
}

const ASSISTANT_TOGGLE = 'button[aria-label="assistant"]'
const TARGET_BUTTON = 'button[aria-label="target this block for the AI command"]'

const row = {
	block: {
		ID: "b1",
		ParentID: "root",
		Position: 1,
		Content: "a claim",
		CreatedBy: Author.AuthorOwner,
		Mode: Mode.ModeOutline,
		CreatedAt: "",
		UpdatedAt: "",
		Unreviewed: false,
	},
	children: [],
}

beforeEach(() => {
	backend.aiEnabled = true
	base.clearEdit()
	base.forest = []
})

describe("the AI gate", () => {
	it("renders no AI control anywhere while AI is off", () => {
		ai.set(false)

		expect(render(ModeRail).container.querySelector(ASSISTANT_TOGGLE)).toBeNull()
		expect(render(TopicView).container.textContent).toContain("Pick one in the sidebar")

		const tree = render(TreeNode, { node: row }).container
		expect(tree.querySelector(TARGET_BUTTON)).toBeNull()
		expect(tree.querySelector('button[aria-label="toggle linked references"]')).not.toBeNull()
	})

	it("renders them again once AI is on", () => {
		ai.set(true)

		expect(render(ModeRail).container.querySelector(ASSISTANT_TOGGLE)).not.toBeNull()
		expect(render(TopicView).container.textContent).toContain("ask the assistant")
		expect(render(TreeNode, { node: row }).container.querySelector(TARGET_BUTTON)).not.toBeNull()
	})

	it("opens and closes the assistant from the rail", async () => {
		ai.set(true)
		shell.assistantOpen = false
		const toggle = render(ModeRail).container.querySelector(ASSISTANT_TOGGLE) as HTMLButtonElement

		await fireEvent.click(toggle)
		expect(shell.assistantOpen).toBe(true)
		expect(toggle.getAttribute("aria-pressed")).toBe("true")

		await fireEvent.click(toggle)
		expect(shell.assistantOpen).toBe(false)
	})

	it("closes an open assistant when AI is switched off", () => {
		ai.set(true)
		shell.assistantOpen = true

		ai.set(false)

		expect(shell.assistantOpen).toBe(false)
	})

	it("keeps the switch reachable on the settings screen while AI is off", async () => {
		backend.aiEnabled = false
		ai.set(false)

		const { container, findByLabelText } = render(SettingsView)
		const box = (await findByLabelText("Enable AI features", { exact: false })) as HTMLInputElement
		expect(box.checked).toBe(false)
		expect(box.disabled).toBe(false)
		expect(container.textContent).toContain("Every AI control is hidden")
	})
})
