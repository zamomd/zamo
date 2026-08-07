<script lang="ts">
	import { base } from "../stores/base.svelte";
	import { crumbTrail } from "./tree";

	const trail = $derived(base.focusID === null ? null : crumbTrail(base.forest, base.focusID));

	// Converting away from a document deletes nothing the user can lose — the
	// text becomes blocks — so that direction goes straight through. The other
	// tombstones every block in the topic, and the round trip is not exact, so
	// one misclick on a large outline is not recoverable to the byte. confirm()
	// is a no-op in the Wails webview, so the confirmation is a real row.
	let pendingConvert = $state(false);

	function toggleTopicMode(): void {
		if (base.topicMode === "doc") {
			void base.setTopicMode("outline");
			return;
		}
		pendingConvert = true;
	}

	// A pending confirmation belongs to the topic it was opened on; moving to
	// another one drops it rather than letting "convert" land on that topic.
	$effect(() => {
		void base.selectedTopicID;
		pendingConvert = false;
	});

	async function confirmConvert(): Promise<void> {
		if (base.busy) {
			return;
		}
		pendingConvert = false;
		await base.setTopicMode("doc");
	}
</script>

<nav aria-label="path to the focused block and the topic's shape">
	{#if trail !== null && trail.length > 0}
		{#each trail as crumb, i (crumb.id)}
			{#if i > 0}<span class="sep" aria-hidden="true">›</span>{/if}
			<button
				type="button"
				disabled={base.busy}
				onclick={() => (i === 0 ? base.clearFocus() : base.focus(crumb.id))}
			>
				{crumb.title}
			</button>
		{/each}
		<span class="sep" aria-hidden="true">›</span>
	{/if}
	<button
		type="button"
		class="mode-switch"
		aria-expanded={pendingConvert}
		disabled={base.busy}
		onclick={toggleTopicMode}
	>
		{base.topicMode === "doc" ? "Convert to blocks" : "Convert to document"}
	</button>
</nav>

{#if pendingConvert}
	<div class="confirm">
		<span>Convert to a document? Every block in this topic becomes text, and converting back does not restore them exactly.</span>
		<button type="button" disabled={base.busy} onclick={confirmConvert}>convert</button>
		<button type="button" onclick={() => (pendingConvert = false)}>cancel</button>
	</div>
{/if}

<style>
	nav {
		flex: none;
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 0.2rem;
		font-size: var(--size-small);
		color: var(--text-faint);
		line-height: var(--row-height);
	}
	button {
		border: none;
		background: none;
		padding: 0;
		font-size: inherit;
		color: inherit;
		cursor: pointer;
	}
	/* A crumb opted out of button chrome at rest and has to stay out on hover
	   too, or app.css's global button:hover:not(:disabled) paints a chip behind
	   text with no padding. Declaring background here is not enough on its own:
	   that global weighs (0,2,1), a bare scoped button:hover emits as
	   button.svelte-x:hover and weighs exactly the same, and this component's CSS
	   lands BEFORE app.css in the bundle, so the tie would go to the global. The
	   nav qualifier is what carries it — nav.svelte-x button:where(.svelte-x):hover
	   is (0,2,2), one element more.

	   :not(:disabled) for the same reason app.css's own hover rule carries it: a
	   crumb that is refusing while a write is in flight must not brighten under
	   the pointer as though it would answer. The rest state needs no rule of its
	   own — the crumb already inherits nav's --text-faint, which is what
	   button:disabled would paint it anyway. */
	nav button:hover:not(:disabled) {
		background: none;
		color: var(--text);
	}
	.sep {
		opacity: 0.6;
	}
	/* A persistent page-level control, not a contextual crumb, so it opts back
	   into button chrome (the plain `button` rule above strips it) and sits at
	   the row's far end regardless of whether a trail is showing. */
	.mode-switch {
		margin-left: auto;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		padding: 0.1rem 0.5rem;
		color: var(--text-dim);
	}
	.mode-switch:hover:not(:disabled) {
		background: var(--hover);
		color: var(--text);
	}
	/* The same quiet hanging panel TreeNode and DocView use to confirm creating
	   a topic from an unresolved reference. */
	.confirm {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin: 0.25rem 0;
		padding: 0.2rem 0.4rem;
		font-size: var(--size-small);
		border-left: 2px solid var(--hairline);
	}
	.confirm button {
		flex: none;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.1rem 0.5rem;
		cursor: pointer;
		color: inherit;
	}
	.confirm button:hover:not(:disabled) {
		background: var(--hover);
	}
</style>
