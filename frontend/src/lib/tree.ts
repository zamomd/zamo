import type { Block, Mode } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js"
import { plainText } from "./plaintext"
import { firstLine } from "./refs"

// TreeNodeData is one node of the rendered forest: a block and its children in
// Position order.
export type TreeNodeData = { block: Block; children: TreeNodeData[] }

// buildTree turns a pre-ordered flat subtree into a nested node. The root is
// the block whose ParentID is empty; every other block is attached to its
// parent in slice order, which GetSubtree already sorts by Position.
export function buildTree(blocks: Block[]): TreeNodeData | null {
	const byId = new Map<string, TreeNodeData>()
	for (const b of blocks) {
		byId.set(b.ID, { block: b, children: [] })
	}
	let root: TreeNodeData | null = null
	for (const b of blocks) {
		const node = byId.get(b.ID)
		if (!node) {
			continue
		}
		if (b.ParentID === "") {
			root = node
		} else {
			byId.get(b.ParentID)?.children.push(node)
		}
	}
	return root
}

// findBlock walks the forest for a block by id; the forest is a personal
// base's worth of nodes, so a plain walk is cheap enough.
export function findBlock(nodes: TreeNodeData[], id: string): Block | null {
	for (const node of nodes) {
		if (node.block.ID === id) {
			return node.block
		}
		const hit = findBlock(node.children, id)
		if (hit) {
			return hit
		}
	}
	return null
}

// hasUnreviewed reports whether a node or any of its descendants carries an
// unreviewed revision (including a ghost, which is merged into the forest
// with Unreviewed set — see base.svelte.ts's reload). Drives the topic root's
// "Accept all / Reject all" affordance.
export function hasUnreviewed(node: TreeNodeData): boolean {
	return node.block.Unreviewed || node.children.some(hasUnreviewed)
}

// findPath returns the block's ancestor ids, root-first and excluding the block
// itself, so a jump can un-collapse exactly the rows above the target. The empty
// slice (a root) is distinct from null (id absent from the forest), which the
// caller needs to tell "reveal a root" from "not here".
export function findPath(nodes: TreeNodeData[], id: string): string[] | null {
	for (const node of nodes) {
		if (node.block.ID === id) {
			return []
		}
		const sub = findPath(node.children, id)
		if (sub) {
			return [node.block.ID, ...sub]
		}
	}
	return null
}

// findNode walks the forest for a node by id. findBlock answers "what does this
// block say"; this answers "what hangs under it", which is what zooming into a
// block needs.
export function findNode(nodes: TreeNodeData[], id: string): TreeNodeData | null {
	for (const node of nodes) {
		if (node.block.ID === id) {
			return node
		}
		const hit = findNode(node.children, id)
		if (hit) {
			return hit
		}
	}
	return null
}

// Crumb is one step of the zoom-in trail: an ancestor the reader can click to
// come back out to.
export type Crumb = { id: string; title: string }

// crumbTrail returns id's ancestors, root-first and excluding id itself, with
// titles already reduced to one line. An empty trail (a topic root) is distinct
// from null (absent from the forest) for the same reason findPath draws that
// line: the caller has to tell "already at the top" from "not here".
export function crumbTrail(nodes: TreeNodeData[], id: string): Crumb[] | null {
	const path = findPath(nodes, id)
	if (path === null) {
		return null
	}
	return path.map((ancestorID) => ({
		id: ancestorID,
		title: plainText(findBlock(nodes, ancestorID)?.Content ?? ""),
	}))
}

// topicOf reports which topic root's tree a block belongs to. The sidebar
// selects a topic while search and the changes list jump to blocks, so every
// jump has to answer this before it can decide whether the main area must
// switch topics first.
export function topicOf(nodes: TreeNodeData[], id: string): string | null {
	for (const node of nodes) {
		if (findBlock([node], id) !== null) {
			return node.block.ID
		}
	}
	return null
}

// TopicRow is one line of the sidebar: a topic, and — for the selected one
// only — its first level of children. The sidebar is an index, not a second
// outliner, so it never goes deeper than one level.
export type TopicRow = {
	id: string
	title: string
	mode: Mode
	selected: boolean
	children: { id: string; title: string }[]
}

export function topicRows(nodes: TreeNodeData[], selected: string | null): TopicRow[] {
	return nodes.map((node) => ({
		id: node.block.ID,
		title: plainText(firstLine(node.block.Content)),
		mode: node.block.Mode,
		selected: node.block.ID === selected,
		children:
			node.block.ID === selected
				? node.children.map((c) => ({ id: c.block.ID, title: plainText(c.block.Content) }))
				: [],
	}))
}

// visibleOrder returns rendered rows in document order. Skipped rows are not
// targets, but their visible descendants remain in the order.
export function visibleOrder(nodes: TreeNodeData[], collapsed: Set<string>, skip: Set<string>): string[] {
	const out: string[] = []
	const walk = (list: TreeNodeData[]): void => {
		for (const node of list) {
			if (!skip.has(node.block.ID)) {
				out.push(node.block.ID)
			}
			if (!collapsed.has(node.block.ID)) {
				walk(node.children)
			}
		}
	}
	walk(nodes)
	return out
}

// prevVisible returns the previous rendered target.
export function prevVisible(
	nodes: TreeNodeData[],
	id: string,
	collapsed: Set<string>,
	skip: Set<string>,
): string | null {
	const order = visibleOrder(nodes, collapsed, skip)
	const i = order.indexOf(id)
	return i > 0 ? order[i - 1] : null
}

// nextVisible returns the next rendered target.
export function nextVisible(
	nodes: TreeNodeData[],
	id: string,
	collapsed: Set<string>,
	skip: Set<string>,
): string | null {
	const order = visibleOrder(nodes, collapsed, skip)
	const i = order.indexOf(id)
	return i >= 0 && i < order.length - 1 ? order[i + 1] : null
}

// prevSibling returns the previous non-skipped child of the same parent.
export function prevSibling(nodes: TreeNodeData[], id: string, skip: Set<string>): string | null {
	const search = (list: TreeNodeData[]): string | null => {
		for (let i = 0; i < list.length; i++) {
			if (list[i].block.ID === id) {
				for (let j = i - 1; j >= 0; j--) {
					if (!skip.has(list[j].block.ID)) {
						return list[j].block.ID
					}
				}
				return null
			}
			const hit = search(list[i].children)
			if (hit !== null) {
				return hit
			}
		}
		return null
	}
	return search(nodes)
}
