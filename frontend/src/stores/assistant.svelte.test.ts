import { beforeEach, describe, expect, it, vi } from "vitest"

const backend = vi.hoisted(() => ({
	handlers: {} as Record<string, (e: { data: unknown }) => void>,
	send: null as unknown as ReturnType<typeof import("vitest").vi.fn>,
	absorbed: 0,
	scopeID: null as string | null,
	viewRoot: null as { block: { ID: string; Content: string; Mode: string } } | null,
	selectedTree: null as { block: { ID: string; Content: string; Mode: string } } | null,
	mode: "tree",
}))

vi.mock("@wailsio/runtime", () => ({
	Events: {
		On: (name: string, fn: (e: { data: unknown }) => void) => {
			backend.handlers[name] = fn
			return () => {}
		},
	},
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/wailsbind", () => ({
	App: { CancelAgent: vi.fn(async () => 1) },
}))
vi.mock("../../bindings/github.com/zamomd/zamo/internal/modules/assistant", () => ({
	Module: {
		Send: vi.fn(),
		Reset: vi.fn(async () => {}),
		SaveAnswer: vi.fn(async () => "blk-saved"),
	},
}))
vi.mock("./base.svelte", () => ({
	base: {
		get scopeID() {
			return backend.scopeID
		},
		get scopeLabel() {
			return backend.scopeID === null ? "" : "the selected block\nsecond line"
		},
		get viewRoot() {
			return backend.viewRoot
		},
		get selectedTree() {
			return backend.selectedTree
		},
		forest: [],
		absorbWrite: async () => {
			backend.absorbed++
		},
	},
}))
vi.mock("./shell.svelte", () => ({
	shell: {
		get mode() {
			return backend.mode
		},
	},
}))

import { Module } from "../../bindings/github.com/zamomd/zamo/internal/modules/assistant"
import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"
import { assistant } from "./assistant.svelte"

const topic = (id: string, content: string, mode = "outline") => ({ block: { ID: id, Content: content, Mode: mode } })
const answer = (turn: number, text: string, extra: Record<string, unknown> = {}) => ({
	turn,
	text,
	exhausted: false,
	groupId: "",
	...extra,
})
const failure = (code: string) => new Error(JSON.stringify({ cause: { code, message: "x" } }))

function gate() {
	let release = () => {}
	const held = new Promise<void>((resolve) => {
		release = resolve
	})
	return { held, release }
}

beforeEach(() => {
	vi.mocked(Module.Send).mockReset()
	vi.mocked(Module.Reset).mockClear()
	vi.mocked(Module.SaveAnswer).mockClear()
	vi.mocked(App.CancelAgent).mockClear()
	backend.absorbed = 0
	backend.scopeID = null
	backend.viewRoot = null
	backend.selectedTree = null
	backend.mode = "tree"
	assistant.turns = []
	assistant.draft = ""
	assistant.running = false
	assistant.cancelling = false
	assistant.steps = []
	assistant.start()
})

describe("the write context", () => {
	it("is the selected block when there is one", () => {
		backend.scopeID = "blk"
		backend.viewRoot = topic("go", "Go contexts")

		expect(assistant.context).toEqual({ scopeId: "blk", topicId: "go", label: "the selected block" })
	})

	it("is the open topic otherwise", () => {
		backend.viewRoot = topic("go", "# Go contexts\nbody", "doc")

		expect(assistant.context).toEqual({ scopeId: "", topicId: "go", label: "Go contexts" })
	})

	it("is empty away from the tree, whatever was open there", () => {
		backend.scopeID = "blk"
		backend.viewRoot = topic("go", "Go contexts")
		backend.mode = "graph"

		expect(assistant.context).toEqual({ scopeId: "", topicId: "", label: "" })
	})
})

describe("sending a message", () => {
	it("shows the question at once and the answer when it arrives", async () => {
		const g = gate()
		vi.mocked(Module.Send).mockImplementation((async () => {
			await g.held
			return answer(0, "X is a thing")
		}) as never)
		backend.viewRoot = topic("go", "Go contexts")
		assistant.draft = "  what is X?  "

		const sending = assistant.send()
		await vi.waitFor(() => expect(assistant.running).toBe(true))

		expect(assistant.turns).toHaveLength(1)
		expect(assistant.turns[0].question).toBe("what is X?")
		expect(assistant.turns[0].answer).toBeNull()
		expect(assistant.draft).toBe("")
		expect(Module.Send).toHaveBeenCalledWith("what is X?", { scopeId: "", topicId: "go" })

		g.release()
		await sending

		expect(assistant.running).toBe(false)
		expect(assistant.turns[0].answer).toBe("X is a thing")
		expect(assistant.turns[0].turn).toBe(0)
	})

	it("ignores an empty draft and a second send while one is running", async () => {
		assistant.draft = "   "
		await assistant.send()
		expect(Module.Send).not.toHaveBeenCalled()

		assistant.running = true
		assistant.draft = "hello"
		await assistant.send()
		expect(Module.Send).not.toHaveBeenCalled()
		expect(assistant.draft).toBe("hello")
	})

	it("re-reads the base after an answer that wrote", async () => {
		vi.mocked(Module.Send).mockResolvedValue(answer(0, "tightened it", { groupId: "g1" }) as never)
		assistant.draft = "tighten this"

		await assistant.send()

		expect(backend.absorbed).toBe(1)
		expect(assistant.turns[0].groupId).toBe("g1")
	})

	it("leaves the base alone after an answer that only answered", async () => {
		vi.mocked(Module.Send).mockResolvedValue(answer(0, "X is a thing") as never)
		assistant.draft = "what is X?"

		await assistant.send()

		expect(backend.absorbed).toBe(0)
	})

	it("marks an answer that ran out of rounds", async () => {
		vi.mocked(Module.Send).mockResolvedValue(answer(0, "", { exhausted: true }) as never)
		assistant.draft = "everything"

		await assistant.send()

		expect(assistant.turns[0].exhausted).toBe(true)
	})

	it("keeps the question and a readable error when the send fails", async () => {
		vi.mocked(Module.Send).mockRejectedValue(failure("llm_no_tools"))
		assistant.draft = "what is X?"

		await assistant.send()

		expect(assistant.running).toBe(false)
		expect(assistant.turns[0].answer).toBeNull()
		expect(assistant.turns[0].error).toContain("can't call tools")
		expect(backend.absorbed, "a run that failed may still have written").toBe(1)
	})

	it("sends a failed question again in place", async () => {
		vi.mocked(Module.Send).mockRejectedValueOnce(failure("llm_unavailable"))
		vi.mocked(Module.Send).mockResolvedValueOnce(answer(0, "X is a thing") as never)
		assistant.draft = "what is X?"
		await assistant.send()

		await assistant.resend(assistant.turns[0].id)

		expect(assistant.turns).toHaveLength(1)
		expect(assistant.turns[0].error).toBe("")
		expect(assistant.turns[0].answer).toBe("X is a thing")
		expect(Module.Send).toHaveBeenCalledTimes(2)
	})
})

