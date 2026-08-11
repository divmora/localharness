# Examples

Usage examples for LocalHarness.

> **Note**: The SDK manages the binary lifecycle automatically. It spawns
> the binary, performs a pipe-based handshake for secure port assignment
> and API key exchange, then communicates over WebSocket. You never need
> to start the binary manually.

## Prerequisites

```bash
# Build the binary (SDK auto-finds ./bin/localharness)
make build

# Set your API key
export LITELLM_API_KEY="your-key-here"
```

## Go ADK Examples

Runnable Go programs using the `adk` package:

| Example | Description | Run |
|:---|:---|:---|
| [adk-basic](adk-basic/) | Simple prompt → response | `go run ./examples/adk-basic` |
| [adk-policy](adk-policy/) | Declarative permission control | `go run ./examples/adk-policy` |
| [adk-safe-agent](adk-safe-agent/) | Read-only agent with interactive write approval | `go run ./examples/adk-safe-agent` |
| [adk-budget-logging](adk-budget-logging/) | Custom logging and session token budgets | `go run ./examples/adk-budget-logging` |
| [adk-streaming](adk-streaming/) | Streaming step-by-step output | `go run ./examples/adk-streaming` |
| [adk-structured-prompt](adk-structured-prompt/) | Modular system prompt composition | `go run ./examples/adk-structured-prompt` |
| [adk-planning-mode](adk-planning-mode/) | Plan-before-act workflow with artifacts | `go run ./examples/adk-planning-mode` |
| [adk-ephemeral-messages](adk-ephemeral-messages/) | Per-turn directives (security context, language, feature gates) | `go run ./examples/adk-ephemeral-messages` |
| [adk-host-tools](adk-host-tools/) | SDK-registered custom tools with auto-dispatch handlers | `go run ./examples/adk-host-tools` |
| [adk-slash-commands](adk-slash-commands/) | Slash command recommendations (agent suggests, user triggers) | `go run ./examples/adk-slash-commands` |
| [adk-skills-plugins](adk-skills-plugins/) | ADK-injected skills and plugins | `go run ./examples/adk-skills-plugins` |
| [adk-subagents](adk-subagents/) | Custom subagent types with tool-group control | `go run ./examples/adk-subagents` |
| [adk-middleware](adk-middleware/) | Middleware pipeline: logging, token guard, tool selector, retry | `go run ./examples/adk-middleware` |
| [adk-research](adk-research/) | Read-only research agent with checkpoints and resume | `go run ./examples/adk-research` |
| [adk-code-review](adk-code-review/) | Code review agent with dynamic tool selection and failover | `go run ./examples/adk-code-review` |
| [adk-subtask](adk-subtask/) | Ergonomic subtask API: sync, async, parallel, model override | `go run ./examples/adk-subtask` |
| [auto-discovery](auto-discovery/) | Auto-discover skills/plugins from workspace `.agents/` directory | `go run ./examples/auto-discovery` |
## Test Client Examples (CLI)

Usage patterns via `go run ./cmd/testclient`:

| Example | Description |
|:---|:---|
| [basic-gemini](basic-gemini/) | Simple prompt with Gemini |
| [openai-ollama](openai-ollama/) | Local LLM via Ollama |
| [tool-usage](tool-usage/) | Agentic file operations |
| [custom-system-prompt](custom-system-prompt/) | Custom system instructions |
| [compaction](compaction/) | Context window compaction |

## Reference

| Example | Description |
|:---|:---|
| [host-tools](host-tools/) | Wire protocol reference for SDK-registered custom tools |
