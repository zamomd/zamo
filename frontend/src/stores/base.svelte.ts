import { SvelteMap, SvelteSet } from "svelte/reactivity";
import {
	type Block,
	type LinkedBlock,
	Relation,
} from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { messageFor } from "../lib/callerror";
import { buildTree, findBlock, findPath, type TreeNodeData } from "../lib/tree";

// How long typing settles before a link-target query is sent, so search-as-
// you-type is not a backend call per keystroke. Mirrors the search store.
const linkQueryDebounceMs = 200;

// BaseStore is the Base tab's view-model: browse the tree and drive the two AI
// flows from one command field. Expansion (no target) and restructuring (a
// targeted block) both queue a proposal for review — never a direct write —
// then the base is re-read.
//
// It lives at module level so the view can unmount on a tab switch and come
// back to the same place: the fold state, the targeted block, a half-typed
// command and an open edit all survive; only the data is re-read on return.
class BaseStore {
	// One command field feeds both AI flows. The target decides which: none
	// means expand a new topic from scratch, a block/page means ask the AI to
	// restructure it. scopeID null is the no-target (expansion) case.
	command = $state("");
	scopeID = $state<string | null>(null);
	status = $state("");
	error = $state("");
	busy = $state(false);
	forest = $state<TreeNodeData[]>([]);

	// collapsed holds the ids of nodes whose children are hidden. The base
	// opens fully expanded; the reader folds away what they are not looking at.
	collapsed = new SvelteSet<string>();

	// Inline block editing: the owner's own edits apply straight to the base,
	// bypassing the review queue (review guards against AI, not the owner).
	editingID = $state<string | null>(null);
	editText = $state("");

	// Jump-to-block target for a search result. highlightID names the row to
	// flash and scroll to; highlightNonce is bumped on every reveal so a second
	// jump to the same row re-triggers the one-shot flash the id alone would not.
	highlightID = $state<string | null>(null);
	highlightNonce = $state(0);

	// Typed links, loaded lazily per open panel. openLinks holds the blocks
	// whose panels are open; linksById caches what GetLinks returned. Both are
	// rebuilt on reload: an edit can change a neighbour's text.
	openLinks = new SvelteSet<string>();
	linksById = new SvelteMap<string, LinkedBlock[]>();

	// The +link picker: the block a link is created from, the live target
	// search, and the relation to record.
	linkingID = $state<string | null>(null);
	linkQuery = $state("");
	linkResults = $state<Block[]>([]);
	linkRelation = $state<Relation>(Relation.RelationRelated);

	// A reveal whose block is not yet in the forest, flushed at the end of reload.
	private pendingReveal: string | null = null;

	private linkQueryTimer: ReturnType<typeof setTimeout> | null = null;
	// linkQuerySeq stamps each link-target query so a slow earlier response
	// cannot overwrite a later query's results; only the newest seq is allowed
	// to land. Mirrors the search store's guard.
	private linkQuerySeq = 0;

	// The scope label is read out of the live forest rather than copied on
	// targeting: a targeted block that is edited or disappears can never leave
	// a stale label behind.
	scopeLabel = $derived(this.scopeID === null ? "" : (findBlock(this.forest, this.scopeID)?.Content ?? ""));

	// activate re-reads the data on every mount; the UI state above persists.
	activate(): void {
		this.reload().catch((err: unknown) => {
			this.error = messageFor(err);
		});
	}

	// reload reads every topic root and its full subtree, rebuilding the forest.
	async reload(): Promise<void> {
		const topics = (await App.TopicMap()) ?? [];
		const roots = topics.filter((b) => b.ParentID === "");
		const trees: TreeNodeData[] = [];
		for (const r of roots) {
			const blocks = (await App.GetSubtree(r.ID)) ?? [];
			const tree = buildTree(blocks);
			if (tree) {
				trees.push(tree);
			}
		}
		this.forest = trees;
		// Links reflect the re-read base: drop the cache, close panels whose
		// block vanished, and re-load the panels still open.
		this.linksById.clear();
		for (const id of [...this.openLinks]) {
			if (findBlock(this.forest, id) === null) {
				this.openLinks.delete(id);
				continue;
			}
			await this.refreshLinks(id);
		}
		// Apply a reveal that raced the initial load. reveal re-parks it if the
		// block is still absent, so a missing target self-heals on a later reload.
		if (this.pendingReveal !== null) {
			const id = this.pendingReveal;
			this.pendingReveal = null;
			this.reveal(id);
		}
	}

