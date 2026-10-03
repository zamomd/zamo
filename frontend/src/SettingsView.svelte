<script lang="ts">
	import { onMount } from "svelte";
	import { Module as BaseBackup } from "../bindings/github.com/zamomd/zamo/internal/modules/basebackup";
	import { Module as Export } from "../bindings/github.com/zamomd/zamo/internal/modules/export";
	import { Module as VaultImport } from "../bindings/github.com/zamomd/zamo/internal/modules/vaultimport";
	import type { Plan } from "../bindings/github.com/zamomd/zamo/internal/modules/vaultimport/models";
	import type { In, View } from "../bindings/github.com/zamomd/zamo/internal/settings/models.js";
	import { App } from "../bindings/github.com/zamomd/zamo/internal/wailsbind";
	import { messageFor } from "./lib/callerror";
	import { submittedSwitch, submittedText } from "./lib/settingsinput";
	import { canApply, count, describeKind, groupByKind, summaryLine } from "./lib/vaultimport";
	import { ai } from "./stores/ai.svelte";

	let view = $state<View | null>(null);
	let aiEnabled = $state(true);
	let aiEnabledDirty = $state(false);
	let provider = $state("");
	let model = $state("");
	let baseUrl = $state("");
	let apiKey = $state("");
	let status = $state("");
	let error = $state("");
	let busy = $state(false);

	let providerDirty = $state(false);
	let modelDirty = $state(false);
	let baseUrlDirty = $state(false);

	let resetConfirming = $state(false);

	// The whole base: one archive holding what Markdown export cannot carry.
	let pendingRestore = $state("");
	let baseStatus = $state("");
	let baseError = $state("");
	let baseBusy = $state(false);
	// The section whose action reported last, so its outcome reads next to it.
	type BaseArea = "transfer" | "backup";
	let baseArea = $state<BaseArea>("transfer");
	let restoreConfirming = $state(false);

	// An import previewed and not yet applied or dropped. The backend holds
	// the same plan under its token; nothing is written until Apply names it.
	let importPlan = $state<Plan | null>(null);

	onMount(() => {
		void refreshPending();
	});

	async function refreshPending(): Promise<void> {
		try {
			pendingRestore = await BaseBackup.PendingRestore();
		} catch (err: unknown) {
			baseError = messageFor(err);
		}
	}

	async function runBase(area: BaseArea, action: () => Promise<void>): Promise<void> {
		baseArea = area;
		baseBusy = true;
		baseStatus = "";
		baseError = "";
		try {
			await action();
		} catch (err: unknown) {
			baseError = messageFor(err);
		} finally {
			baseBusy = false;
		}
	}

	function exportMarkdown(): Promise<void> {
		return runBase("transfer", async () => {
			const dir = await Export.Export();
			// An empty path is the directory dialog being dismissed, not a failure.
			baseStatus = dir === "" ? "" : `Exported to ${dir}`;
		});
	}

	function previewImport(): Promise<void> {
		return runBase("transfer", async () => {
			const plan = await VaultImport.Preview();
			// An empty token is the directory dialog being dismissed, not a failure.
			importPlan = plan.token === "" ? null : plan;
		});
	}

	function applyImport(): Promise<void> {
		const plan = importPlan;
		if (plan === null) {
			return Promise.resolve();
		}
		return runBase("transfer", async () => {
			const result = await VaultImport.Apply(plan.token);
			importPlan = null;
			baseStatus = `Imported ${count(result.pages, "page")} as ${count(result.blocks, "block")}`;
		});
	}

	function discardImport(): Promise<void> {
		return runBase("transfer", async () => {
			await VaultImport.Discard();
			importPlan = null;
		});
	}

	function backUp(): Promise<void> {
		return runBase("backup", async () => {
			const dest = await BaseBackup.Backup();
			// An empty path is the file dialog being dismissed, not a failure.
			baseStatus = dest === "" ? "" : `Backed up to ${dest}`;
		});
	}

	function chooseRestore(): Promise<void> {
		restoreConfirming = false;
		return runBase("backup", async () => {
			const archive = await BaseBackup.Restore();
			if (archive === "") {
				return;
			}
			pendingRestore = archive;
			baseStatus = "";
		});
	}

	function cancelRestore(): Promise<void> {
		return runBase("backup", async () => {
			await BaseBackup.CancelRestore();
			pendingRestore = "";
			baseStatus = "The pending restore was dropped.";
		});
	}

	let ro = $derived(view?.declarative ?? false);
	let noProvider = $derived(provider === "");

	async function load(): Promise<void> {
		const v = await App.Settings();
		view = v;
		aiEnabled = v.aiEnabled.value;
		ai.set(v.aiEnabled.value);
		provider = v.provider.value;
		model = v.model.value;
		baseUrl = v.baseUrl.value;
		apiKey = "";
		aiEnabledDirty = false;
		providerDirty = false;
		modelDirty = false;
		baseUrlDirty = false;
		resetConfirming = false;
	}

	// Where a value comes from, as the short tag beside its field and the
	// sentence behind it.
	const sources: Record<string, { tag: string; detail: string }> = {
		env: { tag: "environment", detail: "Set from the environment; it cannot be changed here." },
		config: { tag: "config.toml", detail: "Set in config.toml." },
		api_key_file: { tag: "api_key_file", detail: "Read from the api_key_file in config.toml." },
		gui: { tag: "set here", detail: "Set on this screen, over config.toml." },
	};

	let providerFallbackNote = $derived.by((): string => {
		if (!view) return "";
		if (provider === "" && view.provider.origin === "config" && view.provider.value !== "") {
			return (
				`config.toml declares "${view.provider.value}" here - saving with no provider selected ` +
				"falls back to that value instead. Use the switch above to turn AI off."
			);
		}
		return "";
	});

	let aiLocked = $derived(ro || view?.aiEnabled.origin === "env");

	function submittedAIEnabled(): boolean | null {
		return submittedSwitch(aiEnabledDirty, aiEnabled, view?.aiEnabled.override ?? null);
	}

	function input(): In {
		const submittedProvider = submittedText(providerDirty, provider, view?.provider.override ?? null);
		if (noProvider) {
			return {
				aiEnabled: submittedAIEnabled(),
				provider: submittedProvider,
				model: null,
				baseUrl: null,
				apiKey: "",
				clearApiKey: false,
			};
		}
		const submittedModel = submittedText(modelDirty, model, view?.model.override ?? null);
		const submittedBaseUrl = submittedText(baseUrlDirty, baseUrl, view?.baseUrl.override ?? null);
		return {
			aiEnabled: submittedAIEnabled(),
			provider: submittedProvider,
			model: submittedModel,
			baseUrl: submittedBaseUrl,
			apiKey,
			clearApiKey: false,
		};
	}

	// submit runs one settings action, reporting done on success. Every action
	// on this screen answers in the same status line, so they share the guard.
	async function submit(op: () => Promise<void>, done: string): Promise<void> {
		busy = true;
		status = "";
		error = "";
		try {
			await op();
			status = done;
		} catch (e) {
			error = messageFor(e);
		} finally {
			busy = false;
		}
	}

	function save(): Promise<void> {
		return submit(async () => {
			await App.SaveSettings(input());
			await load();
		}, "Saved. The change is already in effect.");
	}

	function check(): Promise<void> {
		return submit(() => App.CheckSettings(input()), "The provider answered.");
	}

	function clearKey(): Promise<void> {
		return submit(async () => {
			await App.SaveSettings({ ...input(), apiKey: "", clearApiKey: true });
			await load();
		}, "GUI key override removed.");
	}

	type Overridable = "aiEnabled" | "provider" | "model" | "baseUrl";

	// resetField drops one override and leaves the others as stored, so it
	// never saves an edit still sitting unsaved in another field.
	function resetField(field: Overridable): Promise<void> {
		const v = view;
		if (v === null) {
			return Promise.resolve();
		}
		return submit(async () => {
			await App.SaveSettings({
				aiEnabled: v.aiEnabled.override,
				provider: v.provider.override,
				model: v.model.override,
				baseUrl: v.baseUrl.override,
				apiKey: "",
				clearApiKey: false,
				[field]: null,
			});
			await load();
		}, "Override removed.");
	}

	function requestReset(): void {
		resetConfirming = true;
	}

	function cancelReset(): void {
		resetConfirming = false;
	}

	function confirmReset(): Promise<void> {
		resetConfirming = false;
		return submit(async () => {
			await App.SaveSettings({
				aiEnabled: null,
				provider: null,
				model: null,
				baseUrl: null,
				apiKey: "",
				clearApiKey: true,
			});
			await load();
		}, "Reset to config.toml.");
	}

	onMount(() => {
		load().catch((e: unknown) => {
			error = messageFor(e);
		});
	});
