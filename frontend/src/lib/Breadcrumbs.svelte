<script lang="ts">
	import { base } from "../stores/base.svelte";
	import { crumbTrail } from "./tree";

	// The way back out of a zoom-in. Absent when nothing is focused: an
	// unfocused view is already at the top of its topic, and an empty bar there
	// would be furniture with nothing to say.
	//
	// A trail of null means the focused id is not in the forest — a reload can
	// drop it — so the bar renders nothing rather than guessing.
	const trail = $derived(base.focusID === null ? null : crumbTrail(base.forest, base.focusID));
</script>

{#if trail !== null && trail.length > 0}
	<nav aria-label="path to the focused block">
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
	</nav>
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
</style>
