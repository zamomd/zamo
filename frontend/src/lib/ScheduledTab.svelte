<script lang="ts">
	import { Module as Smartcron } from "../../bindings/github.com/zamomd/zamo/internal/modules/smartcron";
	import type { ScheduleEntry } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind/models.js";
	import { base } from "../stores/base.svelte";
	import { schedules } from "../stores/schedules.svelte";
	import { messageFor } from "./callerror";
	import { plainText } from "./plaintext";
	import { firstLine } from "./refs";
	import { formatWhen, outcomeLabel } from "./schedule";
	import { findBlock, findPath } from "./tree";

	// onjump reveals a block in the tree; onreview switches to the review tab
	// filtered to a topic — the place a skipped run is unblocked.
	let { onjump, onreview }: { onjump: (id: string) => void; onreview: (topicID: string) => void } = $props();

	let error = $state("");
	const now = new Date();

	function label(e: ScheduleEntry): string {
		const block = findBlock(base.forest, e.key);
		return block ? plainText(firstLine(block.Content)) : "(deleted block)";
	}

	// The topic a block lives in, for the review link: the root of its path,
	// or the block itself when it is a root.
	function topicOf(key: string): string | null {
		const path = findPath(base.forest, key);
		if (path === null) return null;
		return path.length > 0 ? path[0] : key;
	}

	function stuckOnReview(e: ScheduleEntry): boolean {
		return e.outcome === "skipped" && (e.detail ?? "").includes("unreviewed");
	}

	function open(e: ScheduleEntry): void {
		base.openSchedules.add(e.key);
		onjump(e.key);
	}

	async function toggle(e: ScheduleEntry): Promise<void> {
		error = "";
		try {
			await Smartcron.SetEnabled(e.key, !e.enabled);
			await schedules.refresh();
		} catch (err: unknown) {
			error = messageFor(err);
		}
	}
</script>

{#if error}
	<p class="error" role="alert">{error}</p>
{/if}
{#if schedules.entries.length === 0}
	<p>(nothing scheduled)</p>
{:else}
	<ul>
		{#each schedules.entries as e (`${e.module}/${e.key}`)}
			<li class="row" class:off={!e.enabled} class:unavailable={!e.available}>
				<button type="button" class="block" title="open this block and its schedule" disabled={base.busy} onclick={() => open(e)}>
					{label(e)}
				</button>
				<code class="spec">{e.spec}</code>
				<span class="meta next">{e.enabled ? formatWhen(e.nextRun, now) : "—"}</span>
				<span class="meta attempt" class:attention={e.outcome === "failed" || e.outcome === "skipped"}>
					{#if schedules.running.has(e.key) || e.running}
						running…
					{:else if e.lastRun}
						{outcomeLabel(e.outcome, e.detail)}
						{#if stuckOnReview(e) && topicOf(e.key) !== null}
							<button
								type="button"
								class="review"
								title="review the changes in this topic"
								onclick={() => onreview(topicOf(e.key) ?? "")}
							>
								→
							</button>
						{/if}
					{:else}
						not run yet
					{/if}
				</span>
				{#if e.available}
					<button
						type="button"
						class="toggle"
						role="switch"
						aria-checked={e.enabled}
						title={e.enabled ? "switch this schedule off" : "switch this schedule on"}
						disabled={base.busy}
						onclick={() => toggle(e)}
					>
						{e.enabled ? "on" : "off"}
					</button>
				{:else}
					<span class="meta">module unavailable</span>
				{/if}
			</li>
		{/each}
	</ul>
{/if}

<style>
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.25rem 0;
		border-bottom: 1px solid var(--hairline);
	}
	.row.off,
	.row.unavailable {
		opacity: 0.6;
	}
	.block {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		text-align: left;
		border: none;
		background: none;
		padding: 0.1rem 0.4rem;
		cursor: pointer;
		color: inherit;
	}
	.block:hover:not(:disabled) {
		background: var(--hover);
	}
	.spec {
		flex: none;
		color: var(--text-dim);
	}
	.meta {
		flex: none;
		font-size: var(--size-small);
		opacity: 0.7;
	}
	.next {
		min-width: 9rem;
	}
	.attempt {
		min-width: 14rem;
	}
	.attempt.attention {
		opacity: 1;
		color: var(--accent);
	}
	.review,
	.toggle {
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		font: inherit;
	}
	.toggle {
		flex: none;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		padding: 0 0.5rem;
		opacity: 0.75;
	}
	.toggle:hover:not(:disabled),
	.review:hover {
		opacity: 1;
	}
	.error {
		margin: 0 0 0.5rem;
		color: var(--danger);
	}
</style>
