<script lang="ts">
	let { error }: { error: string } = $props()
</script>

{#if error !== ""}
	<p><code>error: {error}</code></p>
{/if}
