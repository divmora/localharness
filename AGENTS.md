# AGENTS.md

Context for AI coding agents. See [README.md](README.md) for full docs.

## Prerequisites

- **Go 1.25** or higher (required for build and SDK usage)

## Rules

- **Keep docs in sync.** When you modify code, update the corresponding docs in `docs/`, `README.md`, and `examples/`. Outdated documentation is a bug.
- **Update examples.** If you change a public API, tool schema, or CLI flag, update the relevant files in `examples/`.
- **Plan a version bump for every change.** Before finishing any PR or change set, determine the version impact (patch / minor / major) following the Versioning section below. Use [Conventional Commits](https://www.conventionalcommits.org/) so release-please can auto-bump the version.


## Quick Reference

- **Language**: Go 1.25, `log/slog`, `fmt.Errorf("ctx: %w", err)`
- **Proto**: Single file `proto/localharness/v1/localharness.proto` → `make proto`
- **Never edit**: `gen/go/` (generated)
- **Build**: `make build` → `bin/localharness`
- **Build examples**: `go build -o bin/<name> ./examples/<name>/` — always output to `bin/` (gitignored) to avoid accidental commits
- **Test**: `make test` or `go run ./cmd/testclient --api-key=$KEY --prompt "..."`
- **Version**: `internal/config/config.go` → `HarnessVersion` (set to `0.0.0-dev` in source; release-please injects real version via `-ldflags` at build time)
- **Debug CLI**: `make build-lhctl` → `bin/lhctl conversation inspect <id>` (see [docs/lhctl.md](docs/lhctl.md))

## Versioning

We follow [Semantic Versioning 2.0](https://semver.org/). Format: **`MAJOR.MINOR.PATCH`** (e.g. `0.3.1`).

While the project is in `0.x.y` (pre-1.0), the rules are relaxed:
- Minor bumps may include breaking changes.
- After `1.0.0`, breaking changes **require** a major bump.

### When to bump

| Bump | When | Examples |
|:---|:---|:---|
| **PATCH** `0.1.x` | Bug fixes, typo corrections, internal refactors with no behavior change | Fix crash in `edit_file`, fix off-by-one in line counting, update docs |
| **MINOR** `0.x.0` | New features, new tools, new CLI flags, new proto fields (backward-compatible) | Add `run_command` tool, add `--timeout` flag, add `ThinkingConfig` to proto |
| **MAJOR** `x.0.0` | Breaking changes to wire protocol, public API, CLI interface, or SDK contract | Remove/rename proto fields, change WebSocket handshake, drop a tool |

### How to decide (flowchart)

1. **Does this change break existing ADK clients or the CLI interface?** → **MAJOR**
2. **Does this add new user-visible functionality?** (new tool, new flag, new proto message, new behavior) → **MINOR**
3. **Everything else** (bug fix, perf improvement, refactor, docs-only) → **PATCH**

### Single source of truth

The version in source code is always `0.0.0-dev`:

```
internal/config/config.go → var HarnessVersion = "0.0.0-dev"
```

**Do NOT manually change `HarnessVersion` in code.** The release-please GitHub Action:
1. Reads conventional commit messages (`feat:`, `fix:`, `feat!:`)
2. Auto-creates a release PR with the correct version bump
3. Injects the real version at build time via `-ldflags`

All runtime references **must** read from `config.HarnessVersion` — never hardcode version strings elsewhere.

### Commit conventions (used by release-please)

- `fix:` → PATCH bump
- `feat:` → MINOR bump
- `feat!:` or `BREAKING CHANGE:` → MAJOR bump
- `chore:`, `docs:`, `refactor:` → no version bump

## Adding a Tool

1. Create `internal/tools/<name>.go` with `Execute(ctx, *pb.StepUpdate) error`
2. Register in `tools/registry.go` → `RegisterBuiltinTools()`
3. Add JSON Schema in `Schemas()` method
4. Validate paths via `workspace.Manager.ValidatePath()`
5. Use `feat:` commit prefix (triggers MINOR bump via release-please)

> **Note**: Some tools are **engine-intercepted** — they register a schema for LLM discovery but are handled directly by the engine instead of the tool registry's `Execute()`. Examples: `ask_question` (blocks for user response), `invoke_subagent` (spawns child agents). See `executeAskQuestion()` and `executeSubagent()` in `engine.go`.

## Adding an LLM Provider

1. Create `internal/llm/<name>.go` implementing `Provider` interface
2. Add config message to proto `HarnessConfig.model_config` oneof
3. Wire in `session.go` `createProvider()`
4. Use `feat:` commit prefix (triggers MINOR bump via release-please)

## Key Paths

| Path | Purpose |
|:---|:---|
| `~/.divmora/localharness/conversations/*.pb` | Conversation state |
| `~/.divmora/localharness/brain/<id>/` | Artifacts, logs, traces |
| `~/.divmora/localharness/bin/v<VER>/localharness` | Cached binary (shared across all SDKs) |
| `~/.divmora/localharness/projects.json` | Project registry (workspace → UUID mapping) |
| `~/.divmora/localharness/knowledge/<project-uuid>/` | Knowledge Items (per-project, persistent) |
| `~/.divmora/localharness/plugins/` | Global plugins (auto-discovered at session init) |
| `~/.divmora/localharness/skills/` | Global standalone skills (auto-discovered at session init) |
| `<workspace>/.agents/plugins/` | Workspace-level plugins (auto-discovered) |
| `<workspace>/.agents/skills/` | Workspace-level skills (auto-discovered) |
| `~/.divmora/config/settings.json` | Global settings (telemetry, trusted workspaces) |
| `~/.divmora/config/mcp_config.json` | Global MCP server config (shared across all agents) |

## Binary Distribution

All SDKs (Go, Python, JS) resolve the `localharness` binary using the same 5-step chain:

1. Explicit path (config `BinaryPath`)
2. `$LOCALHARNESS_BIN` environment variable
3. `localharness` in system PATH
4. Cached version at `~/.divmora/localharness/bin/v<VERSION>/localharness`
5. Auto-download from GitHub releases → cache

**Key file**: `sdk/connection/binary_resolver.go` — the Go ADK reference implementation.

See [docs/binary-distribution.md](docs/binary-distribution.md) for full documentation.
