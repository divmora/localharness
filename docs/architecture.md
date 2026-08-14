# Architecture

## System Overview

```
SDK/CLI Client ←→ WebSocket + Protobuf ←→ LocalHarness Binary (Go)
                                                       ├── Built-in Tools
                                                       │   ├── view_file
                                                       │   ├── write_to_file
                                                       │   ├── replace_file_content / multi_replace_file_content
                                                       │   ├── list_dir
                                                       │   ├── grep_search (ripgrep)
                                                       │   ├── find_file
                                                       │   ├── run_command (sync, background, persistent)
                                                       │   ├── manage_task
                                                       │   ├── finish
                                                       │   ├── invoke_subagent (child trajectories)
                                                       │   ├── search_web / read_url_content
                                                       │   ├── schedule (one-shot timers, cron)
                                                       │   ├── ask_question (interactive MCQ)
                                                       │   └── browser (Playwright via MCP)
                                                       ├── Engine (agentic loop)
                                                       ├── LLM Providers (Gemini, OpenAI)
                                                       ├── Background Tasks & Persistent Terminals
                                                       ├── Conversation Persistence
                                                       └── LLM Call Tracing
```

## Connection Protocol

The SDK communicates with the LocalHarness binary via a two-phase protocol:
a **pipe-based handshake** followed by **WebSocket communication**.

### Pipe Handshake (stdin/stdout)

```
SDK (Go)                                         Binary
  │── 1. Spawn process (capture stdin/stdout/stderr pipes) ─→│
  │── 2. Write 4-byte LE length + InputConfig ──────────────→│  (via stdin, protobuf)
  │                                                          │  Binary binds localhost:0,
  │                                                          │  generates crypto-random API key
  │←─ 3. Read 4-byte LE length + OutputConfig ──────────────│  (via stdout: port + API key)
  │                                                          │  Binary closes stdout
  │── 4. Connect ws://localhost:<port>/ ────────────────────→│  (x-localharness-api-key header)
  │── 5. Send InitRequest (binary protobuf) ────────────────→│
  │←─ 6. Read InitResponse ─────────────────────────────────│
  │←→ 7. Bidirectional StepUpdate stream ───────────────────│
```

**Key design decisions**:
- **Atomic port selection**: The binary binds `localhost:0` — the OS assigns a free port atomically. No race condition.
- **API key authentication**: A 32-byte crypto-random key is generated inside the binary and returned via stdout. The SDK passes it as a WebSocket header. Any local process without the key cannot connect.
- **Stderr capture**: The SDK captures stderr from the binary into a bounded ring buffer (100 lines). On crash, the buffer contents are included in error messages.

### Wire Format

- **Handshake (stdin/stdout)**: Length-prefixed protobuf — 4-byte little-endian uint32 + protobuf payload
- **WebSocket**: Binary protobuf frames (`ClientMessage` / `ServerMessage`)

### Graceful Teardown

The SDK uses a multi-phase shutdown sequence:

1. **WebSocket close**: Send `CloseNormalClosure` frame and close the connection
2. **Stdin close**: Close the stdin pipe, which triggers the binary's EOF watcher
3. **Wait 5s**: Allow the binary to flush trajectories and run `defer` blocks
4. **SIGTERM**: If still running after 5s, send SIGTERM
5. **Wait 1s → SIGKILL**: Last resort if SIGTERM fails

The binary watches stdin for EOF as the "please exit" signal. On EOF, it runs all deferred cleanup (trajectory persistence, child process termination) and exits gracefully.

## Agentic Loop

The core loop in `engine.go`:

```
User Prompt → LLM Call → [Tool Calls?]
                             ↓ yes
                         Execute Tools → Feed Results → LLM Call → ...
                             ↓ no
                         Final Response → IDLE
```

Each iteration:
1. **Compaction check** — if tokens > threshold, summarize old messages
2. **Trace request** — log LLM call to `traces/step_NNNN_request.json`
3. **LLM generate** — call Gemini/OpenAI with history + tools
4. **Trace response** — log response with latency
5. **Dispatch tools** — execute each tool call, emit StepUpdate events
6. **Loop or finish** — repeat if more tool calls, stop on text/finish

## Data Directory Layout