describe("steps", () => {
	it("shows the steps of the run in flight and clears them when it ends", async () => {
		const g = gate()
		vi.mocked(Module.Send).mockImplementation((async () => {
			await g.held
			return answer(0, "done")
		}) as never)
		assistant.draft = "find it"

		const sending = assistant.send()
		await vi.waitFor(() => expect(assistant.running).toBe(true))
		backend.handlers["agent:step"]({ data: { kind: "thinking" } })
		backend.handlers["agent:step"]({ data: { kind: "tool", tool: "search", detail: "it" } })

		expect(assistant.steps).toEqual([{ kind: "thinking" }, { kind: "tool", tool: "search", detail: "it" }])

		g.release()
		await sending
		expect(assistant.steps).toEqual([])
	})

	it("ignores the steps of a run that is not the assistant's", () => {
		backend.handlers["agent:step"]({ data: { kind: "thinking" } })

		expect(assistant.steps).toEqual([])
	})
})

describe("cancelling", () => {
	it("is a no-op with nothing running", async () => {
		await assistant.cancel()

		expect(App.CancelAgent).not.toHaveBeenCalled()
	})

	it("stops the run and marks the turn cancelled, not failed", async () => {
		const g = gate()
		vi.mocked(Module.Send).mockImplementation((async () => {
			await g.held
			throw failure("cancelled")
		}) as never)
		assistant.draft = "a long question"

		const sending = assistant.send()
		await vi.waitFor(() => expect(assistant.running).toBe(true))
		await assistant.cancel()

		expect(App.CancelAgent).toHaveBeenCalled()
		expect(assistant.cancelling).toBe(true)

		g.release()
		await sending

		expect(assistant.cancelling).toBe(false)
		expect(assistant.turns[0].cancelled).toBe(true)
		expect(assistant.turns[0].error).toBe("")
	})
})

describe("a new conversation", () => {
	it("forgets the turns here and in the backend", async () => {
		vi.mocked(Module.Send).mockResolvedValue(answer(0, "X is a thing") as never)
		assistant.draft = "what is X?"
		await assistant.send()

		await assistant.reset()

		expect(Module.Reset).toHaveBeenCalled()
		expect(assistant.turns).toEqual([])
	})

	it("is refused while a run is in flight", async () => {
		assistant.running = true
		assistant.turns = [{ id: 1, question: "q", answer: null, turn: null } as never]

		await assistant.reset()

		expect(Module.Reset).not.toHaveBeenCalled()
		expect(assistant.turns).toHaveLength(1)
	})
})

describe("saving an answer", () => {
	beforeEach(async () => {
		vi.mocked(Module.Send).mockResolvedValue(answer(3, "X is a thing") as never)
		assistant.draft = "what is X?"
		await assistant.send()
	})

	it("is offered under an open outline topic", () => {
		backend.selectedTree = topic("go", "Go contexts")

		expect(assistant.canSave(assistant.turns[0])).toBe(true)
	})

	it("is not offered without a topic, under a document, away from the tree, or twice", () => {
		expect(assistant.canSave(assistant.turns[0])).toBe(false)

		backend.selectedTree = topic("essay", "# Essay", "doc")
		expect(assistant.canSave(assistant.turns[0])).toBe(false)

		backend.selectedTree = topic("go", "Go contexts")
		backend.mode = "settings"
		expect(assistant.canSave(assistant.turns[0])).toBe(false)

		backend.mode = "tree"
		assistant.turns[0].saved = true
		expect(assistant.canSave(assistant.turns[0])).toBe(false)
	})

	it("writes the answer under the open topic and re-reads the base", async () => {
		backend.selectedTree = topic("go", "Go contexts")

		await assistant.save(assistant.turns[0].id)

		expect(Module.SaveAnswer).toHaveBeenCalledWith(3, "go")
		expect(assistant.turns[0].saved).toBe(true)
		expect(backend.absorbed).toBe(1)
	})

	it("reports a refused save on the turn", async () => {
		backend.selectedTree = topic("go", "Go contexts")
		vi.mocked(Module.SaveAnswer).mockRejectedValueOnce(failure("assistant_no_answer"))

		await assistant.save(assistant.turns[0].id)

		expect(assistant.turns[0].saved).toBe(false)
		expect(assistant.turns[0].error).toContain("no longer available")
	})
})
