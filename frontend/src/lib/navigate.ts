import { base } from "../stores/base.svelte"
import { shell } from "../stores/shell.svelte"

// jumpToBlock commits before revealing because reveal may switch topics.
export async function jumpToBlock(id: string): Promise<void> {
	if (base.busy) {
		return
	}
	await shell.setMode("tree")
	if (!(await base.commitEdit())) {
		return
	}
	base.reveal(id)
}

export function openTopic(id: string): void {
	void shell.setMode("tree")
	void base.selectTopic(id)
}
