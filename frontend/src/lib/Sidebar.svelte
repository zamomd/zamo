<script lang="ts">
	import { Mode } from '../../bindings/github.com/zamomd/zamo/internal/knowledge';
	import { base } from "../stores/base.svelte";
	import { changes } from "../stores/changes.svelte";
	import { search } from "../stores/search.svelte";
	import { shell } from "../stores/shell.svelte";
	import { jumpToBlock, openTopic } from "./navigate";
	import { plainText } from "./plaintext";
	import SearchBox from "./SearchBox.svelte";
	import { SIDEBAR_MIN, SIDEBAR_STEP, sidebarMax, sidebarSize } from "./sidebarsize";
	import { hasTag, tagCounts } from "./topicprops"
	import { topicRows } from "./tree";

	const tags = $derived(tagCounts(base.tags));
	// A filter naming a tag no topic carries any more is no filter: otherwise
	// removing the last such tag would leave the list empty with nothing on
	// screen to undo it.
	const activeTag = $derived(tags.some((t) => t.key === base.tagFilter) ? base.tagFilter : null);
	const rows = $derived(
		topicRows(base.forest, base.selectedTopicID).filter(
			(row) => activeTag === null || hasTag(base.tags, row.id, activeTag),
		),
	);
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

	let aside: HTMLElement | undefined = $state();
	let dragging = $state(false);
	// Left edge of the sidebar and the width to return to if the drag ends hidden.
	let dragOrigin = 0;
	let dragStartWidth: number | null = null;

	function currentWidth(): number {
		return aside?.offsetWidth ?? 0;
	}

	function resize(desired: number): void {
		const size = sidebarSize(desired, sidebarMax(window.innerWidth));
		shell.sidebarHidden = size.hidden;
		if (!size.hidden) {
			shell.sidebarWidth = size.width;
		}
	}

	function toggle(): void {
		shell.sidebarHidden = !shell.sidebarHidden;
		shell.saveSidebar();
	}

	function startDrag(e: PointerEvent): void {
		if (e.button !== 0) {
			return;
		}
		e.preventDefault();
		const handle = e.currentTarget as HTMLElement;
		handle.setPointerCapture(e.pointerId);
		dragOrigin = handle.getBoundingClientRect().left - currentWidth();
		dragStartWidth = shell.sidebarWidth;
		dragging = true;
	}

	function drag(e: PointerEvent): void {
		if (dragging) {
			resize(e.clientX - dragOrigin);
		}
	}

	function endDrag(): void {
		if (!dragging) {
			return;
		}
		dragging = false;
		if (shell.sidebarHidden) {
			shell.sidebarWidth = dragStartWidth;
		}
		shell.saveSidebar();
	}

	function onHandleKey(e: KeyboardEvent): void {
		if (e.key === "Enter") {
			toggle();
		} else if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
			const step = e.key === "ArrowLeft" ? -SIDEBAR_STEP : SIDEBAR_STEP;
			// From hidden, one step right lands on the minimum width rather than
			// a step past nothing.
			resize(shell.sidebarHidden ? (step > 0 ? SIDEBAR_MIN : 0) : currentWidth() + step);
			shell.saveSidebar();
		} else {
			return;
		}
		e.preventDefault();
	}
</script>

<aside
	hidden={shell.sidebarHidden}
	bind:this={aside}
	style:width={shell.sidebarWidth === null ? undefined : `${shell.sidebarWidth}px`}
	style:min-width="{SIDEBAR_MIN}px"
