import { base } from "../stores/base.svelte";
import { shell } from "../stores/shell.svelte";

// The stores stay ignorant of the shell — base.reveal is also called from a
// background reload's pending-reveal flush, and a reload that switched modes
// would throw the reader out of whatever they were reading. Pairing the mode
// switch with the jump therefore belongs to callers, and belongs here rather
// than repeated at every call site in markup, where nothing can test it.
//
// Awaits the mode switch, unlike openTopic below: reveal can move the main
// area to a different topic, which can unmount an open editor's row, and
// nothing else here commits it first — openTopic's own call to selectTopic
// does that itself, but reveal does not.
export async function jumpToBlock(id: string): Promise<void> {
	await shell.setMode("tree");
	// setMode's own commit can fail and leave the previous session open (spec
	// §8), but setMode's Promise<void> does not carry that verdict back here.
	// editingID is what survives a failed commit — the write failed and the
	// text is still in the editor — so it stands in for the boolean every
	// other caller of commitEdit checks directly. Proceeding to reveal() would
	// switch topics under that still-open editor's row.
	if (base.editingID !== null) {
		return;
	}
	base.reveal(id);
}

export function openTopic(id: string): void {
	// setMode's own commit is redundant here — selectTopic below commits the
	// same open edit itself, sharing its in-flight promise — so this fires
	// without waiting, like every other store call from markup.
	void shell.setMode("tree");
	// selectTopic may need to re-read the base before it can resolve id (see
	// its own comment) — this caller does not wait on that either.
	void base.selectTopic(id);
}