```
~/.divmora/localharness/
├── conversations/
│   └── <uuid>.pb                      # ConversationState protobuf
└── brain/
    └── <uuid>/
        └── .system_generated/
            ├── logs/
            │   └── transcript.jsonl   # Human-readable step log
            ├── steps/
            │   └── <N>/content.md     # Per-step content
            └── traces/
                ├── step_0001_request.json
                └── step_0001_response.json
```

## Proto Schema

Single file: `proto/localharness/v1/localharness.proto`

### Message Flow

```
Client → Server:
  ClientMessage { InitRequest | UserMessage | ToolResult | CancelRequest | PermissionResponse | QuestionResponse }

Server → Client:
  ServerMessage { InitResponse | StepUpdate | TrajectoryState | ErrorEvent }
```

### StepUpdate Actions (oneof)

| Field # | Proto Field | Action Message | Purpose |
|:---|:---|:---|:---|
| 20 | `view_file` | `ActionViewFile` | Read file with line range |
| 21 | `write_to_file` | `ActionWriteToFile` | Create/overwrite file |
| 22 | `replace_file_content` | `ActionReplaceFileContent` | Search-and-replace edit (single or multi-site) |
| 23 | `list_dir` | `ActionListDir` | Directory listing |
| 24 | `grep_search` | `ActionGrepSearch` | Grep/ripgrep search |
| 25 | `find_file` | `ActionFindFile` | Find by name pattern |
| 26 | `run_command` | `ActionRunCommand` | Shell execution (sync, background, persistent) |
| 27 | `finish` | `ActionFinish` | Task completion |
| 28 | `host_tool_call` | `ActionHostToolCall` | SDK-registered tool |
| 29 | `compaction` | `ActionCompaction` | Context compaction |
| 30 | `user_question` | `ActionUserQuestion` | Interactive question (STATE_WAITING → response) |
| 31 | `manage_task` | `ActionManageTask` | Background task management |
| 32 | `permission_request` | `ActionPermissionRequest` | SDK permission request (STATE_WAITING → response) |
| 33 | `invoke_subagent` | `ActionInvokeSubagent` | Launch async child agents with typed roles |
| 34 | `search_web` | `ActionSearchWeb` | Search the web using DuckDuckGo scraping |
| 35 | `read_url_content` | `ActionReadUrlContent` | Fetch page contents and extract clean text/markdown |
| 36 | `mcp_tool` | `ActionMcpTool` | MCP server tool call |
| 37 | `schedule` | `ActionSchedule` | One-shot timer or recurring cron job |
| 50 | `define_subagent` | `ActionDefineSubagent` | Define a new subagent type for this conversation |
| 51 | `manage_subagents` | `ActionManageSubagents` | List, inspect, or kill active subagent instances |
| 52 | `send_message_action` | `ActionSendMessage` | Send messages to another agent by conversation ID |

## Context Compaction

Long-running agent conversations accumulate tool calls, results, and messages
that can exceed the LLM's context window. The compaction system in
`engine/compaction.go` automatically summarizes older messages.

### Flow

```
Before each LLM call:
  1. Estimate tokens (real provider count preferred, else ~4 chars/token)
  2. If tokens > threshold → trigger compaction
  3. Split history: [old messages | recent N messages]
  4. Send old messages to LLM with summarization prompt
  5. Replace old messages with [summary, model_ack]
  6. If still > 80% threshold → retry with fewer kept messages (sliding window)
```

### Token Counting Strategy

- **Hybrid approach**: Uses real `Usage.TotalTokens` from the last LLM response
  when available, falling back to improved character-based estimation
- **Estimation includes**: Per-message overhead (4 tokens), content, tool call
  args, tool result content, and function name tokens
- **System prompt**: Accounted for in the total but not included in the
  compactable message history

### Summarization Prompt

The compaction prompt instructs the LLM to preserve:
- Decisions and their rationale
- All file paths and what was done to them
- Tool call sequences and outcomes
- User goals and current progress
- Code context (function names, patterns)
- Errors, root causes, and resolutions

### Configuration

| Proto Field | Default | Description |
|:---|:---|:---|
| `compaction_threshold` | `0` (disabled) | Token count trigger. `0` = off, `>0` = threshold. |
| `keep_recent_messages` | `10` | Messages preserved during compaction. |

## Streaming Responses

When the LLM provider implements `StreamingProvider`, the engine streams
text and thinking content to the client in real-time as tokens are generated.

### Flow

