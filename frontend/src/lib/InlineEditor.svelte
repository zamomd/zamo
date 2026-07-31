<script lang="ts">
	import { untrack } from "svelte";
	import { base } from "../stores/base.svelte";
	import { caretColumn, lineCount, lineIndexOf } from "./caret";

	// The store owns editor state so it survives row remounts.
	let { id }: { id: string } = $props();

	let el: HTMLTextAreaElement | undefined;

	// WKWebView does not reliably support field-sizing: content.
	function fit(): void {
		if (el === undefined) {
			return;
		}
		el.style.height = "auto";
		el.style.height = `${el.scrollHeight}px`;
	}

	// The nonce represents a placement event, including repeated offsets.
	// Caret reports and text updates must not retrigger placement.
	$effect(() => {
		void base.editCaretNonce;
		if (el === undefined) {
			return;
		}
		const at = untrack(() => base.editCaret);
		el.focus();
		el.setSelectionRange(at, at);
		fit();
	});

	// These events cover caret changes without relying on selectionchange support
	// for text controls.
	function reportCaret(): void {
		if (el === undefined) {
			return;
		}
		base.syncCaret(id, el.selectionStart);
	}

	$effect(() => {
		void base.editText;
		fit();
	});

	// visualLines derives wrapped line count from the fitted textarea height.
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
		// Composition keystrokes belong to the IME.
		if (e.isComposing || el === undefined) {
			return;
		}
		const value = base.editText;
		const caret = el.selectionStart;
		const selecting = el.selectionStart !== el.selectionEnd;
		// Leave uncommitted text to the textarea's native undo history.
		if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "z") {
			const redo = e.shiftKey;
			if (redo ? base.canRedo() : base.canUndo()) {
				e.preventDefault();
				void (redo ? base.redo() : base.undo());
			}
			return;
		}
		// Ctrl+Y is redo on Windows and Linux, but Cmd+Y is not redo on macOS.
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
				// Cross-block navigation starts only at a visual edge line.
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
				// Wrapped line boundaries are observable only after the browser moves.
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

	// Losing window focus must not commit the edit.
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

<style>
	/* Match the rendered row metric so entering edit mode does not move text. */
	.editor {
		flex: 1;
		min-width: 0;
		display: block;
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
