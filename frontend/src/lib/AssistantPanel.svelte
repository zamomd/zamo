<script lang="ts">
	import { Browser } from "@wailsio/runtime";
	import { onMount } from "svelte";
	import { assistant, type Turn } from "../stores/assistant.svelte";
	import { base } from "../stores/base.svelte";
	import { changes } from "../stores/changes.svelte";
	import { shell } from "../stores/shell.svelte";
	import { ASSISTANT_MIN, ASSISTANT_STEP, assistantMax, assistantWidth } from "./assistantsize";
	import { stepLabel } from "./assistantsteps";
	import { renderMarkdown } from "./markdown";
	import { jumpToBlock } from "./navigate";
	import { firstLine } from "./refs";
	import { makeResolver, refClick } from "./resolve";
	import { findBlock } from "./tree";

	const resolveRef = $derived(makeResolver(base.forest, base.aliases));
	const context = $derived(assistant.context);

	let panel: HTMLElement | undefined = $state();
	let field: HTMLTextAreaElement | undefined = $state();
	let scroller: HTMLElement | undefined = $state();
	let dragging = $state(false);

	onMount(() => field?.focus());

	// The newest turn and the step under way stay in view as they arrive.
	$effect(() => {
		void assistant.turns.length;
		void assistant.steps.length;
		void assistant.running;
		if (scroller) {
			scroller.scrollTop = scroller.scrollHeight;
		}
	});

	function nameOf(id: string): string | null {
		const block = findBlock(base.forest, id);
		return block ? firstLine(block.Content) : null;
	}

	function changed(turn: Turn): number {
		return changes.blocks.filter((b) => b.GroupID === turn.groupId).length;
	}

	function onKey(e: KeyboardEvent): void {
		if (e.key === "Escape") {
			e.preventDefault();
			shell.toggleAssistant();
		}
	}

	function onFieldKey(e: KeyboardEvent): void {
		if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
			e.preventDefault();
			void assistant.send();
		}
	}

	function submit(e: SubmitEvent): void {
		e.preventDefault();
		void assistant.send();
	}

	// A resolved reference opens its target in the tree; a link opens in the
	// browser. A reference to nothing does nothing here: creating its topic is
	// the tree's gesture.
	function onAnswerClick(e: Event): void {
		const target = e.target as HTMLElement;
		const ref = target.closest<HTMLElement>(".ref[data-target]");
		if (ref) {
			e.preventDefault();
			const action = refClick(base.forest, base.aliases, ref);
			if (action?.kind === "navigate") {
				void jumpToBlock(action.id);
			}
			return;
		}
		const link = target.closest("a");
		if (link) {
			e.preventDefault();
			const href = link.getAttribute("href");
			if (href) void Browser.OpenURL(href);
		}
	}

	function onAnswerKey(e: KeyboardEvent): void {
		if ((e.key === "Enter" || e.key === " ") && (e.target as HTMLElement).closest(".ref[data-target]")) {
			onAnswerClick(e);
		}
	}

	// Rendered references are plain spans; a resolved one is made a link the
	// keyboard can reach, since it is one to the mouse.
	function reachable(node: HTMLElement, _html: string) {
		const mark = () => {
			for (const ref of node.querySelectorAll<HTMLElement>(".ref[data-target]:not(.ref-missing)")) {
				ref.tabIndex = 0;
				ref.setAttribute("role", "link");
			}
		};
		mark();
		return { update: mark };
	}

	function resize(desired: number): void {
		shell.assistantWidth = assistantWidth(desired, window.innerWidth);
	}

	function startDrag(e: PointerEvent): void {
		if (e.button !== 0) {
			return;
		}
		e.preventDefault();
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		dragging = true;
	}

	function drag(e: PointerEvent): void {
		if (dragging) {
			resize(window.innerWidth - e.clientX);
		}
	}

	function endDrag(): void {
		if (!dragging) {
			return;
		}
		dragging = false;
		shell.saveAssistant();
	}

	function onHandleKey(e: KeyboardEvent): void {
		if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") {
			return;
		}
		e.preventDefault();
		const step = e.key === "ArrowLeft" ? ASSISTANT_STEP : -ASSISTANT_STEP;
		resize((shell.assistantWidth ?? panel?.offsetWidth ?? ASSISTANT_MIN) + step);
		shell.saveAssistant();
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<!-- biome-ignore lint/a11y/useSemanticElements: a focusable separator is a window splitter, which <hr> cannot be -->
<div
	class="resizer"
	class:dragging
	role="separator"
	aria-orientation="vertical"
	aria-label="assistant width"
	aria-valuemin={ASSISTANT_MIN}
	aria-valuemax={assistantMax(window.innerWidth)}
	aria-valuenow={shell.assistantWidth ?? panel?.offsetWidth ?? ASSISTANT_MIN}
	title="drag to resize"
	tabindex="0"
	onpointerdown={startDrag}
	onpointermove={drag}
	onpointerup={endDrag}
	onpointercancel={endDrag}
	onkeydown={onHandleKey}
