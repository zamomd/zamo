<script lang="ts">
	import { onMount } from "svelte";
	import type { Block } from "../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";

	// Raw slice screen: type a topic, expand it into a tree, auto-apply the
	// proposal (deliberately review-less), then re-read the whole base and
	// render it. No styling: this is a risk-check tool, not a UI.
	type TreeNode = { block: Block; children: TreeNode[] };

	let topic: string = $state("");
	let hint: string = $state("");
	let status: string = $state("");
	let error: string = $state("");
	let busy: boolean = $state(false);
	let forest: TreeNode[] = $state([]);

	// Inline block editing: the owner's own edits apply straight to the base,
	// bypassing the review queue (review guards against AI, not the owner).
	let editingID: string | null = $state(null);
	let editText: string = $state("");

	// buildTree turns a pre-ordered flat subtree into a nested node. The root is
	// the block whose ParentID is empty; every other block is attached to its
	// parent in slice order, which GetSubtree already sorts by Position.
	function buildTree(blocks: Block[]): TreeNode | null {
		const byId = new Map<string, TreeNode>();
		for (const b of blocks) {
			byId.set(b.ID, { block: b, children: [] });
		}
		let root: TreeNode | null = null;
		for (const b of blocks) {
			const node = byId.get(b.ID);
			if (!node) {
				continue;
			}
			if (b.ParentID === "") {
				root = node;
			} else {
				byId.get(b.ParentID)?.children.push(node);
			}
		}
		return root;
	}

	// reload reads every topic root and its full subtree, rebuilding the forest.
	async function reload(): Promise<void> {
		const topics = (await App.TopicMap()) ?? [];
		const roots = topics.filter((b) => b.ParentID === "");
		const trees: TreeNode[] = [];
		for (const r of roots) {
			const blocks = (await App.GetSubtree(r.ID)) ?? [];
			const tree = buildTree(blocks);
			if (tree) {
				trees.push(tree);
			}
		}
		forest = trees;
	}

	async function expand(): Promise<void> {
		const t = topic.trim();
		if (t === "" || busy) {
			return;
		}
		busy = true;
		error = "";
		status = `expanding "${t}"…`;
		try {
			const proposal = await App.ExpandTopic(t, hint.trim());
			const ops = proposal.Ops ?? [];
			const selected = ops.map((_, i) => i);
			await App.ApplyProposal(proposal.ID, selected);
			await reload();
			status = `applied "${t}" (${ops.length} ops)`;
			topic = "";
			hint = "";
		} catch (err: unknown) {
			error = String(err);
			status = "";
		} finally {
			busy = false;
		}
	}

	function startEdit(block: Block): void {
		// Ignore while busy or while another block is already being edited, so an
		// in-progress edit is never silently discarded by switching blocks.
		if (busy || editingID !== null) {
			return;
		}
		editingID = block.ID;
		editText = block.Content;
	}

	function cancelEdit(): void {
		editingID = null;
		editText = "";
	}

	// saveEdit writes the block's new content directly, then re-reads the base.
	async function saveEdit(): Promise<void> {
		if (editingID === null || busy) {
			return;
		}
		busy = true;
		error = "";
		try {
			await App.EditBlockContent(editingID, editText);
			await reload();
			status = "saved edit";
			cancelEdit();
		} catch (err: unknown) {
			error = String(err);
			status = "";
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		reload().catch((err: unknown) => {
			error = String(err);
		});
	});
</script>

{#snippet treeNode(node: TreeNode)}
	<li>
		{#if editingID === node.block.ID}
			<textarea aria-label="block content" bind:value={editText} disabled={busy}></textarea>
			<button type="button" onclick={saveEdit} disabled={busy || editText.trim() === ""}>
				Save
			</button>
			<button type="button" onclick={cancelEdit} disabled={busy}>Cancel</button>
		{:else}
			<button
				type="button"
				title="click to edit"
				disabled={busy}
				onclick={() => startEdit(node.block)}
			>
				<code>{node.block.Content}</code>
			</button>
		{/if}
		{#if node.children.length > 0}
			<ul>
				{#each node.children as child (child.block.ID)}
					{@render treeNode(child)}
				{/each}
			</ul>
		{/if}
	</li>
{/snippet}

<main>
	<h1>Comind — raw slice</h1>

	<form
		onsubmit={(e) => {
			e.preventDefault();
			expand();
		}}
	>
		<input placeholder="topic" bind:value={topic} disabled={busy} />
		<input placeholder="hint (optional)" bind:value={hint} disabled={busy} />
		<button type="submit" disabled={busy}>Expand</button>
	</form>

	{#if status !== ""}
		<p><code>{status}</code></p>
	{/if}
	{#if error !== ""}
		<p><code>error: {error}</code></p>
	{/if}

	{#if forest.length === 0}
		<p>(no topics yet)</p>
	{:else}
		<ul>
			{#each forest as tree (tree.block.ID)}
				{@render treeNode(tree)}
			{/each}
		</ul>
	{/if}
</main>
