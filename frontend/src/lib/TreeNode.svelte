<script lang="ts">
	import { Browser } from "@wailsio/runtime";
	import { base } from "../stores/base.svelte";
	import BlockLinks from "./BlockLinks.svelte";
	import HistoryPanel from "./HistoryPanel.svelte";
	import InlineEditor from "./InlineEditor.svelte";
	import { renderMarkdown } from "./markdown";
	import TreeNode from "./TreeNode.svelte";
	import { hasUnreviewed, type TreeNodeData } from "./tree";

	// One row of the base tree, recursing into its children. All state and
	// actions come from the base store; the node is the only input.
	let { node }: { node: TreeNodeData } = $props();

	const hasChildren = $derived(node.children.length > 0);
	const expanded = $derived(!base.collapsed.has(node.block.ID));
	const editing = $derived(base.editingID === node.block.ID);
	const linksOpen = $derived(base.openLinks.has(node.block.ID));
	const historyOpen = $derived(base.historyID === node.block.ID);
	// unreviewed marks a block carrying its own pending revision; ghost is the
	// subset that is also a pending deletion (merged into the forest by
	// base.svelte.ts's reload — see base.ghosts). A topic root additionally
	// gets a page-wide Accept all / Reject all when anything under it is dirty.
	const unreviewed = $derived(node.block.Unreviewed);
	const ghost = $derived(base.ghosts.has(node.block.ID));
	const topicRoot = $derived(node.block.ParentID === "");
	const pageDirty = $derived(topicRoot && hasUnreviewed(node));

	// Scroll this row into view and flash it when a search jump targets it. The
	// effect keys on highlightNonce so a repeat jump to the same block re-fires;
	// flash is toggled off then back on across a frame so the CSS animation
	// restarts rather than sitting at its finished state.
	let rowEl: HTMLElement | undefined;
	let flash = $state(false);
	$effect(() => {
		void base.highlightNonce; // subscribe: bumped on every reveal
		if (base.highlightID !== node.block.ID) {
			return;
		}
		rowEl?.scrollIntoView({ block: "center", behavior: "smooth" });
		flash = false;
		requestAnimationFrame(() => {
			flash = true;
		});
	});

	// The content is a click-to-edit surface that now holds rendered markdown, so
	// it is a div with button semantics rather than a <button> (a real button
	// cannot contain the links markdown may emit). A click on a link opens it in
	// the system browser — keeping the webview on the SPA — and must not also
	// start an edit; any other click, or Enter/Space, edits.
	function editFromClick(e: MouseEvent): void {
		const link = (e.target as HTMLElement).closest("a");
		if (link) {
			e.preventDefault();
			const href = link.getAttribute("href");
			if (href) void Browser.OpenURL(href);
			return;
		}
		if (!base.busy) base.startEdit(node.block);
	}
	function editFromKey(e: KeyboardEvent): void {
		if (e.key !== "Enter" && e.key !== " ") return;
		// A rendered link is focusable; activating it by keyboard must open the
		// link, not edit the block, so leave it to the anchor's own handling —
		// mirror the anchor guard in editFromClick.
		if ((e.target as HTMLElement).closest("a")) return;
		e.preventDefault();
		if (!base.busy) base.startEdit(node.block);
	}
</script>