></div>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<aside
	class="assistant"
	aria-label="assistant"
	bind:this={panel}
	style:width={shell.assistantWidth === null ? undefined : `${shell.assistantWidth}px`}
	style:min-width="{ASSISTANT_MIN}px"
	onkeydown={onKey}
>
	<header>
		<span class="name">assistant</span>
		<button
			type="button"
			class="plain"
			disabled={assistant.running || assistant.turns.length === 0}
			onclick={() => assistant.reset()}
		>
			new conversation
		</button>
	</header>

	<div class="turns" bind:this={scroller}>
		{#if assistant.turns.length === 0}
			<p class="empty">Ask about anything in your base, or ask for a change to what you have open.</p>
		{/if}
		{#each assistant.turns as turn (turn.id)}
			<article class="turn">
				<p class="question">{turn.question}</p>
				{#if turn.answer !== null}
					{#if turn.exhausted}
						<p class="note">I ran out of steps before finishing. Try a narrower question.</p>
					{:else}
						{@const html = renderMarkdown(turn.answer, resolveRef)}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- biome-ignore lint/a11y/noStaticElementInteractions: the handlers serve the references and links inside the rendered markdown, which carry their own roles -->
						<div class="answer" use:reachable={html} onclick={onAnswerClick} onkeydown={onAnswerKey}>{@html html}</div>
					{/if}
					{#if turn.groupId !== ""}
						{@const n = changed(turn)}
						<p class="note wrote">
							{#if n === 0}
								made a change, since reviewed
							{:else}
								changed {n} {n === 1 ? "block" : "blocks"}, awaiting review ·
								<button type="button" class="plain" onclick={() => shell.setMode("changes")}>review</button>
							{/if}
						</p>
					{/if}
					{#if turn.error !== ""}
						<p class="error">{turn.error}</p>
					{/if}
					{#if turn.saved}
						<p class="note">saved as a block, awaiting review</p>
					{:else if !turn.exhausted}
						<p class="actions">
							<button
								type="button"
								class="plain"
								disabled={!assistant.canSave(turn)}
								title={assistant.canSave(turn) ? undefined : "open an outline topic to save into"}
								onclick={() => assistant.save(turn.id)}
							>
								save as block
							</button>
						</p>
					{/if}
				{:else if turn.cancelled || turn.error !== ""}
					{#if turn.cancelled}
						<p class="note">cancelled</p>
					{:else}
						<p class="error">{turn.error}</p>
					{/if}
					<p class="actions">
						<button
							type="button"
							class="plain"
							disabled={assistant.running}
							onclick={() => assistant.resend(turn.id)}
						>
							send again
						</button>
					</p>
				{:else}
					<ul class="steps" aria-live="polite">
						{#each assistant.steps as step, i (i)}
							<li class:current={i === assistant.steps.length - 1}>{stepLabel(step, nameOf)}</li>
						{:else}
							<li class="current">working…</li>
						{/each}
					</ul>
				{/if}
			</article>
		{/each}
	</div>

	<footer>
		<p class="context">
			{#if context.label === ""}
				Nothing open: I can answer, or start a new topic.
			{:else}
				Can change <code>{context.label}</code>
				{#if context.scopeId !== ""}
					<button type="button" class="plain" onclick={() => base.clearScope()} disabled={base.busy}>clear</button>
				{/if}
			{/if}
		</p>
		<form class="ask" class:working={assistant.running} onsubmit={submit}>
			<textarea
				aria-label="message to the assistant"
				rows="2"
				placeholder="ask, or say what to change"
				bind:this={field}
				bind:value={assistant.draft}
				onkeydown={onFieldKey}
			></textarea>
			{#if assistant.running}
				<button type="button" onclick={() => assistant.cancel()} disabled={assistant.cancelling}>cancel</button>
			{:else}
				<button type="submit" disabled={assistant.draft.trim() === ""}>send</button>
			{/if}
		</form>
	</footer>
</aside>

<style>
	.assistant {
		flex: none;
		width: 360px;
		max-width: 50%;
		display: flex;
		flex-direction: column;
		border-left: 1px solid var(--hairline);
		overflow: hidden;
	}
	/* Zero-width in the layout, so the panel's hairline stays the only visible
	   edge; the grab area is the pseudo-element straddling it. */
	.resizer {
		flex: none;
		position: relative;
		z-index: 1;
		width: 0;
		user-select: none;
		touch-action: none;
	}
	.resizer::before {
		content: "";
		position: absolute;
		top: 0;
		bottom: 0;
		left: -3px;
		width: 7px;
		cursor: col-resize;
	}
	.resizer::after {
		content: "";
		position: absolute;
		top: 0;
		bottom: 0;
		left: 0;
		width: 1px;
		background: transparent;
	}
	.resizer:hover::after,
	.resizer:focus-visible::after,
	.resizer.dragging::after {
		background: var(--accent);
	}
	.resizer:focus-visible {
		outline: none;
	}
	/* Covers the window for the length of a drag: the cursor holds and nothing
	   underneath gets hovered or selected. */
	.resizer.dragging::before {
		position: fixed;
		inset: 0;
		width: auto;
	}

	header {
		flex: none;
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		padding: calc(var(--unit) * 2) calc(var(--unit) * 2) var(--unit);
	}
	.name {
		color: var(--text-dim);
	}

	/* Unframed: the global button rule frames every button, and a box around a
	   minor action inside running text would outweigh the text. */
	button.plain {
		border: none;
		background: none;
		padding: 0;
		font-size: var(--size-small);
		color: var(--text-dim);
		text-decoration: underline;
		cursor: pointer;
	}
	button.plain:hover:not(:disabled) {
		color: var(--text);
		background: none;
	}
	button.plain:disabled {
		color: var(--text-faint);
		text-decoration: none;
		cursor: default;
	}

	.turns {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		scrollbar-gutter: stable;
		padding: 0 calc(var(--unit) * 2);
	}
	.empty {
		margin: 0;
		color: var(--text-faint);
	}
	.turn {
		padding-bottom: calc(var(--unit) * 2);
	}
	/* The owner's words sit on a surface; the answer is plain text on the
	   ground, like everything else the base shows. */
	.question {
		margin: 0 0 var(--unit);
		padding: calc(var(--unit) / 2) var(--unit);
		border-radius: var(--radius);
		background: var(--surface);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.answer {
		overflow-wrap: anywhere;
	}
	.note,
	.actions,
	.error {
		margin: calc(var(--unit) / 2) 0 0;
		font-size: var(--size-small);
		color: var(--text-dim);
	}
	.error {
		color: var(--danger);
	}
	.steps {
		list-style: none;
		margin: 0;
		padding: 0;
		font-size: var(--size-small);
		color: var(--text-faint);
	}
	.steps .current {
		color: var(--text-dim);
	}

	/* Rendered markdown is injected via {@html}, so Svelte's scoping does not
	   reach it — style it through :global under .answer. */
	.answer :global(p) {
		margin: 0 0 var(--unit);
	}
	.answer :global(:last-child) {
		margin-bottom: 0;
	}
	.answer :global(h1),
	.answer :global(h2),
	.answer :global(h3),
	.answer :global(h4),
	.answer :global(h5),
	.answer :global(h6) {
		margin: var(--unit) 0 calc(var(--unit) / 2);
		font-size: 1em;
		font-weight: 650;
	}
	.answer :global(ul),
	.answer :global(ol) {
		margin: 0 0 var(--unit);
		padding-left: 1.1rem;
	}
	.answer :global(code) {
		white-space: pre-wrap;
		font-size: 0.92em;
		padding: 0.05rem 0.25rem;
		border-radius: var(--radius);
		background: var(--surface);
	}
	.answer :global(pre) {
		margin: 0 0 var(--unit);
		padding: 0.35rem 0.5rem;
		border-radius: var(--radius);
		overflow-x: auto;
		background: var(--surface);
	}
	.answer :global(pre code) {
		padding: 0;
		background: none;
	}
	.answer :global(a) {
		color: var(--accent);
	}
	.answer :global(.ref) {
		color: var(--accent);
		cursor: pointer;
		text-decoration: underline dotted;
		text-underline-offset: 2px;
	}
	.answer :global(.ref-missing) {
		color: var(--text-dim);
		cursor: default;
	}
	.answer :global(blockquote) {
		margin: 0 0 var(--unit);
		padding-left: 0.5rem;
		border-left: 2px solid var(--hairline);
		opacity: 0.85;
	}

	footer {
		flex: none;
		display: flex;
		flex-direction: column;
		gap: calc(var(--unit) / 2);
		padding: var(--unit) calc(var(--unit) * 2) calc(var(--unit) * 2);
	}
	.context {
		margin: 0;
		font-size: var(--size-small);
		color: var(--text-dim);
	}
	/* Keep a long block on one line so the footer does not balloon; the full
	   text still lives in the tree. */
	.context code {
		display: inline-block;
		max-width: 60%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		vertical-align: bottom;
	}

	/* One frame around the pair; the divider between them is the frame's own
	   hairline, so the action reads as part of the field rather than as a
	   separate filled button. */
	.ask {
		display: flex;
		align-items: stretch;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		overflow: hidden;
	}
	.ask:focus-within {
		border-color: var(--accent);
	}
	.ask.working {
		border-color: transparent;
		background:
			linear-gradient(var(--ground), var(--ground)) padding-box,
			linear-gradient(90deg, var(--accent), #a06bd6, #d06f9c, #d9a441, var(--accent)) border-box;
		background-size:
			auto,
			200% 100%;
		animation: shimmer 2.4s linear infinite;
	}
	@keyframes shimmer {
		to {
			background-position:
				0 0,
				200% 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.ask.working {
			animation: none;
		}
	}
	.ask textarea {
		flex: 1;
		min-width: 0;
		border: none;
		border-radius: 0;
		resize: none;
		background: none;
		color: inherit;
		font: inherit;
		padding: calc(var(--unit) / 2) var(--unit);
	}
	.ask textarea:focus {
		border: none;
		outline: none;
	}
	.ask button {
		flex: none;
		border: none;
		border-left: 1px solid var(--hairline-strong);
		border-radius: 0;
		color: var(--text-dim);
		padding: 0 calc(var(--unit) * 2);
	}
	.ask button:hover:not(:disabled) {
		color: var(--text);
		background: var(--hover);
	}
</style>