```
Engine calls GenerateStream()
  → Provider opens SSE connection to LLM API
  → For each SSE chunk:
    → Emit StepUpdate with STATE_STREAMING + text_delta / thinking_delta
  → Stream ends → accumulate final GenerateResponse
  → Emit StepUpdate with STATE_DONE + full text
  → Continue normal tool dispatch if tool_calls present
```

### Key Details

- **Fallback**: If the provider doesn't implement `StreamingProvider`,
  the engine uses `Generate()` (blocking) — no behavior change
- **Deltas use the same step index**: All streaming chunks for one LLM call
  share the same `step_index`. The final `STATE_DONE` step also uses a unique index.
- **Tool calls are not streamed**: Tool call arguments arrive in the final chunk
  and are dispatched normally through the agentic loop
- **Proto fields**: Uses existing `text_delta` (field 5) and `thinking_delta` (field 7)
  on `StepUpdate`, with `STATE_STREAMING = 5` in the State enum

## Background Tasks & Persistent Terminals

### Background Tasks

The `run_command` tool supports `background=true` to start long-running processes
that the agent can check back on later. Background tasks:

- Capture output in a 100KB ring buffer (no unbounded memory growth)
- Can be listed, inspected, killed, or have stdin sent via `manage_task`
- Auto-prune after 30 minutes of completion
- Limited to 20 concurrent tasks (configurable)

### Persistent Terminals

The `run_command` tool supports `persistent=true` to run commands in a long-lived
bash session that preserves environment variables across invocations. Terminals:

- Each terminal is a bash process with stdin pipe
- Commands are delimited by unique markers to separate output
- Reuse terminals by passing `terminal_id` from a previous invocation
- All terminals are cleaned up on session disconnect

## Knowledge Items (KI)

Knowledge Items provide a persistent, project-scoped knowledge store that agents can read, write, and update across conversations. KIs capture curated context about the codebase (patterns, conventions, known issues) that survives conversation boundaries.

### Project Registry

KIs are scoped to **projects** — stable identities backed by UUIDs. A `projects.json` registry maps workspace paths to project UUIDs:

```
appDataDir/
├── projects.json                          # UUID ↔ workspace paths
├── knowledge/
│   ├── <project-uuid-A>/                  # KIs for project A
│   │   ├── error-handling-patterns/
│   │   │   ├── metadata.json
│   │   │   └── artifacts/
│   │   │       └── overview.md
│   │   └── api-conventions/
│   │       ├── metadata.json
│   │       └── artifacts/
│   │           └── api_surface.md
│   └── <project-uuid-B>/                  # KIs for project B (isolated)
│       └── ...
```

At session init, the engine finds/creates a project matching the current workspace and loads only that project's KIs. This ensures KIs from a Go backend project never leak into a React frontend project.

### Tools (Engine-Intercepted)

| Tool | Description |
|:---|:---|
| `knowledge_write` | Create/update a KI + write artifact file |
| `knowledge_replace` | Search-and-replace within a KI artifact |
| `knowledge_delete` | Delete an entire KI or specific artifact |

These are **schema-only** registrations (via `RegisterSchemaOnly`) — the engine intercepts them before they reach the tool registry's `Execute()` method.

### Staleness Detection

KIs track `references` — the source files they were derived from. At session init, `CheckStaleness()` stats each referenced file and compares mtime to the KI's `updated_at`. Stale KIs get a ⚠️ marker in per-message injection. Writing to a KI clears staleness.

### Per-Message Injection

When KIs exist, a `<knowledge_items>` block is injected into the enriched user message with KI names, summaries, artifact paths, and staleness indicators. This uses the data-driven pattern (non-empty = enabled).

### Key Files

| File | Purpose |
|:---|:---|
| `internal/engine/project_registry.go` | Project struct, ProjectRegistry, workspace→UUID mapping |
| `internal/engine/knowledge.go` | KnowledgeItem struct, KnowledgeStore, CRUD + staleness |
| `internal/engine/knowledge_tools.go` | Engine-intercepted tool handlers |
| `internal/tools/knowledge.go` | Tool schema definitions (schema-only) |

## Subagent Support

Subagents allow the parent agent’s LLM to spawn nested child agentic loops to perform isolated tasks asynchronously.

### Architecture

