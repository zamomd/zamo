<script lang="ts">
	import { onMount } from "svelte";
	import type { In, View } from "../bindings/github.com/cominddev/comind/internal/settings/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";
	import { messageFor } from "./lib/callerror";
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

	let ro = $derived(view?.declarative ?? false);
	// Two distinct states, deliberately not merged: the switch is the owner's
	// policy, while no provider is an unfinished configuration. Turning AI off
	// keeps the provider fields on screen, because it keeps them in the file.
	let noProvider = $derived(provider === "");

	async function load(): Promise<void> {
		const v = await App.Settings();
		view = v;
		aiEnabled = v.aiEnabled;
		ai.set(v.aiEnabled);
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

	function fieldNote(field: { origin: string }): string {
		if (field.origin === "config") return "set in config.toml";
		if (field.origin === "gui") return "overridden here";
		return "";
	}
	let providerNote = $derived(view ? fieldNote(view.provider) : "");
	let modelNote = $derived(view ? fieldNote(view.model) : "");
	let baseUrlNote = $derived(view ? fieldNote(view.baseUrl) : "");

	let apiKeyNote = $derived.by((): string => {
		if (!view) return "";
		switch (view.apiKeyOrigin) {
			case "env":
				return "set from the environment; it cannot be changed here";
			case "api_key_file":
				return "read from the api_key_file in config.toml";
			case "config":
				return "set in config.toml";
			case "gui":
				return "overridden here";
			default:
				return ro ? "set api_key_file in config.toml, or the provider's API key environment variable" : "";
		}
	});

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

	let aiEnabledNote = $derived.by((): string => {
		if (!view) return "";
		switch (view.aiEnabledOrigin) {
			case "env":
				return "set from COMIND_AI_ENABLED; it cannot be changed here";
			case "config":
				return "set in config.toml";
			case "gui":
				return "overridden here";
			default:
				return "";
		}
	});
	// The environment wins over both files, so offering the switch would be
	// offering a control that cannot take effect.
	let aiLocked = $derived(ro || view?.aiEnabledOrigin === "env");

	// submittedAIEnabled follows the string fields' rule: null means "not
	// overridden", so an untouched switch leaves config.toml deciding.
	function submittedAIEnabled(): boolean | null {
		return aiEnabledDirty || view?.aiEnabledOrigin === "gui" ? aiEnabled : null;
	}

	function input(): In {
		const submittedProvider = providerDirty || view?.provider.origin === "gui" ? provider : "";
		if (noProvider) {
			return {
				aiEnabled: submittedAIEnabled(),
				provider: submittedProvider,
				model: "",
				baseUrl: "",
				apiKey: "",
				clearApiKey: false,
			};
		}
		const submittedModel = modelDirty || view?.model.origin === "gui" ? model : "";
		const submittedBaseUrl = baseUrlDirty || view?.baseUrl.origin === "gui" ? baseUrl : "";
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
				provider: "",
				model: "",
				baseUrl: "",
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

{#if view}
	<section>
		{#if ro}
			<p class="banner">
				Settings are managed by <code>config.toml</code>. This screen is read-only; Check still works.
			</p>
		{/if}

		<div class="field switch">
			<label class="switch-label">
				<input
					type="checkbox"
					bind:checked={aiEnabled}
					disabled={aiLocked}
					onchange={() => (aiEnabledDirty = true)}
				/>
				Enable AI features
			</label>
			{#if aiEnabledNote !== ""}<small>{aiEnabledNote}</small>{/if}
			{#if !aiEnabled}
				<small>
					Every AI control is hidden and nothing is sent to a provider. Blocks written earlier stay
					where they are and unreviewed ones stay reviewable. Your provider settings below are kept.
				</small>
			{/if}
		</div>

		<label>
			Provider
			<select bind:value={provider} disabled={ro} onchange={() => (providerDirty = true)}>
				<option value="">None — no provider configured</option>
				<option value="anthropic">Anthropic</option>
				<option value="openai-compatible">OpenAI-compatible</option>
			</select>
			{#if providerNote !== ""}<small>{providerNote}</small>{/if}
			{#if providerFallbackNote !== ""}<small class="warning">{providerFallbackNote}</small>{/if}
		</label>

		{#if noProvider}
			<p class="hint">
				No provider is configured. Choose one above to set a model, an API key, and (for
				OpenAI-compatible) a base URL. There is nothing to check until then.
			</p>
		{:else}
			<label>
				Model
				<input bind:value={model} disabled={ro} placeholder="provider default" oninput={() => (modelDirty = true)} />
				{#if modelNote !== ""}<small>{modelNote}</small>{/if}
			</label>

			{#if provider === "openai-compatible"}
				<label>
					Base URL
					<input
						bind:value={baseUrl}
						disabled={ro}
						placeholder="http://localhost:11434/v1"
						oninput={() => (baseUrlDirty = true)}
					/>
					{#if baseUrlNote !== ""}<small>{baseUrlNote}</small>{/if}
				</label>
			{/if}

			<div class="field">
				<label class="field-label" for="api-key">API key</label>
				<input
					id="api-key"
					type="password"
					bind:value={apiKey}
					disabled={ro || view.apiKeyOrigin === "env"}
					placeholder={view.apiKeySet ? "set — type to replace" : "not set"}
				/>
				{#if apiKeyNote !== ""}<small>{apiKeyNote}</small>{/if}
				<button
					type="button"
					class="link"
					onclick={clearKey}
					disabled={ro || busy || view.apiKeyOrigin !== "gui"}
				>
					Clear key
				</button>
			</div>
		{/if}

		<div class="actions">
			<button type="button" onclick={save} disabled={ro || busy}>Save</button>
			<button type="button" onclick={check} disabled={busy || noProvider || !aiEnabled}>Check</button>
			{#if !resetConfirming}
				<button type="button" onclick={requestReset} disabled={ro || busy}>
					Reset to config (clears key too)
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

		{#if status !== ""}<p><code>{status}</code></p>{/if}
		{#if error !== ""}<p><code>error: {error}</code></p>{/if}
	</section>
{/if}

<style>
	label {
		display: block;
		margin-bottom: 0.75rem;
	}
	.field {
		margin-bottom: 0.75rem;
	}
	.field-label {
		margin-bottom: 0.25rem;
	}
	/* The switch governs every other field on the screen, so it is separated
	   from them rather than sitting in the same run of inputs. */
	.switch {
		padding-bottom: 0.75rem;
		border-bottom: 1px solid var(--hairline);
	}
	.switch-label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-bottom: 0.25rem;
	}
	.switch-label input {
		width: auto;
		margin: 0;
	}
	small {
		display: block;
		opacity: 0.7;
	}
	small.warning {
		opacity: 1;
		color: currentColor;
		font-weight: bold;
	}
	.hint {
		opacity: 0.8;
		max-width: 32rem;
	}
	.banner {
		padding: 0.5rem;
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
	}
	.actions {
		display: flex;
		gap: 0.5rem;
		margin-top: 0.5rem;
	}
	.confirm {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
		margin-top: 0.5rem;
		padding: 0.5rem;
		border: 1px solid var(--hairline);
		border-radius: var(--radius);
	}
	button.link {
		border: none;
		background: none;
		padding: 0;
		color: inherit;
		text-decoration: underline;
		cursor: pointer;
		font-size: var(--size-small);
		opacity: 0.7;
		display: block;
		margin-top: 0.25rem;
	}
	button.link:disabled {
		cursor: default;
		opacity: 0.35;
		text-decoration: none;
	}
</style>
