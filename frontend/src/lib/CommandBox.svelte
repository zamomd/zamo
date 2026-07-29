<script lang="ts">
	import { base } from "../stores/base.svelte";

	// The one AI command field: expansion when nothing is targeted, a
	// restructure of the targeted block otherwise. The write applies directly
	// and lands unreviewed; the view stays on the Base tab (see base.askAI).
	async function submit(e: SubmitEvent): Promise<void> {
		e.preventDefault();
		await base.askAI();
	}
</script>

<form onsubmit={submit}>
	<p class="scope">
		{#if base.scopeID === null}
			New topic — expanded from scratch.
		{:else}
			Editing <code>{base.scopeLabel}</code>
			<button type="button" class="scope-clear" onclick={() => base.clearScope()} disabled={base.busy}>
				clear
			</button>
		{/if}
	</p>
	<div class="ask">
		<input
			aria-label="AI command"
			placeholder={base.scopeID === null ? "ask ai to expand a new topic" : "ask ai how to restructure this"}
			bind:value={base.command}
			disabled={base.busy || base.editingID !== null}
		/>
		<button
			type="submit"
			disabled={base.busy || base.editingID !== null || base.command.trim() === ""}
		>
			send
		</button>
	</div>
</form>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
		margin-bottom: 1rem;
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
