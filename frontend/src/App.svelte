<script lang="ts">
	import { onMount } from "svelte";
	import BaseView from "./BaseView.svelte";
	import ReviewView from "./ReviewView.svelte";
	import SettingsView from "./SettingsView.svelte";
	import { queue } from "./stores/queue.svelte";

	// Tab shell: the Base view (the tree), the Review view (the pending
	// queue), and Settings (the provider configuration). Switching tabs
	// remounts the view: each one re-reads its data on mount while its UI
	// state survives in its store. The pending badge reads the shared queue
	// store, whose polling starts here for the window's lifetime.
	let tab: "base" | "review" | "settings" = $state("base");

	function queued(): void {
		queue.refresh().catch(() => {});
		tab = "review";
	}

	onMount(() => queue.start());
</script>

<main>
	<nav>
		<button type="button" class:active={tab === "base"} onclick={() => (tab = "base")}>
			Base
		</button>
		<button type="button" class:active={tab === "review"} onclick={() => (tab = "review")}>
			Review ({queue.count})
		</button>
		<button type="button" class:active={tab === "settings"} onclick={() => (tab = "settings")}>
			Settings
		</button>
	</nav>

	{#if tab === "base"}
		<BaseView onqueued={queued} />
	{:else if tab === "review"}
		<ReviewView />
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
	nav button.active {
		font-weight: bold;
		text-decoration: underline;
	}
</style>
