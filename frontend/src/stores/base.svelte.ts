import { Events } from "@wailsio/runtime";
import { SvelteMap, SvelteSet } from "svelte/reactivity";
import {
	type Block,
	type LinkedBlock,
	Relation,
	type Revision,
} from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
import { callError, messageFor } from "../lib/callerror";
import { buildTree, findBlock, findNode, findPath, type TreeNodeData, topicOf } from "../lib/tree";
import { changes } from "./changes.svelte";

// How long typing settles before a link-target query is sent, so search-as-
// you-type is not a backend call per keystroke. Mirrors the search store.
const linkQueryDebounceMs = 200;

// generationProgressEvent mirrors wailsbind.GenerationProgressEvent: the Go
// side emits it with a { phase, detail } payload while an AI generation runs.
const generationProgressEvent = "generation:progress";

// generationPhaseLabels turns a backend phase into the line shown while a
// generation is in flight; an unknown phase falls back to a generic message.
const generationPhaseLabels: Record<string, string> = {
	requesting: "sending the request…",
	awaiting: "the model is working…",
	parsing: "applying the response…",
};

// BaseStore is the knowledge base's view-model: browse the tree and drive the
// two AI flows from one command field. Expansion (no target) and restructuring
// (a targeted block) both write directly — every touched block lands flagged
// unreviewed, review is retroactive — then the base is re-read.
//
// It lives at module level so the view can unmount on a mode switch and come
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

	// The topic the main area shows. The sidebar picks it; a jump from search or
	// the changes list can move it (see reveal). null means the base is empty or
	// has not loaded yet.
	selectedTopicID = $state<string | null>(null);

	// Zoom-in: the block the main area treats as the page's root, so a deeply
	// nested branch can be read at full width instead of under six levels of
	// indent. null means the whole selected topic. A focus belongs to one
	// topic, so anything that moves the selection clears it (see
	// applySelection and reveal).
	focusID = $state<string | null>(null);

	// collapsed holds the ids of nodes whose children are hidden. The base
	// opens fully expanded; the reader folds away what they are not looking at.
	collapsed = new SvelteSet<string>();

	// ghosts holds the ids of blocks with an unreviewed pending deletion:
	// gone from the live blocks table but merged back into the forest (see
	// reload) so the tree can render them struck through in place, pending
	// Accept (drop for good) or Reject (restore).
	ghosts = new SvelteSet<string>();

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

	// The history panel: at most one block's revisions are loaded at a time,
	// newest first as BlockHistory returns them. Deleting revisions IS the
	// rollback mechanism (see internal/sqlite/revision.go), so this doubles as
	// the undo UI, not just a read-only log.
	historyID = $state<string | null>(null);
	revisions = $state<Revision[]>([]);

	// A reveal whose block is not yet in the forest, flushed at the end of reload.
	private pendingReveal: string | null = null;

	private linkQueryTimer: ReturnType<typeof setTimeout> | null = null;
	// linkQuerySeq stamps each link-target query so a slow earlier response
	// cannot overwrite a later query's results; only the newest seq is allowed
	// to land. Mirrors the search store's guard.
	private linkQuerySeq = 0;

	// historySeq stamps every BlockHistory fetch (open, switch, close, and
	// deleteRevision's own refresh) so a slow response for a panel the user
	// has since switched away from — or closed — cannot land and overwrite
	// another block's revisions. Same pattern as linkQuerySeq.
	private historySeq = 0;

	// selectSeq stamps every act that moves the main area's topic — a sidebar
	// or graph selection, and a jump from search or the changes list — so a
	// selection that had to re-read the base cannot land after the reader has
	// already moved somewhere else and drag them back. Same guard as
	// linkQuerySeq and historySeq, for the same reason.
	private selectSeq = 0;

	// The scope label is read out of the live forest rather than copied on
	// targeting: a targeted block that is edited or disappears can never leave
	// a stale label behind.
	scopeLabel = $derived(this.scopeID === null ? "" : (findBlock(this.forest, this.scopeID)?.Content ?? ""));

	// The tree the main area renders. Derived rather than stored so a reload
	// that rebuilds the forest cannot leave a stale subtree on screen.
	selectedTree = $derived(
		this.selectedTopicID === null ? null : (this.forest.find((t) => t.block.ID === this.selectedTopicID) ?? null),
	);

	// The subtree the main area renders: the focused block when there is one,
	// the whole topic otherwise. Derived rather than stored so a reload that
	// rebuilds the forest cannot leave a stale subtree on screen — the same
	// reason selectedTree is derived.
	viewRoot = $derived(this.focusID === null ? this.selectedTree : findNode(this.forest, this.focusID));

	constructor() {
		// Live generation progress: while a command is in flight (busy), reflect
		// the backend's phase in the status line. Events outside a generation are
		// ignored so a late one never overwrites an idle or already-final status.
		Events.On(generationProgressEvent, (e) => {
			if (!this.busy) return;
			const data = e.data as { phase?: string } | undefined;
			this.status = (data?.phase ? generationPhaseLabels[data.phase] : undefined) ?? "the model is working…";
		});
	}

	// activate re-reads the data once, for the window's lifetime: App.svelte
	// calls it from its own onMount; the UI state above persists across every
	// mode switch after that.
	activate(): void {
		this.reload().catch((err: unknown) => {
			this.error = messageFor(err);
		});
	}

	// reload reads every topic root and its full subtree, rebuilding the
	// forest, and merges in any pending deletion as a ghost row so it renders
	// in place rather than vanishing before it is reviewed.
	async reload(): Promise<void> {
		const topics = (await App.TopicMap()) ?? [];
		const roots = topics.filter((b) => b.ParentID === "");

		// Ghosts: a pending delete drops the block from GetSubtree, so it is
		// reconstructed from its unreviewed head revision and grouped by the
		// topic it belongs to, ready to splice into that topic's block list
		// before buildTree.
		await changes.refresh();
		this.ghosts.clear();
		const ghostsByTopic = new Map<string, Block[]>();
		for (const u of changes.blocks) {
			if (!u.Head.Deleted) {
				continue;
			}
			this.ghosts.add(u.Head.BlockID);
			const ghost: Block = {
				ID: u.Head.BlockID,
				ParentID: u.Head.ParentID,
				Position: u.Head.Position,
				Content: u.Head.Content,
				Meta: {},
				CreatedAt: "",
				UpdatedAt: "",
				Unreviewed: true,
			};
			const list = ghostsByTopic.get(u.TopicID) ?? [];
			list.push(ghost);
			ghostsByTopic.set(u.TopicID, list);
		}

		const trees: TreeNodeData[] = [];
		for (const r of roots) {
			const blocks = (await App.GetSubtree(r.ID)) ?? [];
			const extra = ghostsByTopic.get(r.ID) ?? [];
			ghostsByTopic.delete(r.ID);
			const tree = buildTree(extra.length === 0 ? blocks : [...blocks, ...extra]);
			if (tree) {
				trees.push(tree);
			}
		}
		// Whatever is left is a ghost topic root itself: its whole page was
		// deleted, so it has no live subtree to attach to and forms its own tree.
		for (const list of ghostsByTopic.values()) {
			const tree = buildTree(list);
			if (tree) {
				trees.push(tree);
			}
		}
		this.forest = trees;
		// A reload can drop the selected topic (its page was accepted away) or
		// arrive with nothing selected on first load. Fall back to the first
		// topic so the main area is never blank while the base has content.
		if (this.selectedTopicID === null || !trees.some((t) => t.block.ID === this.selectedTopicID)) {
			this.selectedTopicID = trees.length > 0 ? trees[0].block.ID : null;
		}
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
		// A history panel whose block vanished entirely (its last revision was
		// finalized away by Accept, not by deleteRevision's own not_found path)
		// closes rather than keep showing a dead block's stale history.
		if (this.historyID !== null && findBlock(this.forest, this.historyID) === null) {
			this.historyID = null;
			this.revisions = [];
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
		// A jump is an act of the reader's, so it outranks a selection still
		// waiting on a re-read — see selectSeq.
		this.selectSeq++;
		const path = findPath(this.forest, id);
		if (path === null) {
			this.pendingReveal = id;
			return;
		}
		this.pendingReveal = null;
		// A jump names a block somewhere in the forest, and staying zoomed into
		// an unrelated subtree would scroll to a row that is not rendered.
		this.focusID = null;
		// The main area shows one topic, so a jump into a different one has to
		// move the selection first or it would scroll to a row that is not
		// rendered.
		const topic = topicOf(this.forest, id);
		// findPath above already located id in this.forest, so topicOf cannot
		// return null here; the guard is defensive, not load-bearing.
		if (topic !== null) {
			this.selectedTopicID = topic;
		}
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

	// focus zooms the main area into one block. An id absent from the forest is
	// ignored rather than blanking the pane: the caller is markup that cannot
	// await a reload, so a stale id is a real case, not a programming error.
	focus(id: string): void {
		if (findNode(this.forest, id) === null) {
			return;
		}
		this.focusID = id;
	}

	clearFocus(): void {
		this.focusID = null;
	}

	// selectTopic switches the main area to the given topic. The sidebar is
	// the usual caller, and it reads ids straight out of this.forest, so they
	// are always already present. The topic graph is not: it re-reads on
	// every entry into graph mode, while the forest only refreshes on a GUI
	// mutation (see reload), so a base written from outside the GUI —
	// `comind mcp` adding or deleting a topic while the reader is looking at
	// the graph — hands this an id the forest has never seen, or one the
	// forest's own last read has since dropped. Assigning selectedTopicID
	// unconditionally would leave selectedTree null for an id that is
	// perfectly real, mislabelling the pane "no topic selected". Re-reading
	// once before giving up distinguishes that from a topic that is actually
	// gone, without ever pointing the selection at an id this store cannot
	// currently show.
	async selectTopic(id: string): Promise<void> {
		const mine = ++this.selectSeq;
		if (this.forest.some((t) => t.block.ID === id)) {
			this.applySelection(id);
			return;
		}
		try {
			await this.reload();
		} catch (err: unknown) {
			// Callers fire this from markup and cannot await it, so a rejection
			// here has nobody to surface it: without this the reader clicks a
			// node, stays on the topic they were already looking at, and is told
			// nothing at all.
			if (mine === this.selectSeq) {
				this.error = messageFor(err);
			}
			return;
		}
		// Anything the reader did while the re-read was in flight outranks it —
		// a second click, or a jump from search — or this would drag the
		// selection back to where they no longer are.
		if (mine !== this.selectSeq) {
			return;
		}
		if (this.forest.some((t) => t.block.ID === id)) {
			this.applySelection(id);
			return;
		}
		this.error = "That topic no longer exists.";
	}

	private applySelection(id: string): void {
		this.selectedTopicID = id;
		// A selection that lands clears whatever the last one reported: the
		// "no longer exists" message above outlives its own click otherwise,
		// and would sit under a topic the reader is now looking at, describing
		// a different one they are not.
		this.error = "";
		// A flash armed for a row in the previously selected topic must not
		// replay against whatever row now occupies that position after the
		// switch — see highlightID.
		this.highlightID = null;
		// A focus belongs to one topic; a switch away must not leave the view
		// zoomed into a subtree of a topic nobody is looking at any more.
		this.focusID = null;
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
	// block/page otherwise. Both write directly — every touched block lands
	// flagged unreviewed rather than queued as a proposal — so the base is
	// simply re-read afterwards. An expansion also moves the main-area
	// selection onto the newcomer: the reader asked for a new topic, and
	// leaving the view on the old one would give them nothing to look at.
	async askAI(): Promise<void> {
		const text = this.command.trim();
		// Locked mid inline-edit like every other tree action: reload moves the
		// forest under an open editor, so submitting here would silently drop
		// an open edit.
		if (text === "" || this.busy || this.editingID !== null) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			const scope = this.scopeID;
			this.status = scope === null ? `expanding "${text}"…` : "asking the AI to restructure…";
			if (scope === null) {
				await App.ExpandTopic(text, "");
			} else {
				await App.EditWithAI(scope, text);
			}
			this.command = "";
			this.clearScope();
			// Reload rebuilds the forest; diff the topic-root ids against what was
			// there before to find the fresh one an expansion created. reload's own
			// selection fallback only fires when the current selection is invalid —
			// here it is still valid, so without this the view would stay on the
			// topic the reader was already looking at.
			const before = new Set(this.forest.map((t) => t.block.ID));
			await this.reload();
			if (scope === null) {
				const fresh = this.forest.find((t) => !before.has(t.block.ID));
				if (fresh) {
					this.selectedTopicID = fresh.block.ID;
				}
			}
			this.status = "changes applied, awaiting review";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
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

	// accept marks a block's pending revisions reviewed: a live block keeps its
	// new content, a ghost's tombstone becomes final and it drops out of the
	// tree for good.
	async accept(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			await App.AcceptBlock(id);
			await this.reload();
			this.status = "accepted";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// reject rolls a block back to its last reviewed state: a live block's AI
	// edit is undone (an AI-added block disappears entirely), a ghost's
	// pending deletion is undone and the block is restored.
	async reject(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			await App.RejectBlock(id);
			await this.reload();
			this.status = "rejected";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// acceptPage marks every pending revision under a topic reviewed in one step.
	async acceptPage(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			await App.AcceptPage(id);
			await this.reload();
			this.status = "accepted page";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// rejectPage rolls every pending block under a topic back to its last
	// reviewed state in one step.
	async rejectPage(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			await App.RejectPage(id);
			await this.reload();
			this.status = "rejected page";
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

	// toggleHistory opens or closes a block's revision-history panel, loading
	// the revisions on open. Allowed on ghosts too: a pending deletion's
	// history is exactly how its removal is inspected or undone before review.
	// Bumping historySeq on every call — open, switch, or close — invalidates
	// whatever fetch a previous call started, so a slow response for a panel
	// the user has since switched away from can never land and overwrite the
	// now-current block's revisions.
	async toggleHistory(id: string): Promise<void> {
		this.historySeq++;
		if (this.historyID === id) {
			this.historyID = null;
			this.revisions = [];
			return;
		}
		this.historyID = id;
		this.error = "";
		const mine = this.historySeq;
		try {
			const revs = (await App.BlockHistory(id)) ?? [];
			if (mine !== this.historySeq) {
				return;
			}
			this.revisions = revs;
		} catch (err: unknown) {
			if (mine !== this.historySeq) {
				return;
			}
			this.error = messageFor(err);
			this.revisions = [];
		}
	}

	// deleteRevision removes one revision from a block's history — the
	// rollback mechanism. Deleting the head reverts the block to the previous
	// revision; deleting the last remaining revision removes the block
	// entirely, in which case the follow-up BlockHistory errors not_found —
	// that closes the panel rather than surfacing as an error. An invalid
	// rollback (a live parent or children in the way) is refused by the
	// backend with a message surfaced verbatim in base.error. The refresh is
	// stamped with historySeq too, like toggleHistory: history-toggle buttons
	// are disabled while busy so a panel switch cannot race this in practice,
	// but the guard keeps the two call sites' invariant in one place rather
	// than relying on that UI lock.
	async deleteRevision(blockID: string, revID: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		this.error = "";
		const mine = ++this.historySeq;
		try {
			await App.DeleteRevision(blockID, revID);
			try {
				const revs = (await App.BlockHistory(blockID)) ?? [];
				if (mine === this.historySeq) {
					this.revisions = revs;
				}
			} catch (err: unknown) {
				if (callError(err).code !== "not_found") {
					throw err;
				}
				if (mine === this.historySeq) {
					this.historyID = null;
					this.revisions = [];
				}
			}
			await this.reload();
			await changes.refresh();
			this.status = "deleted revision";
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
