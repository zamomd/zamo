<script lang="ts">
	import { base } from "../stores/base.svelte";
	import { tabs } from "../stores/tabs.svelte";
	import { plainText } from "./plaintext";
	import { firstLine } from "./refs";
	import { hasUnreviewed } from "./tree";

	const items = $derived(
		tabs.tabs.map((tab) => {
			const node = base.forest.find((t) => t.block.ID === tab.topicID);
			return {
				id: tab.id,
				title: node === undefined ? "" : plainText(firstLine(node.block.Content)),
				unreviewed: node !== undefined && hasUnreviewed(node),
				active: tab.id === tabs.activeID,
			};
		}),
	);

	let strip: HTMLElement | undefined = $state();

	$effect(() => {
		void tabs.activeID;
		void items.length;
		strip?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
	});

	// Arrow keys move the selection along with the focus: a tab is cheap to
	// show, so there is nothing to gain from a separate confirming key.
	async function onKey(e: KeyboardEvent, index: number): Promise<void> {
		const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
		if (step === 0) {
			return;
		}
		e.preventDefault();
		const target = items[(index + step + items.length) % items.length];
		await tabs.activate(target.id);
		strip?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus();
	}

	function onAux(e: MouseEvent, id: string): void {
		if (e.button === 1) {
			e.preventDefault();
			void tabs.close(id);
		}
	}
</script>

<!-- One tab is the tree screen as it always was: the strip appears with the
     second. -->
{#if items.length > 1}
	<div class="strip" role="tablist" aria-label="open topics" bind:this={strip}>
		{#each items as item, i (item.id)}
			<div class="tab" class:active={item.active}>
				<button
					type="button"
					role="tab"
					class="title"
					aria-selected={item.active}
					tabindex={item.active ? 0 : -1}
					title={item.title}
					disabled={base.busy}
					onclick={() => tabs.activate(item.id)}
					onauxclick={(e) => onAux(e, item.id)}
					onkeydown={(e) => onKey(e, i)}
				>
					{#if item.unreviewed}
						<span class="dot" title="has unreviewed changes" aria-label="has unreviewed changes" role="img"></span>
					{/if}
					<span class="name">{item.title}</span>
				</button>
				<button
					type="button"
					class="close"
					tabindex="-1"
					aria-label="close {item.title}"
					title="close tab"
					disabled={base.busy}
					onclick={() => tabs.close(item.id)}
				>
					✕
				</button>
			</div>
		{/each}
	</div>
{/if}

<style>
	.strip {
		flex: none;
		display: flex;
		overflow-x: auto;
		scrollbar-width: none;
		border-bottom: 1px solid var(--hairline);
		margin-bottom: var(--unit);
	}
	.tab {
		flex: 0 1 calc(var(--unit) * 26);
		min-width: calc(var(--unit) * 12);
		display: flex;
		align-items: center;
		border-bottom: 2px solid transparent;
		margin-bottom: -1px;
		color: var(--text-dim);
	}
	.tab.active {
		border-bottom-color: var(--accent);
		color: var(--text);
	}
	.tab:hover {
		background: var(--hover);
	}
	/* Unframed, like the rail: a box around every tab would outweigh the
	   topic below it. */
	.tab button {
		border: none;
		border-radius: 0;
		background: none;
		color: inherit;
	}
	.title {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--unit);
		padding: 0 var(--unit);
		line-height: calc(var(--row-height) * 1.2);
		text-align: left;
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.dot {
		flex: none;
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--accent);
	}
	.close {
		flex: none;
		padding: 0 var(--unit);
		font-size: var(--size-small);
		color: var(--text-faint);
		visibility: hidden;
	}
	.tab.active .close,
	.tab:hover .close {
		visibility: visible;
	}
	.close:hover {
		color: var(--text);
	}
</style>
