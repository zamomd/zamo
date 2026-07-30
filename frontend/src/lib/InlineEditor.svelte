<script lang="ts">
	import { base } from "../stores/base.svelte";
	import { caretColumn, lineCount, lineIndexOf } from "./caret";

	// The block editor: a textarea dressed to be invisible. It sits exactly where
	// the rendered line was, so clicking into a block does not move the text — the
	// one requirement that makes this feel like a document rather than a form.
	//
	// It reads the store directly rather than through props because the caret it
	// restores has to survive this component unmounting: a collapse, a zoom or a
	// re-read destroys the row, and a structural gesture hands the session to a
	// different block, whose editor is a fresh instance of this component.
	let { id }: { id: string } = $props();

	let el: HTMLTextAreaElement | undefined;

	// Grow to fit the text. field-sizing: content is not dependable in WKWebView,
	// so the height is synced from scrollHeight whenever the text changes.
	function fit(): void {
		if (el === undefined) {
			return;
		}
		el.style.height = "auto";
		el.style.height = `${el.scrollHeight}px`;
	}

	// Mount with the caret where the gesture that opened this block put it, then
	// take focus. Both have to happen after the value is in the DOM, which is why
	// this is an effect and not an attribute.
	//
	// It tracks editCaretNonce as well as editCaret because a placement is an
	// event, not a value: a merge lands the caret at the seam, the arrow lands it
	// in the same column it left, and either can repeat the offset the store
	// already held — which the effect would not see. The store bumps the nonce on
	// every placement, so every placement re-runs this.
	//
	// It deliberately does not track editText: that would re-run focus() and
	// setSelectionRange() on every keystroke and pin the caret where the session
	// started. The store's half of the bargain is that editText is already final
	// whenever a placement is announced — see base.svelte.ts's editCaret.
	$effect(() => {
		void base.editCaretNonce;
		if (el === undefined) {
			return;
		}
		el.focus();
		el.setSelectionRange(base.editCaret, base.editCaret);
		fit();
	});

	// The height tracks the text, and only the text. This covers typing (the
	// binding writes editText on every input, so the effect re-runs) and the
	// store replacing the text wholesale, which fires no input event at all —
	// which is why oninput is not also wired to fit(): it would be the same call
	// twice per keystroke, and it would not have covered the second case.
	$effect(() => {
		void base.editText;
		fit();
	});

	function onkeydown(e: KeyboardEvent): void {
		// An IME composition owns these keys until it commits; intercepting Enter
		// mid-composition would split a block in the middle of a candidate.
		if (e.isComposing || el === undefined) {
			return;
		}
		const value = base.editText;
		const caret = el.selectionStart;
		const selecting = el.selectionStart !== el.selectionEnd;

		switch (e.key) {
			case "Enter":
				// Shift+Enter is a line break inside the block: the textarea's own
				// behaviour, so it is deliberately not intercepted.
				if (e.shiftKey) {
					return;
				}
				e.preventDefault();
				// Enter at the very start of non-empty text inserts above instead of
				// splitting, so the text keeps its block id and with it its history.
				// An empty block has no text to protect and follows the split rule.
				if (caret === 0 && value !== "") {
					void base.insertAbove(id);
				} else {
					void base.splitAtCaret(id, caret);
				}
				return;
			case "Tab":
				e.preventDefault();
				void (e.shiftKey ? base.outdent(id) : base.indent(id));
				return;
			case "Backspace":
				// Only at the very start, and only without a selection — otherwise
				// this is an ordinary delete and the textarea should handle it.
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
				// Only leave the block from its first line; inside a wrapped block the
				// arrow is the textarea's own.
				if (lineIndexOf(value, caret) !== 0) {
					return;
				}
				e.preventDefault();
				void base.moveCaret(id, "up", caretColumn(value, caret));
				return;
			case "ArrowDown":
				if (lineIndexOf(value, caret) !== lineCount(value) - 1) {
					return;
				}
				e.preventDefault();
				void base.moveCaret(id, "down", caretColumn(value, caret));
				return;
			case "Escape":
				e.preventDefault();
				void base.commitEdit();
				return;
		}
	}

	// Clicking anywhere else commits. document.hasFocus() distinguishes "focus
	// moved somewhere in the page" from "the window went to the background":
	// alt-tabbing away must not close the editor out from under the reader.
	function onblur(): void {
		// A gesture is in flight and owns the session. writeIfChanged compares
		// editText against the forest, and the forest is only refreshed by the
		// re-read the gesture has not reached yet — so a commit landing here sees
		// the pre-write content, issues a second EditBlockContent for the same
		// session (two revisions where the spec promises one), and closes a
		// session the gesture then goes on using.
		//
		// This is the same reasoning as readonly rather than disabled on the
		// element below, for the route a blur takes rather than the keyboard's:
		// insertAbove, indent and outdent all re-order a keyed {#each} around the
		// focused textarea, and WebKit has historically fired blur when a focused
		// element is detached and reinserted.
		if (base.busy) {
			return;
		}
		if (!document.hasFocus()) {
			return;
		}
		void base.commitEdit();
	}
</script>

<!-- A write is in flight: the gestures already refuse to start a second one,
     and this stops the keyboard from typing into a block whose text is about
     to be replaced by the re-read. readonly rather than disabled because a
     disabled textarea loses focus without getting it back — and, where the
     engine fires blur on disable, onblur would commit the session out from
     under the gesture that set busy in the first place. -->
<textarea
	bind:this={el}
	bind:value={base.editText}
	class="editor"
	aria-label="block content"
	rows="1"
	spellcheck="false"
	readonly={base.busy}
	{onkeydown}
	{onblur}
></textarea>

<style>
	/* The metric is the whole point: the editor has to occupy exactly the box the
	   rendered line occupied, or the text jumps at the moment of the click.
	   font: inherit rather than a size of its own, so the page title's own row —
	   which is larger — stays right when it is the block being edited.

	   The class lives on the <textarea> itself, not a wrapper, so Svelte emits
	   `.editor.svelte-hash` with no element type in it: (0,2,0) against the global
	   `input, textarea, select` rule's (0,0,1), and the focus rule below emits
	   `.editor.svelte-hash:focus-visible`, (0,3,0) against (0,1,1). Both still win
	   on the class column and neither depends on source order — the margin is
	   wider than a selector with a nested `textarea` would have needed. */
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
		/* The height is driven from scrollHeight, so the element must never keep a
		   scrollbar of its own or the measurement chases itself. */
		overflow: hidden;
		resize: none;
	}
	.editor:focus-visible {
		outline: none;
		border: none;
		box-shadow: none;
	}
</style>
