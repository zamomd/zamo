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
			placeholder={base.scopeID === null ? "new topic to expand…" : "how should the AI restructure this?"}
			bind:value={base.command}
			disabled={base.busy || base.editingID !== null}
		/>
		<button
			type="submit"
			disabled={base.busy || base.editingID !== null || base.command.trim() === ""}
		>
			Ask AI
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
		font-size: 0.85rem;
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
	.ask {
		display: flex;
		gap: 0.5rem;
	}
	.ask input {
		flex: 1;
	}
</style>
