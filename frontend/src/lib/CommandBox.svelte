<script lang="ts">
	import { base } from '../stores/base.svelte'

	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault()
		await base.askAI()
	}

	const placeholder = $derived.by(() => {
		if (base.generating) return 'working…'
		if (base.unchanged) return 'the ai found nothing to change'
		return base.scopeID === null ? 'ask ai to expand a new topic' : 'ask ai how to restructure this'
	})
</script>

<form onsubmit={submit}>
	{#if base.scopeID !== null}
		<p class="scope">
			Editing <code>{base.scopeLabel}</code>
			<button type="button" class="scope-clear" onclick={() => base.clearScope()} disabled={base.busy}>
				clear
			</button>
		</p>
	{/if}
	<div class="ask" class:working={base.generating}>
		<input aria-label="AI command" {placeholder} bind:value={base.command} disabled={base.busy} />
		{#if base.generating}
			<button type="button" onclick={() => base.cancelAI()} disabled={base.cancelling}>cancel</button>
		{:else}
			<button type="submit" disabled={base.busy || base.command.trim() === ""}>send</button>
		{/if}
	</div>
</form>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.scope {
		margin: 0;
		font-size: var(--size-small);
		opacity: 0.75;
	}

	/* Keep a long targeted-block content on one line so the command area does not
	   balloon; the full text still lives in the tree. */
	.scope code {
		display: inline-block;
		max-width: 22rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		vertical-align: bottom;
	}

	.scope-clear {
		border: none;
		background: none;
		padding: 0 0.2rem;
		cursor: pointer;
		color: inherit;
		text-decoration: underline;
	}

	/* One frame around the pair; the divider between them is the frame's own
	   hairline, so the action reads as part of the field rather than as a
	   separate filled button. */
	.ask {
		display: flex;
		align-items: stretch;
		border: 1px solid var(--hairline-strong);
		border-radius: var(--radius);
		overflow: hidden;
	}

	.ask:focus-within {
		border-color: var(--accent);
	}

	.ask.working {
		border-color: transparent;
		background: linear-gradient(var(--ground), var(--ground)) padding-box,
		linear-gradient(90deg, var(--accent), #a06bd6, #d06f9c, #d9a441, var(--accent)) border-box;
		background-size: auto,
		200% 100%;
		animation: shimmer 2.4s linear infinite;
	}

	@keyframes shimmer {
		to {
			background-position: 0 0,
			200% 0;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.ask.working {
			animation: none;
		}
	}

	.ask input {
		flex: 1;
		min-width: 0;
		border: none;
		border-radius: 0;
		height: calc(var(--row-height) * 1.5);
	}

	.ask input:focus {
		border: none;
	}

	.ask button {
		flex: none;
		border: none;
		border-left: 1px solid var(--hairline-strong);
		border-radius: 0;
		color: var(--text-dim);
		padding: 0 calc(var(--unit) * 2);
	}

	.ask button:hover:not(:disabled) {
		color: var(--text);
		background: var(--hover);
	}
</style>
