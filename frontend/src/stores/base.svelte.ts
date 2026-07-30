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
import { lineCount, offsetAtLine } from "../lib/caret";
import {
	buildTree,
	findBlock,
	findNode,
	findPath,
	nextVisible,
	prevSibling,
	prevVisible,
	type TreeNodeData,
	topicOf,
} from "../lib/tree";
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
	// topic: every write to selectedTopicID is expected to clear this in the
	// same act (applySelection, reveal, reload's fallback, askAI's post-
	// expansion move), so a future selection write that skips it reopens a
	// bug rather than a documented gap.
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
	// Where the caret is. It lives in the store, not in the component, because
	// the component is destroyed and rebuilt under the caret it is supposed to
	// keep: a collapse, a zoom or a re-read unmounts the row, and every
	// structural gesture hands the session to a *different* block, whose editor
	// is a fresh instance with no memory of its own.
	//
	// Two writers, and the difference between them is the nonce: the store places
	// the caret (a gesture's landing, an opening click) and announces it, while
	// the editor reports the reader's own movements through syncCaret and
	// announces nothing. Both are needed — a mount restores this offset, so an
	// offset that only ever recorded where the session opened would throw the
	// reader back to their first click every time the editor remounts.
	//
	// editCaretNonce is bumped on every placement so the editor's effect fires
	// even when the offset repeats — a merge and the arrow both land on offsets
	// the previous session may already have been at, and an unchanged number is
	// not an event. Same idiom, and the same reason, as highlightNonce below.
	//
	// The invariant both halves serve: when the editor mounts, or when a caret is
	// placed into a mounted one, editText is already the final text. Assigning
	// editText afterwards would move the caret to the end regardless of what was
	// placed — bind:value writes the textarea's value, and the HTML spec puts the
	// caret at the end of a text control whose value is written.
	editCaret = $state(0);
	editCaretNonce = $state(0);

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
	// reason selectedTree is derived. Falls back to selectedTree rather than
	// going null when the focused block itself vanishes (an edit or a
	// concurrent comind mcp write deletes it before the topic changes): an
	// absent id is ignored, never a blank pane, same principle as focus().
	viewRoot = $derived(
		this.focusID === null ? this.selectedTree : (findNode(this.forest, this.focusID) ?? this.selectedTree),
	);

	// The rows a gesture may reach. Navigation and merges are scoped to the view's
	// own root, which does three things at once: a gesture cannot wander into
	// another topic, it cannot escape a zoom, and the root is never a merge target
	// — so Backspace in the first block of a page is the silent no-op the spec
	// asks for rather than a refusal from the domain.
	visibleNodes = $derived<TreeNodeData[]>(this.viewRoot === null ? [] : [this.viewRoot]);

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

	// ghostsFor rebuilds one topic's pending deletions as rows. A deleted block
	// drops out of GetSubtree, so it is reconstructed from its unreviewed head
	// revision and spliced back in before buildTree — otherwise a pending
	// deletion would vanish from the page before anyone reviewed it.
	//
	// It reads the queue already in memory rather than re-fetching it: only an AI
	// write changes that queue, and this path serves owner writes, which are born
	// reviewed.
	private ghostsFor(topicID: string): Block[] {
		const out: Block[] = [];
		for (const u of changes.blocks) {
			if (!u.Head.Deleted || u.TopicID !== topicID) {
				continue;
			}
			out.push({
				ID: u.Head.BlockID,
				ParentID: u.Head.ParentID,
				Position: u.Head.Position,
				Content: u.Head.Content,
				Meta: {},
				CreatedAt: "",
				UpdatedAt: "",
				Unreviewed: true,
			});
		}
		return out;
	}

	// reload reads every topic root and its full subtree, rebuilding the
	// forest, and merges in any pending deletion as a ghost row so it renders
	// in place rather than vanishing before it is reviewed.
	async reload(): Promise<void> {
		const topics = (await App.TopicMap()) ?? [];
		const roots = topics.filter((b) => b.ParentID === "");

		// changes.blocks backs ghostsFor below; refresh it once per whole-base
		// reload rather than once per topic. this.ghosts and the set of topics
		// that need a ghost-only root (see below) are collected in the same pass.
		await changes.refresh();
		this.ghosts.clear();
		const ghostTopics = new Set<string>();
		for (const u of changes.blocks) {
			if (!u.Head.Deleted) {
				continue;
			}
			this.ghosts.add(u.Head.BlockID);
			ghostTopics.add(u.TopicID);
		}

		const trees: TreeNodeData[] = [];
		for (const r of roots) {
			const blocks = (await App.GetSubtree(r.ID)) ?? [];
			const extra = this.ghostsFor(r.ID);
			ghostTopics.delete(r.ID);
			const tree = buildTree(extra.length === 0 ? blocks : [...blocks, ...extra]);
			if (tree) {
				trees.push(tree);
			}
		}
		// Whatever topic remains is a ghost topic root itself: its whole page was
		// deleted, so it has no live subtree to attach to and forms its own tree.
		for (const topicID of ghostTopics) {
			const tree = buildTree(this.ghostsFor(topicID));
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
			// The topic just moved out from under the reader; a focus from the old
			// one would otherwise still resolve inside the new selection's forest.
			this.focusID = null;
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

	// reloadTopic re-reads one topic and swaps its tree in place. An owner write
	// touches one topic and cannot change the review queue, so the full reload's
	// TopicMap + a GetSubtree per topic + changes.refresh is the wrong amount of
	// work for a keystroke. reload() stays for the AI flows, mode entry and
	// error recovery, where all of that really can change.
	async reloadTopic(topicID: string): Promise<void> {
		if (this.forest.findIndex((t) => t.block.ID === topicID) < 0) {
			return;
		}
		const blocks = (await App.GetSubtree(topicID)) ?? [];
		// The forest can be replaced while the read is in flight — an AI flow's
		// full reload, or another gesture's narrow one — so the slot is located
		// again here rather than before the await.
		const i = this.forest.findIndex((t) => t.block.ID === topicID);
		if (i < 0) {
			return;
		}
		const extra = this.ghostsFor(topicID);
		const tree = buildTree(extra.length === 0 ? blocks : [...blocks, ...extra]);
		if (tree === null) {
			// The topic itself is gone — nothing to render from, so fall back the
			// same way reload does rather than leaving a hole in the forest. It is
			// reachable from a keystroke: Backspace merging away a topic's last
			// block takes the topic with it.
			this.forest.splice(i, 1);
			if (this.selectedTopicID === topicID) {
				this.selectedTopicID = this.forest.length > 0 ? this.forest[0].block.ID : null;
				this.focusID = null;
			}
			// Every block of that topic went with it, so the panels keyed to one of
			// them have nothing left to show. reload does this for the whole base;
			// this is the same cleanup narrowed to what one topic's disappearance
			// can invalidate.
			for (const id of [...this.openLinks]) {
				if (findBlock(this.forest, id) === null) {
					this.openLinks.delete(id);
					this.linksById.delete(id);
				}
			}
			if (this.historyID !== null && findBlock(this.forest, this.historyID) === null) {
				this.historyID = null;
				this.revisions = [];
			}
			return;
		}
		this.forest[i] = tree;
	}

	// refresh re-reads one topic after an act that has already landed in storage,
	// and reports a failure through this.error instead of throwing.
	//
	// That is the whole of it: by the time this runs the act's verdict is settled,
	// and a failing re-read must not be allowed to look like the act failing. The
	// two are opposite events — the act's failure means storage is untouched and
	// the session is kept so the reader can try again, while a failed re-read
	// means storage took the change and only this cache is behind. Turning the
	// second into the first tells the reader a write did not happen when it did,
	// and abandons the caret landing or the caller's own gesture on top of it.
	//
	// A null topic is the block whose topic could not be resolved — nothing to
	// re-read, and not an error.
	private async refresh(topic: string | null): Promise<void> {
		if (topic === null) {
			return;
		}
		try {
			await this.reloadTopic(topic);
		} catch (err: unknown) {
			this.error = messageFor(err);
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

	// toggle folds a node's children away, or unfolds them again. Collapsing an
	// ancestor can unmount the row holding an open editor, so it commits first —
	// see commitEdit's own comment for why every caller that can unmount the
	// editor goes through it.
	async toggle(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		if (!(await this.commitEdit())) {
			return;
		}
		if (this.collapsed.has(id)) {
			this.collapsed.delete(id);
		} else {
			this.collapsed.add(id);
		}
	}

	// focus zooms the main area into one block. An id absent from the forest is
	// ignored rather than blanking the pane: the caller is markup that cannot
	// await a reload, so a stale id is a real case, not a programming error.
	async focus(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		// Zooming in unmounts every row outside the new root, so it commits first
		// like toggle — see commitEdit's own comment.
		if (!(await this.commitEdit())) {
			return;
		}
		if (findNode(this.forest, id) === null) {
			return;
		}
		// Zooming in is a request to see inside, so it cannot leave the block
		// folded: the view's root renders as the page's title row, which carries
		// no collapse triangle, so a block collapsed before the zoom would arrive
		// as a lone title on a blank page with nothing to open it — while its own
		// dot's halo advertised the children it was hiding.
		this.collapsed.delete(id);
		this.focusID = id;
	}

	// clearFocus zooms back out to the whole topic. It commits nothing, unlike
	// focus: widening the view only adds rows, so an open editor's row stays
	// mounted (it re-mounts one level deeper with editText intact), and toggle's
	// guard above is what keeps a collapsed node from appearing between the topic
	// root and an open editor in the first place. It still turns back while a
	// write is in flight — see commitEdit's own comment: that re-mount restores
	// the caret and the text the store holds mid-gesture, and neither is final
	// until the re-read lands.
	clearFocus(): void {
		if (this.busy) {
			return;
		}
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
		if (this.busy) {
			return;
		}
		// Switching topic unmounts the whole tree the open editor's row lives in,
		// so it commits first — see commitEdit's own comment.
		if (!(await this.commitEdit())) {
			return;
		}
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
	async setScope(block: Block): Promise<void> {
		// Ignore while busy, mirroring the tree's other actions; an open edit
		// commits instead of blocking — see commitEdit's own comment.
		if (this.busy) {
			return;
		}
		// busy goes up before the commit, not after: the commit's own write is
		// already in flight, so the control greys out for its whole duration
		// rather than staying clickable until the state change lands.
		this.busy = true;
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
		this.scopeID = this.scopeID === block.ID ? null : block.ID;
		this.busy = false;
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
		if (text === "" || this.busy) {
			return;
		}
		// busy goes up before the commit, not after: the commit's own write is
		// already in flight, so the send control greys out for its whole
		// duration rather than staying clickable until ExpandTopic/EditWithAI
		// starts.
		this.busy = true;
		// reload moves the forest under an open editor, so an open edit commits
		// first — see commitEdit's own comment.
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
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
					// This move onto the newcomer bypasses reload's own fallback
					// above (the prior selection was still valid), so it has to
					// clear the focus itself or viewRoot would keep resolving
					// against the topic the reader just left.
					this.focusID = null;
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

	// startEdit puts one block into edit with the caret at a known offset.
	// Opening a different block commits the current one first, which is what
	// makes "click another block" a save rather than a discard.
	async startEdit(block: Block, caret: number): Promise<void> {
		if (this.editingID !== null && this.editingID !== block.ID) {
			// A failed write keeps the previous session open (spec §8); opening a
			// different block on top of it would abandon the text just as surely
			// as collapsing its ancestor would, so this bails rather than proceeds.
			if (!(await this.commitEdit())) {
				return;
			}
		}
		// commitEdit re-read the topic, so the caller's copy of the block may be
		// one generation old; its content is what the editor round-trips.
		const fresh = findBlock(this.forest, block.ID) ?? block;
		// Text first, then the session and the caret: the editor mounts on the
		// assignment to editingID, and it must find the final text already there.
		this.editText = fresh.Content;
		this.editingID = fresh.ID;
		this.editCaret = caret;
		this.editCaretNonce++;
	}

	// clearEdit drops the session without writing. It is the tail of a successful
	// commit and of a block that no longer exists — never a way to discard the
	// reader's typing, which is why there is no Cancel any more.
	clearEdit(): void {
		this.editingID = null;
		this.editText = "";
		this.editCaret = 0;
		this.editCaretNonce++;
	}

	// syncCaret records where the reader has just moved the caret, reported by
	// the editor from the DOM. It deliberately does not bump editCaretNonce: a
	// nonce bump is an announcement the editor acts on, and this travels the
	// other way — it copies an offset the textarea already shows. That is also
	// why it cannot fight a placement: the value it writes is whatever the last
	// setSelectionRange put there.
	//
	// The id is the reporting editor's own. A structural gesture hands the
	// session to a different block, and the outgoing instance can still emit an
	// event on its way out; that report belongs to a session that is over.
	syncCaret(id: string, offset: number): void {
		if (this.editingID !== id) {
			return;
		}
		this.editCaret = offset;
	}

	// writeIfChanged persists editText when it differs from what storage holds,
	// and reports whether it wrote. Unchanged text produces no write and no
	// revision: one editing session is meant to leave exactly one revision, and
	// leaving a block without touching it should leave none.
	//
	// A write that lands is copied into the forest. That comparison above is the
	// only thing between one session and two revisions, and it is made against the
	// forest, which is a cache of storage that nothing but a re-read refreshes —
	// so every path that writes and then fails before reaching its re-read (a
	// structural op that throws behind the write, an arrow whose re-read fails)
	// would otherwise leave the cache asserting a difference storage no longer
	// has, and the session's next commit would issue a second EditBlockContent.
	//
	// The direction is what makes this sound: App.EditBlockContent has returned,
	// so storage holds this text and the cache is being brought into line with
	// what actually landed. Writing a block into the forest *ahead* of a write —
	// a caller's own copy, on the way into an edit — is the opposite act and is
	// wrong for the opposite reason: it puts a snapshot on screen that no write
	// stands behind, and hands the comparison the caller instead of storage.
	//
	// The text is captured before the await rather than read twice: nothing stops
	// the reader typing while a commit's write is in flight, and the cache must
	// record what storage took, not what the editor has since become.
	//
	// It throws on failure so a structural gesture can abandon itself with the
	// text still in hand.
	private async writeIfChanged(id: string): Promise<boolean> {
		const stored = findBlock(this.forest, id);
		const text = this.editText;
		if (stored === null || stored.Content === text) {
			return false;
		}
		await App.EditBlockContent(id, text);
		stored.Content = text;
		return true;
	}

	// The view's root is the page itself — the topic's title row, or the block a
	// zoom is anchored to. It has no siblings to move among, nothing to be
	// indented under and nothing to absorb, so no structural gesture applies to
	// it. Its caret is legitimate (editing that row is how a topic is renamed);
	// only the structure is fixed. Two of the six already refuse it because
	// prevSibling and prevVisible are null at index 0 — this states the rule
	// instead of relying on where the root happens to sit in the order.
	private isViewRoot(id: string): boolean {
		return id === this.viewRoot?.block.ID;
	}

	// A ghost is the second row that is visible but is not a legal target, and
	// the reason the walks in lib/tree.ts take an exclusion set at all. It is a
	// tombstone the reader can still see: storage has dropped the block, and it
	// is rebuilt into the forest only so a pending deletion can be reviewed in
	// place (see ghosts and ghostsFor). Every gesture that names a row by id —
	// merge into it, indent under it, put the caret in it — would therefore name
	// a block storage does not have, so this.ghosts goes to every walk below.
	// The view's root is refused by name in each gesture because there is exactly
	// one of it; ghosts are refused inside the walk because there can be any
	// number of them, anywhere in the order.

	// splitAtCaret is Enter with the caret inside a block: this block keeps the
	// head, a new sibling directly below takes the tail, and the caret follows the
	// tail. The two halves are siblings even when the block has children — a split
	// makes two equal parts, and the tail must not land a level deeper than the
	// head.
	//
	// SplitBlock carries both halves in its arguments, so there is nothing to
	// write first: the editor's text goes straight into the call.
	//
	// The re-read shares the operation's catch here, unlike structural and
	// moveCaret, which separate them: the block this lands the caret on is the one
	// SplitBlock has just created, so a forest whose refresh failed cannot contain
	// it, and moving the session there would point the editor at a row nothing
	// renders. Staying on the un-split text is the recoverable outcome, and the
	// next successful re-read shows the split.
	//
	// end is where a selection stops. The run [caret, end) is what the split
	// replaces, so Enter over a selection drops it the way it does in any other
	// text field instead of leaving a copy in each half. Without a selection it is
	// the caret itself and nothing is dropped, which is why it defaults to it.
	async splitAtCaret(id: string, caret: number, end: number = caret): Promise<void> {
		if (this.busy || this.isViewRoot(id)) {
			return;
		}
		const topic = topicOf(this.forest, id);
		const text = this.editText;
		const tail = text.slice(end);
		this.busy = true;
		this.error = "";
		try {
			const newID = await App.SplitBlock(id, text.slice(0, caret), tail);
			if (topic !== null) {
				await this.reloadTopic(topic);
			}
			// After the re-read, so the row holding the tail exists before the
			// session moves to it and the editor mounts on the tail already in
			// place — see editCaret.
			this.editText = tail;
			this.editingID = newID;
			this.editCaret = 0;
			this.editCaretNonce++;
		} catch (err: unknown) {
			this.error = messageFor(err);
		} finally {
			this.busy = false;
		}
	}

	// insertAbove is Enter with the caret at the very start of a non-empty block:
	// an empty sibling appears above and this block is not touched, so it keeps
	// its id — and with it its revision history, its review state and its links.
	// The caret stays where the reader left it.
	async insertAbove(id: string): Promise<void> {
		if (this.busy || this.isViewRoot(id)) {
			return;
		}
		const topic = topicOf(this.forest, id);
		this.busy = true;
		this.error = "";
		try {
			await App.InsertBlockBefore(id, "");
			if (topic !== null) {
				await this.reloadTopic(topic);
			}
		} catch (err: unknown) {
			this.error = messageFor(err);
		} finally {
			this.busy = false;
		}
	}

	// indent is Tab: this block becomes the last child of the sibling above it,
	// carrying its own children along. No previous sibling means nothing to indent
	// under — a silent no-op, and deliberately not a call: the client answers
	// "does this gesture apply here" itself, so an error from the bridge always
	// means something actually went wrong.
	async indent(id: string): Promise<void> {
		if (this.busy || this.isViewRoot(id) || prevSibling(this.visibleNodes, id, this.ghosts) === null) {
			return;
		}
		await this.structural(id, () => App.IndentBlock(id));
	}

	// outdent is Shift+Tab: this block becomes its parent's next sibling, carrying
	// its own children but leaving its former following siblings where they are —
	// the gesture moves one block, it does not re-cut the branch. A child of the
	// view's root has nowhere to go.
	async outdent(id: string): Promise<void> {
		const node = findNode(this.visibleNodes, id);
		const root = this.viewRoot;
		if (this.busy || this.isViewRoot(id) || node === null || root === null || node.block.ParentID === root.block.ID) {
			return;
		}
		await this.structural(id, () => App.OutdentBlock(id));
	}

	// mergeIntoPrevious is Backspace at the start: the block visible above absorbs
	// this one's text, this one's children move to it, and this one is gone. The
	// caret lands on the seam.
	//
	// A collapsed target is expanded first, or the text would disappear into a
	// block whose content is not on screen.
	async mergeIntoPrevious(id: string): Promise<void> {
		const target = prevVisible(this.visibleNodes, id, this.collapsed, this.ghosts);
		// The view's root is the page's title row, and it *is* in the visible
		// order — so it has to be excluded by name as a target, on top of the
		// isViewRoot check on id itself: without this the first block of a page
		// would try to merge into its own title, which the domain refuses ("a
		// topic root cannot absorb text") and which would arrive back as an
		// invalid_ops the reader did nothing to deserve.
		if (this.busy || this.isViewRoot(id) || target === null || target === this.viewRoot?.block.ID) {
			return;
		}
		this.collapsed.delete(target);
		const seam = findBlock(this.forest, target)?.Content.length ?? 0;
		await this.structural(id, () => App.MergeBlocks(target, id), { id: target, caret: seam });
	}

	// pullNextInto is Delete at the end: the mirror of Backspace. The block visible
	// below is absorbed into this one, its children move here, and the caret stays
	// where it is.
	//
	// The caret is an argument, like splitAtCaret's, rather than read from
	// editCaret: the keystroke that triggers this is acting on the offset the DOM
	// holds at keydown, and syncCaret reports on keyup — so a caret moved by a key
	// still held down has not been reported yet. Passing it explicitly means the
	// gesture never depends on a report having landed first, and for Delete-at-end
	// that is the difference between the join and the start of the block.
	async pullNextInto(id: string, caret: number): Promise<void> {
		const source = nextVisible(this.visibleNodes, id, this.collapsed, this.ghosts);
		if (this.busy || this.isViewRoot(id) || source === null) {
			return;
		}
		await this.structural(id, () => App.MergeBlocks(id, source), { id, caret });
	}

	// moveCaret is ↑ or ↓ leaving the block: the caret goes to the same column of
	// the block visible above or below, on that block's last or first line. At the
	// ends of the visible order it stays put.
	async moveCaret(id: string, direction: "up" | "down", column: number): Promise<void> {
		const target =
			direction === "up"
				? prevVisible(this.visibleNodes, id, this.collapsed, this.ghosts)
				: nextVisible(this.visibleNodes, id, this.collapsed, this.ghosts);
		if (this.busy || target === null) {
			return;
		}
		const topic = topicOf(this.forest, id);
		this.busy = true;
		this.error = "";
		try {
			// Only the write can throw here — refresh reports its own failure rather
			// than raising it — and a write that did not land is the one thing that
			// stops the arrow: the text is left in the editor for the reader to try
			// again with (spec §8), which is pointless if the caret has moved off it.
			try {
				if (await this.writeIfChanged(id)) {
					await this.refresh(topic);
				}
			} catch (err: unknown) {
				this.error = messageFor(err);
				return;
			}
			// The target's text is read from the re-read forest and assigned before
			// the session moves, so the editor never sees the block it is leaving
			// under the caret it is being given — see editCaret.
			const text = findBlock(this.forest, target)?.Content ?? "";
			this.editText = text;
			this.editingID = target;
			this.editCaret = offsetAtLine(text, direction === "up" ? lineCount(text) - 1 : 0, column);
			this.editCaretNonce++;
		} finally {
			this.busy = false;
		}
	}

	// structural is the shape every gesture below Enter shares: write the pending
	// text (the backend methods take ids, not content, so an unwritten edit would
	// be acted on stale), run the operation, re-read the topic, land the caret.
	//
	// The landing comes last, after the re-read: the block it lands on carries
	// text the operation itself produced — a merge's joined content — and the
	// editor has to mount on that text, not on the text of the block the reader
	// was in when they pressed the key. See editCaret for what goes wrong when
	// the text arrives second.
	//
	// The write and the operation are what the gesture is: a failure in either
	// means it did not happen, and leaves the session exactly as it was — the text
	// is still in the editor, which is the whole point of not clearing it (spec
	// §8). The re-read behind them is not part of that verdict and is separated
	// from it here so the two cannot be confused: once the operation has landed in
	// storage the gesture has happened, and the caret goes where it was meant to
	// go even if the cache it reads out of is a failed re-read behind (see
	// refresh) — the alternative strands the session on the block a merge has
	// already consumed.
	private async structural(
		id: string,
		op: () => Promise<unknown>,
		land?: { id: string; caret: number },
	): Promise<void> {
		const topic = topicOf(this.forest, id);
		this.busy = true;
		this.error = "";
		try {
			try {
				await this.writeIfChanged(id);
				await op();
			} catch (err: unknown) {
				this.error = messageFor(err);
				return;
			}
			await this.refresh(topic);
			if (land !== undefined) {
				this.editText = findBlock(this.forest, land.id)?.Content ?? this.editText;
				this.editingID = land.id;
				this.editCaret = land.caret;
				this.editCaretNonce++;
			}
		} finally {
			this.busy = false;
		}
	}

	// The commit in flight, shared by concurrent callers (see commitEdit). It is
	// $state so commitInFlight below reads it reactively wherever it is asked.
	private committing = $state<Promise<boolean> | null>(null);

	// commitInFlight says whether a commit is running. busy cannot answer that:
	// commitEdit neither raises nor checks it — the eight acts that raise busy
	// call commitEdit while holding it — so a commit started on its own, by
	// Escape or by a blur, is invisible to everything outside this class.
	//
	// The deferred arrow path in InlineEditor is what needs it: that path decides
	// a frame after the keystroke, and a commit started inside that frame would
	// otherwise leave two writeIfChanged calls running against one session.
	get commitInFlight(): boolean {
		return this.committing !== null;
	}

	// commitEdit resolves to whether the session is closed: true when it
	// committed or had nothing to commit, false when the write failed and the
	// text is still in the editor. A caller that would unmount the editor has to
	// honour that — spec §8 keeps the paragraph on screen, and keeping it is
	// pointless if the row it lives in is collapsed away underneath it.
	//
	// Every caller that can unmount the editor commits through here first —
	// collapsing an ancestor, zooming in, switching topic, targeting a block,
	// sending an AI command, opening the link picker, opening a different block.
	// That is the whole of what used to be a modal lock: while the editor was a
	// box with Save and Cancel it was right to refuse those actions, and it
	// stopped being right the moment editing became the default state (spec
	// §4.5). busy still greys out buttons; nothing refuses because a caret is
	// somewhere — a failed write is the one thing that still stops a gesture,
	// and it stops it by aborting the gesture rather than by refusing to start.
	//
	// That is the whole distinction that survived: a caret being somewhere
	// refuses nothing, a write being in flight refuses everything. The acts that
	// move the view without a write of their own — toggle, focus, clearFocus,
	// selectTopic and shell.setMode — therefore turn back at busy instead of
	// committing through it. A gesture owns the session for the whole of its
	// flight: it is about to hand the caret to a different block, and its
	// operation is still running against storage. A commit landing in the middle
	// would close that session underneath it and race the operation with a write,
	// with a re-read of the topic the operation is still changing, or — where the
	// reader has emptied the block — with a delete of the very row the operation
	// is merging. The acts that raise busy themselves (setScope, askAI,
	// startLink, the four review methods, deleteRevision) check it first and then
	// own it, so their commit runs with nothing else in flight — which is exactly
	// why commitEdit does not check busy itself: all eight call it while holding
	// it.
	//
	// It writes only if the text changed, and drops a block left empty with no
	// children — press Enter, think better of it, leave, and nothing is left
	// behind.
	//
	// Concurrent callers share one in-flight promise rather than the second being
	// refused: the textarea's blur and the next block's click both land here in
	// the same tick, and a guard would swallow the click.
	commitEdit(): Promise<boolean> {
		if (this.committing !== null) {
			return this.committing;
		}
		this.committing = this.runCommit().finally(() => {
			this.committing = null;
		});
		return this.committing;
	}

	// runCommit resolves true on every path that leaves no open session —
	// nothing to do, the block vanished, deleted, written, or unchanged — and
	// false only when the write itself failed, where the text and caret are left
	// exactly as the reader typed them (spec §8).
	//
	// The verdict is about the write and nothing else, which is why the try below
	// closes the moment the write is settled and the re-read sits outside it. They
	// used to share one catch, and a re-read that failed after a successful write
	// returned false with clearEdit already run: the caller aborted as though
	// storage had refused the change, while the session it was refusing on behalf
	// of no longer existed. After a delete it was worse still — the block was gone
	// from storage and the reader was told the commit had failed, looking at a row
	// the stale forest was still rendering. A failed re-read is reported as itself
	// through this.error; see refresh.
	private async runCommit(): Promise<boolean> {
		const id = this.editingID;
		if (id === null) {
			return true;
		}
		const node = findNode(this.forest, id);
		if (node === null) {
			// The block went away under the editor — nothing to write it to.
			this.clearEdit();
			return true;
		}
		const topic = topicOf(this.forest, id);
		this.error = "";
		try {
			// An empty block that carries no children carries nothing at all. One
			// that has children still carries structure, and a topic root is a page:
			// neither is deleted for being blank.
			const empty = this.editText.trim() === "";
			const disposable = empty && node.children.length === 0 && node.block.ParentID !== "";
			if (disposable) {
				await App.DeleteBlock(id);
			} else if (!(await this.writeIfChanged(id))) {
				// Nothing was written, so there is nothing to re-read for.
				this.clearEdit();
				return true;
			}
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
			return false;
		}
		this.clearEdit();
		await this.refresh(topic);
		return true;
	}

	// The five methods below rebuild the whole forest, and the row holding an open
	// editor goes with it — the review controls sit outside the editing branch in
	// TreeNode, so ✓, ✗ and the page-wide pair are clickable on the row being
	// edited as well as on any other. Each therefore commits first and honours the
	// result, like every other caller that can unmount the editor (see
	// commitEdit). Rejecting the edited row itself throws the write away again;
	// that is one wasted call, and it buys the same rule for all five.
	//
	// busy goes up before the commit, as in setScope and askAI: the commit's own
	// write is already in flight, so the controls grey out for its whole duration
	// rather than staying clickable until the review call starts.

	// accept marks a block's pending revisions reviewed: a live block keeps its
	// new content, a ghost's tombstone becomes final and it drops out of the
	// tree for good.
	async accept(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
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
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
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
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
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
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
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

	async startLink(id: string): Promise<void> {
		// Locked while busy, like the tree's other actions; an open edit commits
		// instead of blocking — see commitEdit's own comment.
		if (this.busy) {
			return;
		}
		// busy goes up before the commit, not after: the commit's own write is
		// already in flight, so the +link control greys out for its whole
		// duration rather than staying clickable until the picker opens.
		this.busy = true;
		if (!(await this.commitEdit())) {
			this.busy = false;
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
		this.busy = false;
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
	// than relying on that UI lock. It re-reads the whole base afterwards, so it
	// commits an open edit first for the same reason the four review methods
	// above do.
	async deleteRevision(blockID: string, revID: string): Promise<void> {
		if (this.busy) {
			return;
		}
		this.busy = true;
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
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
