import type { TopicAlias } from "../../bindings/github.com/zamomd/zamo/internal/knowledge"
import type { RefResolver } from "./markdown"
import { firstLine, normalizeTitle, type Ref } from "./refs"
import { findBlock, type TreeNodeData } from "./tree"

// AliasIndex maps the normalized key of every alias to the topic it resolves
// to and the alias as its owner wrote it.
export type AliasIndex = ReadonlyMap<string, { topicID: string; alias: string }>

export function aliasIndex(aliases: TopicAlias[]): AliasIndex {
	return new Map(aliases.map((a) => [normalizeTitle(a.Alias), { topicID: a.TopicID, alias: a.Alias }]))
}

// findTopicRoot looks a [[Topic]] reference up by its normalized key among the
// topics the forest holds — a topic's own name first, then its aliases, which
// is the order storage resolves in, so this is the client-side half of
// resolution. TreeNode and DocView both route reference clicks through it,
// alongside makeResolver below.
export function findTopicRoot(forest: TreeNodeData[], aliases: AliasIndex, targetKey: string): TreeNodeData | null {
	const named = forest.find((t) => normalizeTitle(t.block.Content) === targetKey)
	if (named !== undefined) {
		return named
	}
	const topicID = aliases.get(targetKey)?.topicID
	return forest.find((t) => t.block.ID === topicID) ?? null
}

// makeResolver backs renderMarkdown's live lookup over a loaded forest: null
// means unresolved, in which case the renderer falls back to the reference's
// own written text. A reference written through an alias shows the alias, not
// the topic's name: the author chose that word for the sentence.
export function makeResolver(forest: TreeNodeData[], aliases: AliasIndex): RefResolver {
	return (ref: Ref): string | null => {
		if (ref.kind === "topic") {
			const root = findTopicRoot(forest, aliases, ref.targetKey)
			if (root === null) {
				return null
			}
			const name = firstLine(root.block.Content)
			return normalizeTitle(name) === ref.targetKey ? name : (aliases.get(ref.targetKey)?.alias ?? name)
		}
		const block = findBlock(forest, ref.targetKey)
		return block ? firstLine(block.Content) : null
	}
}

// RefClick is the outcome of clicking a rendered .ref span: navigate to a
// resolved target, or offer to create an unresolved topic. TreeNode and
// DocView both route their {@html} output's clicks through refClick, so a
// reference behaves identically in an outline and inside a document.
export type RefClick = { kind: "navigate"; id: string } | { kind: "create"; name: string }

export function refClick(forest: TreeNodeData[], aliases: AliasIndex, el: HTMLElement): RefClick | null {
	const targetKey = el.dataset.target ?? ""
	const isTopic = el.classList.contains("ref-topic")
	if (el.classList.contains("ref-missing")) {
		return isTopic ? { kind: "create", name: el.textContent ?? targetKey } : null
	}
	if (isTopic) {
		const root = findTopicRoot(forest, aliases, targetKey)
		return root ? { kind: "navigate", id: root.block.ID } : null
	}
	return findBlock(forest, targetKey) ? { kind: "navigate", id: targetKey } : null
}
