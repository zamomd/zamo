import { Events } from "@wailsio/runtime"
import { Module } from "../../bindings/github.com/zamomd/zamo/internal/modules/assistant"
import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind"
import type { Step } from "../lib/assistantsteps"
import { callError, messageFor } from "../lib/callerror"
import { firstLine, topicName } from "../lib/refs"
import { base } from "./base.svelte"
import { shell } from "./shell.svelte"

const stepEvent = "agent:step"

// Turn is one exchange as the panel shows it: the owner's message and what
// became of it.
export type Turn = {
	// id is the panel's own key; turn is the backend's number for the answer,
	// known once there is one.
	id: number
	question: string
	answer: string | null
	turn: number | null
	exhausted: boolean
	// groupId is the batch the answer wrote, empty when it wrote none.
	groupId: string
	error: string
	cancelled: boolean
	saved: boolean
}

// What the assistant may change right now, and how the panel names it.
export type WriteContext = { scopeId: string; topicId: string; label: string }

// The conversation with the assistant. It lives for the window's lifetime;
// the backend holds the model's side of it and forgets on restart too.
class AssistantStore {
	turns = $state<Turn[]>([])
	draft = $state("")
	running = $state(false)
	// cancelling is true from the cancel gesture until the run settles.
	cancelling = $state(false)
	steps = $state<Step[]>([])

	private seq = 0

	// Only the tree screen has something open to change. Elsewhere a block
	// selected earlier is out of sight, so it is not the context either.
	context = $derived.by<WriteContext>(() => {
		if (shell.mode !== "tree") {
			return { scopeId: "", topicId: "", label: "" }
		}
		const root = base.viewRoot?.block
		const topicId = root?.ID ?? ""
		if (base.scopeID !== null) {
			return { scopeId: base.scopeID, topicId, label: firstLine(base.scopeLabel) }
		}
		return { scopeId: "", topicId, label: root ? topicName(root.Content) : "" }
	})

	start(): () => void {
		return Events.On(stepEvent, (e) => {
			// The app's other AI gestures announce steps on the same event.
			if (this.running) {
				this.steps.push((e.data ?? {}) as Step)
			}
		})
	}

	async send(): Promise<void> {
		const question = this.draft.trim()
		if (question === "" || this.running) {
			return
		}
		this.draft = ""
		this.turns.push({
			id: ++this.seq,
			question,
			answer: null,
			turn: null,
			exhausted: false,
			groupId: "",
			error: "",
			cancelled: false,
			saved: false,
		})
		await this.run(this.turns[this.turns.length - 1])
	}

	// resend puts a question that failed or was cancelled to the assistant
	// again, in the turn it already has.
	async resend(id: number): Promise<void> {
		const turn = this.turns.find((t) => t.id === id)
		if (turn === undefined || turn.answer !== null || this.running) {
			return
		}
		turn.error = ""
		turn.cancelled = false
		await this.run(turn)
	}

	private async run(turn: Turn): Promise<void> {
		this.running = true
		this.steps = []
		const { scopeId, topicId } = this.context
		try {
			const answer = await Module.Send(turn.question, { scopeId, topicId })
			turn.answer = answer.text
			turn.turn = answer.turn
			turn.exhausted = answer.exhausted
			turn.groupId = answer.groupId
			if (answer.groupId !== "") {
				await base.absorbWrite()
			}
		} catch (err: unknown) {
			if (callError(err).code === "cancelled") {
				turn.cancelled = true
			} else {
				turn.error = messageFor(err)
			}
			// A write lands before the run ends, so a run that then failed or
			// was cancelled may have left one behind.
			await base.absorbWrite().catch(() => {})
		} finally {
			this.running = false
			this.cancelling = false
			this.steps = []
		}
	}

	// cancel stops the run in flight; the awaiting send then fails with the
	// "cancelled" code and settles the turn.
	async cancel(): Promise<void> {
		if (!this.running) {
			return
		}
		this.cancelling = true
		try {
			await App.CancelAgent()
		} catch {
			this.cancelling = false
		}
	}

	async reset(): Promise<void> {
		if (this.running) {
			return
		}
		try {
			await Module.Reset()
		} catch {
			return
		}
		this.turns = []
	}

	// An answer is saved as a child of the open topic, so there has to be one,
	// on screen, and of a shape that takes children.
	canSave(turn: Turn): boolean {
		const topic = base.selectedTree?.block
		return (
			turn.turn !== null &&
			!turn.saved &&
			turn.answer !== null &&
			turn.answer !== "" &&
			shell.mode === "tree" &&
			topic !== undefined &&
			topic.Mode !== "doc"
		)
	}

	async save(id: number): Promise<void> {
		const turn = this.turns.find((t) => t.id === id)
		const topic = base.selectedTree?.block
		if (turn === undefined || turn.turn === null || topic === undefined || !this.canSave(turn)) {
			return
		}
		try {
			await Module.SaveAnswer(turn.turn, topic.ID)
			turn.saved = true
			turn.error = ""
			await base.absorbWrite()
		} catch (err: unknown) {
			turn.error = messageFor(err)
		}
	}
}

// One instance for the window's lifetime; module state outlives the views.
export const assistant = new AssistantStore()
