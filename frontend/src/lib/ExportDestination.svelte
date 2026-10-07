<script lang="ts">
	import { onMount } from "svelte"
	import type { Destination } from "../../bindings/github.com/zamomd/zamo/internal/modules/export"
	import { Module as Export } from "../../bindings/github.com/zamomd/zamo/internal/modules/export"
	import { messageFor } from "./callerror"
	import { repoLine, resultLine, scheduleChoices, scheduleLine } from "./exportdestination"

	let dest = $state<Destination | null>(null)
	let message = $state("")
	let busy = $state(false)
	let status = $state("")
	let error = $state("")
	let forgetting = $state(false)
	const now = new Date()

	const chosen = $derived(dest !== null && dest.dir !== "")

	onMount(() => {
		void run(async () => show(await Export.Destination()))
	})

	function show(d: Destination): void {
		dest = d
		message = d.message
	}

	async function run(action: () => Promise<void>): Promise<void> {
		busy = true
		status = ""
		error = ""
		try {
			await action()
		} catch (err: unknown) {
			error = messageFor(err)
		} finally {
			busy = false
		}
	}

	function choose(): Promise<void> {
		return run(async () => show(await Export.ChooseDestination()))
	}

	// A refused change leaves the stored destination as it was, so the form is
	// put back to it rather than left showing what was not saved.
	function configure(change: Partial<Destination>): Promise<void> {
		const current = dest
		if (current === null) return Promise.resolve()
		const next = { ...current, message, ...change }
		return run(async () => {
			try {
				show(
					await Export.Configure({
						commit: next.commit,
						message: next.message,
						spec: next.spec,
						scheduled: next.scheduled,
					}),
				)
			} catch (err: unknown) {
				show(await Export.Destination())
				throw err
			}
		})
	}

	function reschedule(spec: string): Promise<void> {
		return spec === "" ? configure({ scheduled: false }) : configure({ spec, scheduled: true })
	}

	function exportNow(): Promise<void> {
		const commits = dest?.commit ?? false
		return run(async () => {
			const result = await Export.ExportNow()
			status = resultLine(result, commits)
		})
	}

	function forget(): Promise<void> {
		forgetting = false
		return run(async () => {
			await Export.ForgetDestination()
			show(await Export.Destination())
		})
	}
</script>

<div class="destination">
	<div class="head">
		<div class="about">
			<span class="title">Kept export</span>
			<p class="note">
				One directory Zamo exports into again and again — by hand or on a schedule — removing the files of
				topics that are gone. Each export can be committed to git, so the directory keeps its history.
			</p>
		</div>
		{#if !chosen}
			<button type="button" onclick={choose} disabled={busy || dest === null}>Choose a directory…</button>
		{/if}
	</div>

	{#if dest !== null && chosen}
		<div class="row">
			<span class="name">Directory</span>
			<code class="dir" title={dest.dir}>{dest.dir}</code>
			<button type="button" onclick={choose} disabled={busy}>Change…</button>
		</div>
		<label class="row">
			<span class="name">Schedule</span>
			<select
				value={dest.scheduled ? dest.spec : ""}
				disabled={busy}
				onchange={(e) => reschedule(e.currentTarget.value)}
			>
				<option value="">Off</option>
				{#each scheduleChoices(dest.spec) as choice (choice.spec)}
					<option value={choice.spec}>{choice.label}</option>
				{/each}
			</select>
		</label>
		<label class="row">
			<span class="name">Commit to git</span>
			<input
				type="checkbox"
				checked={dest.commit}
				disabled={busy}
				onchange={(e) => configure({ commit: e.currentTarget.checked })}
			/>
		</label>
		{#if dest.commit}
			<label class="row">
				<span class="name">Commit message</span>
				<input type="text" bind:value={message} disabled={busy} spellcheck="false"
					   onchange={() => configure({})} />
			</label>
			<p class="note">{repoLine(dest)} Only this directory is committed, and nothing is pushed.</p>
		{/if}
		{#if scheduleLine(dest, now) !== ""}
			<p class="note">{scheduleLine(dest, now)}</p>
		{/if}

		<div class="actions">
			<button type="button" onclick={exportNow} disabled={busy}>Export now</button>
			{#if forgetting}
				<span class="note">Stop keeping this export? The directory stays as it is.</span>
				<button type="button" onclick={forget} disabled={busy}>Forget it</button>
				<button type="button" onclick={() => (forgetting = false)} disabled={busy}>Cancel</button>
			{:else}
				<button type="button" class="forget" onclick={() => (forgetting = true)} disabled={busy}>Forget…
				</button>
			{/if}
		</div>
	{/if}

	{#if status !== ""}<p class="status" role="status">{status}</p>{/if}
	{#if error !== ""}<p class="status error" role="alert">error: {error}</p>{/if}
</div>

<style>
	.destination {
		padding: var(--unit) 0;
		border-top: 1px solid var(--hairline);
		border-bottom: 1px solid var(--hairline);
	}

	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: calc(var(--unit) * 2);
	}

	.head button {
		flex: none;
	}

	.note {
		margin: calc(var(--unit) / 2) 0 0;
		color: var(--text-dim);
		font-size: var(--size-small);
		line-height: 1.6;
	}

	.head .note,
	.actions .note {
		margin-top: 0;
	}

	.row {
		display: grid;
		grid-template-columns: 11rem minmax(0, 1fr) auto;
		column-gap: calc(var(--unit) * 2);
		align-items: baseline;
		padding: calc(var(--unit) / 2) 0;
	}

	.name {
		color: var(--text-dim);
	}

	.dir {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.row input[type="checkbox"] {
		justify-self: start;
		margin: 0;
		accent-color: var(--accent);
	}

	.actions {
		display: flex;
		align-items: center;
		gap: var(--unit);
		margin-top: var(--unit);
	}

	.forget {
		margin-left: auto;
	}

	.status {
		margin: var(--unit) 0 0;
		color: var(--text-dim);
		font-size: var(--size-small);
	}

	.status.error {
		color: var(--danger);
	}
</style>
