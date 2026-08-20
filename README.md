# LocalHarness

[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/divmora/localharness)

**An Agent Runtime Engine** — a Go binary that runs a complete agentic loop: receives user prompts via WebSocket, calls Gemini, dispatches built-in tools (file I/O, search, shell), and streams structured events back to ADK clients.

Uses **pipe-based handshake** (stdin/stdout) for secure startup and **WebSocket + Protobuf** for wire protocol.

## Architecture

```
lhctl (Interactive TUI) / SDK ◄── WebSocket + Protobuf ──► LocalHarness Binary / Daemon (Go)
                                                                 │
                                                                 ├── Dual Execution Modes
                                                                 │   ├── Ephemeral Subprocess (Stdin/Stdout pipe handshake)
                                                                 │   └── Persistent Background Daemon (Unix socket + TCP)
                                                                 │
                                                                 ├── Interactive TUI (lhctl)
                                                                 │   ├── Real-time token streaming & markdown rendering
                                                                 │   ├── Animated tool spinners & execution duration
                                                                 │   ├── Interactive unified diff approvals & YOLO mode
                                                                 │   ├── Subagent tree & transcript drill-down
                                                                 │   └── @file autocompletion across workspaces
                                                                 │
                                                                 ├── Agentic Engine
                                                                 │   ├── LLM ↔ Tools ↔ Results loop
                                                                 │   ├── Turn pause/resume (InterruptRequest / ResumeRequest)
                                                                 │   └── Context compaction & reduction
                                                                 │
                                                                 ├── LLM Providers
                                                                 │   ├── OpenAI-compatible (GPT-4o, Claude, DeepSeek, etc.)
                                                                 │   └── Resilient backoff retry with Retry-After header parsing
                                                                 │
                                                                 ├── Multi-Workspace & Trust
                                                                 │   ├── First-time trust prompts (~/.divmora/config/settings.json)
                                                                 │   └── Dynamic /workspace add/remove/list
                                                                 │
                                                                 └── Built-in Tools
                                                                     ├── view_file, write_to_file, replace_file_content
                                                                     ├── list_dir, grep_search, find_file
                                                                     ├── run_command, manage_task, finish
                                                                     ├── invoke_subagent, define_subagent, manage_subagents
                                                                     ├── search_web, read_url_content, schedule
                                                                     └── knowledge_write/replace/delete, browser (Playwright)
```

## Prerequisites

- **Go 1.25** or higher (required for build and SDK usage)

## Installation

The SDK auto-downloads the binary on first run (zero-install). For manual control:

```bash
# Option 1: go install (builds from source)
go install github.com/divmora/localharness/cmd/localharness@latest

# Option 2: Download prebuilt binary
curl -sSL https://github.com/divmora/localharness/releases/latest/download/localharness-linux-amd64.tar.gz | tar xz
sudo mv localharness /usr/local/bin/

# Option 3: Build from source
make build
```

### macOS "Damaged File" Error

If you download the `.dmg` installer or the binary for macOS from GitHub Releases, Gatekeeper may flag the file as "damaged" because it is not officially code-signed with an Apple Developer Program certificate. 

To bypass this error and run the app, you must remove the quarantine flag using the terminal:

```bash
# If using the Divmora.app from the .dmg
xattr -cr /Applications/Divmora.app

# If using the raw binary
xattr -cr localharness
```

See [docs/binary-distribution.md](docs/binary-distribution.md) for the full resolution chain and cross-SDK strategy.
## Quick Start

### 1. Interactive Terminal CLI (`lhctl`)

Build and run the interactive multi-agent chat interface:

```bash
make build-lhctl

# Launch a fresh interactive session (default)
./bin/lhctl

# Resume the most recent conversation (or specify full/partial ID with -c <id>)
./bin/lhctl -c

# Or run with explicit model and YOLO mode
./bin/lhctl run --model=gpt-4o --yolo

# Launch background task and detach
./bin/lhctl run --prompt="Audit project dependencies" --detach

# Attach to a running session
./bin/lhctl attach <session-id>
```

