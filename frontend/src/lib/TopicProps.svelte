<script lang="ts">
	import type { TopicProps } from "../../bindings/github.com/zamomd/zamo/internal/knowledge";
	import { App } from "../../bindings/github.com/zamomd/zamo/internal/wailsbind";
	import { base } from "../stores/base.svelte";
	import { messageFor } from "./callerror";
	import { splitNames, tagKey, unreferenceable } from "./topicprops";

	let { id }: { id: string } = $props();

	let current = $state<TopicProps>({ Aliases: [], Tags: [] });
	let editing = $state(false);
	let aliasText = $state("");
	let tagText = $state("");
	let error = $state("");
	let saving = $state(false);

	const aliases = $derived(current.Aliases ?? []);
	const tags = $derived(current.Tags ?? []);

	function toggleFilter(tag: string): void {
		const key = tagKey(tag);
		base.tagFilter = base.tagFilter === key ? null : key;
	}

	// The answer belongs to the topic it was asked for: one that arrives after
	// the view moved on must not overwrite the properties now on screen.
	$effect(() => {
		const asked = id;
		editing = false;
		error = "";
		current = { Aliases: [], Tags: [] };
		App.TopicProps(asked)
			.then((got) => {
				if (asked === id) {
					current = got;
				}
			})
			.catch((err: unknown) => {
				if (asked === id) {
					error = messageFor(err);
				}
			});
	});

	function open(): void {
		aliasText = aliases.join(", ");
		tagText = tags.join(", ");
		error = "";
		editing = true;
	}

	async function save(): Promise<void> {
		const newAliases = splitNames(aliasText);
		const newTags = splitNames(tagText);
		const bad = newAliases.find(unreferenceable);
		if (bad !== undefined) {
			error = `“${bad}” can't be an alias: a reference can't carry [ or ].`;
			return;
		}
		const asked = id;
		saving = true;
		error = "";
		try {
			const got = await App.SetTopicProps(asked, newAliases, newTags);
			// Aliases change how references resolve and tags what the sidebar can
			// filter by; both indexes are read with the forest.
			await base.reload();
			if (asked === id) {
				current = got;
				editing = false;
			}
		} catch (err: unknown) {
			error = messageFor(err);
		} finally {
			saving = false;
		}
	}
</script>

{#if editing}
	<form
		class="props editing"
		onsubmit={(e) => {
			e.preventDefault();
			void save();
		}}
	>
		<label>
			<span>aliases</span>
			<input type="text" placeholder="other names, comma-separated" bind:value={aliasText} disabled={saving} />
		</label>
		<label>
			<span>tags</span>
			<input type="text" placeholder="labels to find this by, comma-separated" bind:value={tagText} disabled={saving} />
		</label>
		<div class="actions">
			<button type="submit" disabled={saving || base.busy}>save</button>
			<button type="button" disabled={saving} onclick={() => (editing = false)}>cancel</button>
		</div>
		{#if error !== ""}
			<p class="error" role="alert">{error}</p>
		{/if}
	</form>
{:else}
	<div class="props">
		{#if aliases.length > 0}
			<span class="group"><span class="key">aliases</span> {aliases.join(", ")}</span>
		{/if}
		{#if tags.length > 0}
			<span class="group">
				<span class="key">tags</span>
				{#each tags as tag (tag)}
					<button
						type="button"
						class="tag"
						title="show only topics with this tag"
						aria-pressed={base.tagFilter === tagKey(tag)}
						onclick={() => toggleFilter(tag)}>#{tag}</button
					>
				{/each}
			</span>
		{/if}
		<button type="button" class="edit" disabled={base.busy} onclick={open}>
			{aliases.length === 0 && tags.length === 0 ? "+ aliases, tags" : "edit"}
		</button>
		{#if error !== ""}
			<span class="error" role="alert">{error}</span>
		{/if}
	</div>
{/if}

<style>
	.props {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 0 calc(var(--unit) * 2);
		font-size: var(--size-small);
		color: var(--text-dim);
		line-height: var(--row-height);
	}
	.props.editing {
		flex-direction: column;
		align-items: stretch;
		gap: calc(var(--unit) / 2);
		padding-bottom: var(--unit);
		max-width: 44rem;
	}
	.key {
		color: var(--text-faint);
	}
	label {
		display: flex;
		align-items: baseline;
		gap: var(--unit);
	}
	label span {
		flex: none;
		width: 4rem;
		color: var(--text-faint);
	}
	input {
		flex: 1;
		min-width: 0;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.1rem 0.4rem;
		color: var(--text);
		font: inherit;
	}
	.actions {
		display: flex;
		gap: var(--unit);
	}
	/* Unframed, like the rail: these sit in a line of text, and a box around
	   each would outweigh the title above them. */
	.tag,
	.edit {
		border: none;
		background: none;
		padding: 0;
		font: inherit;
		cursor: pointer;
	}
	.tag {
		color: var(--accent);
		margin-right: var(--unit);
	}
	.edit {
		color: var(--text-faint);
	}
	.tag:hover:not(:disabled),
	.edit:hover:not(:disabled) {
		background: none;
		text-decoration: underline;
	}
	.error {
		margin: 0;
		color: var(--danger);
	}
</style>
