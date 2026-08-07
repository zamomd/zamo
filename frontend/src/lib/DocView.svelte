<script lang="ts">
	import { Browser } from "@wailsio/runtime";
	import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { base } from "../stores/base.svelte";
	import { renderedOffsetFromPoint, sourceOffset } from "./caret";
	import { renderMarkdown } from "./markdown";
	import { makeResolver, refClick } from "./resolve";

	let { block }: { block: Block } = $props();

	const editing = $derived(base.editingID === block.ID);
	const resolveRef = $derived(makeResolver(base.forest));
	const html = $derived(renderMarkdown(block.Content, resolveRef));

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

{#if editing}
	<textarea
		bind:this={area}
		bind:value={base.editText}
		aria-label="document content"
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
