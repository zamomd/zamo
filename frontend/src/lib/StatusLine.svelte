<script lang="ts">
	// The status/error pair every screen keeps: one place to render them.
	let { status, error }: { status: string; error: string } = $props();
</script>

{#if status !== ""}
	<p><code>{status}</code></p>
{/if}
{#if error !== ""}
	<p><code>error: {error}</code></p>
{/if}
