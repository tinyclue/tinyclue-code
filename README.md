<div align="center">

# Tinyclue

**A terminal-native AI coding agent written in Go.**

[![Go version](https://img.shields.io/badge/Go-1.26-blue)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/tinyclue/tinyclue-code)](https://github.com/tinyclue/tinyclue-code/releases)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

## What is Tinyclue?

**Tinyclue** is an open-source, terminal-native AI coding agent. It lives entirely in your
terminal — you describe a task in plain language, and an AI agent plans, reads your code,
runs shell commands, edits files, and iterates until the job is done.

It is written in **Go** (a single static binary) and is **model-agnostic**: it ships with a
self-maintained catalog of **35 providers / 1081 models** (DeepSeek, OpenAI, Anthropic,
Google, OpenRouter, Groq, Mistral, and many more), so you can use whichever model fits your
budget — including a **free** out-of-the-box default.

## Key Features

- **🖥️ Terminal-native TUI** — markdown rendering, multi-line editor with Emacs-style
  key bindings, `/`-command autocomplete, permission dialogs, and a status line showing
  live token / cost / context usage.
- **🌐 Model-agnostic** — 35 providers / 1081 models aggregated into a local model catalog
  (`~/.tinyclue/caches/model-cache.json`), refreshed automatically when missing.
- **📡 Dual protocol adapters** — speaks Anthropic **Messages** and OpenAI
  **Completions / Responses / Codex** APIs.
- **🔌 MCP client** — Model Context Protocol over **stdio** and **HTTP/SSE**, with
  interactive OAuth for HTTP servers, token persistence, automatic reconnection, and
  per-server connect / authenticate / enable / disable from the UI.
- **🛠️ Rich tool set** — `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`, `WebSearch`,
  `WebFetch`, `AskUserQuestion`, `RunAgent`, `SendMessage`, `TaskCreate/Get/List/Update`,
  `EnterPlanMode` / `ExitPlanMode`, `ToolSearch`, `Skill`, and MCP bridge tools.
- **🧠 Sub-agents** — `General`, `Explore`, `Plan`, `GeneralPurpose`, and `Verification`
  agent types, spawnable via `RunAgent`.
- **📝 Plan mode** — dedicated planning stage with a persistent plan file and
  enter/exit plan-mode tools.
- **✅ Task management** — file-backed, cross-process safe task lists per session.
- **💾 Session persistence** — every conversation is recorded to JSONL; resume with
  `tinyclue -c [sessionId]`.
- **🧠 Context compaction** — automatic compaction keeps long sessions inside the model
  context window.
- **📄 Project instructions** — `TINYCLUE.md` discovery from managed / user / project /
  local / auto-memory / team levels, with `@include`, YAML frontmatter, and rules dirs.
- **🧠 Auto memory** — a persistent `MEMORY.md` memory system shared across sessions.
- **🔐 Fine-grained permissions** — allow-list rules per project
  (`.tinyclue/config/settings.local.json`) with glob patterns; one-shot or remembered
  approvals.
- **🔑 Multiple auth flows** — API keys and OAuth subscription login (e.g. Claude
  Pro/Max) via a localhost callback + PKCE.
- **⚡ Free default** — the installer seeds a zero-cost model
  (`deepseek-v4-flash-free` through the opencode zen gateway) that works immediately.

## Quick Start

### Requirements

- **Go 1.26+** (only needed when building from source or via `install.sh`)
- Primarily targets macOS / Linux (the install scripts are Unix-based).

### Install

Run the installer from a clone of this repository:

```bash
./install.sh          # interactive — asks before overwriting existing config files
./install.sh -y       # non-interactive — overwrite config files without asking
```

`install.sh` will:

1. Build the binary and install it to `$PREFIX/bin/tinyclue` (default `/usr/local/bin`,
   override with `PREFIX=/custom/path`).
2. Initialize the configuration directory (`~/.tinyclue/config/`), seeding
   `auth.json` (a shared anonymous `opencode` key `"public"`), `settings.json`
   (default provider `opencode`, default model `deepseek-v4-flash-free`), and
   `slog.json` (file logging config). Existing files are backed up as `.bak` before
   being overwritten.
3. Seed the model catalog into `~/.tinyclue/caches/model-cache.json` (only if absent).

Alternatively, build and install manually:

```bash
make            # or: go build -o bin/tinyclue ./cmd
make install    # runs ./install.sh
```

To uninstall:

```bash
./uninstall.sh            # interactive — keeps data unless confirmed
./uninstall.sh -y         # remove everything (config is backed up first)
./uninstall.sh --keep-data # remove binary + cache, keep config/sessions/tasks
```

### Usage

```bash
tinyclue              # start in the current directory
tinyclue -c           # resume the most recent session for this project
tinyclue -c <id>      # resume a specific session by id
```

The very first launch is ready to use — the default free model requires no API key.
To switch providers/models or add credentials, use the in-session commands:

| Command   | Description                                                        |
| --------- | ------------------------------------------------------------------ |
| `/login`  | Log in: enter an API key, or start OAuth subscription sign-in      |
| `/model`  | Pick a provider, model, and reasoning effort                       |
| `/mcp`    | Inspect MCP servers: connect, authenticate, enable / disable       |
| `/exit`   | Quit                                                                |

Editor shortcuts follow Emacs conventions: `Ctrl+P`/`Ctrl+N` move up/down history,
`Ctrl+B`/`Ctrl+F` move left/right, `Ctrl+W` deletes the word before the cursor,
`Alt+B`/`Alt+F` jump word-wise, etc. Type `/` at the start of a line for command
autocomplete. Run an interactive shell command directly by prefixing it with `!`.

## Configuration

All user configuration lives under `~/.tinyclue/` (override the base directory with the
`TINYCLUE_CONFIG_DIR` environment variable).

### `config/settings.json`

```json
{
  "defaultProvider": "opencode",
  "defaultModel": "deepseek-v4-flash-free",
  "reasoning_effort": "high",
  "language": "",
  "autoMemory": false
}
```

- `defaultProvider` / `defaultModel` — the active provider and model.
- `reasoning_effort` — reasoning level: `low` / `medium` / `high` / `max` (default `high`).
- `language` — preferred response language.
- `autoMemory` — enable the cross-session auto-memory system.

### `config/auth.json`

```json
{
  "opencode": { "type": "api-key", "key": "public" },
  "anthropic": { "type": "oauth" }
}
```

Maps each provider to `api-key` or `oauth` credentials.

### MCP servers

MCP servers are loaded from the user-level `~/.tinyclue/mcp.json`, merged with any
project-level `.mcp.json` found while walking up from the working directory (deeper
directories win). Each entry supports `stdio` or `http`:

```json
{
  "mcpServers": {
    "my-tools": { "command": "my-mcp-server", "args": ["--flag"] },
    "remote":   { "type": "http", "url": "https://example.com/mcp" }
  }
}
```

### Permissions

Per-project allow rules are stored in `<project>/.tinyclue/config/settings.local.json`:

```json
{
  "permissions": {
    "allow": ["Bash(git status *)", "Read", "Edit"]
  }
}
```

Rules use a `Tool(pattern)` format with `*` wildcards; a bare tool name allows it with
any arguments. Rules can also be added from the UI's permission dialog ("Allow, and
don't ask again…").

### Environment variables

| Variable                               | Purpose                                                    |
| -------------------------------------- | ---------------------------------------------------------- |
| `TINYCLUE_CONFIG_DIR`                  | Override the config root (default `~/.tinyclue`)           |
| `TINYCLUE_CACHE_DIR`                   | Override the cache directory (default `<config>/caches`)   |
| `TINYCLUE_OAUTH_CLIENT_ID_<PROVIDER>`  | Provide an OAuth client id for a subscription provider     |

## Model Support

Tinyclue aggregates model metadata from [models.dev](https://models.dev), OpenRouter,
NVIDIA, and an AI-gateway feed into a single local catalog. The seed catalog that ships
in this repository covers **35 providers and 1081 models**, including:

DeepSeek · OpenAI · OpenAI Codex · Anthropic · Google · Google Vertex · Amazon Bedrock ·
Azure OpenAI · OpenRouter · OpenCode · OpenCode Go · Groq · Cerebras · Mistral · xAI ·
Hugging Face · Fireworks · Together · NVIDIA · Cloudflare Workers AI / AI Gateway ·
GitHub Copilot · MiniMax · Kimi · Moonshot AI · ZAI · Xiaomi · and more.

Every model entry carries its API protocol, base URL, context window, max tokens,
reasoning capability, and per-token pricing — which powers live cost estimates in the
status line.

## Architecture

```
tinyclue
├── cmd/                 # entrypoint (main.go)
├── coding_agent/        # agent core
│   ├── core/            #   agent loop, tools, prompts, sessions, plan mode,
│   │                    #   tasks, TINYCLUE.md user context, auto memory
│   └── interactive.go   #   TUI session bootstrap
├── api_provider/        # protocol adapters (Anthropic Messages / OpenAI)
├── models_cache/        # model catalog aggregation pipeline (35+ providers)
├── mcp/                 # MCP client manager (stdio + http), lifecycle & auto-reconnect
├── tui/                 # terminal UI: components, renderer, key handling
├── config/              # settings.json / auth.json / mcp.json / permissions
├── oauth/               # OAuth callback server + token store (PKCE)
├── subscription/        # subscription (OAuth) login registry
└── log/                 # leveled file logging with rotation
```

Data flow: your input → the TUI editor → the agent loop → the model API (via an
adapter) → tool calls (`Bash`, `Read`, `Edit`, MCP tools, …) → results stream back into
the conversation and render in the terminal.

## Development

```bash
# Build
make            # compile ./cmd into bin/tinyclue
make clean      # remove build artifacts

# Test
go test ./...   # unit + integration tests (coding_agent/core, mcp, tui, config, ...)
```

The repository also contains standalone test programs under `tests/`
(`tests/mcp_test_server`, `tests/ripgreptest`, `tests/globcoverage`, …).

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for how to
contribute — from reporting bugs and requesting features to submitting pull requests.

## License

[MIT](LICENSE) © Tinyclue