```
Engine (parent)
├── SubagentRegistry       ← merges built-in + SDK + agent-defined types
├── SubagentTracker        ← tracks running instances, routes messages
├── define_subagent tool   ← register new types at runtime
├── invoke_subagent tool   ← launch 1+ typed subagents (async goroutines)
├── manage_subagents tool  ← list/kill active instances
└── send_message tool      ← inter-agent messaging by conversation ID
```

### Subagent Types

Each subagent type (`SubagentTypeDef`) defines a named template with:
- **System prompt** — specialized behavior instructions
- **Tool group flags** — `EnableWriteTools`, `EnableMCPTools`, `EnableSubagentTools`

Types come from three sources, merged in priority order:
1. **Built-in** (`research`, `self`) — always available unless excluded
2. **SDK-registered** — custom types via `HarnessConfig.subagent_types`
3. **Agent-defined** — created via `define_subagent` tool during a conversation

### Tool Group Filtering

Each built-in tool is tagged with a `ToolGroup` (`read` or `write`). When a child engine is created, the `SubagentTypeDef` flags control which groups are visible:
- `EnableWriteTools=false` → write-group tools and host tools are excluded
- `EnableMCPTools=false` → MCP tools are excluded
- `EnableSubagentTools=false` → subagent tools are excluded (no recursion)

### Execution Model

- **Async**: `invoke_subagent` launches child engines in background goroutines and returns immediately with launch results.
- **Context Isolation**: Each child engine gets a fresh message history with only the provided prompt.
- **Resource Sharing**: Children share the parent’s LLM provider, tool registry, workspace configs, and permission handler.
- **Step Bubbling**: Child engines use the same step/trajectory callbacks as the parent. All child steps stream to the client in real-time.
- **Completion Notification**: When a child finishes, it sends a `SystemMessage` to the parent’s notification channel.

### Trajectory and ID Hierarchies

- **Trajectory ID Naming**: Child engines use `<parent_trajectory_id>/sub_<step_index>_<type_name>` (e.g., `traj_0/sub_3_research`).
- **Conversation ID Naming**: `<parent_conv_id>-sub-<step_index>-<type_name>`.
- **Nesting Context**: All trajectory states include `parent_trajectory_id` and `depth` for clean grouping in UIs.

### Safety Limits

To prevent infinite loops and runaway billing/resource usage:
- **Depth Limiting**: `max_subagent_depth` (default: 3). Spawning is rejected if depth exceeded.
- **Concurrency Limiting**: `max_concurrent_subagents` (default: 5). Atomic counter tracks active children.
- **Turn Limiting**: Child engines have a reduced max turns (default: 25 vs parent’s 50).
- **Auto-Wake Limiting**: `max_auto_wake_turns` caps synthetic turns from background notifications.

## Go ADK Client

The Go ADK (`sdk/`) provides a programmatic client interface to integrate with LocalHarness, supporting interactive loops and safety controls.

### Client Architecture

```
User Program ──► sdk.Agent (Orchestration)
                   │
                   ├── sdk.connection.Connection (WebSocket Client)
                   │     └── Spawns and manages localharness process
                   │
                   ├── sdk.hooks.HookRunner (Lifecycle events dispatch)
                   │
                   └── sdk.policy (Declarative security enforcement)
```

- **`Agent`**: The main orchestrator. Manages turn-level loops, initiates the connection, maps input/output models, and coordinates local policy checks.
- **`Connection`**: Manages communication with the LocalHarness binary. `LocalConnection` spawns the binary, performs the pipe-based handshake (stdin/stdout) to exchange port and API key, establishes the authenticated WebSocket connection, and implements graceful multi-phase teardown.
- **`hooks`**: Structured lifecycle listeners (Session, Turn, and Operation level) that observe and intercept tool calls, LLM results, and start/end boundaries. All 9 hook types:
  - `PreToolCallDecideHook` — block/allow tool execution (policy enforcement)
  - `PostToolCallHook` — observe tool completion (logging, metrics)
  - `PreTurnHook` / `PostTurnHook` — observe turn boundaries
  - `OnSessionStartHook` / `OnSessionEndHook` — session lifecycle
  - `OnToolErrorHook` — intercept tool failures, provide recovery values
  - `OnInteractionHook` — answer agent questions programmatically
  - `OnCompactionHook` — observe context window compaction events
