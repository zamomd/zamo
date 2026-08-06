<script lang="ts">
	import { untrack } from "svelte";
	import { base } from "../stores/base.svelte";
	import { caretColumn, lineCount, lineIndexOf } from "./caret";
	import RefAutocomplete from "./RefAutocomplete.svelte";
	import { triggerAt } from "./reftrigger";

	let { id }: { id: string } = $props();

	// $state, not a plain let: passed to RefAutocomplete as the `target` prop,
	// and a prop sourced from a non-reactive binding would not propagate.
	// Unmounting writes null here, and a queued select event still finds it.
	let el: HTMLTextAreaElement | null = $state(null);

	const trigger = $derived(triggerAt(base.editText, base.editCaret));

	function fit(): void {
		if (el === null) {
			return;
		}
		el.style.height = "auto";
		el.style.height = `${el.scrollHeight}px`;
	}

	$effect(() => {
		void base.editCaretNonce;
		if (el === null) {
			return;
		}
		const at = untrack(() => base.editCaret);
		el.focus();
		el.setSelectionRange(at, at);
		fit();
	});

	function reportCaret(): void {
		if (el === null) {
			return;
		}
		base.syncCaret(id, el.selectionStart);
	}

	$effect(() => {
		void base.editText;
		fit();
	});

	function visualLines(area: HTMLTextAreaElement): number | null {
		const style = getComputedStyle(area);
		const height = Number.parseFloat(style.lineHeight);
		if (!Number.isFinite(height) || height <= 0) {
			return null;
		}
		const inner = area.scrollHeight - Number.parseFloat(style.paddingTop) - Number.parseFloat(style.paddingBottom);
		return Math.max(1, Math.round(inner / height));
	}

	function onkeydown(e: KeyboardEvent): void {
		if (e.isComposing || el === null) {
			return;
		}
		const value = base.editText;
		const caret = el.selectionStart;
		const selecting = el.selectionStart !== el.selectionEnd;
		if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "z") {
			const redo = e.shiftKey;
			if (redo ? base.canRedo() : base.canUndo()) {
				e.preventDefault();
				void (redo ? base.redo() : base.undo());
			}
			return;
		}
		if (e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "y") {
			if (base.canRedo()) {
				e.preventDefault();
				void base.redo();
			}
			return;
		}

		switch (e.key) {
			case "Enter":
				if (e.shiftKey) {
					return;
				}
				e.preventDefault();
				if (caret === 0 && value !== "" && !selecting) {
					void base.insertAbove(id);
				} else {
					void base.splitAtCaret(id, caret, el.selectionEnd);
				}
				return;
			case "Tab":
				e.preventDefault();
				void (e.shiftKey ? base.outdent(id) : base.indent(id));
				return;
			case "Backspace":
				if (caret !== 0 || selecting) {
					return;
				}
				e.preventDefault();
				void base.mergeIntoPrevious(id);
				return;
			case "Delete":
				if (caret !== value.length || selecting) {
					return;
				}
				e.preventDefault();
				void base.pullNextInto(id, caret);
				return;
			case "ArrowUp":
			case "ArrowDown": {
				const direction = e.key === "ArrowUp" ? "up" : "down";
				const column = caretColumn(value, caret);
				const visual = visualLines(el);
				if (visual === null || visual === lineCount(value)) {
					if (lineIndexOf(value, caret) !== (direction === "up" ? 0 : lineCount(value) - 1)) {
						return;
					}
					e.preventDefault();
					void base.moveCaret(id, direction, column);
					return;
				}
				requestAnimationFrame(() => {
					if (el?.isConnected !== true || document.activeElement !== el) {
						return;
					}
					if (base.editingID !== id || base.editText !== value) {
						return;
					}
					if (base.commitInFlight) {
						return;
					}
					if (el.selectionStart !== caret || el.selectionEnd !== caret) {
						return;
					}
					void base.moveCaret(id, direction, column);
				});
				return;
			}
			case "Escape":
				e.preventDefault();
				void base.commitEdit();
				return;
		}
	}

	function onblur(): void {
		if (base.busy) {
			return;
		}
		if (!document.hasFocus()) {
			return;
		}
		void base.commitEdit();
	}
</script>

<div class="editor-wrap">
	<!-- readonly preserves focus while a write is in flight. -->
	<textarea
		bind:this={el}
		bind:value={base.editText}
		class="editor"
		aria-label="block content"
		rows="1"
		spellcheck="false"
		readonly={base.busy || base.commitInFlight}
		{onkeydown}
		{onblur}
		onkeyup={reportCaret}
		onclick={reportCaret}
		onselect={reportCaret}
		oninput={reportCaret}
	></textarea>
	{#if trigger}
		<RefAutocomplete {trigger} target={el} />
	{/if}
</div>

<style>
	.editor-wrap {
		position: relative;
		flex: 1;
		min-width: 0;
	}
	/* Match the rendered row metric so entering edit mode does not move text.
	   width: 100%, not flex: the wrapper is a plain block (position: relative
	   is what it needs to anchor the popup), not a flex container. */
	.editor {
		display: block;
		width: 100%;
		margin: 0;
		padding: 0 var(--unit) 0 0;
		border: none;
		border-radius: 0;
		background: none;
		color: inherit;
		font: inherit;
		line-height: var(--row-height);
		overflow: hidden;
		resize: none;
	}
	.editor:focus-visible {
		outline: none;
		border: none;
		box-shadow: none;
	}
</style>
