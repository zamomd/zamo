import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";

// TreeNodeData is one node of the rendered forest: a block and its children in
// Position order.
export type TreeNodeData = { block: Block; children: TreeNodeData[] };

// buildTree turns a pre-ordered flat subtree into a nested node. The root is
// the block whose ParentID is empty; every other block is attached to its
// parent in slice order, which GetSubtree already sorts by Position.
export function buildTree(blocks: Block[]): TreeNodeData | null {
	const byId = new Map<string, TreeNodeData>();
	for (const b of blocks) {
		byId.set(b.ID, { block: b, children: [] });
	}
	let root: TreeNodeData | null = null;
	for (const b of blocks) {
		const node = byId.get(b.ID);
		if (!node) {
			continue;
		}
		if (b.ParentID === "") {
			root = node;
		} else {
			byId.get(b.ParentID)?.children.push(node);
		}
	}
	return root;
}

// findBlock walks the forest for a block by id; the forest is a personal
// base's worth of nodes, so a plain walk is cheap enough.
export function findBlock(nodes: TreeNodeData[], id: string): Block | null {
	for (const node of nodes) {
		if (node.block.ID === id) {
			return node.block;
		}
		const hit = findBlock(node.children, id);
		if (hit) {
			return hit;
		}
	}
	return null;
}

// hasUnreviewed reports whether a node or any of its descendants carries an
// unreviewed revision (including a ghost, which is merged into the forest
// with Unreviewed set — see base.svelte.ts's reload). Drives the topic root's
// "Accept all / Reject all" affordance.
export function hasUnreviewed(node: TreeNodeData): boolean {
	return node.block.Unreviewed || node.children.some(hasUnreviewed);
}

// findPath returns the block's ancestor ids, root-first and excluding the block
// itself, so a jump can un-collapse exactly the rows above the target. The empty
// slice (a root) is distinct from null (id absent from the forest), which the
// caller needs to tell "reveal a root" from "not here".
export function findPath(nodes: TreeNodeData[], id: string): string[] | null {
	for (const node of nodes) {
		if (node.block.ID === id) {
			return [];
		}
		const sub = findPath(node.children, id);
		if (sub) {
			return [node.block.ID, ...sub];
		}
	}
	return null;
}

// topicOf reports which topic root's tree a block belongs to. The sidebar
// selects a topic while search and the changes list jump to blocks, so every
// jump has to answer this before it can decide whether the main area must
// switch topics first.
export function topicOf(nodes: TreeNodeData[], id: string): string | null {
	for (const node of nodes) {
		if (findBlock([node], id) !== null) {
			return node.block.ID;
		}
	}
	return null;
}