	// reveal un-collapses a block's ancestors and arms its flash. It only ever
	// expands, never collapses, so unlike toggle it is safe with an inline edit
	// open: it cannot unmount a live editor. A block absent from the forest is
	// parked (see pendingReveal) rather than dropped, for the first-mount race
	// where the field is usable before reload has built the forest.
	reveal(id: string): void {
		const path = findPath(this.forest, id);
		if (path === null) {
			this.pendingReveal = id;
			return;
		}
		this.pendingReveal = null;
		for (const ancestor of path) {
			this.collapsed.delete(ancestor);
		}
		this.highlightID = id;
		this.highlightNonce++;
	}

	toggle(id: string): void {
		if (this.collapsed.has(id)) {
			this.collapsed.delete(id);
		} else {
			this.collapsed.add(id);
		}
	}

	// setScope toggles: targeting the already-targeted row clears it, which is
	// what the button's aria-pressed state promises.
	setScope(block: Block): void {
		// Ignore while busy or mid inline-edit, mirroring the tree's other actions.
		if (this.busy || this.editingID !== null) {
			return;
		}
		this.scopeID = this.scopeID === block.ID ? null : block.ID;
	}

	clearScope(): void {
		this.scopeID = null;
	}

	// askAI routes the one command field to the matching AI flow: a new-topic
	// expansion when nothing is targeted, a structure edit on the targeted
	// block/page otherwise. Both return a proposal that lands in the review
	// queue, never a direct write. Reports whether one was queued, so the
	// caller can hand focus to the queue.
	async askAI(): Promise<boolean> {
		const text = this.command.trim();
		// Locked mid inline-edit like every other tree action: queueing moves
		// focus off this view, so submitting here would silently drop an open
		// edit.
		if (text === "" || this.busy || this.editingID !== null) {
			return false;
		}
		this.busy = true;
		this.error = "";
		try {
			const scope = this.scopeID;
			this.status = scope === null ? `expanding "${text}"…` : "asking the AI to restructure…";
			const proposal = scope === null ? await App.ExpandTopic(text, "") : await App.ProposeEdit(scope, text);
			const ops = (proposal.Ops ?? []).length;
			this.status =
				scope === null ? `queued "${text}" for review (${ops} ops)` : `queued the restructure for review (${ops} ops)`;
			this.command = "";
			this.clearScope();
			return true;
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
			return false;
		} finally {
			this.busy = false;
		}
	}

	startEdit(block: Block): void {
		// Ignore while busy or while another block is already being edited, so
		// an in-progress edit is never silently discarded by switching blocks.
		if (this.busy || this.editingID !== null) {
			return;
		}
		this.editingID = block.ID;
		this.editText = block.Content;
	}

	cancelEdit(): void {
		this.editingID = null;
		this.editText = "";
	}

