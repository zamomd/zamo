<script lang="ts">
	import { Browser } from "@wailsio/runtime";
	import { base } from "../stores/base.svelte";
	import { changes } from "../stores/changes.svelte";
	import Backlinks from "./Backlinks.svelte";
	import { renderedOffsetFromPoint, sourceOffset } from "./caret";
	import HistoryPanel from "./HistoryPanel.svelte";
	import InlineEditor from "./InlineEditor.svelte";
	import { renderMarkdown } from "./markdown";
	import { firstLine, normalizeTitle, type Ref } from "./refs";
	import TreeNode from "./TreeNode.svelte";
	import { findBlock, hasUnreviewed, type TreeNodeData } from "./tree";

	let {
		node,
		nested = false,
		title = false,
	}: { node: TreeNodeData; nested?: boolean; title?: boolean } = $props();

	const hasChildren = $derived(node.children.length > 0);
	const expanded = $derived(!base.collapsed.has(node.block.ID));
	const editing = $derived(base.editingID === node.block.ID);
	const backlinksOpen = $derived(base.openBacklinks.has(node.block.ID));
	const historyOpen = $derived(base.historyID === node.block.ID);
	const unreviewed = $derived(node.block.Unreviewed);
	const atomic = $derived(changes.atomic.has(node.block.ID));
	const ghost = $derived(base.ghosts.has(node.block.ID));
	const topicRoot = $derived(node.block.ParentID === "");
	const pageDirty = $derived(topicRoot && hasUnreviewed(node));
	const straddling = $derived(changes.straddling.has(node.block.ID));
	const byAI = $derived(node.block.Meta?.author === "ai");
	const hasHidden = $derived(hasChildren && !expanded);

	let rowEl: HTMLElement | undefined;
	let contentEl: HTMLElement | undefined = $state();
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

	// findTopicRoot looks a [[Topic]] reference up by its normalized key among
	// the topics the store already holds — the same key blocks.title_key
	// carries on the Go side, so this is the client-side half of resolution.
	function findTopicRoot(targetKey: string): TreeNodeData | null {
		return base.forest.find((t) => normalizeTitle(t.block.Content) === targetKey) ?? null;
	}

	// resolveRef backs renderMarkdown's live-lookup: null means unresolved, in
	// which case the renderer falls back to the reference's own written text.
	function resolveRef(ref: Ref): string | null {
		if (ref.kind === "topic") {
			const root = findTopicRoot(ref.targetKey);
			return root ? firstLine(root.block.Content) : null;
		}
		const block = findBlock(base.forest, ref.targetKey);
		return block ? firstLine(block.Content) : null;
	}

	// pendingTopic holds the unresolved [[Topic]] name a click just asked to
	// create. confirm()/alert()/prompt() are no-ops in the Wails webview (see
	// AGENTS.md), so the confirmation has to be a real element in the row
	// rather than a blocking dialog.
	let pendingTopic: { name: string } | null = $state(null);

	// clickRef routes a click on a rendered reference: a resolved one navigates
	// within the tree, an unresolved topic offers to create it, and an
	// unresolved block reference — nothing points at a gone or not-yet-synced
	// block — does nothing.
	function clickRef(el: HTMLElement): void {
		const targetKey = el.dataset.target ?? "";
		const isTopic = el.classList.contains("ref-topic");
		if (el.classList.contains("ref-missing")) {
			if (isTopic) {
				pendingTopic = { name: el.textContent ?? targetKey };
			}
			return;
		}
		if (isTopic) {
			const root = findTopicRoot(targetKey);
			if (root) base.reveal(root.block.ID);
			return;
		}
		if (findBlock(base.forest, targetKey)) {
			base.reveal(targetKey);
		}
	}

	async function confirmCreateTopic(): Promise<void> {
		if (pendingTopic === null || base.busy) {
			return;
		}
		const { name } = pendingTopic;
		pendingTopic = null;
		await base.createTopic(name);
	}

	function cancelCreateTopic(): void {
		pendingTopic = null;
	}

	function editFromClick(e: MouseEvent): void {
		const ref = (e.target as HTMLElement).closest<HTMLElement>(".ref[data-target]");
		if (ref) {
			e.preventDefault();
			clickRef(ref);
			return;
		}
		const link = (e.target as HTMLElement).closest("a");
		if (link) {
			e.preventDefault();
			const href = link.getAttribute("href");
			if (href) void Browser.OpenURL(href);
			return;
		}
		if (base.busy) {
			return;
		}
		const raw = node.block.Content;
		const rendered =
			contentEl === undefined ? null : renderedOffsetFromPoint(contentEl, e.clientX, e.clientY);
		const caret = rendered === null ? raw.length : sourceOffset(raw, rendered, resolveRef);
		void base.startEdit(node.block, caret);
	}
	function editFromKey(e: KeyboardEvent): void {
		if (e.key !== "Enter" && e.key !== " ") return;
		if ((e.target as HTMLElement).closest("a")) return;
		e.preventDefault();
		if (!base.busy) {
			void base.startEdit(node.block, node.block.Content.length);
		}
	}
