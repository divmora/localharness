# Streaming API

LocalHarness provides real-time step delivery via `ChatStream()`. Instead of blocking until the full turn completes, you receive events as the model generates text and calls tools.

## Quick Start

```go
events, err := agent.ChatStream(ctx, "build the project")
if err != nil { log.Fatal(err) }

for event := range events {
    switch event.Type {
    case sdk.EventTextDelta:
        fmt.Print(event.TextDelta)          // Real-time text

    case sdk.EventThinkingDelta:
        // Model's reasoning trace (thinking models only)

    case sdk.EventToolCallStart:
        fmt.Printf("🔧 %s\n", event.Step.ToolName)

    case sdk.EventToolCallDone:
        fmt.Printf("✅ %s done\n", event.Step.ToolName)

    case sdk.EventError:
        fmt.Printf("❌ %s: %s\n", event.Step.ToolName, event.Step.ErrorMessage)

    case sdk.EventTurnComplete:
        fmt.Printf("Done! %d steps\n", len(event.Response.Steps))
    }
}
```

## Event Types

| Event | When | Data |
|:---|:---|:---|
| `EventTextDelta` | Model generates text | `TextDelta` |
| `EventThinkingDelta` | Model generates thinking/reasoning | `ThinkingDelta` |
| `EventToolCallStart` | Tool call begins (STATE_ACTIVE) | `Step` |
| `EventToolCallDone` | Tool call completes (STATE_DONE) | `Step` |
| `EventError` | Step error (non-fatal) | `Step` |
| `EventTurnComplete` | Turn finished | `Response` |

## Chat vs ChatStream

| Method | Returns | Blocking | Use Case |
|:---|:---|:---|:---|
| `Chat()` | `*ChatResponse` | Yes | Simple scripts, batch processing |
| `ChatStream()` | `<-chan StreamEvent` | No (channel) | UIs, CLIs, real-time displays |

`Chat()` is a convenience wrapper around `ChatStream()` that drains the channel and returns the final response.

## Error Handling

Transport-level errors (WebSocket disconnect) cause the channel to close. The final `EventTurnComplete` will have `Error` set and `Response` may be nil:

```go
for event := range events {
    if event.Type == sdk.EventTurnComplete {
        if event.Error != nil {
            log.Printf("Turn failed: %v", event.Error)
        } else {
            fmt.Println(event.Response.Text)
        }
    }
}
```

## Channel Semantics

- The channel is **buffered** (32 elements) to avoid blocking the processing goroutine
- The channel is **closed** when the turn completes
- The **last event** is always `EventTurnComplete`
- Events arrive in **chronological order**
- **Multiple consumers** are not safe — only one goroutine should read from the channel

## Complete Example

See [examples/adk-streaming/main.go](../examples/adk-streaming/main.go) for a full terminal streaming example.
