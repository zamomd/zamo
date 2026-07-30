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
	// setMode's own commit already ran inside the await above; a failed one
	// leaves the previous session open (spec §8), but setMode's Promise<void>
	// does not carry that verdict back here. Reading it via commitEdit's own
	// return, the same idiom every other call site uses, costs nothing when
	// nothing is open — runCommit resolves true immediately for editingID ===
	// null — and does not depend on a fact stated only in shell.svelte.ts.
	if (!(await base.commitEdit())) {
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