>
	<SearchBox />

	{#if tags.length > 0 && !searching}
		<div class="tags">
			{#each tags as tag (tag.key)}
				<button
					type="button"
					class="tag"
					class:active={activeTag === tag.key}
					aria-pressed={activeTag === tag.key}
					title="{tag.topics} {tag.topics === 1 ? 'topic' : 'topics'}"
					onclick={() => (base.tagFilter = activeTag === tag.key ? null : tag.key)}
				>
					#{tag.name}
				</button>
			{/each}
		</div>
	{/if}

	<div class="list">
		{#if searching}
			{#if search.error !== ""}
				<p class="note error" role="alert">{search.error}</p>
			{:else if search.results.length === 0}
				<p class="note">{search.status === "" ? "searching…" : search.status}</p>
			{:else}
				<ul>
					{#each search.results as hit (hit.Block.ID)}
						<li>
							<button type="button" class="row hit" disabled={base.busy} onclick={() => jumpToBlock(hit.Block.ID)}>
								<span class="hit-title">{plainText(hit.Block.Content)}</span>
								{#if hit.Snippet !== ""}
									<span class="hit-snippet">{hit.Snippet}</span>
								{/if}
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		{:else if rows.length === 0}
			<p class="note">{activeTag === null ? "no topics yet" : "no topics with this tag"}</p>
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

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<!-- biome-ignore lint/a11y/useSemanticElements: a focusable separator is a window splitter, which <hr> cannot be -->
<div
	class="resizer"
	class:dragging
	role="separator"
	aria-orientation="vertical"
	aria-label="sidebar width"
	aria-valuemin={0}
	aria-valuemax={sidebarMax(window.innerWidth)}
	aria-valuenow={shell.sidebarHidden ? 0 : (shell.sidebarWidth ?? aside?.offsetWidth ?? SIDEBAR_MIN)}
	title={shell.sidebarHidden ? "drag to show topics" : "drag to resize, drag to the rail to hide"}
	tabindex="0"
	onpointerdown={startDrag}
	onpointermove={drag}
	onpointerup={endDrag}
	onpointercancel={endDrag}
	ondblclick={toggle}
	onkeydown={onHandleKey}
></div>

<style>
	aside {
		flex: none;
		width: 22%;
		max-width: 60%;
		display: flex;
		flex-direction: column;
		gap: var(--unit);
		padding: calc(var(--unit) * 2) var(--unit);
		border-right: 1px solid var(--hairline);
		overflow: hidden;
	}
	/* Zero-width in the layout, so the hairline it sits on stays the only
	   visible edge; the grab area is the pseudo-element straddling it. */
	.resizer {
		flex: none;
		position: relative;
		z-index: 1;
		width: 0;
		user-select: none;
		touch-action: none;
	}
	.resizer::before {
		content: "";
		position: absolute;
		top: 0;
		bottom: 0;
		left: -4px;
		width: 7px;
		cursor: col-resize;
	}
	.resizer::after {
		content: "";
		position: absolute;
		top: 0;
		bottom: 0;
		left: -1px;
		width: 1px;
		background: transparent;
	}
	.resizer:hover::after,
	.resizer:focus-visible::after,
	.resizer.dragging::after {
		background: var(--accent);
	}
	.resizer:focus-visible {
		outline: none;
	}
	/* Covers the window for the length of a drag: the cursor holds and nothing
	   underneath gets hovered or selected. */
	.resizer.dragging::before {
		position: fixed;
		inset: 0;
		width: auto;
	}
	/* display: flex above outranks the attribute's own display: none. */
	aside[hidden] {
		display: none;
	}
	.tags {
		flex: none;
		display: flex;
		flex-wrap: wrap;
		gap: 0 var(--unit);
		padding: 0 var(--unit);
		max-height: calc(var(--row-height) * 3);
		overflow-y: auto;
		font-size: var(--size-small);
	}
	/* Unframed: a row of boxed chips would outweigh the topic list it filters. */
	.tag {
		border: none;
		background: none;
		padding: 0;
		line-height: var(--row-height);
		color: var(--text-faint);
		cursor: pointer;
	}
	.tag:hover {
		color: var(--text);
		background: none;
	}
	.tag.active {
		color: var(--accent);
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
	.row.child {
		font-size: var(--size-small);
	}
	.row.hit {
		display: flex;
		flex-direction: column;
		gap: 0.05rem;
		white-space: normal;
		padding-top: 0.15rem;
		padding-bottom: 0.15rem;
		font-size: var(--size-small);
	}
	.hit-title {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.hit-snippet {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--text-faint);
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
