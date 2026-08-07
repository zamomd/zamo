import { Events } from "@wailsio/runtime";
import { SvelteMap, SvelteSet } from "svelte/reactivity";
import type {
	Backlink,
	Block,
	Mode,
	OutlineChange,
	Revision,
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

const generationProgressEvent = "generation:progress";

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

// BaseStore is the persistent view model for browsing, editing, and reviewing
// the knowledge tree.
class BaseStore {
	command = $state("");
	scopeID = $state<string | null>(null);
	status = $state("");
	error = $state("");
	busy = $state(false);
	forest = $state<TreeNodeData[]>([]);

	selectedTopicID = $state<string | null>(null);

	focusID = $state<string | null>(null);

	collapsed = new SvelteSet<string>();

	ghosts = new SvelteSet<string>();

	editingID = $state<string | null>(null);
	editText = $state("");
	editCaret = $state(0);
	editCaretNonce = $state(0);

	private undoStack = $state<Command[]>([]);
	private redoStack = $state<Command[]>([]);

	highlightID = $state<string | null>(null);
	highlightNonce = $state(0);

	openBacklinks = new SvelteSet<string>();
	backlinksById = new SvelteMap<string, Backlink[]>();

	historyID = $state<string | null>(null);
	revisions = $state<Revision[]>([]);

	private pendingReveal: string | null = null;

	private historySeq = 0;

	private selectSeq = 0;

	scopeLabel = $derived(this.scopeID === null ? "" : (findBlock(this.forest, this.scopeID)?.Content ?? ""));

	selectedTree = $derived(
		this.selectedTopicID === null ? null : (this.forest.find((t) => t.block.ID === this.selectedTopicID) ?? null),
	);

	viewRoot = $derived(
		this.focusID === null ? this.selectedTree : (findNode(this.forest, this.focusID) ?? this.selectedTree),
	);

	visibleNodes = $derived<TreeNodeData[]>(this.viewRoot === null ? [] : [this.viewRoot]);

	// topicMode is the shape of the selected topic. An unloaded or missing topic
	// reads as an outline, which is what an empty screen renders as anyway.
	get topicMode(): "outline" | "doc" {
		return this.viewRoot?.block.Mode === "doc" ? "doc" : "outline";
	}

	constructor() {
		Events.On(generationProgressEvent, (e) => {
			if (!this.busy) return;
			const data = e.data as { phase?: string } | undefined;
			this.status = (data?.phase ? generationPhaseLabels[data.phase] : undefined) ?? "the model is working…";
		});
	}

	activate(): void {
		this.reload().catch((err: unknown) => {
			this.error = messageFor(err);
		});
	}

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
				Mode: u.Head.Mode,
				Meta: {},
				CreatedAt: "",
				UpdatedAt: "",
				Unreviewed: true,
			});
		}
		return out;
	}

	async reload(): Promise<void> {
		const topics = (await App.TopicMap()) ?? [];
		const roots = topics.filter((b) => b.ParentID === "");

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
		for (const topicID of ghostTopics) {
			const tree = buildTree(this.ghostsFor(topicID));
			if (tree) {
				trees.push(tree);
			}
		}
		this.forest = trees;
		this.clearUndoHistory();
		if (this.selectedTopicID === null || !trees.some((t) => t.block.ID === this.selectedTopicID)) {
			this.selectedTopicID = trees.length > 0 ? trees[0].block.ID : null;
			this.focusID = null;
		}
		this.backlinksById.clear();
		await this.syncBacklinks();
		this.dropStaleHistory();
		if (this.pendingReveal !== null) {
			const id = this.pendingReveal;
			this.pendingReveal = null;
			this.reveal(id);
		}
	}

	async reloadTopic(topicID: string): Promise<void> {
		if (this.forest.findIndex((t) => t.block.ID === topicID) < 0) {
			return;
		}
		const blocks = (await App.GetSubtree(topicID)) ?? [];
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
			await this.syncBacklinks();
			this.dropStaleHistory();
			return;
		}
		this.forest[i] = tree;
		await this.syncBacklinks();
	}

	// syncBacklinks brings every open "Linked references" panel back in line
	// with the forest just read: a panel whose block is gone closes, and the
	// rest are re-read. Both halves matter after any write — typing [[Zig]]
	// into a block and blurring reaches here through reloadTopic, and the panel
	// that exists to list that reference would otherwise keep the count it had
	// before the edit.
	private async syncBacklinks(): Promise<void> {
		for (const id of [...this.openBacklinks]) {
			if (findBlock(this.forest, id) === null) {
				this.openBacklinks.delete(id);
				this.backlinksById.delete(id);
				continue;
			}
			await this.refreshBacklinks(id);
		}
	}

	// dropStaleHistory closes the history panel once its block has left the
	// forest: the panel must not outlive the block it describes.
	private dropStaleHistory(): void {
		if (this.historyID !== null && findBlock(this.forest, this.historyID) === null) {
			this.historyID = null;
			this.revisions = [];
		}
	}

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

	reveal(id: string): void {
		this.selectSeq++;
		const path = findPath(this.forest, id);
		if (path === null) {
			this.pendingReveal = id;
			return;
		}
		this.pendingReveal = null;
		this.focusID = null;
		const topic = topicOf(this.forest, id);
		if (topic !== null) {
			this.selectedTopicID = topic;
		}
		for (const ancestor of path) {
			this.collapsed.delete(ancestor);
		}
		this.highlightID = id;
		this.highlightNonce++;
	}

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
		this.collapsed.delete(id);
		this.focusID = id;
	}

	clearFocus(): void {
		if (this.busy) {
			return;
		}
		this.focusID = null;
	}

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
			if (mine === this.selectSeq) {
				this.error = messageFor(err);
			}
			return;
		}
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
		this.error = "";
		this.highlightID = null;
		this.focusID = null;
	}

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
			if (scope === null) {
				await this.expandNewTopic(text);
			} else {
				this.status = "asking the AI to restructure…";
				await App.EditWithAI(scope, text);
				await this.reload();
				this.status = "changes applied, awaiting review";
			}
			this.command = "";
			this.clearScope();
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// createTopic makes an empty topic named name and selects it — how clicking
	// an unresolved [[Topic]] reference in the tree offers to create its
	// target. Deliberately not an AI expansion: the provider has no default, so
	// routing this through the model would make creating a topic impossible on
	// a fresh install. Expanding a topic with the AI stays its own gesture, the
	// untargeted half of askAI.
	async createTopic(name: string, mode: "outline" | "doc" = "outline"): Promise<void> {
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
			const id = await App.CreateTopic(name, mode as Mode);
			await this.reload();
			if (this.forest.some((t) => t.block.ID === id)) {
				this.selectedTopicID = id;
				this.focusID = null;
			}
			this.status = `created "${name}"`;
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	// setTopicMode converts the selected topic. The conversion is one atomic
	// batch, so the reload afterwards is what shows either shape whole.
	async setTopicMode(mode: "outline" | "doc"): Promise<void> {
		const id = this.selectedTopicID;
		if (this.busy || id === null) {
			return;
		}
		this.busy = true;
		if (!(await this.commitEdit())) {
			this.busy = false;
			return;
		}
		this.error = "";
		try {
			await App.SetTopicMode(id, mode as Mode);
			await this.reloadTopic(id);
			this.status = mode === "doc" ? "converted to a document" : "converted to blocks";
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	private async expandNewTopic(name: string): Promise<void> {
		this.status = `expanding "${name}"…`;
		await App.ExpandTopic(name, "");
		const before = new Set(this.forest.map((t) => t.block.ID));
		await this.reload();
		const fresh = this.forest.find((t) => !before.has(t.block.ID));
		if (fresh) {
			this.selectedTopicID = fresh.block.ID;
			this.focusID = null;
		}
		this.status = "changes applied, awaiting review";
	}

	async startEdit(block: Block, caret: number): Promise<void> {
		if (this.editingID !== null && this.editingID !== block.ID) {
			if (!(await this.commitEdit())) {
				return;
			}
		}
		const fresh = findBlock(this.forest, block.ID) ?? block;
		this.editText = fresh.Content;
		this.editingID = fresh.ID;
		this.editCaret = caret;
		this.editCaretNonce++;
	}

	clearEdit(): void {
		this.editingID = null;
		this.editText = "";
		this.editCaret = 0;
		this.editCaretNonce++;
	}

	syncCaret(id: string, offset: number): void {
		if (this.editingID !== id) {
			return;
		}
		this.editCaret = offset;
	}

	// acceptRef replaces [from, to) of the open edit with text and lands the
	// caret just past it — how RefAutocomplete completes a reference.
	acceptRef(from: number, to: number, text: string): void {
		const before = this.editText.slice(0, from);
		const after = this.editText.slice(to);
		this.editText = before + text + after;
		this.editCaret = before.length + text.length;
		this.editCaretNonce++;
	}

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

	private isViewRoot(id: string): boolean {
		return id === this.viewRoot?.block.ID;
	}

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

	async indent(id: string): Promise<void> {
		if (this.busy || this.isViewRoot(id) || prevSibling(this.visibleNodes, id, this.ghosts) === null) {
			return;
		}
		await this.structural(id, () => App.IndentBlock(id), undefined, {
			caret: this.editCaret,
			gesture: { kind: "indent", id },
		});
	}

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

	private committing = $state<Promise<boolean> | null>(null);

	get commitInFlight(): boolean {
		return this.committing !== null;
	}

	commitEdit(): Promise<boolean> {
		if (this.committing !== null) {
			return this.committing;
		}
		this.committing = this.runCommit().finally(() => {
			this.committing = null;
		});
		return this.committing;
	}

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

	// review runs one review gesture: refuse while busy, commit the open edit,
	// apply, then re-read the forest because a decision can add or remove rows
	// anywhere. The four below differ only in the call and what they report.
	private async review(op: () => Promise<void>, done: string): Promise<void> {
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
			await op();
			await this.reload();
			this.status = done;
		} catch (err: unknown) {
			this.error = messageFor(err);
			this.status = "";
		} finally {
			this.busy = false;
		}
	}

	accept(id: string): Promise<void> {
		return this.review(() => App.AcceptBlock(id), "accepted");
	}

	reject(id: string): Promise<void> {
		return this.review(() => App.RejectBlock(id), "rejected");
	}

	acceptPage(id: string): Promise<void> {
		return this.review(() => App.AcceptPage(id), "accepted page");
	}

	rejectPage(id: string): Promise<void> {
		return this.review(() => App.RejectPage(id), "rejected page");
	}

	async toggleBacklinks(id: string): Promise<void> {
		if (this.openBacklinks.has(id)) {
			this.openBacklinks.delete(id);
			this.backlinksById.delete(id);
			return;
		}
		this.openBacklinks.add(id);
		if (!this.backlinksById.has(id)) {
			await this.refreshBacklinks(id);
		}
	}

	async refreshBacklinks(id: string): Promise<void> {
		try {
			this.backlinksById.set(id, (await App.Backlinks(id)) ?? []);
		} catch (err: unknown) {
			this.error = messageFor(err);
		}
	}

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
