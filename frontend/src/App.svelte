<script lang="ts">
	import { onMount } from "svelte";
	import ChangesView from "./ChangesView.svelte";
	import ModeRail from "./lib/ModeRail.svelte";
	import Sidebar from "./lib/Sidebar.svelte";
	import SettingsView from "./SettingsView.svelte";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";
	import { shell } from "./stores/shell.svelte";
	import TopicView from "./TopicView.svelte";

	// Three zones: the mode rail, the topic sidebar, and the main area whose
	// content the rail selects. Unlike the tab shell this replaced, the views no
	// longer remount on every switch — the sidebar and the base store stay put,
	// so switching to the changes list and back keeps the reader's place.
	function jump(id: string): void {
		shell.setMode("tree");
		base.reveal(id);
	}

	onMount(() => {
		base.activate();
		return changes.start();
	});
</script>

<main>
	<ModeRail />
	<Sidebar />
	<section class="content">
		{#if shell.mode === "tree"}
			<TopicView />
		{:else if shell.mode === "graph"}
			<p class="placeholder">The graph view arrives in the next change.</p>
		{:else if shell.mode === "changes"}
			<ChangesView onjump={jump} />
		{:else}
			<SettingsView />
		{/if}
	</section>
</main>

<style>
	main {
		display: flex;
		height: 100vh;
		overflow: hidden;
	}
	.content {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		padding: calc(var(--unit) * 2);
	}
	.placeholder {
		margin: 0;
		color: var(--text-faint);
	}
</style>
