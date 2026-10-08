import { base } from "../stores/base.svelte"
import { shell } from "../stores/shell.svelte"
import { tabs } from "../stores/tabs.svelte"
import { topicOf } from "./tree"

export interface OpenOptions {
	newTab?: boolean
}

// newTabGesture reports whether a click asks for a new tab: with ⌘ or Ctrl
// held, or with the middle button.
export function newTabGesture(e: MouseEvent): boolean {
	return e.metaKey || e.ctrlKey || e.button === 1
}

// jumpToBlock commits before revealing because reveal may switch topics.
export async function jumpToBlock(id: string, opts: OpenOptions = {}): Promise<void> {
	if (base.busy) {
		return
	}
	await shell.setMode("tree")
	if (!(await base.commitEdit())) {
		return
	}
	// A block the forest does not hold yet has no topic to open a tab for: the
	// reveal waits for the reload and lands in the active tab.
	const topic = topicOf(base.forest, id)
	if (topic !== null && topic !== base.selectedTopicID) {
		await tabs.open(topic, opts)
	}
	base.reveal(id)
}

export function openTopic(id: string, opts: OpenOptions = {}): void {
	void shell.setMode("tree")
	void tabs.open(id, opts)
}
