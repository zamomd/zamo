<script lang="ts">
	import { base } from "../stores/base.svelte";
	import { type Mode, shell } from "../stores/shell.svelte";

	const modes: { id: Mode; glyph: string; label: string }[] = [
		{ id: "tree", glyph: "▤", label: "tree" },
		{ id: "graph", glyph: "◈", label: "graph" },
		{ id: "changes", glyph: "⚑", label: "changes" },
		{ id: "settings", glyph: "⚙", label: "settings" },
	];
</script>

<nav class="rail" aria-label="view">
	{#each modes as m (m.id)}
		<button
			type="button"
			class:active={shell.mode === m.id}
			title={m.label}
			aria-label={m.label}
			aria-pressed={shell.mode === m.id}
			disabled={base.busy}
			onclick={() => shell.setMode(m.id)}
		>
			{m.glyph}
		</button>
	{/each}
</nav>

<style>
	.rail {
		display: flex;
		flex-direction: column;
		gap: var(--unit);
		padding: var(--unit) 0;
		border-right: 1px solid var(--hairline);
	}
	/* Unframed: the global button rule frames every button, and a box around
	   each glyph would make the rail the loudest thing in the window. */
	.rail button {
		border: none;
		border-radius: 0;
		border-left: 2px solid transparent;
		background: none;
		width: calc(var(--unit) * 6);
		font-size: var(--size-title);
		line-height: calc(var(--row-height) * 1.6);
		color: var(--text-faint);
		padding: 0;
	}
	.rail button:hover:not(:disabled) {
		color: var(--text-dim);
		background: none;
	}
	.rail button.active {
		color: var(--text);
		border-left-color: var(--accent);
	}
</style>
