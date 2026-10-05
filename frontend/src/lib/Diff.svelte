<script lang="ts">
	import { diffLines } from "./diff";

	let { before, after }: { before: string; after: string } = $props();
	const lines = $derived(diffLines(before, after));
</script>

<pre class="diff">{#each lines as line, i (i)}<span class={line.kind} class:intraline={line.intraline}
			>{line.kind === "del" ? "- " : line.kind === "add" ? "+ " : "  "}{#each line.parts as part, j (j)}<span
					class:changed={part.changed}>{part.text}</span
				>{/each}
</span>{/each}</pre>

<style>
	.diff {
		margin: 0.25rem 0 0.25rem 1.5rem;
		padding: var(--unit);
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
		font-size: var(--size-small);
		overflow-x: auto;
	}
	/* The +/- prefix already names the change, so the diff is carried by
	   contrast rather than by hue: added lines at full strength, removed
	   lines dimmed. That keeps the window at one accent, and it survives a
	   theme whose green and red land somewhere unexpected. */
	.del {
		color: var(--text-dim);
	}
	.del:not(.intraline),
	.del .changed {
		text-decoration: line-through;
	}
	.add {
		color: var(--text);
	}
	/* Within a rewritten line the changed words carry the same contrast one
	   step further, so the unchanged rest of the line reads as context. */
	.add.intraline {
		color: var(--text-dim);
	}
	.add .changed {
		color: var(--text);
	}
	.same {
		color: var(--text-dim);
	}
</style>
