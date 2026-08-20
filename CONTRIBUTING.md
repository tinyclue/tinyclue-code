# Contributing to Tinyclue

[English](CONTRIBUTING.md) · [简体中文](CONTRIBUTING.zh-CN.md)

Thanks for your interest in contributing to **Tinyclue**! This guide covers how to
report issues, request features, and submit code that fits the project's conventions.

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Repository Layout](#repository-layout)
- [Development Workflow](#development-workflow)
- [Making Changes](#making-changes)
- [Pull Request Checklist](#pull-request-checklist)
- [Reporting Bugs](#reporting-bugs)
- [Requesting Features](#requesting-features)
- [Testing Guidelines](#testing-guidelines)
- [Documentation](#documentation)
- [License](#license)

## Code of Conduct

Be respectful and constructive. Tinyclue is a community project — disagreements
should focus on the code, not the person. Harassment or abusive behavior is not
welcome.

## Getting Started

### Prerequisites

- **Go 1.26+** — required to build from source.
- **macOS / Linux** — the install scripts are Unix-based; a Windows build is not
  currently supported.

### Setup

```bash
git clone <your-fork-url> tinyclue-code
cd tinyclue-code
make            # build the binary into bin/tinyclue
go test ./...   # run the full test suite
```

## Repository Layout

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

When you add or change a feature, keep related code within the owning package and
update the architecture diagram in `README.md` / `README.zh-CN.md` if the layout
changes.

## Development Workflow

### Building

```bash
make            # go build -o bin/tinyclue cmd/main.go
make clean      # remove build artifacts (bin/)
```

### Running tests

```bash
go test ./...   # unit + integration tests across all packages
```

`tests/` also contains standalone test programs (e.g. `tests/mcp_test_server`,
`tests/ripgreptest`, `tests/globcoverage`). They are separate `main` packages, not
part of the main module's build — run their own tests or binaries individually.

### Code style

- Run `gofmt` (or `go fmt ./...`) on all changed files — CI and reviewers expect
  formatted code.
- Run `go vet ./...` before pushing; keep the tree vet-clean.
- Follow the surrounding package's conventions for naming, comments, and error
  handling. Comments in this codebase are written in both English and Simplified
  Chinese — match the style of the code you are touching.

### Dependencies

The project uses standard Go modules (`go.mod`/`go.sum` are the source of truth) and
pins Go in `go.mod`. The `vendor/` directory is **not committed** — it is generated
locally for reproducible builds and stays ignored via `.gitignore`. When adding or
updating a dependency:

```bash
go get <module>@<version>
go mod tidy
```

Make sure `go.mod`/`go.sum` stay consistent and committed. If you regenerate
`vendor/` locally, keep it out of the repository (`.gitignore` already excludes it).

## Making Changes

1. **Find or file an issue first.** For anything non-trivial, describe your intent
   before writing code — this avoids duplicated or unwanted work.
2. **Create a feature branch** off the default branch.
3. **Implement your change.** Keep it focused; resist scope creep into unrelated
   cleanup.
4. **Add or update tests.** Behavior changes need coverage (see
   [Testing Guidelines](#testing-guidelines)).
5. **Verify locally:**
   ```bash
   gofmt -l <changed-files>   # should be empty
   go vet ./...
   go build ./...
   go test ./...
   ```
6. **Commit** with a concise message that explains *why* (see
   [Commit Guidelines](#commit-guidelines)).
7. **Open a pull request** and reference the issue it closes.

### Commit Guidelines

- Write a short, imperative subject line: "Fix token count under-count in long
  sessions", not "fixed things".
- The body should explain *why* the change is needed, not just *what* it does.
- Reference issue numbers when applicable: "Closes #123".

## Pull Request Checklist

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `go vet ./...` is clean
- [ ] Changed files are `gofmt`-formatted
- [ ] Behavior changes include new/updated tests
- [ ] README / docs updated if user-facing behavior changed

## Reporting Bugs

Open an issue and include:

- What you did (steps to reproduce).
- What you expected, and what actually happened.
- Environment: OS version, Go version, Tinyclue version (or commit).
- Anything relevant from `~/.tinyclue/log/` or the session files under
  `~/.tinyclue/sessions/` if the failure relates to a specific run.
- If the crash or render looks UI-related, a terminal capture (or `script` output)
  helps a lot.

## Requesting Features

Describe the problem you are trying to solve and a concrete use case. Proposals
that explain *why* a feature matters and roughly *how* it might fit the architecture
are much easier to evaluate. A short sketch of the intended UX (what the user types,
what they see) is ideal.

## Testing Guidelines

- **Behavior change → test change.** If a fix or feature changes observable
  behavior, add or update a test in the same PR.
- Unit tests live next to the code (`*_test.go` in the same package).
- Component-level UI tests (e.g. TUI panels) render a component with synthetic
  input and assert on the output lines — follow the pattern in
  `tui/component/mcppanel/mcppanel_test.go`.
- Integration-style fixtures live under `tests/` as standalone programs; keep them
  runnable independently (`go test` or direct invocation) so `go build ./...` on the
  main module stays unaffected.
- If a change touches the MCP client, see `tests/mcp_test_server` for a
  stdio-based server you can drive in tests.

## Documentation

- `README.md` (English) and `README.zh-CN.md` (Simplified Chinese) must stay in
  sync — update both when editing either.
- Update the docs when you change CLI flags, slash commands, config file schemas,
  environment variables, or install behavior.

## License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