> In `lhctl`, sessions always start fresh by default. Use `-c` to resume the most recent conversation or `-c <id>` to resume by ID. On exit, `lhctl` displays the exact command to resume your session. Press **`Shift+Tab`** to cycle between **`DEFAULT`** (Safe Mode), **`ACCEPT-EDITS`** (Auto-Accept Edits), and **`PLAN`** (Plan-Before-Act) modes. Type `/` for instant command autocomplete or `@` for workspace file mentions. See [docs/lhctl.md](docs/lhctl.md) for full documentation.

### 2. Build Engine from Source

```bash
make build
# Binary created at bin/localharness
```

### 3. Run via Test Client

```bash
# Run a prompt (requires Gemini / LiteLLM API key)
export LITELLM_API_KEY=your_key
go run ./cmd/testclient --prompt "List the files in the current directory"

# Enable shell commands
go run ./cmd/testclient --enable-commands --prompt "Run 'ls -la'"

# Use thinking model
go run ./cmd/testclient --thinking=medium --prompt "Explain the architecture"

# Auto-approve all tool calls (for CI)
go run ./cmd/testclient --auto-approve --prompt "Create hello.txt with 'Hello World'"
```




## Protocol

The wire protocol uses a **pipe-based handshake** for secure startup, followed by
**WebSocket** with **Protocol Buffer** binary frames for communication.

### Connection Handshake

```
SDK                               Binary
  │── Spawn + capture pipes ──────────►│
  │── Write InputConfig (stdin) ──────►│  Binary binds :0, generates API key
  │◄── Read OutputConfig (stdout) ────┤  Returns port + API key
  │── WS connect with API key ────────►│
```

### Message Flow

```
Client                          Harness
  │                                │
  ├── ClientMessage(InitRequest) ──►│  Configure session
  │◄── ServerMessage(InitResponse) ─┤
  │                                │
  ├── ClientMessage(UserMessage) ──►│  Send prompt
  │                                │
  │◄── ServerMessage(StepUpdate)  ──┤  Tool call (ACTIVE)
  │◄── ServerMessage(StepUpdate)  ──┤  Tool result (DONE)
  │◄── ServerMessage(StepUpdate)  ──┤  Tool call (ACTIVE)
  │◄── ServerMessage(StepUpdate)  ──┤  Tool result (DONE)
  │◄── ServerMessage(StepUpdate)  ──┤  Final text response
  │◄── ServerMessage(TrajState)   ──┤  IDLE
  │                                │
```

### Proto Schema

See [`proto/localharness/v1/localharness.proto`](proto/localharness/v1/localharness.proto) for the full schema.

Key messages:
- `ClientMessage` — wraps `InitRequest`, `UserMessage`, `ToolResult`, `CancelRequest`, `PermissionResponse`, `QuestionResponse`
- `ServerMessage` — wraps `InitResponse`, `StepUpdate`, `TrajectoryState`, `ErrorEvent`
- `StepUpdate` — the core event with tool actions, text, thinking, state machine
- `HarnessConfig` — LLM provider, tools, workspaces, system instructions

## Built-in Tools

The harness provides a rich set of built-in tools organized into categories:

| Category | Tools | Description |
|:---|:---|:---|
| **File I/O** | `view_file`, `write_to_file`, `replace_file_content`, `multi_replace_file_content`, `list_dir` | Read, create, edit files and list directories |
| **Search** | `grep_search`, `find_file` | Ripgrep-powered content search and filename pattern matching |
| **Execution** | `run_command`, `manage_task`, `schedule` | Shell commands (sync/background/persistent), task management, timers and cron |
| **Web** | `search_web`, `read_url_content` | Web search and URL content fetching |
| **Agent Orchestration** | `invoke_subagent`, `define_subagent`, `manage_subagents`, `send_message` | Spawn typed child agents, define types, manage instances |
| **Knowledge** | `knowledge_write`, `knowledge_replace`, `knowledge_delete` | Persistent project-scoped knowledge items (engine-intercepted) |
| **Interactive** | `ask_question`, `finish` | Multiple-choice questions to user, task completion signal |
| **Browser** | `browser_*` | Browser automation via Playwright MCP ([docs](docs/browser.md)) |

All file tools enforce **workspace restrictions** — operations outside configured workspace directories are rejected.

