<script lang="ts">
	import type { Block } from "../../bindings/github.com/zamomd/zamo/internal/knowledge/models.js";
	import { Module as Smartcron } from "../../bindings/github.com/zamomd/zamo/internal/modules/smartcron";
	import type { View } from "../../bindings/github.com/zamomd/zamo/internal/modules/smartcron/models.js";
	import { ai } from "../stores/ai.svelte";
	import { base } from "../stores/base.svelte";
	import { schedules } from "../stores/schedules.svelte";
	import { messageFor } from "./callerror";
	import { attemptLabel, formatWhen, formProblem, invalidSpecMessage, runControl } from "./schedule";

	let { block }: { block: Block } = $props();

	const empty: View = { exists: false, instruction: "", spec: "", enabled: false, running: false };
	let view = $state<View>(empty);
	let instruction = $state("");
	let spec = $state("");
	let enabled = $state(true);
	let preview = $state<{ next: string[]; zone: string } | null>(null);
	let specError = $state("");
	let error = $state("");
	let confirmingRemoval = $state(false);
	let busy = $state(false);
	let loaded = $state(false);

	const running = $derived(view.running || schedules.running.has(block.ID));
	const problem = $derived(formProblem(instruction, spec) || specError);
	const dirty = $derived(instruction !== view.instruction || spec !== view.spec || enabled !== view.enabled);
	const control = $derived(runControl({ ...view, running }, ai.enabled));

	function show(v: View): void {
		view = v;
		instruction = v.instruction;
		spec = v.spec;
		enabled = v.exists ? v.enabled : true;
	}

	async function load(): Promise<void> {
		try {
			show(await Smartcron.Get(block.ID));
			if (view.exists) await refreshPreview(view.spec);
		} catch (err: unknown) {
			error = messageFor(err);
		} finally {
			loaded = true;
		}
	}
	$effect(() => {
		void block.ID;
		void load();
	});

	// The preview is the scheduler's own reading of the expression, so what
	// the form shows is what will fire. Debounced: it is a backend call per
	// keystroke otherwise.
	let previewTimer: ReturnType<typeof setTimeout> | undefined;
	let previewSeq = 0;
	function schedulePreview(value: string): void {
		clearTimeout(previewTimer);
		previewTimer = setTimeout(() => void refreshPreview(value), 250);
	}
	async function refreshPreview(value: string): Promise<void> {
		const mine = ++previewSeq;
		if (value.trim() === "") {
			preview = null;
			specError = "";
			return;
		}
		try {
			const p = await Smartcron.Preview(value);
			if (mine !== previewSeq) return;
			preview = { next: p.next ?? [], zone: p.zone };
			specError = "";
		} catch (err: unknown) {
			if (mine !== previewSeq) return;
			preview = null;
			specError = invalidSpecMessage(err) || messageFor(err);
		}
	}

	async function act(op: () => Promise<void>): Promise<void> {
		if (busy) return;
		busy = true;
		error = "";
		try {
			await op();
			await schedules.refresh();
		} catch (err: unknown) {
			error = invalidSpecMessage(err) || messageFor(err);
		} finally {
			busy = false;
		}
	}

	const save = () => act(async () => show(await Smartcron.Save(block.ID, instruction, spec, enabled)));
	const runNow = () => act(() => Smartcron.RunNow(block.ID));
	const cancel = () => act(() => Smartcron.Cancel(block.ID));
	const remove = () =>
		act(async () => {
			await Smartcron.Remove(block.ID);
			confirmingRemoval = false;
			show(empty);
			preview = null;
		});
</script>

<section class="schedule" aria-label="schedule">
	{#if loaded}
		<label class="field">
			<span class="name">instruction</span>
			<textarea
				aria-label="instruction"
				rows="2"
				placeholder="what the AI should do to this block"
				bind:value={instruction}
				disabled={busy}
			></textarea>
		</label>
		<label class="field">
			<span class="name">cron</span>
			<input
				type="text"
				aria-label="cron"
				placeholder="0 9 * * 1"
				spellcheck="false"
				bind:value={spec}
				disabled={busy}
				oninput={() => schedulePreview(spec)}
			/>
		</label>
		{#if specError}
			<p class="preview error">{specError}</p>
		{:else if preview && preview.next.length > 0}
			<p class="preview">
				next: {preview.next.map((n) => formatWhen(n, new Date())).join(", ")}
				<span class="zone">({preview.zone})</span>
			</p>
		{/if}
		<label class="enabled">
			<input type="checkbox" bind:checked={enabled} disabled={busy} />
			enabled
			<span class="note">runs only while Zamo is open</span>
		</label>
		{#if view.exists && view.lastRun}
			<p class="attempt">last attempt {attemptLabel(view, new Date())}</p>
		{/if}
		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}
		<div class="actions">
			<button type="button" disabled={busy || base.busy || problem !== "" || (!dirty && view.exists)} onclick={save}>
				save
			</button>
			{#if control === "cancel"}
				<button type="button" disabled={busy} onclick={cancel}>cancel</button>
			{:else if control === "run" || control === "run-blocked"}
				<button
					type="button"
					disabled={busy || base.busy || control === "run-blocked"}
					title={control === "run-blocked" ? "AI is switched off in Settings" : "run the instruction now"}
					onclick={runNow}
				>
					run now
				</button>
			{/if}
			{#if view.exists}
				{#if confirmingRemoval}
					<span class="confirm">
						really remove?
						<button type="button" disabled={busy} onclick={remove}>yes</button>
						<button type="button" disabled={busy} onclick={() => (confirmingRemoval = false)}>no</button>
					</span>
				{:else}
					<button type="button" disabled={busy} onclick={() => (confirmingRemoval = true)}>remove</button>
				{/if}
			{/if}
		</div>
	{/if}
</section>

<style>
	/* Same indent and quiet styling as HistoryPanel's .history: a small panel
	   hanging off this row, not a competing block. */
	.schedule {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
		margin: 0.1rem 0 0.25rem 1.25rem;
		padding: 0.3rem 0.4rem;
		font-size: var(--size-small);
		border-left: 2px solid var(--hairline);
	}
	.field {
		display: grid;
		grid-template-columns: 6rem 1fr;
		gap: 0.4rem;
		align-items: start;
	}
	.name {
		opacity: 0.6;
		line-height: 1.6rem;
	}
	textarea,
	input[type="text"] {
		width: 100%;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.15rem 0.4rem;
		color: inherit;
		font: inherit;
		resize: vertical;
	}
	.preview {
		margin: 0 0 0 6.4rem;
		color: var(--text-dim);
	}
	.zone {
		color: var(--text-faint);
	}
	.enabled {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		margin-left: 6.4rem;
	}
	.note {
		color: var(--text-faint);
	}
	.attempt {
		margin: 0 0 0 6.4rem;
		color: var(--text-dim);
	}
	.error {
		margin: 0 0 0 6.4rem;
		color: var(--danger);
	}
	.actions {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		margin-left: 6.4rem;
	}
	.actions button {
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		background: none;
		padding: 0.1rem 0.5rem;
		cursor: pointer;
		color: inherit;
	}
	.actions button:hover:not(:disabled) {
		background: var(--hover);
	}
	.confirm {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
</style>