</script>

<li class:nested>
	<div
		bind:this={rowEl}
		class="row"
		class:title
		class:targeted={base.scopeID === node.block.ID}
		class:flash
		class:unreviewed
		class:ghost
	>
		{#if !title}
			<!-- The dot identifies and zooms the block; the triangle controls collapse. -->
			<button
				type="button"
				class="dot"
				class:ai={byAI}
				class:ring={hasHidden}
				title="zoom into this block"
				aria-label="zoom into this block"
				disabled={base.busy}
				onclick={() => base.focus(node.block.ID)}
			></button>
			{#if hasChildren}
				<button
					type="button"
					class="toggle"
					aria-expanded={expanded}
					aria-label={expanded ? "collapse" : "expand"}
					disabled={base.busy}
					onclick={() => base.toggle(node.block.ID)}
				>
					{expanded ? "▾" : "▸"}
				</button>
			{/if}
		{/if}

		{#if ghost}
			<!-- A pending deletion: struck through, no edit/target affordances —
			     there is nothing left to edit until it is restored. -->
			<span class="content ghost-content">{@html renderMarkdown(node.block.Content, resolveRef)}</span>
		{:else if editing}
			<InlineEditor id={node.block.ID} />
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
				bind:this={contentEl}
				class="content"
				class:busy={base.busy}
				role="button"
				tabindex={base.busy ? -1 : 0}
				title="click to put the caret here"
				onclick={editFromClick}
				onkeydown={editFromKey}
			>
				{@html renderMarkdown(node.block.Content, resolveRef)}
			</div>
		{/if}

		{#if !ghost}
			<!-- Outside the branch above, so the lane keeps its width while the row
			     is edited: handing the actions' width to the text column mid-click
			     rewraps the block under the pointer and lands the caret on a
			     different visual line than the one that was clicked. The controls
			     themselves are hidden (and untabbable) while editing — the same
			     reservation the fade already makes for a row at rest. -->
			<span class="gutter" class:reserved={editing}>
				<button
					type="button"
					class="target"
					title="ask the AI to restructure this block"
					aria-label="target this block for the AI command"
					aria-pressed={base.scopeID === node.block.ID}
					disabled={base.busy || editing}
					onclick={() => base.setScope(node.block)}
				>
					⌖
				</button>
				<button
					type="button"
					class="backlinks-toggle"
					class:open={backlinksOpen}
					title="show blocks that link here"
					aria-expanded={backlinksOpen}
					aria-label="toggle linked references"
					disabled={base.busy || editing}
					onclick={() => base.toggleBacklinks(node.block.ID)}
				>
					∞
				</button>
			</span>
		{/if}

		<!-- History is allowed on ghosts too: it is how a pending deletion is
		     inspected or undone before review, unlike edit/target which stay
		     hidden until the block is restored. It therefore needs a gutter of its
		     own rather than sharing the one above, which lives inside that guard;
		     two adjacent flex containers lay out as one lane. -->
		<span class="gutter">
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
				⌚︎
			</button>
		</span>

		{#if unreviewed && atomic}
			<!-- Part of a topic rename: the backend refuses a decision on this
			     block alone, so the Changes screen's batch controls are named
			     instead of offering a button that could only fail. -->
			<span class="batch-only" title="part of a topic rename — decide on the whole batch in Changes">batch</span>
		{:else if unreviewed}
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

		{#if pageDirty && straddling}
			<!-- The page holds only part of a topic rename, so a page-wide
			     decision is refused the same way a per-block one is. -->
			<span class="batch-only" title="this page holds part of a topic rename — decide on the whole batch in Changes"
				>batch page</span
			>
		{:else if pageDirty}
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
	{#if backlinksOpen}
		<Backlinks block={node.block} />
	{/if}
	{#if historyOpen}
		<HistoryPanel block={node.block} />
	{/if}
	{#if pendingTopic}
		<!-- confirm() is a no-op in the Wails webview, so this row is the
		     confirmation — see clickRef. -->
		<div class="ref-create">
			<span>Create topic “{pendingTopic.name}”?</span>
			<button type="button" disabled={base.busy} onclick={confirmCreateTopic}>create</button>
			<button type="button" onclick={cancelCreateTopic}>cancel</button>
		</div>
	{/if}
	{#if hasChildren && expanded}
		<ul class="children">
			{#each node.children as child (child.block.ID)}
				<TreeNode node={child} nested={true} />
			{/each}
		</ul>
	{/if}
</li>

<style>
	/* The forest is a plain nested list. Indentation and the rails carry the
	   hierarchy: each item draws a vertical segment for its own level and a
	   horizontal tick into its row, so a run of siblings reads as the box-
	   drawing tree the design calls for, without box-drawing characters
	   landing in the text itself. The rails hang off the <li> itself (the
	   nested class the recursion sets below) rather than off a
	   `ul.children > li` descendant selector: this component recurses through
	   a child <TreeNode> component, not a literal <li> in its own template, so
	   Svelte's static CSS scoping cannot see through that boundary — a
	   selector that only matches through it is dead-code-eliminated from the
	   build, not just warned about. */
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	ul.children {
		margin-left: var(--indent);
	}
	/* The positioning context for both the rails and the two leading glyphs is
	   the <li>, never .row: the rails are drawn as the li's own pseudo-elements
	   from its padding edge, while .row starts one full --indent to the right of
	   them. Anchoring .dot or .toggle to .row would slide both that far past the
	   tick they are meant to land on. */
	li {
		position: relative;
	}
	li.nested {
		padding-left: var(--indent);
	}
	/* The vertical segment: full height for a middle sibling, cut at the tick
	   for the last one, which is what turns the corner from a T into an L. */
	/* The rails need their own token, not --hairline: a rectangle's border is
	   four edges the eye integrates, a rail is one 1px line, and at --hairline's
	   ~1.26:1 it simply is not there. */
	li.nested::before {
		content: "";
		position: absolute;
		left: 0;
		top: 0;
		bottom: 0;
		width: 1px;
		background: var(--rail);
	}
	li.nested:last-child::before {
		bottom: auto;
		height: calc(var(--row-height) / 2);
	}
	/* The horizontal tick, stopping short of the text so the join stays open. */
	li.nested::after {
		content: "";
		position: absolute;
		left: 0;
		top: calc(var(--row-height) / 2);
		width: calc(var(--indent) - 6px);
		height: 1px;
		background: var(--rail);
	}
	.row {
		display: flex;
		align-items: flex-start;
		gap: 0.15rem;
		min-height: var(--row-height);
	}
	/* The dot sits on the join between the rail's tick and the text, centred on
	   the row's first line — absolutely, from the top of the <li>, so a block
	   that wraps to four lines keeps its anchor beside the first one. */
	.dot {
		position: absolute;
		left: calc(var(--indent) - 6px);
		top: calc((var(--row-height) - 5px) / 2);
		width: 5px;
		height: 5px;
		padding: 0;
		border: none;
		border-radius: 50%;
		background: var(--text-dim);
		cursor: pointer;
	}
	/* Hollow means the AI wrote it. Fill is the channel, so the halo below can
	   carry collapse without either fact hiding the other. */
	.dot.ai {
		background: transparent;
		border: 1px solid var(--text-dim);
	}
	/* The halo has a token of its own rather than borrowing --hover: it is a
	   resting-state fact about the block, not feedback about the pointer, and
	   --hover is far too faint to carry it — see --halo in app.css. */
	.dot.ring {
		box-shadow: 0 0 0 4px var(--halo);
	}
	.dot:hover:not(:disabled) {
		background: var(--text);
	}
	.dot.ai:hover:not(:disabled) {
		background: transparent;
		border-color: var(--text);
	}
	/* The triangle takes the tick's place on hover rather than sitting beside
	   it: 14px of horizontal rail and a glyph in the same slot read as debris.
	   Three details make that mask hold, none of them visible in the values:
	   z-index lifts the button over li.nested::after, which is the li's last
	   child in tree order and would otherwise paint its 1px rail on top of an
	   opaque background. It does not help on a ghost row, where .row.ghost's
	   opacity makes .row a stacking context that confines this z-index — and
	   makes the mask translucent regardless, so the tick shows through there.
	   left starts at 1px rather than 0 so li.nested::before, the vertical
	   sibling connector, is not erased for a whole row height; the price is the
	   tick's leftmost pixel, which stays visible inside the connector's own 1px
	   column and is the T- or L-junction the eye expects to see there anyway.
	   The background is repeated on :hover below because app.css's global
	   button:hover:not(:disabled) outweighs a single scoped class. */
	.toggle {
		position: absolute;
		left: 1px;
		top: 0;
		z-index: 1;
		width: calc(var(--indent) - 7px);
		padding: 0;
		border: none;
		background: var(--ground);
		cursor: pointer;
		color: var(--text-dim);
		font-size: 8px;
		line-height: var(--row-height);
		text-align: center;
		opacity: 0;
	}
	.row:hover .toggle,
	.toggle:focus-visible {
		opacity: 1;
	}
	.toggle:hover:not(:disabled) {
		background: var(--ground);
		color: var(--text);
	}
	.content {
		flex: 1;
		text-align: left;
		border: none;
		background: none;
		padding: 0 var(--unit) 0 0;
		border-radius: var(--radius);
		cursor: pointer;
		color: inherit;
		min-width: 0;
		line-height: var(--row-height);
	}
	.content.busy {
		cursor: default;
	}
	.content:not(.busy):hover {
		background: var(--hover);
	}
	/* The view's root is the page's title, not a row of the tree: no dot, no
	   rails, and the one place type steps outside the 26px grid on purpose —
	   1.5 cells, which is what the heading it replaces already used. */
	.row.title .content {
		font-size: var(--size-title);
		line-height: calc(var(--row-height) * 1.5);
	}
	.row.title :global(.editor) {
		font-size: var(--size-title);
		line-height: calc(var(--row-height) * 1.5);
	}
	.row.targeted {
		background: var(--hover);
		border-radius: var(--radius);
	}
	/* A jump from search flashes the row once, then fades back. The tint is the
	   accent at low alpha rather than a yellow of its own: the flash says
	   "here", which is the same thing selection says, so it spends the same
	   colour instead of introducing a fourth one. */
	.row.flash {
		border-radius: var(--radius);
		animation: flash 1.8s ease-out;
	}
	@keyframes flash {
		0% {
			background: var(--flash);
		}
		100% {
			background: transparent;
		}
	}
	/* The actions reserve their lane whether or not they are visible: fading
	   them with opacity rather than removing them keeps a long block's text from
	   reflowing under the pointer.
	   The fade goes on the buttons, NOT on this wrapper. A child cannot exceed
	   its parent's opacity, so opacity:0 here would make the targeted row's own
	   mark and an open panel's icon unreachable — both of which must stay lit
	   without a hover. */
	.gutter {
		flex: none;
		display: flex;
		gap: 0.15rem;
	}
	.gutter button {
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0;
	}
	.row:hover .gutter button,
	.row:focus-within .gutter button {
		opacity: 0.6;
	}
	/* Three standing exceptions to the fade: the pointer is on the control, the
	   row is the AI command's target, or the control's panel is open below.
	   The targeted arm has to name .gutter and the button element, not just
	   .target: the hover/focus rule above contributes an element selector that
	   a plain .row.targeted .target cannot match, and the row holding focus
	   right after a click on the mark is exactly when it must stay lit. */
	.gutter button:hover:not(:disabled),
	.row.targeted .gutter button.target,
	.gutter button.open {
		opacity: 1;
	}
	/* An edited row keeps the lane and loses the controls. visibility, not
	   display or opacity: it holds the width the text column is measured
	   against, while taking the buttons out of the tab order — an opacity-only
	   hide would leave two focusable controls in the middle of a keyboard
	   edit. Last among the .gutter rules so it outranks the standing
	   exceptions above, which a targeted or open row would otherwise light up
	   under the caret. */
	.gutter.reserved button {
		visibility: hidden;
	}
	/* Rendered markdown is injected via {@html}, so Svelte's scoping does not
	   reach it — style it through :global under .content. */
	.content :global(p) {
		margin: 0;
	}
	/* Block content is one short block — a heading, a claim — so a heading must
	   not blow up a tree row. h1 and h2 step up one size and h3 half of one;
	   all three still fit the row's 26px line box, so the rails and the dot stay
	   on the grid. Deeper levels carry weight only: markdown's remaining levels
	   are finer distinctions than a block tree needs. */
	.content :global(h1),
	.content :global(h2) {
		margin: 0;
		font-size: var(--size-heading);
		font-weight: 650;
	}
	.content :global(h3) {
		margin: 0;
		font-size: 15px;
		font-weight: 650;
	}
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
		border-radius: var(--radius);
		background: var(--surface);
	}
	.content :global(pre) {
		margin: 0.2rem 0;
		padding: 0.35rem 0.5rem;
		border-radius: var(--radius);
		overflow-x: auto;
		background: var(--surface);
	}
	.content :global(pre code) {
		padding: 0;
		background: none;
	}
	.content :global(a) {
		color: var(--accent);
	}
	/* A reference reads like a link but is not one: a dotted underline instead
	   of a solid one keeps it visually distinct from a real hyperlink. Missing
	   dims to --text-dim rather than --danger — an unresolved reference is
	   routine (the target may just not exist yet), not an error. */
	.content :global(.ref) {
		color: var(--accent);
		cursor: pointer;
		text-decoration: underline dotted;
		text-underline-offset: 2px;
	}
	.content :global(.ref-missing) {
		color: var(--text-dim);
	}
	.content :global(blockquote) {
		margin: 0.15rem 0;
		padding-left: 0.5rem;
		border-left: 2px solid var(--hairline);
		opacity: 0.85;
	}
	.content :global(:first-child) {
		margin-top: 0;
	}
	.content :global(:last-child) {
		margin-bottom: 0;
	}
	/* Unreviewed rows get a standing rail at full text strength while every
	   other rail is a hairline: the mark reads at a glance across the tree.
	   The window's one accent belongs to focus rings, the active tab and
	   markdown links, not to this rail — it stays neutral so the mark
	   survives a repaint of those other things.
	   A ghost (pending delete) additionally dims, since it is on its way out
	   pending Reject. */
	.row.unreviewed {
		padding-left: 0.3rem;
		border-left: 2px solid var(--text);
	}
	.row.ghost {
		opacity: 0.55;
	}
	.ghost-content {
		flex: 1;
		padding: 0.1rem 0.3rem;
		text-decoration: line-through;
	}
	/* Same indent and quiet styling as HistoryPanel's .history: a small panel
	   hanging off this row, not a competing block. */
	.ref-create {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin: 0.1rem 0 0.25rem 1.25rem;
		padding: 0.2rem 0.4rem;
		font-size: var(--size-small);
		border-left: 2px solid var(--hairline);
	}
	.ref-create button {
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.1rem 0.5rem;
		cursor: pointer;
		color: inherit;
	}
	.ref-create button:hover:not(:disabled) {
		background: var(--hover);
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
		font-size: var(--size-small);
	}
	/* Sits where accept/reject would, so an atomic block's row keeps its shape. */
	.batch-only {
		flex: none;
		padding: 0 0.3rem;
		font-size: var(--size-small);
		color: var(--text-dim);
	}
	/* Every control on the title row — accept/reject and Accept all/Reject all as
	   direct children of .row, plus the two gutters' icons — otherwise inherits
	   the body line-height and pins to the top of that row's taller line box (see
	   .row.title .content), above the glyphs it belongs to. Matching the line box
	   rather than centring the row keeps them beside the title's FIRST line when a
	   long title wraps, which is the rule the dot follows on a tree row.
	   Last in the file so it does not sit above the lower-specificity rules for
	   the same buttons; named class by class rather than as .row.title > button so
	   it cannot reach the dot or the triangle, whose geometry is the row grid's
	   and not the title's, if either is ever rendered on a title row. */
	.row.title .accept,
	.row.title .reject,
	.row.title .accept-page,
	.row.title .reject-page,
	.row.title .gutter button {
		line-height: calc(var(--row-height) * 1.5);
	}
</style>
