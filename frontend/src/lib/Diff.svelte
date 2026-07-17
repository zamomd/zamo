<script lang="ts">
	import { diffLines } from "./diff";

	let { before, after }: { before: string; after: string } = $props();
	const lines = $derived(diffLines(before, after));
</script>

<pre class="diff">{#each lines as line, i (i)}<span class={line.kind}>{line.kind === "del"
			? "- "
			: line.kind === "add"
				? "+ "
				: "  "}{line.text}
</span>{/each}</pre>

<style>
	.diff {
		margin: 0.25rem 0 0.25rem 1.5rem;
		padding: 0.5rem;
		background: #f6f6f6;
		font-size: 0.85em;
		overflow-x: auto;
	}
	.del {
		color: #a00;
	}
	.add {
		color: #070;
	}
	.same {
		color: #666;
	}
</style>