- **`middleware`**: A composable pipeline for intercepting and transforming agent turns. Unlike hooks (which are event-driven listeners), middlewares form an ordered chain that processes data flowing through the system:
  - **PreTurnMiddleware** — transforms the prompt before it reaches the harness (runs in registration order)
  - **PostTurnMiddleware** — post-processes the response after a turn completes (runs in reverse order)
  - **StepMiddleware** — intercepts individual streaming events for filtering or observability
  - Built-in middlewares:
    - `PatchToolArgs` — detects malformed JSON in tool call arguments (trailing commas, unescaped newlines, unbalanced braces)
    - `TokenGuard` — enforces cumulative token budget with configurable warning thresholds
    - `ToolSelector` — dynamic tool selection via prompt injection. Supports allow/deny lists (static or per-turn via callback), predefined tool groups (`ReadOnlyTools`, `WriteTools`, `DangerousTools`), and violation monitoring
    - `InterruptResume` — stateless interrupt/resume at turn boundaries. Saves `TurnCheckpoint` on user cancel, budget exceeded, or timeout. `BuildResumePrompt()` constructs a continuation prompt from the checkpoint
    - `CheckpointStore` — persistent file-based storage for `TurnCheckpoint`. Plugs into `InterruptResume.OnInterrupt` as a callback. Supports `Save`, `Latest`, `Load(turn)`, `List`, `Clear`. JSON files stored as `checkpoint_NNNN_timestamp.json`
  - **Middleware vs Hooks**: Hooks are fire-and-forget listeners. Middlewares form a chain where each stage can transform data or abort the pipeline.
- **`policy`**: A declarative security policy engine. Groups custom safety rules into priority buckets (`Specific Deny > Specific Ask > Specific Allow > Wildcard Deny > Wildcard Ask > Wildcard Allow`) to evaluate tool permission requests synchronously.
- **Logging & Diagnostics**: Controlled logging throughout the orchestrator and connection layers. Users can inject a custom `*slog.Logger` or toggle `Verbose` to enable detailed debug tracing (tracing connection requests, websocket steps, tool pre-execution, and hook results).
- **Session Token Budget**: Real-time accumulation of model token counts. If cumulative usage exceeds `MaxTotalTokens`, subsequent turns fail fast to prevent runaway loops and billing overruns.
- **Subtask API** (`adk/subtask.go`): Ergonomic wrapper around the subagent system. `agent.RunSubtask()` creates a child agent with a derived config (fresh context, configurable tool access, process isolation) and runs a single synchronous chat. `RunSubtaskAsync()` launches in the background for parallel execution. Read-only by default, inherits LLM provider/retry/middlewares from parent, disables sub-subagent recursion.

## LLM Providers

### Provider Interface

```go
type Provider interface {
    Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
    ModelName() string
    Close() error
}

// Optional streaming extension — checked at runtime
type StreamingProvider interface {
    Provider
    GenerateStream(ctx context.Context, req *GenerateRequest) (<-chan StreamChunk, <-chan error)
}
```

### Supported Providers

All LLM traffic is routed through **LiteLLM**, which serves as a proxy to downstream providers (OpenAI, Anthropic, Gemini, Vertex AI, Ollama, etc.).

| Provider Configuration | Source | Priority |
|:---|:---|:---|
| ADK Inline Configuration | `LocalAgentConfig` fields (`LitellmAPIKey`, `LitellmBaseURL`, `LitellmModel`) | High (Overrides global) |
| Global Configuration | `~/.divmora/config/litellm.json` | Fallback (Based on `litellm_endpoint`) |

### Retry & Failover

When `retry_config` is set in `HarnessConfig`, the provider is wrapped in a `RetryProvider` that handles transient failures:

- **Exponential backoff**: Configurable base (default 1s) and max (default 30s) durations. Each retry doubles the wait.
- **Retryable error filtering**: Only errors matching configured substrings trigger retries (default: `rate_limit`, `429`, `overloaded`, `503`, `timeout`, `UNAVAILABLE`, etc.).
- **Provider failover**: If `fallback_model_config` is set and the primary exhausts all retries, the system switches to the fallback provider. The switch is **sticky** — subsequent calls use the fallback for the remainder of the session.
- **Streaming**: Stream calls are not retried mid-stream; only initial connection failures trigger retry. If the active provider doesn't support streaming, the wrapper synthesizes a stream from a blocking `Generate()` call.

