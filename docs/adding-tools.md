# Adding Custom Tools

This guide shows how to add a new built-in tool to LocalHarness.

## 1. Define the Proto Message

In `proto/localharness/v1/localharness.proto`:

```protobuf
// ActionMyTool does something useful.
message ActionMyTool {
  // Args (set on STATE_ACTIVE)
  string input = 1;

  // Result (set on STATE_DONE)
  string output = 10;
}
```

Add it to the `StepUpdate.action` oneof:

```protobuf
oneof action {
  // ... existing tools ...
  ActionMyTool my_tool = 31;   // Next available field number
}
```

Run `make proto` to regenerate Go code.

## 2. Implement the Tool

Create `internal/tools/my_tool.go`:

```go
package tools

import (
    "context"
    "fmt"

    pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// executeMyTool handles the my_tool action.
func executeMyTool(ctx context.Context, step *pb.StepUpdate) error {
    action := step.GetMyTool()
    if action == nil {
        return fmt.Errorf("my_tool: missing action")
    }

    // Your logic here
    result := fmt.Sprintf("processed: %s", action.Input)

    // Write result back to the same action message
    action.Output = result
    return nil
}
```

## 3. Register the Tool

In `internal/tools/registry.go`, add to `RegisterBuiltinTools()`:

```go
reg.Register("my_tool", executeMyTool)
```

Add the JSON Schema to the `Schemas()` method:

```go
{
    Name:        "my_tool",
    Description: "Does something useful with the input.",
    Group:       ToolGroupWrite, // ToolGroupRead for read-only, ToolGroupWrite for write
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "input": map[string]interface{}{
                "type":        "string",
                "description": "The input to process",
            },
        },
        "required": []string{"input"},
    },
},
```

## 4. Handle File Paths

If your tool operates on files, **always** validate paths:

```go
absPath, err := reg.workspace.ValidatePath(action.Path)
if err != nil {
    return fmt.Errorf("my_tool: %w", err)
}
// Use absPath, not action.Path
```

## 5. Test

```bash
# Build the binary (SDK auto-finds ./bin/localharness)
make build

# Test via the test client
go run ./cmd/testclient --api-key=$LITELLM_API_KEY --prompt "Use my_tool on some input"
```

---

## Host-side (SDK-registered) Tools

Not all tools need to run inside the harness. **Host-side tools** execute on the SDK client — the harness forwards the LLM's tool call to your SDK, waits for the result, and feeds it back to the LLM.

This is ideal for:
- Tools that need SDK-side state (database connections, auth tokens)
- Domain-specific logic that shouldn't live in the harness
- Rapid prototyping without recompiling the harness

### Go ADK (Recommended)

Use `HostTools` on `LocalAgentConfig` to register tools with auto-dispatch handlers:

```go
cfg.HostTools = []adk.HostToolDef{
    {
        Name:        "get_weather",
        Description: "Get current weather for a city",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "city": map[string]any{
                    "type":        "string",
                    "description": "City name, e.g. 'Tokyo'",
                },
            },
            "required": []string{"city"},
        },
        Handler: func(ctx context.Context, args map[string]any) (any, error) {
            city, _ := args["city"].(string)
            return map[string]string{"city": city, "temp": "22°C"}, nil
        },
    },
}
```

The agent auto-dispatches tool calls to your handlers and sends results back to the LLM. No manual WebSocket interaction needed.

**Validation rules:**
- Tool names must be non-empty and unique.
- Tool names must not collide with built-in harness tools (e.g., `view_file`, `run_command`).
- Each tool must have a non-nil handler.

See [`examples/adk-host-tools/`](../examples/adk-host-tools/) for a runnable example.

### Wire Protocol (Raw JSON Config)

For non-Go SDKs, pass `host_tools` in your `HarnessConfig.InitRequest`:

```json
{
  "host_tools": [
    {
      "name": "get_weather",
      "description": "Get current weather for a city",
      "parameters_json_schema": "{\"type\":\"object\",\"properties\":{\"city\":{\"type\":\"string\"}},\"required\":[\"city\"]}"
    }
  ]
}
```

### How It Works

1. The harness includes your tool schemas alongside built-in tools when calling the LLM
2. When the LLM calls your tool, the harness emits a `StepUpdate` with `state: STATE_WAITING` and action `host_tool_call`
3. The `ActionHostToolCall` contains `tool_name`, `args_json`, `call_id`, and `step_index`
4. Your SDK executes the tool and sends back a `ToolResult` with `step_id` set to the `step_index` (as a string)
5. The harness unblocks, adds the result to conversation history, and continues the agentic loop

### Timeout

The harness waits up to **5 minutes** for a tool result. If the timeout expires, the engine returns an error to the LLM so it can recover gracefully.

See [`examples/host-tools/`](../examples/host-tools/) for the wire protocol reference.
