<script lang="ts">
	import { onMount } from "svelte";
	import type { Block } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";

	// The Base view: browse the tree and drive the two AI flows from one command
	// field. Expansion (no target) and restructuring (a targeted block) both queue
	// a proposal for review — never a direct write — then the base is re-read.
	type TreeNode = { block: Block; children: TreeNode[] };

	let { onqueued }: { onqueued: () => void } = $props();

	// One command field feeds both AI flows. The target decides which: none means
	// expand a new topic (the whole base is the context), a block/page means ask
	// the AI to restructure it. scopeID null is the no-target (expansion) case.
	let command: string = $state("");
	let scopeID: string | null = $state(null);
	let scopeLabel: string = $state("");
	let status: string = $state("");
	let error: string = $state("");
	let busy: boolean = $state(false);
	let forest: TreeNode[] = $state([]);

	// collapsed holds the ids of nodes whose children are hidden. A fresh Set is
	// assigned on every toggle because mutating a Set in place does not trigger a
	// re-render. The base opens fully expanded; the reader folds away what they
	// are not looking at.
	let collapsed: Set<string> = $state(new Set());

	function toggle(id: string): void {
		const next = new Set(collapsed);
		if (next.has(id)) {
			next.delete(id);
		} else {
			next.add(id);
		}
		collapsed = next;
	}

	// Inline block editing: the owner's own edits apply straight to the base,
	// bypassing the review queue (review guards against AI, not the owner).
	let editingID: string | null = $state(null);
	let editText: string = $state("");

	// buildTree turns a pre-ordered flat subtree into a nested node. The root is
	// the block whose ParentID is empty; every other block is attached to its
	// parent in slice order, which GetSubtree already sorts by Position.
	function buildTree(blocks: Block[]): TreeNode | null {
		const byId = new Map<string, TreeNode>();
		for (const b of blocks) {
			byId.set(b.ID, { block: b, children: [] });
		}
		let root: TreeNode | null = null;
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

	// reload reads every topic root and its full subtree, rebuilding the forest.
	async function reload(): Promise<void> {
		const topics = (await App.TopicMap()) ?? [];
		const roots = topics.filter((b) => b.ParentID === "");
		const trees: TreeNode[] = [];
		for (const r of roots) {
			const blocks = (await App.GetSubtree(r.ID)) ?? [];
			const tree = buildTree(blocks);
			if (tree) {
				trees.push(tree);
			}
		}
		forest = trees;
	}

	function setScope(block: Block): void {
		// Ignore while busy or mid inline-edit, mirroring the tree's other actions.
		if (busy || editingID !== null) {
			return;
		}
		scopeID = block.ID;
		scopeLabel = block.Content;
	}

	function clearScope(): void {
		scopeID = null;
		scopeLabel = "";
	}

	// askAI routes the one command field to the matching AI flow: a new-topic
	// expansion when nothing is targeted, a structure edit on the targeted
	// block/page otherwise. Both return a proposal that lands in the review queue,
	// never a direct write, and both hand focus to the queue via onqueued.
	async function askAI(): Promise<void> {
		const text = command.trim();
		// Locked mid inline-edit like every other tree action: onqueued() unmounts
		// this view, so submitting here would silently drop an open edit.
		if (text === "" || busy || editingID !== null) {
			return;
		}
		busy = true;
		error = "";
		try {
			const scope = scopeID;
			status = scope === null ? `expanding "${text}"…` : "asking the AI to restructure…";
			const proposal =
				scope === null
					? await App.ExpandTopic(text, "")
					: await App.ProposeEdit(scope, text);
			status = `queued for review (${(proposal.Ops ?? []).length} ops)`;
			command = "";
			clearScope();
			onqueued();
		} catch (err: unknown) {
			error = String(err);
			status = "";
		} finally {
			busy = false;
		}
	}

	function startEdit(block: Block): void {
		// Ignore while busy or while another block is already being edited, so an
		// in-progress edit is never silently discarded by switching blocks.
		if (busy || editingID !== null) {
			return;
		}
		editingID = block.ID;
		editText = block.Content;
	}

	function cancelEdit(): void {
		editingID = null;
		editText = "";
	}

	// saveEdit writes the block's new content directly, then re-reads the base.
	async function saveEdit(): Promise<void> {
		if (editingID === null || busy) {
			return;
		}
		busy = true;
		error = "";
		try {
			await App.EditBlockContent(editingID, editText);
			await reload();
			// Keep the scope indicator honest if the targeted block is the one edited.
			if (scopeID === editingID) {
				scopeLabel = editText;
			}
			status = "saved edit";
			cancelEdit();
		} catch (err: unknown) {
			error = String(err);
			status = "";
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		reload().catch((err: unknown) => {
			error = String(err);
		});
	});
</script>

{#snippet treeNode(node: TreeNode)}
	{@const hasChildren = node.children.length > 0}
	{@const expanded = !collapsed.has(node.block.ID)}
	<li>
		<div class="row" class:targeted={scopeID === node.block.ID}>
			<!-- Toggles are locked while an edit is open: collapsing an ancestor would
			     unmount the editor while editingID lingers, silently dead-ending every
			     other block (startEdit no-ops when a block is already being edited). -->
			{#if hasChildren}
				<button
					type="button"
					class="toggle"
					aria-expanded={expanded}
					aria-label={expanded ? "collapse" : "expand"}
					disabled={editingID !== null}
					onclick={() => toggle(node.block.ID)}
				>
					{expanded ? "▾" : "▸"}
				</button>
			{:else}
				<span class="toggle leaf" aria-hidden="true">·</span>
			{/if}

			{#if editingID === node.block.ID}
				<div class="editor">
					<textarea aria-label="block content" bind:value={editText} disabled={busy}></textarea>
					<button type="button" onclick={saveEdit} disabled={busy || editText.trim() === ""}>
						Save
					</button>
					<button type="button" onclick={cancelEdit} disabled={busy}>Cancel</button>
				</div>
			{:else}
				<button
					type="button"
					class="content"
					title="click to edit"
					disabled={busy}
					onclick={() => startEdit(node.block)}
				>
					<code>{node.block.Content}</code>
				</button>
			{/if}

			{#if editingID !== node.block.ID}
				<button
					type="button"
					class="target"
					title="ask the AI to restructure this block"
					aria-label="target this block for the AI command"
					aria-pressed={scopeID === node.block.ID}
					disabled={busy || editingID !== null}
					onclick={() => setScope(node.block)}
				>
					⌖
				</button>
			{/if}
		</div>
		{#if hasChildren && expanded}
			<ul>
				{#each node.children as child (child.block.ID)}
					{@render treeNode(child)}
				{/each}
			</ul>
		{/if}
	</li>
{/snippet}

<section>
	<h1>Comind</h1>

	<form
		onsubmit={(e) => {
			e.preventDefault();
			askAI();
		}}
	>
		<p class="scope">
			{#if scopeID === null}
				New topic — the whole base is the context.
			{:else}
				Editing <code>{scopeLabel}</code>
				<button type="button" class="scope-clear" onclick={clearScope} disabled={busy}>
					clear
				</button>
			{/if}
		</p>
		<div class="ask">
			<input
				aria-label="AI command"
				placeholder={scopeID === null
					? "new topic to expand…"
					: "how should the AI restructure this?"}
				bind:value={command}
				disabled={busy || editingID !== null}
			/>
			<button type="submit" disabled={busy || editingID !== null || command.trim() === ""}>
				Ask AI
			</button>
		</div>
	</form>

	{#if status !== ""}
		<p><code>{status}</code></p>
	{/if}
	{#if error !== ""}
		<p><code>error: {error}</code></p>
	{/if}

	{#if forest.length === 0}
		<p>(no topics yet)</p>
	{:else}
		<ul>
			{#each forest as tree (tree.block.ID)}
				{@render treeNode(tree)}
			{/each}
		</ul>
	{/if}
</section>

<style>
	h1 {
		font-size: 1.3rem;
		margin: 0 0 0.75rem;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
		margin-bottom: 1rem;
	}
	.scope {
		margin: 0;
		font-size: 0.85rem;
		opacity: 0.75;
	}
	/* Keep a long targeted-block content on one line so the command area does not
	   balloon; the full text still lives in the tree. */
	.scope code {
		display: inline-block;
		max-width: 22rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		vertical-align: bottom;
	}
	.scope-clear {
		border: none;
		background: none;
		padding: 0 0.2rem;
		cursor: pointer;
		color: inherit;
		text-decoration: underline;
	}
	.ask {
		display: flex;
		gap: 0.5rem;
	}
	.ask input {
		flex: 1;
	}
	/* The forest is a plain nested list; indentation and a faint guide rail carry
	   the hierarchy. Colours are theme-neutral (grey alpha) so the rail and hover
	   read on both light and dark backgrounds. */
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	li ul {
		margin-left: 0.55rem;
		padding-left: 0.7rem;
		border-left: 1px solid rgba(127, 127, 127, 0.25);
	}
	.row {
		display: flex;
		align-items: flex-start;
		gap: 0.15rem;
	}
	.toggle {
		flex: none;
		width: 1.1rem;
		padding: 0;
		border: none;
		background: none;
		cursor: pointer;
		color: inherit;
		font-size: 0.8rem;
		line-height: 1.5;
	}
	.toggle.leaf {
		cursor: default;
		opacity: 0.35;
	}
	.content {
		flex: 1;
		text-align: left;
		border: none;
		background: none;
		padding: 0.1rem 0.3rem;
		border-radius: 3px;
		cursor: pointer;
		color: inherit;
	}
	.content:hover:not(:disabled) {
		background: rgba(127, 127, 127, 0.15);
	}
	.row.targeted {
		background: rgba(127, 127, 127, 0.12);
		border-radius: 3px;
	}
	/* The AI target is a per-row affordance kept out of the way until the row is
	   hovered or focused, so the tree stays quiet; the currently-targeted row keeps
	   it visible. Grey-alpha again, for both themes. */
	.target {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0;
	}
	.row:hover .target,
	.row:focus-within .target,
	.row.targeted .target {
		opacity: 0.6;
	}
	.target:hover:not(:disabled),
	.row.targeted .target {
		opacity: 1;
	}
	.content code {
		white-space: pre-wrap;
	}
	.editor {
		display: flex;
		flex: 1;
		flex-wrap: wrap;
		gap: 0.3rem;
	}
	.editor textarea {
		flex: 1;
		min-width: 12rem;
		min-height: 3rem;
	}
</style>