Proto fields:
- `HarnessConfig.retry_config` → `RetryConfig` (max_retries, backoff_base_ms, backoff_max_ms, retryable_errors)
- `HarnessConfig.fallback_gemini` / `fallback_openai` → secondary provider config


## Interactive Questions (`ask_question`)

The `ask_question` tool enables the agent to present multiple-choice questions to the user.
It follows the same **STATE_WAITING → block → response → STATE_DONE** pattern as permission requests:

```
LLM calls ask_question
  → Engine builds ActionUserQuestion + emits STATE_WAITING
  → Session blocks on pendingQuestions channel
      → SDK receives STATE_WAITING step with questions
      → Calls user's QuestionHandler callback
      → User selects answers
      → SDK sends QuestionResponse back via WebSocket
  → Session routes response to pending channel
  → Engine unblocks, formats answers as JSON for LLM
  → Emits STATE_DONE step
```

If no `QuestionHandler` is registered in the SDK, the question is auto-skipped.

See [docs/ask-question.md](ask-question.md) for full details.

## Schedule/Timer (`schedule`)

The `schedule` tool provides one-shot timers and recurring cron jobs:

- **One-shot**: `duration_seconds` (max 900s) — fires once after delay, sends notification
- **Cron**: `cron_expression` (standard 5-field) with optional `max_iterations` — fires repeatedly

The `ScheduleManager` runs background goroutines and delivers notifications via a buffered channel.
Timer/cron IDs (`sched-*`, `cron-*`) are killable via `manage_task`.

See [docs/schedule.md](schedule.md) for full details.

## Error Handling

LocalHarness uses a structured error handling system that provides programmatic error classification, enhanced debugging information, and wire protocol transmission of error context.

### Error Type System

The core error type is `HarnessError` in `internal/errors/errors.go`:

```go
type HarnessError struct {
    Code      ErrorCode              // Categorizable error code
    Message   string                 // Human-readable message
    Cause     error                  // Underlying error (for unwrapping)
    Context   map[string]interface{} // Structured context
    Component string                 // Component that generated the error
    RequestID string                 // Correlation ID for request tracking
}
```

### Error Codes

Error codes are categorized by subsystem:

| Category | Error Codes |
|:---|:---|
| **Workspace** | `WORKSPACE_VALIDATION`, `PATH_TRAVERSAL`, `SYMLINK_ATTACK`, `FILE_NOT_FOUND`, `PERMISSION_DENIED` |
| **Tool Execution** | `TOOL_EXECUTION`, `TOOL_TIMEOUT`, `TOOL_VALIDATION`, `COMMAND_INJECTION` |
| **LLM Provider** | `LLM_PROVIDER`, `LLM_TIMEOUT`, `LLM_RATE_LIMIT`, `LLM_TOKEN_LIMIT` |
| **Resource** | `RESOURCE_EXHAUSTION`, `MEMORY_LIMIT`, `TASK_LIMIT` |
| **Configuration** | `CONFIGURATION`, `INVALID_CONFIG`, `MISSING_CONFIG` |
| **Network** | `NETWORK`, `CONNECTION_FAILED`, `CONNECTION_TIMEOUT` |
| **Authentication** | `AUTHENTICATION`, `UNAUTHORIZED`, `INVALID_API_KEY` |
| **Engine** | `ENGINE_ERROR`, `MAX_TURNS_EXCEEDED`, `COMPACTION_FAILED`, `SUBAGENT_DEPTH_LIMIT` |

### Error Creation

**New error:**
```go
err := errors.New(errors.ErrCodeFileNotFound, "file not found").
    WithContext("path", "/tmp/test.txt").
    WithContext("operation", "view_file").
    WithComponent("view_file").
    WithRequestID("req-123")
```

**Wrapped error:**
```go
err := errors.Wrap(originalErr, errors.ErrCodeLLMProvider, "LLM call failed").
    WithContext("model", "gpt-4").
    WithContext("provider", "openai").
    WithComponent("engine")
```

### Error Inspection

**Check error code:**
```go
if errors.IsErrorCode(err, errors.ErrCodeLLMRateLimit) {
    // Implement retry logic
}
```

**Extract context:**
```go
if ctx := errors.GetContext(err); ctx != nil {
    path := ctx["path"]
    operation := ctx["operation"]
}
```

