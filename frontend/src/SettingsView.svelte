<script lang="ts">
	import { onMount } from "svelte";
	import type { In, View } from "../bindings/github.com/cominddev/comind/internal/settings/models.js";
	import { App } from "../bindings/github.com/cominddev/comind/internal/wailsbind";

	// The screen edits the mutable layer only (settings.toml). A field whose
	// origin is "config" is declared in config.toml and shown as such; "gui"
	// means this layer overrides it and can be reset to fall back to
	// config.toml. "env" cannot be overridden here at all. In declarative mode
	// every field is read-only, but Check still works: it writes nothing.
	let view = $state<View | null>(null);
	let provider = $state("");
	let model = $state("");
	let baseUrl = $state("");
	let apiKey = $state("");
	let status = $state("");
	let error = $state("");
	let busy = $state(false);

	let ro = $derived(view?.declarative ?? false);
	// An empty provider is a legal, supported state - AI features off, not a
	// missing default (see config.LLM.Provider). Model, base URL and the API
	// key are meaningless without a provider, so the form hides them instead
	// of showing disabled fields nobody can act on.
	let aiOff = $derived(provider === "");

	async function load(): Promise<void> {
		const v = await App.Settings();
		view = v;
		provider = v.provider.value;
		model = v.model.value;
		baseUrl = v.baseUrl.value;
		apiKey = "";
	}

	// note explains a field's origin. "default" gets no annotation: nothing is
	// overriding the built-in default, so there is nothing to say.
	function note(field: { origin: string }): string {
		if (field.origin === "config") return "set in config.toml";
		if (field.origin === "gui") return "overridden here";
		return "";
	}

	function apiKeyNote(): string {
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
				// Only worth saying when the field itself is unusable (declarative
				// mode); otherwise the empty, enabled input already says it.
				return ro ? "set api_key_file in config.toml, or the provider's API key environment variable" : "";
		}
	}

	// providerFallbackNote flags the one gap the absent-means-reset design
	// cannot close: when config.toml itself declares a provider, submitting an
	// empty selection does not turn AI off - it falls back to that declared
	// value, because an empty field means "not overridden" everywhere else
	// too. Accepted limitation (see internal/settings.Manager.overrides); not
	// something this screen can fix without changing the wire contract, which
	// is out of scope here.
	function providerFallbackNote(): string {
		if (!view) return "";
		if (provider === "" && view.provider.origin === "config" && view.provider.value !== "") {
			return (
				`config.toml declares "${view.provider.value}" here - saving with no provider selected ` +
				"falls back to that value instead of turning AI off. Edit config.toml directly to disable AI."
			);
		}
		return "";
	}

	function input(): In {
		return { provider, model, baseUrl, apiKey, clearApiKey: false };
	}

	async function save(): Promise<void> {
		busy = true;
		status = "";
		error = "";
		try {
			await App.SaveSettings(input());
			await load();
			status = "Saved. The change is already in effect.";
		} catch (e) {
			error = String(e);
		} finally {
			busy = false;
		}
	}

	async function check(): Promise<void> {
		busy = true;
		status = "";
		error = "";
		try {
			await App.CheckSettings(input());
			status = "The provider answered.";
		} catch (e) {
			error = String(e);
		} finally {
			busy = false;
		}
	}

	// clearKey drops only the stored API key, leaving every other override as
	// it stands. The plain Save button cannot express that: input() always
	// carries clearApiKey: false, so an untouched key field never gets wiped
	// by accident. This sends the same field values Save would, plus
	// clearApiKey - the one combination the existing contract already
	// supports for "clear just the key".
	async function clearKey(): Promise<void> {
		busy = true;
		status = "";
		error = "";
		try {
			await App.SaveSettings({ provider, model, baseUrl, apiKey: "", clearApiKey: true });
			await load();
			status = "API key cleared.";
		} catch (e) {
			error = String(e);
		} finally {
			busy = false;
		}
	}

	async function reset(): Promise<void> {
		// Dropping every override is what "reset to config" means: an absent
		// field falls through to config.toml. That includes the API key, so
		// this confirms first - the button alone would otherwise silently
		// destroy a key the user typed into this screen.
		const sure = confirm(
			"Reset every field to config.toml? This also permanently discards any API key entered in this screen.",
		);
		if (!sure) return;
		busy = true;
		status = "";
		error = "";
		try {
			await App.SaveSettings({ provider: "", model: "", baseUrl: "", apiKey: "", clearApiKey: true });
			await load();
			status = "Reset to config.toml.";
		} catch (e) {
			error = String(e);
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		load().catch((e: unknown) => {
			error = String(e);
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

		<label>
			Provider
			<select bind:value={provider} disabled={ro}>
				<option value="">None — AI features off</option>
				<option value="anthropic">Anthropic</option>
				<option value="openai-compatible">OpenAI-compatible</option>
			</select>
			{#if note(view.provider) !== ""}<small>{note(view.provider)}</small>{/if}
			{#if providerFallbackNote() !== ""}<small class="warning">{providerFallbackNote()}</small>{/if}
		</label>

		{#if aiOff}
			<p class="hint">
				AI features are off. Choose a provider above to configure a model, an API key, and (for
				OpenAI-compatible) a base URL. There is nothing to check while no provider is selected.
			</p>
		{:else}
			<label>
				Model
				<input bind:value={model} disabled={ro} placeholder="provider default" />
				{#if note(view.model) !== ""}<small>{note(view.model)}</small>{/if}
			</label>

			{#if provider === "openai-compatible"}
				<label>
					Base URL
					<input bind:value={baseUrl} disabled={ro} placeholder="http://localhost:11434/v1" />
					{#if note(view.baseUrl) !== ""}<small>{note(view.baseUrl)}</small>{/if}
				</label>
			{/if}

			<label>
				API key
				<input
					type="password"
					bind:value={apiKey}
					disabled={ro || view.apiKeyOrigin === "env"}
					placeholder={view.apiKeySet ? "set — type to replace" : "not set"}
				/>
				{#if apiKeyNote() !== ""}<small>{apiKeyNote()}</small>{/if}
				<button
					type="button"
					class="link"
					onclick={clearKey}
					disabled={ro || busy || view.apiKeyOrigin !== "gui"}
				>
					Clear key
				</button>
			</label>
		{/if}

		<div class="actions">
			<button type="button" onclick={save} disabled={ro || busy}>Save</button>
			<button type="button" onclick={check} disabled={busy || aiOff}>Check</button>
			<button type="button" onclick={reset} disabled={ro || busy}>Reset to config (clears key too)</button>
		</div>

		{#if status !== ""}<p><code>{status}</code></p>{/if}
		{#if error !== ""}<p><code>error: {error}</code></p>{/if}
	</section>
{/if}

<style>
	label {
		display: block;
		margin-bottom: 0.75rem;
	}
	small {
		display: block;
		opacity: 0.7;
	}
	small.warning {
		opacity: 1;
		color: #b45309;
	}
	.hint {
		opacity: 0.8;
		max-width: 32rem;
	}
	.banner {
		padding: 0.5rem;
		border: 1px solid currentColor;
		border-radius: 4px;
	}
	.actions {
		display: flex;
		gap: 0.5rem;
		margin-top: 0.5rem;
	}
	button.link {
		border: none;
		background: none;
		padding: 0;
		color: inherit;
		text-decoration: underline;
		cursor: pointer;
		font-size: 0.85rem;
		opacity: 0.7;
	}
	button.link:disabled {
		cursor: default;
		opacity: 0.35;
		text-decoration: none;
	}
</style>