> For the complete list of proto action fields, message types, and field numbers, see the [StepUpdate Actions table in docs/architecture.md](docs/architecture.md#stepupdate-actions-oneof).

## Go ADK Usage

The project includes a Go ADK client under `sdk/` that allows running the agent programmatically:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/divmora/localharness/adk"
)

func main() {
	cfg := adk.NewLocalAgentConfig()
	cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("failed to start agent: %v", err)
	}

	resp, err := agent.Chat(ctx, "List the files in the current directory")
	if err != nil {
		log.Fatalf("chat failed: %v", err)
	}
	fmt.Println(resp.Text)
}
```

See the [Examples](examples/) folder for advanced SDK usage patterns:
- **[Policy Enforcement](examples/adk-policy/)**: Declarative safety rules.
- **[Safe Defaults](examples/adk-safe-agent/)**: Read-only access with write approval confirmation handlers.
- **[Logging & Token Limits](examples/adk-logging/)**: Configuring custom `slog.Logger`, toggling `Verbose` debug logs, and setting a session-level `MaxTotalTokens` limit.

Detailed architectural specifications can be found in [docs/architecture.md](docs/architecture.md).

## Error Handling

LocalHarness uses a structured error handling system with machine-readable error codes and contextual metadata. See:

- **[Error Handling Guide](docs/error-handling.md)** - SDK developer guide for handling structured errors
- **[Proto Schema Changes](docs/proto-schema-changes.md)** - Schema migration guide for SDK maintainers
- **[Architecture - Error Handling](docs/architecture.md#error-handling)** - System-level error handling architecture

## CLI Flags

The binary is managed by the SDK via pipe handshake. These flags are for the binary:

| Flag | Default | Description |
|:---|:---|:---|
| `--workspace` | `cwd` | Default workspace directory |
| `--data-dir` | `~/.divmora/localharness/` | Data directory for conversations |
| `--debug` | `false` | Enable debug logging |
| `--version` | — | Print version and exit |

## Project Structure

```
local-harness/
├── cmd/
│   ├── localharness/main.go          # CLI entry point
│   └── testclient/main.go            # CLI test client
├── proto/localharness/v1/            # Protobuf schema
│   └── localharness.proto
├── gen/go/localharness/v1/           # Generated Go code (gitignored)
├── internal/
│   ├── server/                       # WebSocket server + session
│   ├── engine/                       # Agentic loop orchestrator
│   ├── llm/                          # LLM provider interface + Gemini
│   ├── tools/                        # Built-in tool implementations
│   ├── workspace/                    # Path validation
│   └── config/                       # CLI config
├── Makefile
└── buf.yaml / buf.gen.yaml
```

## Development

```bash
# Generate protobuf code
make proto
# or: protoc --go_out=gen/go --go_opt=paths=source_relative -I proto proto/localharness/v1/localharness.proto

# Build everything
make all

# Run tests
make test

# Lint protos
make lint
```

## Roadmap

### Post-1.0

**Triggers & Reactivity**
- [ ] File watcher triggers — `OnFileChange()` for auto-reactive agents
- [ ] Interval triggers — `Every(duration, fn)` for periodic tasks

**Tools**
- [ ] Image generation tool
- [ ] Knowledge Items (KI) — `KnowledgeWriteToFile`, `KnowledgeReplaceFileContent`, `DeleteKnowledge`
- [ ] MCP Resources — `ListResources`, `ReadResource` for MCP resource access
- [ ] Notebook editing — `NotebookEdit` for Jupyter support
- [ ] `ListPermissions` tool — show current permission grants

**Protocol & Wire Format**
- [ ] JSON wire format option — protobuf + JSON toggle for debugging
- [ ] Multimedia user messages — image/audio parts in user prompts
- [ ] ToolContext injection — host tools access conversation state

**Developer Experience**
- [ ] Interactive REPL — auto-upgrade policies in interactive mode
- [ ] Shell injection detection — `isDangerousBinary()`, `isDangerousSubcommand()`
- [ ] Auto-run command policies — `ShouldAutoRun()` with trust levels
- [ ] Workspace API tool — programmatic workspace management

## License

Apache 2.0
