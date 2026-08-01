<script lang="ts">
	import type { Block } from "../../bindings/github.com/cominddev/comind/internal/knowledge/models.js";
	import { App } from "../../bindings/github.com/cominddev/comind/internal/wailsbind";
	import { base } from "../stores/base.svelte";
	import { messageFor } from "./callerror";
	import { caretColumn } from "./caret";
	import { firstLine, normalizeTitle, topicName } from "./refs";
	import { createRefSearch } from "./refsearch";
	import { refLabel, type Trigger, topicReferenceable } from "./reftrigger";

	let { trigger, target }: { trigger: Trigger; target: HTMLTextAreaElement | undefined } = $props();

	type Entry =
		| { kind: "topic"; id: string; label: string }
		| { kind: "create"; name: string }
		| { kind: "block"; id: string; label: string; content: string };

	let topics = $state<Block[]>([]);
	let blocks = $state<Block[]>([]);
	let selected = $state(0);
	let dismissedStart = $state<number | null>(null);

	const open = $derived(trigger.start !== dismissedStart);
	const query = $derived(trigger.query.trim());
	const column = $derived(caretColumn(base.editText, trigger.start));

	const entries = $derived.by((): Entry[] => {
		if (trigger.kind === "block") {
			return blocks.map((b) => ({ kind: "block" as const, id: b.ID, label: firstLine(b.Content), content: b.Content }));
		}
		// A title containing "[" or "]" can never be written as [[name]] — see
		// topicReferenceable — so such a topic is excluded here rather than
		// offered and silently failing to resolve once inserted.
		const key = normalizeTitle(query);
		const candidates = topics.filter((t) => topicReferenceable(topicName(t.Content)));
		const matches = candidates.filter((t) => key === "" || normalizeTitle(t.Content).includes(key));
		const out: Entry[] = matches.map((t) => ({ kind: "topic" as const, id: t.ID, label: topicName(t.Content) }));
		const exact = key !== "" && matches.some((t) => normalizeTitle(t.Content) === key);
		if (query !== "" && !exact && topicReferenceable(query)) {
			out.push({ kind: "create", name: query });
		}
		return out;
	});

	function entryKey(e: Entry): string {
		return e.kind === "create" ? `create:${e.name}` : `${e.kind}:${e.id}`;
	}

	// Reset the selection whenever the candidate set changes — an index held
	// over from a previous list may no longer point at the same entry.
	$effect(() => {
		void entries;
		selected = 0;
	});

	let topicsLoaded = false;
	$effect(() => {
		if (trigger.kind === "topic" && !topicsLoaded) {
			topicsLoaded = true;
			void loadTopics();
		}
	});

	async function loadTopics(): Promise<void> {
		try {
			topics = ((await App.TopicMap()) ?? []).filter((b) => b.ParentID === "");
		} catch (err: unknown) {
			topics = [];
			base.error = messageFor(err);
		}
	}

	// Block search is debounced to avoid a backend call per keystroke, and only
	// the current query's hits are ever listed (createRefSearch). Topics are
	// filtered client-side against one fetch, so they need neither.
	const search = createRefSearch<Block>({
		search: (q) => App.Search(q),
		onResults: (hits) => {
			blocks = hits;
		},
		onError: (err) => {
			blocks = [];
			base.error = messageFor(err);
		},
	});

	$effect(() => {
		if (trigger.kind !== "block") {
			return;
		}
		search.update(query);
		return () => search.cancel();
	});

	function acceptEntry(entry: Entry): void {
		const end = trigger.start + 2 + trigger.query.length;
		if (entry.kind === "topic") {
			base.acceptRef(trigger.start, end, `[[${entry.label}]]`);
		} else if (entry.kind === "create") {
			base.acceptRef(trigger.start, end, `[[${entry.name}]]`);
		} else {
			base.acceptRef(trigger.start, end, `((${entry.id}|${refLabel(entry.content)}))`);
		}
	}

	function dismiss(): void {
		dismissedStart = trigger.start;
	}

	// Registered on document, capturing, so it runs during the capture phase —
	// strictly before the event reaches the textarea's own bubble-phase
	// onkeydown (InlineEditor). A listener added directly on the textarea would
	// not get that guarantee: at the target itself the DOM invokes listeners in
	// registration order regardless of the capture flag, and this component
	// mounts after the textarea already has its own handler.
	//
	// Gated on e.target === target, not just on `open`/base.editingID: onblur
	// declines to commit while base.busy or the window is unfocused, so the
	// editing textarea can stay mounted (editingID still set) while focus sits
	// elsewhere. Comparing the event's actual target is what keeps a stray key
	// from being swallowed by, or accepted into, that invisible edit.
	function handleKeydown(e: KeyboardEvent): void {
		if (!open || e.isComposing || e.target !== target) {
			return;
		}
		if (e.key === "Escape") {
			e.preventDefault();
			e.stopPropagation();
			dismiss();
			return;
		}
		if (entries.length === 0) {
			return;
		}
		switch (e.key) {
			case "ArrowDown":
				e.preventDefault();
				e.stopPropagation();
				selected = (selected + 1) % entries.length;
				return;
			case "ArrowUp":
				e.preventDefault();
				e.stopPropagation();
				selected = (selected - 1 + entries.length) % entries.length;
				return;
			case "Enter":
				e.preventDefault();
				e.stopPropagation();
				// entries may have shrunk since the last render (an async search
				// result landing between a keystroke and this effect's flush);
				// clamp rather than trust a possibly stale selected.
				acceptEntry(entries[Math.min(selected, entries.length - 1)]);
				return;
		}
	}

	$effect(() => {
		document.addEventListener("keydown", handleKeydown, { capture: true });
		return () => document.removeEventListener("keydown", handleKeydown, { capture: true });
	});
