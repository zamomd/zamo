<script lang="ts">
	import { Mode } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { base } from "../stores/base.svelte";
	import { changes } from "../stores/changes.svelte";
	import { search } from "../stores/search.svelte";
	import { jumpToBlock, openTopic } from "./navigate";
	import { plainText } from "./plaintext";
	import SearchBox from "./SearchBox.svelte";
	import { topicRows } from "./tree";

	const rows = $derived(topicRows(base.forest, base.selectedTopicID));
	const searching = $derived(search.query.trim() !== "");

	// The new-topic form is a real element, not prompt(): confirm()/alert()/
	// prompt() are no-ops in the Wails webview (see AGENTS.md).
	let creatingTopic = $state(false);
	let newTopicName = $state("");
	let newTopicMode: "outline" | "doc" = $state("outline");

	function openCreateTopic(): void {
		newTopicName = "";
		newTopicMode = "outline";
		creatingTopic = true;
	}

	function cancelCreateTopic(): void {
		creatingTopic = false;
	}

	async function submitCreateTopic(): Promise<void> {
		const name = newTopicName.trim();
		if (name === "" || base.busy) {
			return;
		}
		creatingTopic = false;
		await base.createTopic(name, newTopicMode);
	}
</script>

<aside>
	<header>Comind</header>
	<SearchBox />

	<div class="list">
		{#if searching}
			{#if search.error !== ""}
				<p class="note error" role="alert">{search.error}</p>
			{:else if search.results.length === 0}
				<p class="note">{search.status === "" ? "searching…" : search.status}</p>
			{:else}
				<ul>
					{#each search.results as hit (hit.ID)}
						<li>
							<button type="button" class="row hit" disabled={base.busy} onclick={() => jumpToBlock(hit.ID)}>
								{plainText(hit.Content)}
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
							aria-current={row.selected ? "true" : undefined}
							disabled={base.busy}
							onclick={() => openTopic(row.id)}
						>
							<span class="mode-icon" title={row.mode === Mode.ModeDoc ? "document" : "outline"} aria-hidden="true"
								>{row.mode === Mode.ModeDoc ? "▤" : "☰"}</span
							>
							{row.title}
						</button>
						{#if row.children.length > 0}
							<ul class="sub">
								{#each row.children as child (child.id)}
									<li>
										<button
											type="button"
											class="row child"
											disabled={base.busy}
											onclick={() => jumpToBlock(child.id)}
										>
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

	<div class="new-topic">
		{#if creatingTopic}
			<form
				class="new-topic-form"
				onsubmit={(e) => {
					e.preventDefault();
					void submitCreateTopic();
				}}
			>
				<input
					type="text"
					placeholder="topic name"
					aria-label="new topic name"
					bind:value={newTopicName}
					disabled={base.busy}
				/>
				<div class="mode-choice" role="radiogroup" aria-label="topic shape">
					<label>
						<input type="radio" name="new-topic-mode" value="outline" bind:group={newTopicMode} />
						outline
					</label>
					<label>
						<input type="radio" name="new-topic-mode" value="doc" bind:group={newTopicMode} />
						document
					</label>
				</div>
				<div class="new-topic-actions">
					<button type="submit" disabled={base.busy || newTopicName.trim() === ""}>create</button>
					<button type="button" onclick={cancelCreateTopic}>cancel</button>
				</div>
			</form>
		{:else}
			<button type="button" class="new-topic-button" disabled={base.busy} onclick={openCreateTopic}>
				+ new topic
			</button>
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
		scrollbar-gutter: stable;
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
	.mode-icon {
		display: inline-block;
		width: 1.1em;
		font-size: 0.85em;
		opacity: 0.7;
	}
	.note {
		margin: 0;
		padding: 0 var(--unit);
		font-size: var(--size-small);
		color: var(--text-faint);
	}
	.note.error {
		color: var(--danger);
	}
	.new-topic {
		flex: none;
		padding: 0 var(--unit);
	}
	.new-topic-button {
		width: 100%;
		text-align: left;
		border: none;
		background: none;
		padding: 0;
		line-height: var(--row-height);
		cursor: pointer;
		color: var(--text-dim);
	}
	.new-topic-button:hover:not(:disabled) {
		color: var(--text);
	}
	.new-topic-form {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
		padding: 0.3rem 0;
	}
	.new-topic-form input[type="text"] {
		width: 100%;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.15rem 0.4rem;
		color: inherit;
		font: inherit;
	}
	.mode-choice {
		display: flex;
		gap: 0.75rem;
		font-size: var(--size-small);
		color: var(--text-dim);
	}
	.mode-choice label {
		display: flex;
		align-items: center;
		gap: 0.2rem;
	}
	.new-topic-actions {
		display: flex;
		gap: 0.4rem;
	}
	.new-topic-actions button {
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.1rem 0.5rem;
		cursor: pointer;
		color: inherit;
	}
	.new-topic-actions button:hover:not(:disabled) {
		background: var(--hover);
	}
	footer {
		flex: none;
		padding: 0 var(--unit);
		font-size: var(--size-small);
		color: var(--text-faint);
	}
</style>
