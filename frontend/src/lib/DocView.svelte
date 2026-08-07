<script lang="ts">
	import { Browser } from "@wailsio/runtime";
	import { ai } from "../stores/ai.svelte";
	import { base } from "../stores/base.svelte";
	import { changes } from "../stores/changes.svelte";
	import Backlinks from "./Backlinks.svelte";
	import { renderedOffsetFromPoint, sourceOffset } from "./caret";
	import HistoryPanel from "./HistoryPanel.svelte";
	import { renderMarkdown } from "./markdown";
	import { makeResolver, refClick } from "./resolve";
	import { hasUnreviewed, type TreeNodeData } from "./tree";

	let { node }: { node: TreeNodeData } = $props();

	// A document is one block, so every affordance TreeNode keys by block id
	// keys by this one — the root's. The panels, the review decisions and the
	// AI scope all take it unchanged; only the layout differs.
	const block = $derived(node.block);
	const editing = $derived(base.editingID === block.ID);
	const resolveRef = $derived(makeResolver(base.forest));
	const html = $derived(renderMarkdown(block.Content, resolveRef));

	// The tree says this with a hollow dot, whose meaning comes from the filled
	// dots around it. A document is one block with nothing to contrast against,
	// so it says it in words instead, in the same channel as the lane's other
	// small dim labels.
	const byAI = $derived(block.Meta?.author === "ai");
	const backlinksOpen = $derived(base.openBacklinks.has(block.ID));
	const historyOpen = $derived(base.historyID === block.ID);
	const unreviewed = $derived(block.Unreviewed);
	const atomic = $derived(changes.atomic.has(block.ID));
	// The page carries the tombstones of whatever a conversion removed as well
	// as the document itself, so it can be dirty while the root is not.
	const pageDirty = $derived(hasUnreviewed(node));
	const straddling = $derived(changes.straddling.has(block.ID));
	// A later edit — typically the first keystroke into this document — sits on
	// top of the pending batch, so there is no giving it back any more.
	const writtenOver = $derived(changes.writtenOverPages.has(block.ID));

	let area: HTMLTextAreaElement | undefined = $state();
	let contentEl: HTMLElement | undefined = $state();

	// The textarea replaces the rendered div outright when editing starts, so
	// the browser's own caret cannot survive the swap — it has to be placed
	// explicitly from the offset editFromClick (below) computed.
	$effect(() => {
		if (!editing || area === undefined) {
			return;
		}
		area.focus();
		area.setSelectionRange(base.editCaret, base.editCaret);
	});

	// pendingTopic mirrors TreeNode's own confirmation row: confirm()/alert()/
	// prompt() are no-ops in the Wails webview, so creating an unresolved
	// [[Topic]] reference needs a real element rather than a blocking dialog.
	let pendingTopic: { name: string } | null = $state(null);

	function clickRef(el: HTMLElement): void {
		const action = refClick(base.forest, el);
		if (action === null) {
			return;
		}
		if (action.kind === "create") {
			pendingTopic = { name: action.name };
			return;
		}
		base.reveal(action.id);
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

	// editFromClick routes a click the same way TreeNode's does: a reference
	// navigates or offers to create its target, a real link opens in the
	// system browser, and anything else opens the document for editing with
	// the caret placed under the pointer.
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
		const raw = block.Content;
		const rendered = contentEl === undefined ? null : renderedOffsetFromPoint(contentEl, e.clientX, e.clientY);
		const caret = rendered === null ? raw.length : sourceOffset(raw, rendered, resolveRef);
		void base.startEdit(block, caret);
	}

	function editFromKey(e: KeyboardEvent): void {
		if (e.key !== "Enter" && e.key !== " ") return;
		if ((e.target as HTMLElement).closest("a")) return;
		e.preventDefault();
		if (!base.busy) {
			void base.startEdit(block, block.Content.length);
		}
	}

	// A document is one block edited whole: Enter inserts a newline (the
	// textarea's default) and Tab indents two spaces rather than nesting or
	// leaving the field, which is what a markdown editor is expected to do.
	// Esc ends editing by committing, same as everywhere else in the app
	// (InlineEditor) — the keyboard is never trapped, but it also never
	// silently discards unsaved text.
	function onKeydown(event: KeyboardEvent): void {
		if (event.key === "Escape") {
			event.preventDefault();
			void base.commitEdit();
			return;
		}
		if (event.key !== "Tab") {
			return;
		}
		event.preventDefault();
		const el = event.currentTarget as HTMLTextAreaElement;
		const at = el.selectionStart;
		base.editText = `${el.value.slice(0, at)}  ${el.value.slice(el.selectionEnd)}`;
		queueMicrotask(() => el.setSelectionRange(at + 2, at + 2));
	}