	// saveEdit writes the block's new content directly, then re-reads the base.
	async saveEdit(): Promise<void> {
		if (this.editingID === null || this.busy) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			await App.EditBlockContent(this.editingID, this.editText);
			await this.reload();
			this.status = "saved edit";
			this.cancelEdit();
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// toggleLinks opens or closes a block's links panel, loading the links on
	// first open. Closing also drops a picker opened from that panel.
	async toggleLinks(id: string): Promise<void> {
		if (this.openLinks.has(id)) {
			this.openLinks.delete(id);
			if (this.linkingID === id) {
				this.cancelLink();
			}
			// Link writes refresh only open panels, so this entry can go stale
			// while closed; drop it so reopening re-fetches.
			this.linksById.delete(id);
			return;
		}
		this.openLinks.add(id);
		if (!this.linksById.has(id)) {
			await this.refreshLinks(id);
		}
	}

	async refreshLinks(id: string): Promise<void> {
		try {
			this.linksById.set(id, (await App.GetLinks(id)) ?? []);
		} catch (err: unknown) {
			this.error = messageFor(err);
		}
	}

	startLink(id: string): void {
		// Locked while busy or mid inline-edit, like the tree's other actions.
		if (this.busy || this.editingID !== null) {
			return;
		}
		this.linkingID = id;
		this.linkQuery = "";
		this.linkResults = [];
		this.linkRelation = Relation.RelationRelated;
		// Invalidate any in-flight or pending query from a previous picker.
		this.linkQuerySeq++;
		if (this.linkQueryTimer !== null) {
			clearTimeout(this.linkQueryTimer);
			this.linkQueryTimer = null;
		}
	}

	cancelLink(): void {
		this.linkingID = null;
		this.linkQuery = "";
		this.linkResults = [];
		// A response landing after close must not resurrect the results.
		this.linkQuerySeq++;
		if (this.linkQueryTimer !== null) {
			clearTimeout(this.linkQueryTimer);
			this.linkQueryTimer = null;
		}
	}

	// setLinkQuery live-searches for a link target; the source block is
	// excluded — a block cannot link to itself. Debounced and sequenced like
	// the search store, so a slow earlier response cannot overwrite a later
	// query's results.
	setLinkQuery(q: string): void {
		this.linkQuery = q;
		this.linkQuerySeq++;
		this.linkResults = [];
		if (this.linkQueryTimer !== null) {
			clearTimeout(this.linkQueryTimer);
			this.linkQueryTimer = null;
		}
		if (q.trim() === "") {
			return;
		}
		this.linkQueryTimer = setTimeout(() => {
			this.linkQueryTimer = null;
			void this.runLinkQuery();
		}, linkQueryDebounceMs);
	}

	private async runLinkQuery(): Promise<void> {
		const mine = ++this.linkQuerySeq;
		const q = this.linkQuery.trim();
		try {
			const hits = (await App.Search(q)) ?? [];
			if (mine !== this.linkQuerySeq) {
				return;
			}
			this.linkResults = hits.filter((b) => b.ID !== this.linkingID);
		} catch (err: unknown) {
			if (mine !== this.linkQuerySeq) {
				return;
			}
			this.error = messageFor(err);
		}
	}

	// createLink records the picker's link and refreshes both endpoints' open
	// panels.
	async createLink(target: Block): Promise<void> {
		if (this.linkingID === null || this.busy) {
			return;
		}
		const from = this.linkingID;
		this.busy = true;
		this.error = "";
		try {
			await App.CreateLink(from, target.ID, this.linkRelation);
			this.cancelLink();
			await this.refreshLinks(from);
			if (this.openLinks.has(target.ID)) {
				await this.refreshLinks(target.ID);
			}
			this.status = "linked";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// removeLink deletes one link from a block's panel, refreshing both
	// endpoints' open panels. The edge identity is rebuilt from the panel's
	// view: an outgoing entry keeps the panel's block as the source, an
	// incoming one flips it.
	async removeLink(id: string, link: LinkedBlock): Promise<void> {
		if (this.busy) {
			return;
		}
		const from = link.Outgoing ? id : link.Block.ID;
		const to = link.Outgoing ? link.Block.ID : id;
		this.busy = true;
		this.error = "";
		try {
			await App.RemoveLink(from, to, link.Relation);
			await this.refreshLinks(id);
			if (this.openLinks.has(link.Block.ID)) {
				await this.refreshLinks(link.Block.ID);
			}
			this.status = "unlinked";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}
}

// base is the single window-wide instance; module state outlives the views
// that render it.
export const base = new BaseStore();
