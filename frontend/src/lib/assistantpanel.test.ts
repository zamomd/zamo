// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/svelte"
import { tick } from "svelte"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { Author, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import { assistant, type Turn } from "../stores/assistant.svelte"
import { base } from "../stores/base.svelte"
import { changes } from "../stores/changes.svelte"
import { shell } from "../stores/shell.svelte"
import AssistantPanel from "./AssistantPanel.svelte"
import { ASSISTANT_MIN } from "./assistantsize"
import type { TreeNodeData } from "./tree"

vi.mock("@wailsio/runtime", () => ({
	Events: { On: () => () => {} },
	Browser: { OpenURL: () => {} },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: {
		TopicMap: async () => [],
		GetSubtree: async () => [],
		TopicAliases: async () => [],
		TopicTags: async () => [],
		Unreviewed: async () => ({ Blocks: [] }),
		CancelAgent: vi.fn(async () => 1),
	},
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/assistant", () => ({
	Module: {
		Send: vi.fn(async () => ({ turn: 0, text: "an answer", exhausted: false, groupId: "" })),
		Reset: vi.fn(async () => {}),
		SaveAnswer: vi.fn(async () => "blk"),
	},
}))

import { Module } from "../../bindings/github.com/zamomd/zamo/internal/modules/assistant"

function node(id: string, parent: string, content: string, mode = Mode.ModeOutline): TreeNodeData {
	return {
		block: {
			ID: id,
			ParentID: parent,
			Position: 0,
			Content: content,
			CreatedBy: Author.AuthorOwner,
			Mode: mode,
			CreatedAt: "",
			UpdatedAt: "",
			Unreviewed: false,
		},
		children: [],
	}
}

function answered(overrides: Partial<Turn> = {}): Turn {
	return {
		id: 1,
		question: "what is X?",
		answer: "X is a thing",
		turn: 0,
		exhausted: false,
		groupId: "",
		error: "",
		cancelled: false,
		saved: false,
		...overrides,
	}
}

const text = (el: Element | null) => (el?.textContent ?? "").replace(/\s+/g, " ").trim()

beforeEach(() => {
	localStorage.clear()
	const go = node("go", "", "Go contexts")
	go.children = [node("ctx", "go", "a deadline cancels the request")]
	base.forest = [go]
	base.selectedTopicID = "go"
	base.focusID = null
	base.scopeID = null
	base.busy = false
	changes.blocks = []
	shell.mode = "tree"
	shell.assistantOpen = true
	shell.assistantWidth = null
	assistant.turns = []
	assistant.draft = ""
	assistant.running = false
	assistant.cancelling = false
	assistant.steps = []
	vi.mocked(Module.Send).mockClear()
})

describe("the context line", () => {
	it("names the open topic", () => {
		const { container } = render(AssistantPanel)

		expect(text(container.querySelector(".context"))).toContain("Go contexts")
	})

	it("names the selected block and lets it go", async () => {
		base.scopeID = "ctx"
		const { container, getByRole } = render(AssistantPanel)

		expect(text(container.querySelector(".context"))).toContain("a deadline cancels the request")

		await fireEvent.click(getByRole("button", { name: "clear" }))

		expect(base.scopeID).toBeNull()
		expect(text(container.querySelector(".context"))).toContain("Go contexts")
	})

	it("says there is nothing open away from the tree", () => {
		shell.mode = "settings"
		const { container } = render(AssistantPanel)

		expect(text(container.querySelector(".context"))).toContain("Nothing open")
		expect(text(container.querySelector(".context"))).not.toContain("Go contexts")
	})
})

describe("the conversation", () => {
	it("invites a first message while it is empty", () => {
		const { container } = render(AssistantPanel)

		expect(container.querySelector(".empty")).not.toBeNull()
	})

	it("renders an answer's references as links into the base", async () => {
		assistant.turns = [answered({ answer: "See [[Go contexts]] and [[Nowhere]]." })]
		const { container } = render(AssistantPanel)

		const refs = container.querySelectorAll(".answer .ref")
		expect(refs).toHaveLength(2)
		expect(text(refs[0])).toBe("Go contexts")
		expect(refs[0].classList.contains("ref-missing")).toBe(false)
		expect(refs[1].classList.contains("ref-missing")).toBe(true)

		shell.mode = "graph"
		await fireEvent.click(refs[0])
		await vi.waitFor(() => expect(shell.mode).toBe("tree"))
	})

	it("lets the keyboard reach and follow a resolved reference, and only that", async () => {
		assistant.turns = [answered({ answer: "See [[Go contexts]] and [[Nowhere]]." })]
		const { container } = render(AssistantPanel)
		const [found, missing] = container.querySelectorAll<HTMLElement>(".answer .ref")

		expect(found.tabIndex).toBe(0)
		expect(found.getAttribute("role")).toBe("link")
		expect(missing.getAttribute("role")).toBeNull()

		shell.mode = "graph"
		await fireEvent.keyDown(found, { key: "Enter" })
		await vi.waitFor(() => expect(shell.mode).toBe("tree"))
	})

	it("does not render markup an answer carries as markup", () => {
		assistant.turns = [answered({ answer: 'before <img src=x onerror="alert(1)"> after' })]
		const { container } = render(AssistantPanel)

		expect(container.querySelector(".answer img")).toBeNull()
	})

	it("says so when the assistant ran out of rounds", () => {
		assistant.turns = [answered({ answer: "", exhausted: true })]
		const { container } = render(AssistantPanel)

		expect(text(container.querySelector(".turn"))).toContain("ran out of steps")
	})

	it("counts the blocks an answer changed and leads to their review", async () => {
		changes.blocks = [
			{ GroupID: "g1", TopicID: "go", Head: { BlockID: "ctx" } },
			{ GroupID: "g1", TopicID: "go", Head: { BlockID: "go" } },
			{ GroupID: "other", TopicID: "go", Head: { BlockID: "z" } },
		] as never
		assistant.turns = [answered({ groupId: "g1" })]
		const { container, getByRole } = render(AssistantPanel)

		expect(text(container.querySelector(".wrote"))).toContain("2 blocks")

		await fireEvent.click(getByRole("button", { name: "review" }))
		await vi.waitFor(() => expect(shell.mode).toBe("changes"))
	})

	it("still says a change was made once it has been reviewed", () => {
		assistant.turns = [answered({ groupId: "g1" })]
		const { container } = render(AssistantPanel)

		expect(text(container.querySelector(".wrote"))).toContain("reviewed")
	})

	it("shows a failed turn's error and offers to send it again", () => {
		assistant.turns = [answered({ answer: null, turn: null, error: "The AI provider is unreachable." })]
		const { container, getByRole } = render(AssistantPanel)

		expect(text(container.querySelector(".error"))).toContain("unreachable")
		expect(getByRole("button", { name: "send again" })).not.toBeNull()
	})

	it("shows the steps of the run in flight", () => {
		assistant.turns = [answered({ answer: null, turn: null })]
		assistant.running = true
		assistant.steps = [{ kind: "thinking" }, { kind: "tool", tool: "search", detail: "deadline" }]
		const { container } = render(AssistantPanel)

		expect(text(container.querySelector(".steps"))).toContain("searching “deadline”…")
	})
})

describe("sending", () => {
	it("sends the draft on Enter and keeps a newline on Shift+Enter", async () => {
		const { getByLabelText } = render(AssistantPanel)
		const field = getByLabelText("message to the assistant") as HTMLTextAreaElement
		await fireEvent.input(field, { target: { value: "what is X?" } })

		await fireEvent.keyDown(field, { key: "Enter", shiftKey: true })
		expect(Module.Send).not.toHaveBeenCalled()

		await fireEvent.keyDown(field, { key: "Enter" })
		await vi.waitFor(() => expect(Module.Send).toHaveBeenCalledWith("what is X?", { scopeId: "", topicId: "go" }))
	})

	it("offers cancel instead of send while a run is in flight", async () => {
		assistant.running = true
		const { getByRole, queryByRole } = render(AssistantPanel)

		expect(queryByRole("button", { name: "send" })).toBeNull()
		expect(getByRole("button", { name: "cancel" })).not.toBeNull()
	})

	it("offers a new conversation only when there is one to leave", async () => {
		const { getByRole } = render(AssistantPanel)
		const reset = getByRole("button", { name: "new conversation" }) as HTMLButtonElement
		expect(reset.disabled).toBe(true)

		assistant.turns = [answered()]
		await tick()
		expect(reset.disabled).toBe(false)

		assistant.running = true
		await tick()
		expect(reset.disabled).toBe(true)
	})
})

describe("saving an answer", () => {
	it("is offered under an open outline topic", () => {
		assistant.turns = [answered()]
		const { getByRole } = render(AssistantPanel)

		expect((getByRole("button", { name: "save as block" }) as HTMLButtonElement).disabled).toBe(false)
	})

	it("is disabled with no topic open", () => {
		base.forest = []
		base.selectedTopicID = null
		assistant.turns = [answered()]
		const { getByRole } = render(AssistantPanel)

		expect((getByRole("button", { name: "save as block" }) as HTMLButtonElement).disabled).toBe(true)
	})

	it("turns into a note once saved", () => {
		assistant.turns = [answered({ saved: true })]
		const { container, queryByRole } = render(AssistantPanel)

		expect(queryByRole("button", { name: "save as block" })).toBeNull()
		expect(text(container.querySelector(".turn"))).toContain("saved")
	})
})

describe("the panel itself", () => {
	it("closes on Escape from inside", async () => {
		const { getByLabelText } = render(AssistantPanel)

		await fireEvent.keyDown(getByLabelText("message to the assistant"), { key: "Escape" })

		expect(shell.assistantOpen).toBe(false)
	})

	it("is resized by dragging its edge and remembers the width", async () => {
		Object.defineProperty(window, "innerWidth", { value: 1200, configurable: true })
		const { container } = render(AssistantPanel)
		const handle = container.querySelector(".resizer") as HTMLElement
		handle.setPointerCapture = () => {}

		await fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: 900 })
		await fireEvent.pointerMove(handle, { pointerId: 1, clientX: 800 })
		await fireEvent.pointerUp(handle, { pointerId: 1, clientX: 800 })

		expect(shell.assistantWidth).toBe(400)
		expect(JSON.parse(localStorage.getItem("zamo.assistant") ?? "{}").width).toBe(400)

		await fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: 800 })
		await fireEvent.pointerMove(handle, { pointerId: 1, clientX: 1190 })
		await fireEvent.pointerUp(handle, { pointerId: 1, clientX: 1190 })

		expect(shell.assistantWidth).toBe(ASSISTANT_MIN)
	})
})
