<script lang="ts">
	import { onMount } from "svelte";
	import ChangesView from "./ChangesView.svelte";
	import AssistantPanel from "./lib/AssistantPanel.svelte";
	import GraphView from "./lib/GraphView.svelte";
	import ModeRail from "./lib/ModeRail.svelte";
	import { jumpToBlock } from "./lib/navigate";
	import Sidebar from "./lib/Sidebar.svelte";
	import SettingsView from "./SettingsView.svelte";
	import { ai } from "./stores/ai.svelte";
	import { assistant } from "./stores/assistant.svelte";
	import { base } from "./stores/base.svelte";
	import { changes } from "./stores/changes.svelte";
	import { schedules } from "./stores/schedules.svelte";
	import { shell } from "./stores/shell.svelte";
	import TopicView from "./TopicView.svelte";

	onMount(() => {
		base.activate();
		void ai.load();
		const stopChanges = changes.start();
		const stopSchedules = schedules.start();
		const stopAssistant = assistant.start();
		return () => {
			stopChanges();
			stopSchedules();
			stopAssistant();
		};
	});

	function onKey(e: KeyboardEvent): void {
		if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k" && ai.enabled) {
			e.preventDefault();
			shell.toggleAssistant();
		}
	}
</script>

<svelte:window onkeydown={onKey} />

<div class="shell">
	<ModeRail />
	<Sidebar />
	<main class="content">
		{#if shell.mode === "tree"}
			<TopicView />
		{:else if shell.mode === "graph"}
			<GraphView />
		{:else if shell.mode === "changes"}
			<div class="scroll"><ChangesView onjump={jumpToBlock} /></div>
		{:else}
			<div class="scroll"><SettingsView /></div>
		{/if}
	</main>
	{#if ai.enabled && shell.assistantOpen}
		<AssistantPanel />
	{/if}
</div>

<style>
	/* Top-level landmarks (nav, aside, main, and the assistant's aside when it
	   is open) rather than nav/aside nested inside main: a screen reader's
	   landmark navigation would otherwise see one region instead of several. This div carries the flex layout that used
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
	/* TopicView brings its own scroll container (.tree) and GraphView fills its
	   area rather than scrolling; changes and settings need a scroll container
	   of their own, or overflow: hidden above makes them unscrollable — no
	   scrollbar, no wheel, no trackpad. min-height: 0 is what lets a flex item
	   shrink below its content instead of growing the container. */
	.scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		scrollbar-gutter: stable;
	}
</style>