</script>

{#snippet source(origin: string, reset: () => Promise<void>)}
	{@const src = sources[origin]}
	<span class="source">
		{#if src}
			<span title={src.detail}>{src.tag}</span>
			{#if origin === "gui"}
				<button type="button" class="link" onclick={reset} disabled={ro || busy}>reset</button>
			{/if}
		{/if}
	</span>
{/snippet}

{#if view}
	<section class="settings">
		{#if ro}
			<p class="banner">
				Settings are managed by <code>config.toml</code>. This screen is read-only; Check still works.
			</p>
		{/if}

		<h2>AI</h2>

		<div class="row">
			<label class="name" for="ai-enabled">Enable AI features</label>
			<div class="control">
				<input
					id="ai-enabled"
					type="checkbox"
					bind:checked={aiEnabled}
					disabled={aiLocked}
					onchange={() => (aiEnabledDirty = true)}
				/>
				{#if !aiEnabled}
					<p class="note">
						Every AI control is hidden and nothing is sent to a provider. Blocks written earlier stay
						where they are and unreviewed ones stay reviewable. Your provider settings below are kept.
					</p>
				{/if}
			</div>
			{@render source(view.aiEnabled.origin, () => resetField("aiEnabled"))}
		</div>

		<div class="provider" class:off={!aiEnabled}>
			<div class="row">
				<label class="name" for="provider">Provider</label>
				<div class="control">
					<select
						id="provider"
						bind:value={provider}
						disabled={ro || view.provider.origin === "env"}
						onchange={() => (providerDirty = true)}
					>
						<option value="">None</option>
						<option value="anthropic">Anthropic</option>
						<option value="openai-compatible">OpenAI-compatible</option>
					</select>
					{#if providerFallbackNote !== ""}<p class="note warning">{providerFallbackNote}</p>{/if}
					{#if noProvider}
						<p class="note">
							Choose a provider to set a model, an API key, and (for OpenAI-compatible) a base URL.
							There is nothing to check until then.
						</p>
					{/if}
				</div>
				{@render source(view.provider.origin, () => resetField("provider"))}
			</div>

			{#if !noProvider}
				<div class="row">
					<label class="name" for="model">Model</label>
					<div class="control">
						<input
							id="model"
							bind:value={model}
							disabled={ro || view.model.origin === "env"}
							placeholder="provider default"
							oninput={() => (modelDirty = true)}
						/>
					</div>
					{@render source(view.model.origin, () => resetField("model"))}
				</div>

				{#if provider === "openai-compatible"}
					<div class="row">
						<label class="name" for="base-url">Base URL</label>
						<div class="control">
							<input
								id="base-url"
								bind:value={baseUrl}
								disabled={ro || view.baseUrl.origin === "env"}
								placeholder="http://localhost:11434/v1"
								oninput={() => (baseUrlDirty = true)}
							/>
						</div>
						{@render source(view.baseUrl.origin, () => resetField("baseUrl"))}
					</div>
				{/if}

				<div class="row">
					<label class="name" for="api-key">API key</label>
					<div class="control">
						<input
							id="api-key"
							type="password"
							bind:value={apiKey}
							disabled={ro || view.apiKey.origin === "env"}
							placeholder={view.apiKey.set ? "set — type to replace" : "not set"}
						/>
						{#if ro && view.apiKey.origin === "default"}
							<p class="note">
								Set api_key_file in config.toml, or the provider's API key environment variable.
							</p>
						{/if}
					</div>
					{@render source(view.apiKey.origin, clearKey)}
				</div>
			{/if}
		</div>

		<div class="actions">
			<button type="button" class="primary" onclick={save} disabled={ro || busy}>Save</button>
			<button type="button" onclick={check} disabled={busy || noProvider || !aiEnabled}>Check</button>
			{#if status !== ""}<span class="status" role="status">{status}</span>{/if}
			{#if error !== ""}<span class="status error" role="alert">error: {error}</span>{/if}
			{#if !resetConfirming}
				<button type="button" class="link reset-all" onclick={requestReset} disabled={ro || busy}>
					Reset all to config.toml
				</button>
			{/if}
		</div>

		{#if resetConfirming}
			<p class="confirm">
				Reset every field to config.toml? This also permanently discards any API key entered in this
				screen.
				<button type="button" onclick={confirmReset} disabled={busy}>Confirm reset</button>
				<button type="button" onclick={cancelReset} disabled={busy}>Cancel</button>
			</p>
		{/if}

		<h2>Import &amp; export</h2>

		<div class="item">
			<div class="about">
				<span class="title">Markdown export</span>
				<p class="note">
					One Markdown file per topic, readable by Obsidian and Logseq, into a directory you choose.
					Files named after topics already there are overwritten.
				</p>
			</div>
			<button type="button" onclick={exportMarkdown} disabled={baseBusy}>Export as Markdown…</button>
		</div>

		<div class="item">
			<div class="about">
				<span class="title">Vault import</span>
				<p class="note">
					Brings an Obsidian vault, a Logseq graph or a Zamo export in as topics of your own.
				</p>
			</div>
			{#if importPlan === null}
				<button type="button" onclick={previewImport} disabled={baseBusy}>Import a vault…</button>
			{/if}
		</div>

		{#if importPlan !== null}
			<div class="confirm plan">
				<p>{summaryLine(importPlan)}</p>
				{#if (importPlan.conflicts ?? []).length > 0}
					<p>Left out:</p>
					<ul>
						{#each groupByKind(importPlan.conflicts) as group (group.kind)}
							<li>
								{describeKind(group.kind)}:
								{#each group.items as c, i (c.path)}{i > 0 ? ", " : ""}<code>{c.page}</code>{/each}
							</li>
						{/each}
					</ul>
				{/if}
				{#if (importPlan.warnings ?? []).length > 0}
					<p>Converted with losses:</p>
					<ul>
						{#each groupByKind(importPlan.warnings) as group (group.kind)}
							<li>
								<details>
									<summary>{describeKind(group.kind)} ({group.count})</summary>
									<ul>
										{#each group.items as w, i (i)}
											<li><code>{w.page}</code> {w.detail}</li>
										{/each}
									</ul>
								</details>
							</li>
						{/each}
					</ul>
				{/if}
				<p>
					Nothing is written until you confirm. Imported pages become topics of your own, with
					no review to do.
					<button type="button" onclick={applyImport} disabled={baseBusy || !canApply(importPlan)}>
						Import {count(importPlan.pages, "page")}
					</button>
					<button type="button" onclick={discardImport} disabled={baseBusy}>Cancel</button>
				</p>
			</div>
		{/if}

		{#if baseArea === "transfer"}
			{#if baseStatus !== ""}<p class="status" role="status">{baseStatus}</p>{/if}
			{#if baseError !== ""}<p class="status error" role="alert">error: {baseError}</p>{/if}
		{/if}

		<h2>Backup</h2>
		<p class="note lead">
			A backup holds what Markdown leaves out: stable ids, revisions, review state and references.
			Neither a backup nor an export carries an API key.
		</p>

		<div class="item">
			<div class="about">
				<span class="title">Back up</span>
				<p class="note">Writes the whole base into one archive file.</p>
			</div>
			<button type="button" onclick={backUp} disabled={baseBusy}>Back up now…</button>
		</div>

		<div class="item">
			<div class="about">
				<span class="title">Restore</span>
				<p class="note">Replaces this base with the one in a backup, the next time Zamo starts.</p>
			</div>
			{#if !restoreConfirming && pendingRestore === ""}
				<button type="button" onclick={() => (restoreConfirming = true)} disabled={baseBusy}>
					Restore from a backup…
				</button>
			{/if}
		</div>

		{#if restoreConfirming}
			<p class="confirm">
				Restoring replaces this base with the one in the backup. The base you have now is kept
				beside it, and nothing changes until you restart Zamo.
				<button type="button" onclick={chooseRestore} disabled={baseBusy}>Choose a backup…</button>
				<button type="button" onclick={() => (restoreConfirming = false)} disabled={baseBusy}>
					Cancel
				</button>
			</p>
		{/if}

		{#if pendingRestore !== ""}
			<p class="confirm">
				<code>{pendingRestore}</code> replaces this base the next time Zamo starts. It cannot
				happen while the app is running, because the MCP server may be reading the base too.
				<button type="button" onclick={cancelRestore} disabled={baseBusy}>
					Cancel the restore
				</button>
			</p>
		{/if}

		{#if baseArea === "backup"}
			{#if baseStatus !== ""}<p class="status" role="status">{baseStatus}</p>{/if}
			{#if baseError !== ""}<p class="status error" role="alert">error: {baseError}</p>{/if}
		{/if}
	</section>
{/if}

<style>
	.settings {
		max-width: 44rem;
	}
	h2 {
		margin: calc(var(--unit) * 4) 0 var(--unit);
		padding-bottom: calc(var(--unit) / 2);
		border-bottom: 1px solid var(--hairline);
		font-size: var(--size-heading);
		font-weight: bold;
	}
	h2:first-of-type {
		margin-top: 0;
	}

	/* One grid for every field, so names, controls and sources each line up
	   down the section. */
	.row {
		display: grid;
		grid-template-columns: 11rem minmax(0, 1fr) 9rem;
		column-gap: calc(var(--unit) * 2);
		align-items: baseline;
		padding: calc(var(--unit) / 2) 0;
	}
	.name {
		color: var(--text-dim);
	}
	.control input:not([type="checkbox"]),
	.control select {
		width: 100%;
	}
	.control input[type="checkbox"] {
		margin: 0;
		accent-color: var(--accent);
		vertical-align: middle;
	}
	.source {
		display: flex;
		gap: var(--unit);
		color: var(--text-faint);
		font-size: var(--size-small);
		line-height: var(--row-height);
		white-space: nowrap;
	}
	/* Kept visible while AI is off: the values are still stored, only unused. */
	.provider.off {
		opacity: 0.55;
	}

	.note {
		margin: calc(var(--unit) / 2) 0 0;
		color: var(--text-dim);
		font-size: var(--size-small);
		line-height: 1.6;
	}
	.note.warning {
		color: var(--text);
		font-weight: bold;
	}
	.note.lead {
		margin: 0 0 var(--unit);
		max-width: 36rem;
	}

	.actions {
		display: flex;
		align-items: center;
		gap: var(--unit);
		margin-top: calc(var(--unit) * 1.5);
	}
	.primary {
		border-color: var(--accent);
		color: var(--accent);
	}
	.primary:disabled {
		border-color: var(--hairline-strong);
	}
	.reset-all {
		margin-left: auto;
	}
	.status {
		margin: 0;
		color: var(--text-dim);
		font-size: var(--size-small);
	}
	p.status {
		margin-top: var(--unit);
	}
	.status.error {
		color: var(--danger);
	}

	.item {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: calc(var(--unit) * 2);
		padding: var(--unit) 0;
	}
	.item + .item {
		border-top: 1px solid var(--hairline);
	}
	.item .note {
		margin-top: 0;
	}
	.item button {
		flex: none;
	}

	.banner {
		margin: 0 0 calc(var(--unit) * 2);
		padding: var(--unit);
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
	}
	.confirm {
		display: flex;
		align-items: center;
		gap: var(--unit);
		flex-wrap: wrap;
		margin: var(--unit) 0 0;
		padding: var(--unit);
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
		font-size: var(--size-small);
	}
	.plan {
		display: block;
	}
	.plan p {
		margin: 0;
	}
	.plan ul {
		margin: 0.25rem 0 0.5rem 1.25rem;
		padding: 0;
	}
	.plan summary {
		cursor: pointer;
	}
	button.link {
		border: none;
		background: none;
		padding: 0;
		color: var(--text-dim);
		font-size: var(--size-small);
		text-decoration: underline;
	}
	button.link:hover:not(:disabled) {
		background: none;
		color: var(--text);
	}
	button.link:disabled {
		color: var(--text-faint);
		text-decoration: none;
	}
</style>