<li>
	<div bind:this={rowEl} class="row" class:targeted={base.scopeID === node.block.ID} class:flash class:unreviewed class:ghost>
		<!-- Toggles are locked while an edit is open: collapsing an ancestor would
		     unmount the editor while editingID lingers, silently dead-ending every
		     other block (startEdit no-ops when a block is already being edited). -->
		{#if hasChildren}
			<button
				type="button"
				class="toggle"
				aria-expanded={expanded}
				aria-label={expanded ? "collapse" : "expand"}
				disabled={base.editingID !== null}
				onclick={() => base.toggle(node.block.ID)}
			>
				{expanded ? "▾" : "▸"}
			</button>
		{:else}
			<span class="toggle leaf" aria-hidden="true">·</span>
		{/if}

		{#if ghost}
			<!-- A pending deletion: struck through, no edit/target/links affordances —
			     there is nothing left to edit or link until it is restored. -->
			<span class="content ghost-content">{@html renderMarkdown(node.block.Content)}</span>
		{:else if editing}
			<InlineEditor
				bind:value={base.editText}
				busy={base.busy}
				onsave={() => base.saveEdit()}
				oncancel={() => base.cancelEdit()}
			/>
		{:else}
			<!-- The click-to-edit surface holds rendered markdown (possibly links),
			     which a real <button> cannot legally contain, so it is a div with
			     button semantics (role + focusable + Enter/Space). role="button"
			     carries the same no-interactive-descendants ARIA constraint, so a
			     screen reader still reads the block as one control — a known
			     pragmatic trade-off for a single-user desktop tool, not a fix. -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<!-- biome-ignore lint/a11y/useSemanticElements: a <button> cannot contain the anchors/blocks rendered markdown emits -->
			<div
				class="content"
				class:busy={base.busy}
				role="button"
				tabindex={base.busy ? -1 : 0}
				title="click to edit"
				onclick={editFromClick}
				onkeydown={editFromKey}
			>
				{@html renderMarkdown(node.block.Content)}
			</div>
			<button
				type="button"
				class="target"
				title="ask the AI to restructure this block"
				aria-label="target this block for the AI command"
				aria-pressed={base.scopeID === node.block.ID}
				disabled={base.busy || base.editingID !== null}
				onclick={() => base.setScope(node.block)}
			>
				⌖
			</button>
			<button
				type="button"
				class="links-toggle"
				class:open={linksOpen}
				title="show this block's links"
				aria-expanded={linksOpen}
				aria-label="toggle this block's links"
				disabled={base.busy}
				onclick={() => base.toggleLinks(node.block.ID)}
			>
				∞
			</button>
		{/if}

		<!-- History is allowed on ghosts too: it is how a pending deletion is
		     inspected or undone before review, unlike edit/target/links which stay
		     hidden until the block is restored. -->
		<button
			type="button"
			class="history-toggle"
			class:open={historyOpen}
			title="show this block's revision history"
			aria-expanded={historyOpen}
			aria-label="toggle this block's revision history"
			disabled={base.busy}
			onclick={() => base.toggleHistory(node.block.ID)}
		>
			⌚
		</button>

		{#if unreviewed}
			<button
				type="button"
				class="accept"
				title="accept this change"
				aria-label="accept this change"
				disabled={base.busy}
				onclick={() => base.accept(node.block.ID)}
			>
				✓
			</button>
			<button
				type="button"
				class="reject"
				title={ghost ? "restore this block" : "reject: revert to the last reviewed version"}
				aria-label={ghost ? "restore this block" : "reject: revert to the last reviewed version"}
				disabled={base.busy}
				onclick={() => base.reject(node.block.ID)}
			>
				✗
			</button>
		{/if}

		{#if pageDirty}
			<button
				type="button"
				class="accept-page"
				title="accept every unreviewed change on this page"
				disabled={base.busy}
				onclick={() => base.acceptPage(node.block.ID)}
			>
				Accept all
			</button>
			<button
				type="button"
				class="reject-page"
				title="reject every unreviewed change on this page"
				disabled={base.busy}
				onclick={() => base.rejectPage(node.block.ID)}
			>
				Reject all
			</button>
		{/if}
	</div>
	{#if linksOpen}
		<BlockLinks block={node.block} />
	{/if}
	{#if historyOpen}
		<HistoryPanel block={node.block} />
	{/if}
	{#if hasChildren && expanded}
		<ul>
			{#each node.children as child (child.block.ID)}
				<TreeNode node={child} />
			{/each}
		</ul>
	{/if}
</li>

<style>
	/* The forest is a plain nested list; indentation and a faint guide rail carry
	   the hierarchy. Colours are theme-neutral (grey alpha) so the rail and hover
	   read on both light and dark backgrounds. */
	ul {
		list-style: none;
		margin: 0 0 0 0.55rem;
		padding: 0 0 0 0.7rem;
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
		min-width: 0;
	}
	.content.busy {
		cursor: default;
	}
	.content:not(.busy):hover {
		background: rgba(127, 127, 127, 0.15);
	}
	.row.targeted {
		background: rgba(127, 127, 127, 0.12);
		border-radius: 3px;
	}
	/* A jump from search flashes the row once, then fades back — long enough for
	   the eye to catch it after the scroll, leaving no lingering state. The tint
	   is a yellow alpha that reads on both themes. */
	.row.flash {
		border-radius: 3px;
		animation: flash 1.8s ease-out;
	}
	@keyframes flash {
		0% {
			background: rgba(255, 213, 0, 0.55);
		}
		100% {
			background: rgba(255, 213, 0, 0);
		}
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
	/* Rendered markdown is injected via {@html}, so Svelte's scoping does not
	   reach it — style it through :global under .content. Block content is one
	   short block (a heading, a claim), so headings must not blow up a tree row:
	   collapse every level to the row's own size and let weight carry emphasis. */
	.content :global(p) {
		margin: 0;
	}
	.content :global(h1),
	.content :global(h2),
	.content :global(h3),
	.content :global(h4),
	.content :global(h5),
	.content :global(h6) {
		margin: 0;
		font-size: 1em;
		font-weight: 650;
	}
	.content :global(ul),
	.content :global(ol) {
		margin: 0.15rem 0;
		padding-left: 1.1rem;
	}
	.content :global(code) {
		white-space: pre-wrap;
		font-size: 0.92em;
		padding: 0.05rem 0.25rem;
		border-radius: 3px;
		background: rgba(127, 127, 127, 0.16);
	}
	.content :global(pre) {
		margin: 0.2rem 0;
		padding: 0.35rem 0.5rem;
		border-radius: 4px;
		overflow-x: auto;
		background: rgba(127, 127, 127, 0.12);
	}
	.content :global(pre code) {
		padding: 0;
		background: none;
	}
	.content :global(a) {
		color: #3b6fd6;
	}
	.content :global(blockquote) {
		margin: 0.15rem 0;
		padding-left: 0.5rem;
		border-left: 2px solid rgba(127, 127, 127, 0.35);
		opacity: 0.85;
	}
	.content :global(:first-child) {
		margin-top: 0;
	}
	.content :global(:last-child) {
		margin-bottom: 0;
	}
	.links-toggle {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0;
	}
	.row:hover .links-toggle,
	.row:focus-within .links-toggle,
	.links-toggle.open {
		opacity: 0.6;
	}
	.links-toggle:hover:not(:disabled) {
		opacity: 1;
	}
	.history-toggle {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0;
	}
	.row:hover .history-toggle,
	.row:focus-within .history-toggle,
	.history-toggle.open {
		opacity: 0.6;
	}
	.history-toggle:hover:not(:disabled) {
		opacity: 1;
	}
	/* Unreviewed rows get a standing orange rail — unlike .target/.links-toggle
	   this is not a hover affordance, it is a status that must read at a glance
	   across the whole tree. A ghost (pending delete) additionally dims, since
	   it is on its way out pending Reject. */
	.row.unreviewed {
		padding-left: 0.3rem;
		border-left: 2px solid rgba(255, 165, 0, 0.8);
	}
	.row.ghost {
		opacity: 0.55;
	}
	.ghost-content {
		flex: 1;
		padding: 0.1rem 0.3rem;
		text-decoration: line-through;
	}
	/* accept/reject are a per-row status action, not a hover affordance like
	   .target: always visible while a block is unreviewed, so the decision is
	   never hidden behind a hover the reader has to discover. */
	.accept,
	.reject,
	.accept-page,
	.reject-page {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0.75;
	}
	.accept:hover:not(:disabled),
	.reject:hover:not(:disabled),
	.accept-page:hover:not(:disabled),
	.reject-page:hover:not(:disabled) {
		opacity: 1;
	}
	.accept-page,
	.reject-page {
		font-size: 0.8rem;
	}
</style>
