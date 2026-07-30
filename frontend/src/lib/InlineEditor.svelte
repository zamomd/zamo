<script lang="ts">
	import { untrack } from "svelte";
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
	// editCaretNonce is its sole trigger, because a placement is an event, not a
	// value: a merge lands the caret at the seam, the arrow lands it in the same
	// column it left, and either can repeat the offset the store already held —
	// which the effect would not see. The store bumps the nonce on every
	// placement, so every placement re-runs this.
	//
	// The offset itself is read untracked, which is what keeps the nonce sole:
	// reportCaret below writes editCaret on every movement of the reader's own,
	// and tracking it would turn each of those into a placement — focus() and
	// setSelectionRange() on every keystroke, fighting the caret that provoked
	// them.
	//
	// It does not track editText either, for the same reason. The store's half of
	// the bargain is that editText is already final whenever a placement is
	// announced — see base.svelte.ts's editCaret.
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

	// The reader's caret, back into the store, where it outlives this component:
	// a zoom-out re-renders the tree from a different root, and the fresh editor
	// restores editCaret (see the effect above). Without this it would restore
	// the offset the session opened at.
	//
	// Four events rather than an element-level selectionchange, which is not
	// uniformly available: keyup covers the keyboard (arrows within the block,
	// Home/End, typing), click the pointer, select a drag-selection or a
	// select-all, and input the movements that come from neither — a paste from
	// a context menu, a drop. Overlapping is harmless: each report writes the
	// offset the DOM already has, so a redundant one is a no-op.
	function reportCaret(): void {
		if (el === undefined) {
			return;
		}
		base.syncCaret(id, el.selectionStart);
	}

	// The height tracks the text, and only the text. This covers typing (the
	// binding writes editText on every input, so the effect re-runs) and the
	// store replacing the text wholesale, which fires no input event at all —
	// which is why oninput is not also wired to fit(): it would be the same call
	// twice per keystroke, and it would not have covered the second case.
	$effect(() => {
		void base.editText;
		fit();
	});

	// visualLines counts the lines the textarea actually renders, which is not
	// lineCount(value): a paragraph with no newline in it occupies as many as the
	// width demands. The element never scrolls — overflow is hidden and fit()
	// syncs the height from scrollHeight — so scrollHeight is the whole content's
	// height, and the row's line-height divides it.
	//
	// null means the line-height is not a length. The computed value is "normal"
	// when no rule reaches the element, and .editor sets it from --row-height, so
	// that can only happen with the stylesheet missing altogether — where no
	// measurement here is worth trusting either. The caller answers null with the
	// logical test, which is the behaviour this path exists to replace.
	function visualLines(area: HTMLTextAreaElement): number | null {
		const style = getComputedStyle(area);
		const height = Number.parseFloat(style.lineHeight);
		if (!Number.isFinite(height) || height <= 0) {
			return null;
		}
		// scrollHeight is the content box plus the padding, and .editor's vertical
		// padding is 0 — but a count that is too high only ever costs a second
		// press, while one that is too low would leave the block from the middle of
		// a paragraph, so the padding comes off rather than being assumed away.
		const inner = area.scrollHeight - Number.parseFloat(style.paddingTop) - Number.parseFloat(style.paddingBottom);
		return Math.max(1, Math.round(inner / height));
	}

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
				// So does a selection: it is replaced, so the text changes either way
				// and there is no untouched block to keep the id for — the store drops
				// the selected run between the two halves.
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
			case "ArrowDown": {
				// The arrow leaves the block only from its edge line — the first going
				// up, the last going down. Anywhere else it is the textarea's own, and
				// which line the caret is on is a question about the rendered box, not
				// about the text: a paragraph with no newline in it is one logical line
				// and any number of visual ones.
				const direction = e.key === "ArrowUp" ? "up" : "down";
				const column = caretColumn(value, caret);
				const visual = visualLines(el);
				if (visual === null || visual === lineCount(value)) {
					// Nothing wraps, so a logical line is a visual line and the test is
					// exact: one press leaves from the edge, none leaves from anywhere
					// else. This is the common case and answers in the keydown itself.
					if (lineIndexOf(value, caret) !== (direction === "up" ? 0 : lineCount(value) - 1)) {
						return;
					}
					e.preventDefault();
					void base.moveCaret(id, direction, column);
					return;
				}
				// It wraps, and nothing in the text says where the visual lines fall.
				// So do not preventDefault: let the browser move the caret and look
				// afterwards. A caret that did not move had nowhere to go inside the
				// block, which is exactly what "on the edge visual line" means.
				//
				// The consequence, and it is how an outliner behaves: from a non-zero
				// column on the top visual line, leaving takes a second press, because
				// WebKit's first ArrowUp there moves the caret to offset 0 rather than
				// refusing to move at all.
				requestAnimationFrame(() => {
					// Everything that can have happened in between. The element may be
					// gone (a re-read or a collapse unmounted the row) or no longer the
					// focused one (the reader clicked away, and moving the caret would
					// steal the focus back from them); the session may have been handed
					// to another block; the reader may have typed, or a gesture may have
					// replaced the text under them. moveCaret's own busy check covers
					// the last of them, a write that started meanwhile.
					if (el?.isConnected !== true || document.activeElement !== el) {
						return;
					}
					if (base.editingID !== id || base.editText !== value) {
						return;
					}
					// The caret has to be exactly where it was and still collapsed: a
					// moved one means the browser navigated inside the block, and a
					// selection that appeared means Shift is held, which extends the
					// selection rather than travelling.
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
	onkeyup={reportCaret}
	onclick={reportCaret}
	onselect={reportCaret}
	oninput={reportCaret}
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
