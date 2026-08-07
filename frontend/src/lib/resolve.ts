import type { RefResolver } from "./markdown";
import { firstLine, normalizeTitle, type Ref } from "./refs";
import { findBlock, type TreeNodeData } from "./tree";

// findTopicRoot looks a [[Topic]] reference up by its normalized key among the
// topics the forest holds — the same key blocks.title_key carries on the Go
// side, so this is the client-side half of resolution. TreeNode and DocView
// both route reference clicks through it, alongside makeResolver below.
export function findTopicRoot(forest: TreeNodeData[], targetKey: string): TreeNodeData | null {
	return forest.find((t) => normalizeTitle(t.block.Content) === targetKey) ?? null;
}

// makeResolver backs renderMarkdown's live lookup over a loaded forest: null
// means unresolved, in which case the renderer falls back to the reference's
// own written text.
export function makeResolver(forest: TreeNodeData[]): RefResolver {
	return (ref: Ref): string | null => {
		if (ref.kind === "topic") {
			const root = findTopicRoot(forest, ref.targetKey);
			return root ? firstLine(root.block.Content) : null;
		}
		const block = findBlock(forest, ref.targetKey);
		return block ? firstLine(block.Content) : null;
	};
}

// RefClick is the outcome of clicking a rendered .ref span: navigate to a
// resolved target, or offer to create an unresolved topic. TreeNode and
// DocView both route their {@html} output's clicks through refClick, so a
// reference behaves identically in an outline and inside a document.
export type RefClick = { kind: "navigate"; id: string } | { kind: "create"; name: string };

export function refClick(forest: TreeNodeData[], el: HTMLElement): RefClick | null {
	const targetKey = el.dataset.target ?? "";
	const isTopic = el.classList.contains("ref-topic");
	if (el.classList.contains("ref-missing")) {
		return isTopic ? { kind: "create", name: el.textContent ?? targetKey } : null;
	}
	if (isTopic) {
		const root = findTopicRoot(forest, targetKey);
		return root ? { kind: "navigate", id: root.block.ID } : null;
	}
	return findBlock(forest, targetKey) ? { kind: "navigate", id: targetKey } : null;
}