</script>

<!-- The controls a tree row keeps in its gutters. A document has no rows to
     hang them off, so they sit once above the text, in TreeNode's order and
     with the same labels, and act on the root block exactly as they do there. -->
<div class="controls">
	{#if byAI}
		<span class="by-ai" title="the AI wrote this document">AI</span>
	{/if}
	{#if ai.enabled}
		<button
			type="button"
			class="write"
			title="ask the AI to write this document"
			disabled={base.busy}
			onclick={() => base.expandTopic(block.ID)}
		>
			Write with AI
		</button>
		<button
			type="button"
			class="target"
			title="ask the AI to rewrite this document"
			aria-label="target this document for the AI command"
			aria-pressed={base.scopeID === block.ID}
			disabled={base.busy}
			onclick={() => base.setScope(block)}
		>
			⌖
		</button>
	{/if}
	<button
		type="button"
		class="backlinks-toggle"
		class:open={backlinksOpen}
		title="show blocks that link here"
		aria-expanded={backlinksOpen}
		aria-label="toggle linked references"
		disabled={base.busy}
		onclick={() => base.toggleBacklinks(block.ID)}
	>
		∞
	</button>
	<button
		type="button"
		class="history-toggle"
		class:open={historyOpen}
		title="show this document's revision history"
		aria-expanded={historyOpen}
		aria-label="toggle this document's revision history"
		disabled={base.busy}
		onclick={() => base.toggleHistory(block.ID)}
	>
		⌚︎
	</button>

	{#if unreviewed && atomic}
		<!-- Part of a conversion or a rename: the backend refuses a decision on
		     this block alone, so the Changes screen's batch controls are named
		     instead of offering a button that could only fail. -->
		<span class="batch-only" title="part of one change — decide on the whole batch in Changes">batch</span>
	{:else if unreviewed}
		<button
			type="button"
			class="accept"
			title="accept this change"
			aria-label="accept this change"
			disabled={base.busy}
			onclick={() => base.accept(block.ID)}
		>
			✓
		</button>
		<button
			type="button"
			class="reject"
			title="reject: revert to the last reviewed version"
			aria-label="reject: revert to the last reviewed version"
			disabled={base.busy}
			onclick={() => base.reject(block.ID)}
		>
			✗
		</button>
	{/if}

	{#if pageDirty && straddling}
		<span class="batch-only" title="this page holds part of one change — decide on the whole batch in Changes"
			>batch page</span
		>
	{:else if pageDirty}
		<button
			type="button"
			class="accept-page"
			title="accept every unreviewed change on this page"
			disabled={base.busy}
			onclick={() => base.acceptPage(block.ID)}
		>
			Accept all
		</button>
		{#if writtenOver}
			<!-- Rejecting would have to discard the later edit sitting on top of
			     this batch, which the backend refuses, so the button is not
			     offered. Converting back is the gesture that still works, and it
			     keeps that edit. -->
			<span
				class="batch-only"
				title="a later edit was saved on top of this change, so it can only be accepted now — use Convert to blocks to change the shape back"
				>accept only</span
			>
		{:else}
			<button
				type="button"
				class="reject-page"
				title="reject every unreviewed change on this page"
				disabled={base.busy}
				onclick={() => base.rejectPage(block.ID)}
			>
				Reject all
			</button>
		{/if}
	{/if}
</div>

<div class="document" class:unreviewed>
	{#if editing}
		<!-- readonly, not disabled: it preserves focus while a write is in
		     flight, and a document edit is a long session over a large field,
		     so text typed into a stale value would be dropped silently. -->
		<textarea
			bind:this={area}
			bind:value={base.editText}
			aria-label="document content"
			readonly={base.busy || base.commitInFlight}
			onkeydown={onKeydown}
			onblur={() => base.commitEdit()}
			spellcheck="false"
		></textarea>
	{:else}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<!-- biome-ignore lint/a11y/useSemanticElements: a <button> cannot contain the anchors/blocks rendered markdown emits -->
		<div
			bind:this={contentEl}
			class="rendered"
			class:busy={base.busy}
			role="button"
			tabindex={base.busy ? -1 : 0}
			onclick={editFromClick}
			onkeydown={editFromKey}
		>
			{@html html}
		</div>
	{/if}
</div>

{#if backlinksOpen}
	<Backlinks {block} />
{/if}
{#if historyOpen}
	<HistoryPanel {block} />
{/if}

{#if pendingTopic}
	<!-- confirm() is a no-op in the Wails webview, so this row is the
	     confirmation — see TreeNode's own ref-create row. -->
	<div class="ref-create">
		<span>Create topic “{pendingTopic.name}”?</span>
		<button type="button" disabled={base.busy} onclick={confirmCreateTopic}>create</button>
		<button type="button" onclick={cancelCreateTopic}>cancel</button>
	</div>
{/if}

<style>
	/* One lane for every affordance a tree row spreads across its gutters. It
	   stays visible rather than fading in on hover as TreeNode's gutters do:
	   there is no row to hover, and a document's controls are the page's. */
	.controls {
		display: flex;
		align-items: center;
		gap: 0.15rem;
		min-height: var(--row-height);
	}
	.controls button {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0.6;
	}
	.controls button:hover:not(:disabled),
	.controls button.open,
	.controls button.target[aria-pressed="true"] {
		opacity: 1;
	}
	/* The three word-labelled controls, at the size TreeNode gives its own. */
	.write,
	.accept-page,
	.reject-page {
		font-size: var(--size-small);
	}
	.batch-only,
	.by-ai {
		flex: none;
		padding: 0 0.3rem;
		font-size: var(--size-small);
		color: var(--text-dim);
	}
	/* The same standing mark an unreviewed tree row carries, at the one scale a
	   document has: the whole text. */
	.document.unreviewed {
		padding-left: 0.3rem;
		border-left: 2px solid var(--text);
	}
	textarea {
		width: 100%;
		min-height: 60vh;
		resize: none;
		border: none;
		outline: none;
		background: none;
		color: inherit;
		font: inherit;
		white-space: pre-wrap;
	}
	.rendered {
		min-height: 60vh;
		cursor: text;
	}
	.rendered.busy {
		cursor: default;
	}
	.rendered :global(h1),
	.rendered :global(h2),
	.rendered :global(h3),
	.rendered :global(h4),
	.rendered :global(h5),
	.rendered :global(h6) {
		margin: 1em 0 0.4em;
	}
	.rendered :global(p) {
		margin: 0.4em 0;
	}
	.rendered :global(ul),
	.rendered :global(ol) {
		margin: 0.4em 0;
		padding-left: 1.4rem;
	}
	.rendered :global(code) {
		white-space: pre-wrap;
		font-size: 0.92em;
		padding: 0.05rem 0.25rem;
		border-radius: var(--radius);
		background: var(--surface);
	}
	.rendered :global(pre) {
		margin: 0.6em 0;
		padding: 0.5rem 0.7rem;
		border-radius: var(--radius);
		overflow-x: auto;
		background: var(--surface);
	}
	.rendered :global(pre code) {
		padding: 0;
		background: none;
	}
	.rendered :global(a) {
		color: var(--accent);
	}
	/* A reference reads like a link but is not one — see TreeNode's own .ref
	   rules, which this mirrors so a document behaves identically. */
	.rendered :global(.ref) {
		color: var(--accent);
		cursor: pointer;
		text-decoration: underline dotted;
		text-underline-offset: 2px;
	}
	.rendered :global(.ref-missing) {
		color: var(--text-dim);
	}
	.rendered :global(blockquote) {
		margin: 0.4em 0;
		padding-left: 0.6rem;
		border-left: 2px solid var(--hairline);
		opacity: 0.85;
	}
	.rendered :global(:first-child) {
		margin-top: 0;
	}
	.rendered :global(:last-child) {
		margin-bottom: 0;
	}
	.ref-create {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin: 0.5rem 0;
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
</style>