</script>

{#if open}
	<!-- mousedown preventDefault on the whole popup, not just the buttons: it
	     keeps the textarea focused for any mousedown here, including the
	     scrollbar gutter, list padding, and the buttonless empty state — a real
	     focus shift would blur the textarea first, and InlineEditor's onblur
	     commits and clears the edit before a click ever reaches acceptEntry.
	     mousedown bubbles from any descendant, and the browser's default focus
	     change is decided after the full bubble path runs, so one handler here
	     covers the entire surface. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- biome-ignore lint/a11y/noStaticElementInteractions: mousedown here carries
	     no semantic interaction, only preventDefault to keep the textarea
	     focused — the real controls are the buttons inside. -->
	<div class="ref-popup" style:left="{column}ch" onmousedown={(e) => e.preventDefault()}>
		{#if entries.length === 0}
			<p class="empty">
				{trigger.kind === "topic" ? "no matching topics" : query === "" ? "type to search blocks" : "no matches"}
			</p>
		{:else}
			<ul>
				{#each entries as entry, i (entryKey(entry))}
					<li>
						<button type="button" class:selected={i === selected} onclick={() => acceptEntry(entry)} onmouseenter={() => (selected = i)}>
							{#if entry.kind === "create"}
								Reference “{entry.name}”
							{:else}
								{entry.label}
							{/if}
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
{/if}

<style>
	.ref-popup {
		position: absolute;
		top: 100%;
		z-index: 20;
		min-width: 12rem;
		max-width: 24rem;
		max-height: 10rem;
		overflow-y: auto;
		scrollbar-gutter: stable;
		margin-top: 0.15rem;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: var(--ground);
		box-shadow: 0 2px 8px var(--hover);
		font-size: var(--size-small);
	}
	.empty {
		margin: 0;
		padding: 0.35rem 0.5rem;
		opacity: 0.55;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0.2rem;
	}
	button {
		display: block;
		width: 100%;
		text-align: left;
		border: none;
		border-radius: var(--radius);
		background: none;
		padding: 0.25rem 0.4rem;
		cursor: pointer;
		color: inherit;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	button.selected {
		background: var(--hover);
	}
</style>
