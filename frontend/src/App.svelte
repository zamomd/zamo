<script lang="ts">
	import { onMount } from "svelte";
	import ChangesView from "./ChangesView.svelte";
	import ModeRail from "./lib/ModeRail.svelte";
	import { jumpToBlock } from "./lib/navigate";
	import Sidebar from "./lib/Sidebar.svelte";
	import SettingsView from "./SettingsView.svelte";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";
	import { shell } from "./stores/shell.svelte";
	import TopicView from "./TopicView.svelte";

	onMount(() => {
		base.activate();
		return changes.start();
	});
</script>

<div class="shell">
	<ModeRail />
	<Sidebar />
	<main class="content">
		{#if shell.mode === "tree"}
			<TopicView />
		{:else if shell.mode === "graph"}
			<div class="scroll"><p class="placeholder">The graph view arrives in the next change.</p></div>
		{:else if shell.mode === "changes"}
			<div class="scroll"><ChangesView onjump={jumpToBlock} /></div>
		{:else}
			<div class="scroll"><SettingsView /></div>
		{/if}
	</main>
</div>

<style>
	/* Three top-level landmarks (nav, aside, main) rather than nav/aside nested
	   inside main: a screen reader's landmark navigation would otherwise see
	   one region instead of three. This div carries the flex layout that used
	   to live on main. */
	.shell {
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
	/* TopicView brings its own scroll container (.tree); the other three
	   branches need one of their own, or overflow: hidden above makes them
	   unscrollable — no scrollbar, no wheel, no trackpad. min-height: 0 is
	   what lets a flex item shrink below its content instead of growing the
	   container. */
	.scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
	}
	.placeholder {
		margin: 0;
		color: var(--text-faint);
	}
</style>
