# Configuration

Zamo reads two files. `config.toml` is declarative and the application never
writes to it, so it is safe to manage with home-manager, Ansible, or a dotfiles
repo — it may be a read-only symlink into the nix store. `settings.toml` is the
mutable layer the GUI writes when you change something on the Settings screen;
only the fields you actually changed land there.

## Where the files live

|                              | Linux                                                       | macOS                                   | Windows             |
|------------------------------|-------------------------------------------------------------|-----------------------------------------|---------------------|
| `config.toml`                | `$XDG_CONFIG_HOME/zamo/` (default `~/.config/zamo/`)    | `~/Library/Application Support/zamo/` | `%AppData%\zamo\` |
| `settings.toml`, `zamo.db` | `$XDG_DATA_HOME/zamo/` (default `~/.local/share/zamo/`) | `~/Library/Application Support/zamo/` | `%AppData%\zamo\` |

`settings.toml` is written with mode `0600` because it may hold an API key.
Set `ZAMO_CONFIG` to point at a different config file, and `ZAMO_DB` at a
different database.

## Precedence

Every setting resolves the same way, most important first:

```
environment  >  settings.toml  >  config.toml  >  built-in default
```

Setting `declarative = true` in `config.toml` turns the mutable layer off
entirely: `settings.toml` is neither read nor written, and the Settings screen
becomes read-only. The flag only works from `config.toml` — otherwise the GUI
could unlock itself.

## Turning AI off

`ai_enabled` is the application-wide AI policy:

```toml
ai_enabled = false
```

With AI off, Zamo is a local knowledge base you maintain by hand. No AI
control is rendered anywhere in the window, nothing is sent to a provider, and
the MCP server keeps serving its read tools (`search`, `get_subtree`,
`topic_map`, `get_backlinks`) while refusing `write_blocks` — including a
server that was already running when you flipped the switch, because it reads
the policy on every write rather than at startup. Everything else —
browsing, search, editing, links, history, review, export — works unchanged.
Blocks written earlier stay where they are, and changes still awaiting your
review stay reviewable: switching AI off neither deletes nor accepts them.

Turning it off keeps your provider, model, base URL and key exactly as they
are, so turning it back on needs no reconfiguration. The switch is on the
Settings screen and stays visible while it is off. `ZAMO_AI_ENABLED=false`
overrides both files, for a host that has to guarantee no model is reached.

**This is not the same as leaving `llm.provider` empty.** No provider means the
configuration is unfinished: AI is the intended mode, but nothing is set up
yet, so the Settings screen asks you to choose one. `ai_enabled = false` is a
decision: the provider stays configured and the answer to any AI request is the
policy, not a failure you are meant to fix.

## The LLM provider

There is no default provider — an empty `llm.provider` means none is
configured.

```toml
[llm]
provider = "anthropic"          # or "openai-compatible"
model = "claude-sonnet-5"       # empty selects the provider's default
```

`openai-compatible` covers OpenAI, OpenRouter, DeepSeek and local runtimes such
as Ollama and LM Studio, and needs a `base_url`:

```toml
[llm]
provider = "openai-compatible"
model = "qwen2.5:3b"
base_url = "http://localhost:11434/v1"
```

A local runtime needs no key at all; no `Authorization` header is sent when
none is configured.

## Secrets

Never commit a key. The key resolves as `ANTHROPIC_API_KEY` /
`OPENAI_API_KEY` (whichever matches the provider) → the key saved in the GUI →
`api_key_file` → the inline `api_key`. It is read at every request rather than
cached, so a file rewritten by sops-nix or a similar tool takes effect without
a restart.

```toml
[llm]
provider = "anthropic"
api_key_file = "/run/secrets/zamo-anthropic"
```

When the environment supplies the key, the Settings screen shows it as
env-provided and will not let the GUI override it.

## Environment variables

| Variable              | Effect                                           |
|-----------------------|--------------------------------------------------|
| `ZAMO_CONFIG`       | path to `config.toml`                            |
| `ZAMO_DB`           | path to the SQLite database                      |
| `ZAMO_AI_ENABLED`   | `true` / `false`; overrides both files           |
| `ZAMO_LLM_PROVIDER` | LLM provider; overrides both files               |
| `ZAMO_LLM_MODEL`    | model name; overrides both files                 |
| `ZAMO_LLM_BASE_URL` | OpenAI-compatible endpoint; overrides both files |
| `ANTHROPIC_API_KEY`   | key for the `anthropic` provider                 |
| `OPENAI_API_KEY`      | key for the `openai-compatible` provider         |

An unparsable `ZAMO_AI_ENABLED` is an error rather than a silent "off": a
typo must not disable AI without saying so.
