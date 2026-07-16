<script lang="ts">
	import { onMount } from "svelte";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
	import BaseView from "./BaseView.svelte";
	import ReviewView from "./ReviewView.svelte";
	import SettingsView from "./SettingsView.svelte";

	// Tab shell: the Base view (the tree), the Review view (the pending
	// queue), and Settings (the provider configuration). Switching tabs
	// remounts the view, so each one re-reads its data.
	let tab: "base" | "review" | "settings" = $state("base");
	let pendingCount = $state(0);

	async function refreshCount(): Promise<void> {
		pendingCount = ((await App.PendingProposals()) ?? []).length;
	}

	function queued(): void {
		refreshCount().catch(() => {});
		tab = "review";
	}

	onMount(() => {
		refreshCount().catch(() => {});
	});
</script>

<main>
	<nav>
		<button type="button" class:active={tab === "base"} onclick={() => (tab = "base")}>
			Base
		</button>
		<button type="button" class:active={tab === "review"} onclick={() => (tab = "review")}>
			Review ({pendingCount})
		</button>
		<button type="button" class:active={tab === "settings"} onclick={() => (tab = "settings")}>
			Settings
		</button>
	</nav>

	{#if tab === "base"}
		<BaseView onqueued={queued} />
	{:else if tab === "review"}
		<ReviewView onchanged={refreshCount} />
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
