<script lang="ts">
	import { Dialogs } from "@wailsio/runtime";
	import { onMount } from "svelte";
	import type { Block } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";

	// The Base view: browse the tree and drive the two AI flows from one command
	// field. Expansion (no target) and restructuring (a targeted block) both queue
	// a proposal for review — never a direct write — then the base is re-read.
	type TreeNode = { block: Block; children: TreeNode[] };

	// Block.Meta is an untyped map across the binding, so these mirror
	// knowledge.MetaKeyAuthor and knowledge.Author by hand: change one side and
	// the badge silently misattributes every block. Keep the names greppable
	// together.
	const META_KEY_AUTHOR = "author";
	const AUTHOR_HUMAN = "human";

	let { onqueued }: { onqueued: () => void } = $props();

	// One command field feeds both AI flows. The target decides which: none means
	// expand a new topic from scratch, a block/page means ask the AI to
	// restructure it. scopeID null is the no-target (expansion) case.
	let command: string = $state("");
	let scopeID: string | null = $state(null);
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

	// Search (Scene 4): a query runs the FTS index, the hits list to blocks, and
	// clicking a hit reveals it in the tree (ancestors expanded, scrolled, flashed).
	let searchQuery: string = $state("");
	let searchResults: Block[] | null = $state(null);
	let flashID: string | null = $state(null);
	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	// Every search takes a ticket; only the newest one may paint. clearTimeout
	// alone cannot do this — it drops an unfired timer, but a request already in
	// flight still resolves and would repaint a panel the user has moved on from.
	let searchSeq = 0;

	// dropSearch abandons the pending and in-flight searches and hides the panel.
	function dropSearch(): void {
		clearTimeout(searchTimer);
		searchSeq++;
		searchResults = null;
	}

	function onSearchInput(): void {
		clearTimeout(searchTimer);
		const q = searchQuery.trim();
		const seq = ++searchSeq;
		if (q === "") {
			searchResults = null;
			return;
		}
		// Debounce: the FTS read is cheap, but not worth a call per keystroke.
		searchTimer = setTimeout(() => {
			// The banner belongs to the action in progress, as in every other flow
			// here: claim it on the way in, never clear it on the way out, or a
			// search would silently retract a failure the user has not addressed.
			error = "";
			App.Search(q)
				.then((hits) => {
					if (seq !== searchSeq) {
						return;
					}
					searchResults = hits ?? [];
				})
				.catch((err: unknown) => {
					if (seq !== searchSeq) {
						return;
					}
					error = String(err);
					searchResults = [];
				});
		}, 180);
	}

	// ancestorsOf returns the ids on the path from a root down to id (exclusive),
	// or null if id is not in the forest — the nodes that must be uncollapsed for
	// a hit to be visible.
	function ancestorsOf(nodes: TreeNode[], id: string, trail: string[] = []): string[] | null {
		for (const node of nodes) {
			if (node.block.ID === id) {
				return trail;
			}
			const hit = ancestorsOf(node.children, id, [...trail, node.block.ID]);
			if (hit) {
				return hit;
			}
		}
		return null;
	}

	function revealBlock(id: string): void {
		const next = new Set(collapsed);
		for (const a of ancestorsOf(forest, id) ?? []) {
			next.delete(a);
		}
		collapsed = next;
		// Assigning searchQuery fires no input event, so the debounce would
		// otherwise survive the dismissal and repaint the panel over the block
		// this call is about to scroll to.
		dropSearch();
		searchQuery = "";
		// Scroll and flash once the reveal has rendered.
		requestAnimationFrame(() => {
			document.getElementById(`blk-${id}`)?.scrollIntoView({ block: "center", behavior: "smooth" });
			flashID = id;
			setTimeout(() => {
				flashID = null;
			}, 1500);
		});
	}

	// findBlock walks the forest for a block by id; the forest is a personal
	// base's worth of nodes, so a plain walk is cheap enough.
	function findBlock(nodes: TreeNode[], id: string): Block | null {
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

	// The scope label is read out of the live forest rather than copied on
	// targeting: a targeted block that is edited or disappears can never leave a
	// stale label behind.
	let scopeLabel: string = $derived(
		scopeID === null ? "" : (findBlock(forest, scopeID)?.Content ?? ""),
	);

	// setScope toggles: targeting the already-targeted row clears it, which is
	// what the button's aria-pressed state promises.
	function setScope(block: Block): void {
		// Ignore while busy or mid inline-edit, mirroring the tree's other actions.
		if (busy || editingID !== null) {
			return;
		}
		scopeID = scopeID === block.ID ? null : block.ID;
	}

	function clearScope(): void {
		scopeID = null;
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
			const ops = (proposal.Ops ?? []).length;
			status =
				scope === null
					? `queued "${text}" for review (${ops} ops)`
					: `queued the restructure for review (${ops} ops)`;
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
			status = "saved edit";
			cancelEdit();
		} catch (err: unknown) {
			error = String(err);
			status = "";
		} finally {
			busy = false;
		}
	}

	// exportBase (Scene 8) picks a folder and writes the whole base as markdown —
	// the portability promise. The picker is the Wails native directory dialog.
	async function exportBase(): Promise<void> {
		if (busy) {
			return;
		}
		let picked: string | string[];
		try {
			picked = await Dialogs.OpenFile({
				Title: "Export the base to a folder",
				CanChooseDirectories: true,
				CanChooseFiles: false,
				CanCreateDirectories: true,
				ButtonText: "Export here",
			});
		} catch (err: unknown) {
			error = String(err);
			return;
		}
		const dir = Array.isArray(picked) ? picked[0] : picked;
		if (!dir) {
			return; // cancelled
		}
		busy = true;
		error = "";
		try {
			await App.ExportMarkdown(dir);
			status = `exported the base to ${dir}`;
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
		<div
			id="blk-{node.block.ID}"
			class="row"
			class:targeted={scopeID === node.block.ID}
			class:flash={flashID === node.block.ID}
		>
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

			{#if node.block.Meta?.[META_KEY_AUTHOR] && editingID !== node.block.ID}
				{@const byHuman = node.block.Meta[META_KEY_AUTHOR] === AUTHOR_HUMAN}
				<span class="author" class:you={byHuman}>
					{byHuman ? "you" : "AI"}
				</span>
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
	<header class="head">
		<h1>Comind</h1>
		<button type="button" class="export" onclick={exportBase} disabled={busy}>Export ↓</button>
	</header>

	<form
		onsubmit={(e) => {
			e.preventDefault();
			askAI();
		}}
	>
		<p class="scope">
			{#if scopeID === null}
				New topic — expanded from scratch.
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

	<div class="search">
		<input
			aria-label="search the base"
			placeholder="search the base…"
			bind:value={searchQuery}
			oninput={onSearchInput}
		/>
	</div>

	{#if searchResults !== null}
		<div class="results">
			{#if searchResults.length === 0}
				<p class="muted">No matches.</p>
			{:else}
				<ul class="hits">
					{#each searchResults as hit (hit.ID)}
						<li>
							<button type="button" class="hit" onclick={() => revealBlock(hit.ID)}>
								<code>{hit.Content}</code>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/if}

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
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.75rem;
	}
	h1 {
		font-size: 1.3rem;
		margin: 0;
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
	.search {
		margin-bottom: 0.75rem;
	}
	.search input {
		width: 100%;
	}
	.results {
		margin: 0 0 1rem;
		padding: 0.4rem 0.5rem;
		background: var(--ink-2);
		border: 1px solid var(--line);
		border-radius: 8px;
	}
	.results .muted {
		margin: 0.2rem 0.3rem;
		color: var(--muted);
	}
	.hits {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.hit {
		display: block;
		width: 100%;
		text-align: left;
		border: none;
		background: none;
		padding: 0.3rem 0.4rem;
		border-radius: 5px;
		cursor: pointer;
		color: inherit;
	}
	.hit:hover {
		background: rgba(91, 124, 250, 0.14);
	}
	.hit code {
		white-space: pre-wrap;
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
		background: var(--amber-soft);
		box-shadow: inset 2px 0 0 var(--amber);
		border-radius: 3px;
	}
	/* A search hit flashes with the sea accent when revealed in the tree. */
	.row.flash {
		animation: flash 1.5s ease-out;
		border-radius: 3px;
	}
	@keyframes flash {
		0%,
		30% {
			background: rgba(91, 124, 250, 0.28);
		}
		100% {
			background: transparent;
		}
	}
	/* The badge names the last writer of the content; muted by default so the
	   tree stays quiet, and sea-blue when the owner wrote it. */
	.author {
		flex: none;
		align-self: center;
		font-size: 0.62rem;
		letter-spacing: 0.02em;
		padding: 0.03rem 0.4rem;
		border-radius: 999px;
		border: 1px solid var(--line);
		color: var(--muted);
		opacity: 0.65;
	}
	.author.you {
		color: var(--sea);
		border-color: rgba(91, 124, 250, 0.4);
		opacity: 0.9;
	}
	/* The AI target is a per-row affordance kept out of the way until the row is
	   hovered or focused, so the tree stays quiet; the currently-targeted row keeps
	   it visible. Amber — the knowledge/blocks side of the mark. */
	.target {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: var(--amber);
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
