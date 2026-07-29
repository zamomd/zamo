import { base } from "../stores/base.svelte";
import { shell } from "../stores/shell.svelte";

// The stores stay ignorant of the shell — base.reveal is also called from a
// background reload's pending-reveal flush, and a reload that switched modes
// would throw the reader out of whatever they were reading. Pairing the mode
// switch with the jump therefore belongs to callers, and belongs here rather
// than repeated at every call site in markup, where nothing can test it.
export function jumpToBlock(id: string): void {
	shell.setMode("tree");
	base.reveal(id);
}

export function openTopic(id: string): void {
	shell.setMode("tree");
	base.selectTopic(id);
}