**Standard Go error handling:**
```go
var hErr *errors.HarnessError
if errors.As(err, &hErr) {
    fmt.Printf("Error code: %s\n", hErr.Code)
    fmt.Printf("Component: %s\n", hErr.Component)
    fmt.Printf("Request ID: %s\n", hErr.RequestID)
}
```

### Wire Protocol Transmission

Structured errors are transmitted over WebSocket via protobuf:

**ErrorEvent message:**
```protobuf
message ErrorEvent {
  string message = 1;
  string code = 2;
  bool fatal = 3;
  map<string, string> metadata = 4;  // Structured error context
}
```

**ErrorInfo message (in StepUpdate):**
```protobuf
message ErrorInfo {
  string message = 1;
  string code = 2;
  map<string, string> metadata = 3;
}
```

**Conversion:**
```go
// Convert HarnessError to protobuf ErrorEvent
protoEvent := hErr.ToProto()
// Metadata includes: path, operation, component, request_id, etc.
```

### Error Flow

```
Component Error
  ↓
HarnessError with code + context
  ↓
Session.sendStructuredError() / Engine.emitStructuredErrorStep()
  ↓
Protobuf ErrorEvent / ErrorInfo over WebSocket
  ↓
SDK Client receives structured error
  ↓
Programmatic error handling based on code
```

### Error Context Fields

Common context fields by error type:

| Error Type | Typical Context Fields |
|:---|:---|
| **File operations** | `path`, `operation`, `workspace`, `line_range` |
| **Tool execution** | `tool`, `args`, `cwd`, `timeout`, `exit_code` |
| **LLM calls** | `model`, `provider`, `retry_count`, `finish_reason` |
| **Network** | `url`, `status_code`, `timeout_ms`, `attempt` |
| **Authentication** | `provider`, `credential_type` |
| **Engine** | `trajectory_id`, `conversation_id`, `step_index`, `max_turns` |

### Backward Compatibility

The error system maintains backward compatibility:

- **Legacy string errors**: Non-structured errors fallback to legacy format
- **Human-readable messages**: Error messages remain readable for logs
- **SDK compatibility**: Existing SDK clients continue to work
- **Proto evolution**: New metadata field is optional in proto schema

### Error Recovery Patterns

SDK clients can implement intelligent error recovery:

```go
switch errors.GetCode(err) {
case errors.ErrCodeLLMRateLimit:
    // Wait and retry with exponential backoff
    time.Sleep(time.Second * time.Duration(retryCount * 2))
    return retry()
case errors.ErrCodeLLMTimeout:
    // Retry with longer timeout
    return retryWithTimeout(extendedTimeout)
case errors.ErrCodeToolTimeout:
    // Continue with default handling
    return continueWithoutTool()
case errors.ErrCodeMaxTurnsExceeded:
    // Request user intervention
    return requestUserHelp()
default:
    // Fail with user-friendly message
    return showUserError(err)
}
```

### Implementation Coverage

Structured errors are currently implemented in:

- ✅ `internal/errors/` - Core error type system
- ✅ `internal/workspace/` - Workspace validation errors
- ✅ `internal/tools/` - Tool execution errors (view_file, run_command, write_to_file)
- ✅ `internal/llm/` - LLM provider errors (OpenAI)
- ✅ `internal/engine/` - Engine orchestration errors
- ✅ `internal/server/` - Session management errors

### Migration Guide

To migrate existing error handling:

1. **Replace `fmt.Errorf`** with `errors.New` or `errors.Wrap`
2. **Add error codes** from the appropriate category
3. **Include context** using `WithContext()` for debugging
4. **Set component** using `WithComponent()` for tracking
5. **Update error checking** from string matching to `errors.IsErrorCode()`

**Before:**
```go
return fmt.Errorf("file not found: %s", path)
```

**After:**
```go
return errors.New(errors.ErrCodeFileNotFound, "file not found").
    WithContext("path", path).
    WithContext("operation", "view_file").
    WithComponent("view_file")
```

## Dependencies

| Package | License | Purpose |
|:---|:---|:---|
| `gorilla/websocket` | BSD-2 | WebSocket server |
| `google.golang.org/protobuf` | BSD-3 | Protobuf runtime |
| `google/uuid` | BSD-3 | UUIDv4 generation |

All permissive open-source. No copyleft.
