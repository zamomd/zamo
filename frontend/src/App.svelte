<script lang="ts">
	import { onMount } from "svelte";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
	import BaseView from "./BaseView.svelte";
	import ReviewView from "./ReviewView.svelte";
	import SettingsView from "./SettingsView.svelte";

	// Tab shell: the Base view (the tree), the Review view (the pending
	// queue), and Settings (the provider configuration). Switching tabs
	// remounts the view, so each one re-reads its data.
	// The window has no push channel from the MCP server, so it re-reads the
	// pending queue itself on a timer (and on focus) — see #15: no daemon, no bus.
	const POLL_MS = 5000;

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
		// An MCP-submitted proposal arrives out of band while the window sits idle;
		// re-read it into the badge without a restart. Poll only while the window is
		// visible (no wakeups when minimized) and re-read the moment it regains focus
		// or visibility, so a return from the background is never left stale.
		const refresh = (): void => {
			if (!document.hidden) {
				refreshCount().catch(() => {});
			}
		};
		const timer = setInterval(refresh, POLL_MS);
		window.addEventListener("focus", refresh);
		document.addEventListener("visibilitychange", refresh);
		return () => {
			clearInterval(timer);
			window.removeEventListener("focus", refresh);
			document.removeEventListener("visibilitychange", refresh);
		};
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
		gap: 0.4rem;
		margin-bottom: 1.25rem;
	}
	nav button.active {
		color: var(--sea);
		border-color: var(--sea);
		font-weight: 600;
	}
</style>
