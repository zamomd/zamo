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

	// Per-field dirtiness: an untouched field must submit "" (not overridden)
	// unless it is already a GUI override, so that opening the screen and
	// saving a change to one field can never capture another field's
	// config.toml-declared value into settings.toml (see input()).
	let providerDirty = $state(false);
	let modelDirty = $state(false);
	let baseUrlDirty = $state(false);

	// resetConfirming gates the destructive Reset action behind an inline,
	// in-component confirmation row. window.confirm() is not usable here: the
	// Wails v3 native webview has no runJavaScriptConfirmPanelWithMessage:
	// delegate, so confirm() always resolves as if the user cancelled, and the
	// packaged app's Reset button would otherwise do nothing at all.
	let resetConfirming = $state(false);

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
		providerDirty = false;
		modelDirty = false;
		baseUrlDirty = false;
		resetConfirming = false;
	}

	// fieldNote explains a field's origin. "default" gets no annotation:
	// nothing is overriding the built-in default, so there is nothing to say.
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
				// Only worth saying when the field itself is unusable (declarative
				// mode); otherwise the empty, enabled input already says it.
				return ro ? "set api_key_file in config.toml, or the provider's API key environment variable" : "";
		}
	});

	// providerFallbackNote flags the one gap the absent-means-reset design
	// cannot close: when config.toml itself declares a provider, submitting an
	// empty selection does not turn AI off - it falls back to that declared
	// value, because an empty field means "not overridden" everywhere else
	// too. Accepted limitation (see internal/settings.Manager.overrides); not
	// something this screen can fix without changing the wire contract, which
	// is out of scope here.
	let providerFallbackNote = $derived.by((): string => {
		if (!view) return "";
		if (provider === "" && view.provider.origin === "config" && view.provider.value !== "") {
			return (
				`config.toml declares "${view.provider.value}" here - saving with no provider selected ` +
				"falls back to that value instead of turning AI off. Edit config.toml directly to disable AI."
			);
		}
		return "";
	});

	// input() builds the submission for Save/Check. Per field, an empty string
	// means "not overridden" on the wire (see In's doc comment), so an
	// untouched field must submit "" unless it is already a GUI override -
	// otherwise the field's current effective value (which may only be true
	// because config.toml declares it) gets captured into settings.toml as if
	// the user had typed it themselves.
	//
	// When no provider is selected, model, base URL and the API key are
	// hidden and the panel says AI features are off; the submission must
	// match that claim; the field values are ignored entirely so nothing
	// typed before switching the provider to "None" can be persisted.
	function input(): In {
		const submittedProvider = providerDirty || view?.provider.origin === "gui" ? provider : "";
		if (aiOff) {
			return { provider: submittedProvider, model: "", baseUrl: "", apiKey: "", clearApiKey: false };
		}
		const submittedModel = modelDirty || view?.model.origin === "gui" ? model : "";
		const submittedBaseUrl = baseUrlDirty || view?.baseUrl.origin === "gui" ? baseUrl : "";
		return { provider: submittedProvider, model: submittedModel, baseUrl: submittedBaseUrl, apiKey, clearApiKey: false };
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
			const { provider: p, model: m, baseUrl: b } = input();
			await App.SaveSettings({ provider: p, model: m, baseUrl: b, apiKey: "", clearApiKey: true });
			await load();
			// Honest wording: config.toml may still declare an inline api_key or
			// an api_key_file, in which case a key remains set and in effect
			// after this call - only the GUI's own override is gone.
			status = "GUI key override removed.";
		} catch (e) {
			error = String(e);
		} finally {
			busy = false;
		}
	}

	// requestReset opens the inline confirmation row instead of resetting
	// directly - see resetConfirming's doc comment for why confirm() cannot
	// be used here.
	function requestReset(): void {
		resetConfirming = true;
	}

	function cancelReset(): void {
		resetConfirming = false;
	}

	// confirmReset is what the confirmation row's "Confirm reset" button
	// runs. Dropping every override is what "reset to config" means: an
	// absent field falls through to config.toml. That includes the API key,
	// which is why this is gated behind an explicit confirmation - the button
	// alone would otherwise silently destroy a key typed into this screen.
	async function confirmReset(): Promise<void> {
		resetConfirming = false;
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
			<select bind:value={provider} disabled={ro} onchange={() => (providerDirty = true)}>
				<option value="">None — AI features off</option>
				<option value="anthropic">Anthropic</option>
				<option value="openai-compatible">OpenAI-compatible</option>
			</select>
			{#if providerNote !== ""}<small>{providerNote}</small>{/if}
			{#if providerFallbackNote !== ""}<small class="warning">{providerFallbackNote}</small>{/if}
		</label>

		{#if aiOff}
			<p class="hint">
				AI features are off. Choose a provider above to configure a model, an API key, and (for
				OpenAI-compatible) a base URL. There is nothing to check while no provider is selected.
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
			<button type="button" onclick={check} disabled={busy || aiOff}>Check</button>
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
		border: 1px solid currentColor;
		border-radius: 4px;
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
		border: 1px solid currentColor;
		border-radius: 4px;
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
		display: block;
		margin-top: 0.25rem;
	}
	button.link:disabled {
		cursor: default;
		opacity: 0.35;
		text-decoration: none;
	}
</style>
