import { Events } from "@wailsio/runtime";
import { SvelteMap, SvelteSet } from "svelte/reactivity";
import {
	type Block,
	type LinkedBlock,
	type OutlineChange,
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

type Gesture =
	| { kind: "split"; id: string; caret: number; end: number }
	| { kind: "insertAbove"; id: string }
	| { kind: "indent"; id: string }
	| { kind: "outdent"; id: string }
	| { kind: "mergeIntoPrevious"; id: string }
	| { kind: "pullNextInto"; id: string; caret: number }
	| { kind: "discard"; id: string };

type Command = {
	groupID: string;
	topicID: string;
	land: { id: string; caret: number };
	gesture: Gesture;
	createdID: string;
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
	// Caret state survives editor remounts. The nonce distinguishes repeated
	// placement events from caret movements reported by the editor.
	editCaret = $state(0);
	editCaretNonce = $state(0);

	// Persisted outline edits do not share the textarea's native history.
	private undoStack = $state<Command[]>([]);
	private redoStack = $state<Command[]>([]);

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

	// Structural navigation is restricted to the active topic or focused subtree.
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

	// ghostsFor reconstructs pending deletions omitted by GetSubtree.
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
		this.clearUndoHistory();
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

	// reloadTopic refreshes one topic after an owner write.
	async reloadTopic(topicID: string): Promise<void> {
		if (this.forest.findIndex((t) => t.block.ID === topicID) < 0) {
			return;
		}
		const blocks = (await App.GetSubtree(topicID)) ?? [];
		// Locate the slot after the await because another reload may replace the forest.
		const i = this.forest.findIndex((t) => t.block.ID === topicID);
		if (i < 0) {
			return;
		}
		const extra = this.ghostsFor(topicID);
		const tree = buildTree(extra.length === 0 ? blocks : [...blocks, ...extra]);
		if (tree === null) {
			this.forest.splice(i, 1);
			if (this.selectedTopicID === topicID) {
				this.selectedTopicID = this.forest.length > 0 ? this.forest[0].block.ID : null;
				this.focusID = null;
			}
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

	// refresh reports cache refresh failures without changing the write verdict.
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

	// toggle commits before changing visibility because the editor may unmount.
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

	// clearFocus preserves the open edit because widening the view keeps its row.
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
		if (this.busy) {
			return;
		}
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
		this.busy = true;
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

	// startEdit commits any different open block before starting the new session.
	async startEdit(block: Block, caret: number): Promise<void> {
		if (this.editingID !== null && this.editingID !== block.ID) {
			if (!(await this.commitEdit())) {
				return;
			}
		}
		const fresh = findBlock(this.forest, block.ID) ?? block;
		// Set text before mounting the editor; assigning a control value moves its caret.
		this.editText = fresh.Content;
		this.editingID = fresh.ID;
		this.editCaret = caret;
		this.editCaretNonce++;
	}

	// clearEdit closes the current edit session without clearing outline history.
	clearEdit(): void {
		this.editingID = null;
		this.editText = "";
		this.editCaret = 0;
		this.editCaretNonce++;
	}

	// syncCaret ignores stale reports and does not announce a new placement.
	syncCaret(id: string, offset: number): void {
		if (this.editingID !== id) {
			return;
		}
		this.editCaret = offset;
	}

	// writeIfChanged persists one text snapshot and updates the cache immediately.
	// Updating the cache prevents duplicate revisions when a later refresh fails.
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

	// The active view root can be renamed but cannot participate in structural gestures.
	private isViewRoot(id: string): boolean {
		return id === this.viewRoot?.block.ID;
	}

	// Ghost rows represent deleted storage records and are never gesture targets.

	// splitAtCaret keeps the head in place and moves the tail to a new sibling.
	// The selected range [caret, end) is removed.
	async splitAtCaret(id: string, caret: number, end: number = caret): Promise<void> {
		if (this.busy || this.isViewRoot(id)) {
			return;
		}
		const topic = topicOf(this.forest, id);
		const text = this.editText;
		const head = text.slice(0, caret);
		const tail = text.slice(end);
		this.busy = true;
		this.error = "";
		try {
			const change = await App.SplitBlock(id, head, tail);
			this.record(change, topic, { id, caret }, { kind: "split", id, caret, end });
			if (topic !== null) {
				await this.reloadTopic(topic);
			}
			this.editText = tail;
			this.editingID = change.BlockID;
			this.editCaret = 0;
			this.editCaretNonce++;
		} catch (err: unknown) {
			this.error = messageFor(err);
		} finally {
			this.busy = false;
		}
	}

	private record(
		change: OutlineChange,
		topic: string | null,
		land: { id: string; caret: number },
		gesture: Gesture,
	): void {
		this.undoStack.push({
			groupID: change.GroupID,
			topicID: topic ?? "",
			land,
			gesture,
			createdID: change.BlockID,
		});
		this.redoStack = [];
	}

	private editIsClean(): boolean {
		return this.editingID === null || findBlock(this.forest, this.editingID)?.Content === this.editText;
	}

	canUndo(): boolean {
		return !this.busy && this.undoStack.length > 0 && this.editIsClean();
	}

	canRedo(): boolean {
		return !this.busy && this.redoStack.length > 0 && this.editIsClean();
	}

	async undo(): Promise<void> {
		const command = this.undoStack.at(-1);
		if (command === undefined || !this.canUndo()) {
			return;
		}
		this.busy = true;
		this.error = "";
		try {
			try {
				await App.UndoGroup(command.groupID);
			} catch (err: unknown) {
				this.error = messageFor(err);
				return;
			}
			this.undoStack.pop();
			this.redoStack.push(command);
			await this.refresh(command.topicID);
			this.land(command.land);
		} finally {
			this.busy = false;
		}
	}

	async redo(): Promise<void> {
		const command = this.redoStack.at(-1);
		if (command === undefined || !this.canRedo()) {
			return;
		}
		const branch = this.redoStack.slice(0, -1);
		const depth = this.undoStack.length;
		await this.perform(command.gesture);
		const made = this.undoStack.at(-1);
		if (this.undoStack.length === depth || made === undefined) {
			return;
		}
		this.redoStack = branch;
		if (command.createdID !== "" && made.createdID !== command.createdID) {
			this.rename(command.createdID, made.createdID);
		}
	}

	// rename remaps references to a block recreated by redo.
	private rename(from: string, to: string): void {
		for (const command of this.redoStack) {
			if (command.land.id === from) {
				command.land.id = to;
			}
			if (command.gesture.id === from) {
				command.gesture.id = to;
			}
		}
	}

	private async perform(gesture: Gesture): Promise<void> {
		switch (gesture.kind) {
			case "split":
				await this.splitAtCaret(gesture.id, gesture.caret, gesture.end);
				return;
			case "insertAbove":
				await this.insertAbove(gesture.id);
				return;
			case "indent":
				await this.indent(gesture.id);
				return;
			case "outdent":
				await this.outdent(gesture.id);
				return;
			case "mergeIntoPrevious":
				await this.mergeIntoPrevious(gesture.id);
				return;
			case "pullNextInto":
				await this.pullNextInto(gesture.id, gesture.caret);
				return;
			case "discard":
				await this.discard(gesture.id);
				return;
		}
	}

	// discard repeats deletion of an empty block during redo.
	private async discard(id: string): Promise<void> {
		if (this.busy) {
			return;
		}
		const topic = topicOf(this.forest, id);
		this.busy = true;
		this.error = "";
		try {
			const change = await App.DeleteBlock(id);
			this.record(change, topic, { id, caret: 0 }, { kind: "discard", id });
		} catch (err: unknown) {
			this.error = messageFor(err);
			return;
		} finally {
			this.busy = false;
		}
		await this.refresh(topic);
	}

	private land(at: { id: string; caret: number }): void {
		this.editText = findBlock(this.forest, at.id)?.Content ?? this.editText;
		this.editingID = at.id;
		this.editCaret = at.caret;
		this.editCaretNonce++;
	}

	clearUndoHistory(): void {
		this.undoStack = [];
		this.redoStack = [];
	}

	// insertAbove preserves the current block identity and edit session.
	async insertAbove(id: string): Promise<void> {
		if (this.busy || this.isViewRoot(id)) {
			return;
		}
		const topic = topicOf(this.forest, id);
		this.busy = true;
		this.error = "";
		try {
			const change = await App.InsertBlockBefore(id, "");
			this.record(change, topic, { id, caret: 0 }, { kind: "insertAbove", id });
			if (topic !== null) {
				await this.reloadTopic(topic);
			}
		} catch (err: unknown) {
			this.error = messageFor(err);
		} finally {
			this.busy = false;
		}
	}

	// indent moves a block under its previous visible sibling.
	async indent(id: string): Promise<void> {
		if (this.busy || this.isViewRoot(id) || prevSibling(this.visibleNodes, id, this.ghosts) === null) {
			return;
		}
		await this.structural(id, () => App.IndentBlock(id), undefined, {
			caret: this.editCaret,
			gesture: { kind: "indent", id },
		});
	}

	// outdent moves a block after its parent without moving its former siblings.
	async outdent(id: string): Promise<void> {
		const node = findNode(this.visibleNodes, id);
		const root = this.viewRoot;
		if (this.busy || this.isViewRoot(id) || node === null || root === null || node.block.ParentID === root.block.ID) {
			return;
		}
		await this.structural(id, () => App.OutdentBlock(id), undefined, {
			caret: this.editCaret,
			gesture: { kind: "outdent", id },
		});
	}

	// mergeIntoPrevious joins the block into its previous visible live block.
	async mergeIntoPrevious(id: string): Promise<void> {
		const target = prevVisible(this.visibleNodes, id, this.collapsed, this.ghosts);
		if (this.busy || this.isViewRoot(id) || target === null || target === this.viewRoot?.block.ID) {
			return;
		}
		this.collapsed.delete(target);
		const seam = findBlock(this.forest, target)?.Content.length ?? 0;
		await this.structural(
			id,
			() => App.MergeBlocks(target, id),
			{ id: target, caret: seam },
			{
				caret: 0,
				gesture: { kind: "mergeIntoPrevious", id },
			},
		);
	}

	// pullNextInto joins the next visible live block into this one.
	async pullNextInto(id: string, caret: number): Promise<void> {
		const source = nextVisible(this.visibleNodes, id, this.collapsed, this.ghosts);
		if (this.busy || this.isViewRoot(id) || source === null) {
			return;
		}
		await this.structural(
			id,
			() => App.MergeBlocks(id, source),
			{ id, caret },
			{
				caret,
				gesture: { kind: "pullNextInto", id, caret },
			},
		);
	}

	// moveCaret enters the adjacent block at the nearest matching column.
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
			try {
				if (await this.writeIfChanged(id)) {
					await this.refresh(topic);
				}
			} catch (err: unknown) {
				this.error = messageFor(err);
				return;
			}
			const text = findBlock(this.forest, target)?.Content ?? "";
			this.editText = text;
			this.editingID = target;
			this.editCaret = offsetAtLine(text, direction === "up" ? lineCount(text) - 1 : 0, column);
			this.editCaretNonce++;
		} finally {
			this.busy = false;
		}
	}

	// structural persists pending text before applying an ID-based tree edit.
	private async structural(
		id: string,
		op: () => Promise<OutlineChange>,
		land?: { id: string; caret: number },
		undo?: { caret: number; gesture: Gesture },
	): Promise<void> {
		const topic = topicOf(this.forest, id);
		this.busy = true;
		this.error = "";
		try {
			let change: OutlineChange;
			try {
				await this.writeIfChanged(id);
				change = await op();
			} catch (err: unknown) {
				this.error = messageFor(err);
				return;
			}
			if (undo !== undefined) {
				this.record(change, topic, { id, caret: undo.caret }, undo.gesture);
			}
			await this.refresh(topic);
			if (land !== undefined) {
				this.land(land);
			}
		} finally {
			this.busy = false;
		}
	}

	// Concurrent commit callers share this promise.
	private committing = $state<Promise<boolean> | null>(null);

	// commitInFlight includes standalone blur and Escape commits not covered by busy.
	get commitInFlight(): boolean {
		return this.committing !== null;
	}

	// commitEdit resolves true when the session closed and false when a failed
	// write left it open. Concurrent callers share the same commit.
	commitEdit(): Promise<boolean> {
		if (this.committing !== null) {
			return this.committing;
		}
		this.committing = this.runCommit().finally(() => {
			this.committing = null;
		});
		return this.committing;
	}

	// runCommit bases its result on persistence; a later refresh failure only
	// leaves the cache stale.
	private async runCommit(): Promise<boolean> {
		const id = this.editingID;
		if (id === null) {
			return true;
		}
		const node = findNode(this.forest, id);
		if (node === null) {
			this.clearEdit();
			return true;
		}
		const topic = topicOf(this.forest, id);
		this.error = "";
		try {
			const empty = this.editText.trim() === "";
			const disposable = empty && node.children.length === 0 && node.block.ParentID !== "";
			if (disposable) {
				// The deletion supersedes the block's creation group.
				const change = await App.DeleteBlock(id);
				this.record(change, topic, { id, caret: 0 }, { kind: "discard", id });
			} else if (!(await this.writeIfChanged(id))) {
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

	// Review operations commit the editor before rebuilding the forest.

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
		if (this.busy) {
			return;
		}
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

	// deleteRevision rolls back one revision and closes history if the block vanishes.
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
