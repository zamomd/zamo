<script lang="ts">
	import { base } from "../stores/base.svelte";
	import { changes } from "../stores/changes.svelte";
	import { search } from "../stores/search.svelte";
	import { shell } from "../stores/shell.svelte";
	import SearchBox from "./SearchBox.svelte";
	import { topicRows } from "./tree";

	// The base's index: every topic, and the first level of the selected one.
	// While a search is running the list is replaced by its hits rather than
	// shown beside them — two lists competing for the same column is worse than
	// one that changes meaning with a visible cause (the field above it).
	const rows = $derived(topicRows(base.forest, base.selectedTopicID));
	const searching = $derived(search.query.trim() !== "");
</script>

<aside>
	<header>Comind</header>
	<SearchBox />

	<div class="list">
		{#if searching}
			{#if search.error !== ""}
				<p class="note" role="alert">{search.error}</p>
			{:else if search.results.length === 0}
				<p class="note">{search.status === "" ? "searching…" : search.status}</p>
			{:else}
				<ul>
					{#each search.results as hit (hit.ID)}
						<li>
							<button type="button" class="row hit" onclick={() => search.jumpTo(hit)}>
								{hit.Content}
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		{:else if rows.length === 0}
			<p class="note">no topics yet</p>
		{:else}
			<ul>
				{#each rows as row (row.id)}
					<li>
						<button
							type="button"
							class="row topic"
							class:selected={row.selected}
							onclick={() => {
								shell.setMode("tree");
								base.selectTopic(row.id);
							}}
						>
							{row.title}
						</button>
						{#if row.children.length > 0}
							<ul class="sub">
								{#each row.children as child (child.id)}
									<li>
										<button type="button" class="row child" onclick={() => base.reveal(child.id)}>
											{child.title}
										</button>
									</li>
								{/each}
							</ul>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</div>

	<!-- An indicator, not a control: the rail owns switching to the changes
	     list, and a second door to the same room is a bug, not a shortcut. -->
	<footer>{changes.count} unreviewed</footer>
</aside>

<style>
	aside {
		flex: none;
		width: 22%;
		min-width: 12rem;
		display: flex;
		flex-direction: column;
		gap: var(--unit);
		padding: calc(var(--unit) * 2) var(--unit);
		border-right: 1px solid var(--hairline);
		overflow: hidden;
	}
	header {
		font-size: var(--size-heading);
		line-height: calc(var(--row-height) * 1.2);
		padding: 0 var(--unit);
	}
	.list {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.sub {
		margin-left: var(--indent);
	}
	.row {
		display: block;
		width: 100%;
		text-align: left;
		border: none;
		border-radius: 0;
		border-left: 2px solid transparent;
		background: none;
		padding: 0 var(--unit);
		line-height: var(--row-height);
		cursor: pointer;
		color: var(--text-dim);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.row:hover:not(:disabled) {
		color: var(--text);
		background: var(--hover);
	}
	.row.topic.selected {
		color: var(--accent);
		border-left-color: var(--accent);
	}
	.row.child,
	.row.hit {
		font-size: var(--size-small);
	}
	.note {
		margin: 0;
		padding: 0 var(--unit);
		font-size: var(--size-small);
		color: var(--text-faint);
	}
	footer {
		flex: none;
		padding: 0 var(--unit);
		font-size: var(--size-small);
		color: var(--text-faint);
	}
</style>
