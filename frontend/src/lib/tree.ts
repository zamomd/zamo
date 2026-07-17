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
