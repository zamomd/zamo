<script lang="ts">
	import { onMount } from "svelte";
	import BaseView from "./BaseView.svelte";
	import ChangesView from "./ChangesView.svelte";
	import SettingsView from "./SettingsView.svelte";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";

	// Tab shell: the Base view (the tree), the Changes view (everything
	// awaiting review), and Settings (the provider configuration). Switching
	// tabs remounts the view: each one re-reads its data on mount while its UI
	// state survives in its store. The nav badge reads the shared changes
	// store, whose polling starts here for the window's lifetime.
	let tab: "base" | "changes" | "settings" = $state("base");

	// jump lands a Changes-screen row back in the tree: switch tabs, then
	// reveal (base.reveal already un-collapses ancestors and flashes the row —
	// the same jump the search box uses).
	function jump(id: string): void {
		tab = "base";
		base.reveal(id);
	}

	onMount(() => changes.start());
</script>

<main>
	<nav>
		<button type="button" class:active={tab === "base"} onclick={() => (tab = "base")}>
			Base
		</button>
		<button type="button" class:active={tab === "changes"} onclick={() => (tab = "changes")}>
			Changes ({changes.count})
		</button>
		<button type="button" class:active={tab === "settings"} onclick={() => (tab = "settings")}>
			Settings
		</button>
	</nav>

	{#if tab === "base"}
		<BaseView />
	{:else if tab === "changes"}
		<ChangesView onjump={jump} />
	{:else}
		<SettingsView />
	{/if}
</main>

<style>
	nav {
		display: flex;
		gap: 0.5rem;
		margin-bottom: 1rem;
	}
	nav button {
		border: none;
		border-radius: 0;
		border-bottom: 1px solid transparent;
		color: var(--text-dim);
		padding: 0 var(--unit);
		line-height: calc(var(--row-height) * 1.2);
	}
	nav button:hover:not(:disabled) {
		color: var(--text);
		background: none;
	}
	nav button.active {
		color: var(--text);
		border-bottom-color: var(--accent);
	}
</style>
