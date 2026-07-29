<script lang="ts">
	// The inline block editor: a controlled textarea with save/cancel. The
	// caller owns the text and the busy flag; this only renders and reports.
	let {
		value = $bindable(),
		busy,
		onsave,
		oncancel,
	}: {
		value: string;
		busy: boolean;
		onsave: () => void;
		oncancel: () => void;
	} = $props();
</script>

<div class="editor">
	<textarea aria-label="block content" bind:value disabled={busy}></textarea>
	<button type="button" onclick={onsave} disabled={busy || value.trim() === ""}>Save</button>
	<button type="button" onclick={oncancel} disabled={busy}>Cancel</button>
</div>

<style>
	.editor {
		display: flex;
		flex: 1;
		flex-wrap: wrap;
		gap: 0.3rem;
	}
	.editor textarea {
		flex: 1;
		min-width: 12rem;
		min-height: calc(var(--row-height) * 3);
		padding: var(--unit);
		line-height: var(--row-height);
		resize: vertical;
	}
</style>
