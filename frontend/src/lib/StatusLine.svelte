<script lang="ts">
	let {
		status,
		error,
		onCancel,
	}: { status: string; error: string; onCancel?: () => void } = $props();
</script>

{#if status !== ""}
	<p>
		<code>{status}</code>
		{#if onCancel}
			<button type="button" onclick={onCancel}>Cancel</button>
		{/if}
	</p>
{/if}
{#if error !== ""}
	<p><code>error: {error}</code></p>
{/if}
