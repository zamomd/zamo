<script lang="ts">
	import { base } from "../stores/base.svelte";
	import InlineEditor from "./InlineEditor.svelte";
	import TreeNode from "./TreeNode.svelte";
	import type { TreeNodeData } from "./tree";

	// One row of the base tree, recursing into its children. All state and
	// actions come from the base store; the node is the only input.
	let { node }: { node: TreeNodeData } = $props();

	const hasChildren = $derived(node.children.length > 0);
	const expanded = $derived(!base.collapsed.has(node.block.ID));
	const editing = $derived(base.editingID === node.block.ID);

	// Scroll this row into view and flash it when a search jump targets it. The
	// effect keys on highlightNonce so a repeat jump to the same block re-fires;
	// flash is toggled off then back on across a frame so the CSS animation
	// restarts rather than sitting at its finished state.
	let rowEl: HTMLElement | undefined;
	let flash = $state(false);
	$effect(() => {
		void base.highlightNonce; // subscribe: bumped on every reveal
		if (base.highlightID !== node.block.ID) {
			return;
		}
		rowEl?.scrollIntoView({ block: "center", behavior: "smooth" });
		flash = false;
		requestAnimationFrame(() => {
			flash = true;
		});
	});
</script>

<li>
	<div bind:this={rowEl} class="row" class:targeted={base.scopeID === node.block.ID} class:flash>
		<!-- Toggles are locked while an edit is open: collapsing an ancestor would
		     unmount the editor while editingID lingers, silently dead-ending every
		     other block (startEdit no-ops when a block is already being edited). -->
		{#if hasChildren}
			<button
				type="button"
				class="toggle"
				aria-expanded={expanded}
				aria-label={expanded ? "collapse" : "expand"}
				disabled={base.editingID !== null}
				onclick={() => base.toggle(node.block.ID)}
			>
				{expanded ? "▾" : "▸"}
			</button>
		{:else}
			<span class="toggle leaf" aria-hidden="true">·</span>
		{/if}

		{#if editing}
			<InlineEditor
				bind:value={base.editText}
				busy={base.busy}
				onsave={() => base.saveEdit()}
				oncancel={() => base.cancelEdit()}
			/>
		{:else}
			<button
				type="button"
				class="content"
				title="click to edit"
				disabled={base.busy}
				onclick={() => base.startEdit(node.block)}
			>
				<code>{node.block.Content}</code>
			</button>
			<button
				type="button"
				class="target"
				title="ask the AI to restructure this block"
				aria-label="target this block for the AI command"
				aria-pressed={base.scopeID === node.block.ID}
				disabled={base.busy || base.editingID !== null}
				onclick={() => base.setScope(node.block)}
			>
				⌖
			</button>
		{/if}
	</div>
	{#if hasChildren && expanded}
		<ul>
			{#each node.children as child (child.block.ID)}
				<TreeNode node={child} />
			{/each}
		</ul>
	{/if}
</li>

<style>
	/* The forest is a plain nested list; indentation and a faint guide rail carry
	   the hierarchy. Colours are theme-neutral (grey alpha) so the rail and hover
	   read on both light and dark backgrounds. */
	ul {
		list-style: none;
		margin: 0 0 0 0.55rem;
		padding: 0 0 0 0.7rem;
		border-left: 1px solid rgba(127, 127, 127, 0.25);
	}
	.row {
		display: flex;
		align-items: flex-start;
		gap: 0.15rem;
	}
	.toggle {
		flex: none;
		width: 1.1rem;
		padding: 0;
		border: none;
		background: none;
		cursor: pointer;
		color: inherit;
		font-size: 0.8rem;
		line-height: 1.5;
	}
	.toggle.leaf {
		cursor: default;
		opacity: 0.35;
	}
	.content {
		flex: 1;
		text-align: left;
		border: none;
		background: none;
		padding: 0.1rem 0.3rem;
		border-radius: 3px;
		cursor: pointer;
		color: inherit;
	}
	.content:hover:not(:disabled) {
		background: rgba(127, 127, 127, 0.15);
	}
	.row.targeted {
		background: rgba(127, 127, 127, 0.12);
		border-radius: 3px;
	}
	/* A jump from search flashes the row once, then fades back — long enough for
	   the eye to catch it after the scroll, leaving no lingering state. The tint
	   is a yellow alpha that reads on both themes. */
	.row.flash {
		border-radius: 3px;
		animation: flash 1.8s ease-out;
	}
	@keyframes flash {
		0% {
			background: rgba(255, 213, 0, 0.55);
		}
		100% {
			background: rgba(255, 213, 0, 0);
		}
	}
	/* The AI target is a per-row affordance kept out of the way until the row is
	   hovered or focused, so the tree stays quiet; the currently-targeted row keeps
	   it visible. Grey-alpha again, for both themes. */
	.target {
		flex: none;
		border: none;
		background: none;
		padding: 0 0.3rem;
		cursor: pointer;
		color: inherit;
		opacity: 0;
	}
	.row:hover .target,
	.row:focus-within .target,
	.row.targeted .target {
		opacity: 0.6;
	}
	.target:hover:not(:disabled),
	.row.targeted .target {
		opacity: 1;
	}
	.content code {
		white-space: pre-wrap;
	}
</style>
