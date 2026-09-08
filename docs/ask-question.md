# AskQuestion Tool

The `ask_question` tool enables the agent to present interactive multiple-choice questions to the user. It blocks execution until the user responds (or the request times out / is skipped).

## How It Works

The tool follows the same **STATE_WAITING → block → response → STATE_DONE** pattern as permission requests:

```
Agent calls ask_question(questions=[...])
  → Engine intercepts (not handled by tool registry)
  → Builds ActionUserQuestion with request_id
  → Emits STATE_WAITING step to SDK
  → Session blocks on pendingQuestions channel (5 min timeout)
      → SDK receives STATE_WAITING step with questions
      → Calls user's QuestionHandler callback
      → User selects answers or skips
      → SDK sends QuestionResponse via WebSocket
  → Session routes response to pending channel
  → Engine unblocks
  → Formats answers as JSON for LLM
  → Emits STATE_DONE step
```

## Tool Schema

```json
{
  "name": "ask_question",
  "parameters": {
    "type": "object",
    "properties": {
      "questions": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "question": { "type": "string" },
            "options": { "type": "array", "items": { "type": "string" } },
            "is_multi_select": { "type": "boolean" }
          },
          "required": ["question", "options"]
        }
      }
    },
    "required": ["questions"]
  }
}
```

## SDK Usage

### Setting Up a QuestionHandler

```go
agent, _ := adk.NewAgent(&sdk.LocalAgentConfig{
    LitellmAPIKey: os.Getenv("LITELLM_API_KEY"),
    QuestionHandler: func(ctx context.Context, questions []sdk.Question) (*sdk.QuestionResponse, error) {
        // Present questions to user (e.g., via CLI, UI, etc.)
        var answers []sdk.Answer
        for _, q := range questions {
            fmt.Printf("Q: %s\n", q.Text)
            for i, opt := range q.Options {
                fmt.Printf("  %d. %s\n", i+1, opt)
            }
            // For example, auto-select the first option:
            answers = append(answers, sdk.Answer{
                SelectedIndices: []int32{0},
                SelectedOptions: []string{q.Options[0]},
            })
        }
        return &sdk.QuestionResponse{
            Answers: answers,
            Skipped: false,
        }, nil
    },
})
```

### Without a Handler (Auto-Skip)

If no `QuestionHandler` is set, questions are automatically skipped and the LLM receives:

```json
{"skipped": true, "reason": "no question handler registered — user interaction not available"}
```

The agent can then proceed without user input.

## Answer Format

When the user answers, the LLM receives a structured JSON response:

```json
{
  "skipped": false,
  "answers": [
    {
      "question_index": 0,
      "question": "Which framework should we use?",
      "selected_options": ["React", "Vue"],
      "text": ""
    }
  ]
}
```

Each answer can include:
- **`selected_options`** — the text of chosen options
- **`text`** — free-form write-in response

## Key Design Decisions

1. **Always registered** — `ask_question` is available regardless of `BuiltinToolsConfig` settings (it's harmless and useful for any agent)
2. **Engine-intercepted** — unlike most tools that go through the registry's `Execute()`, `ask_question` is intercepted by the engine directly (similar to `invoke_subagent`)
3. **5-minute timeout** — matches the permission request timeout; prevents indefinite blocking
4. **Graceful fallback** — no crash if handler is nil; agent is told the question was skipped

## Proto Reference

```protobuf
message ActionUserQuestion {
  string request_id = 1;
  repeated UserQuestion questions = 2;
  repeated QuestionAnswer answers = 10;   // Populated on STATE_DONE
  bool skipped = 11;
}

message UserQuestion {
  string question = 1;
  repeated string options = 2;
  bool is_multi_select = 3;
}

message QuestionAnswer {
  repeated int32 selected_indices = 1;
  repeated string selected_options = 2;
  string text = 3;
}

message QuestionResponse {
  string request_id = 1;
  repeated QuestionAnswer answers = 2;
  bool skipped = 3;
}
```

## Key Files

| File | Purpose |
|:---|:---|
| `internal/tools/ask_question.go` | Tool registration + JSON schema |
| `internal/engine/engine.go` | `QuestionHandler` type + `executeAskQuestion()` |
| `internal/server/session.go` | `questionHandler()` + `handleQuestionResponse()` |
| `sdk/connection/connection.go` | `SendQuestionResponse()`, `Step` fields, types |
| `sdk/connection/local_connection.go` | WebSocket implementation |
| `sdk/types.go` | `QuestionHandlerFunc`, `Question`, `Answer` types |
| `sdk/config.go` | `QuestionHandler` config field |
| `sdk/agent.go` | `handleQuestionRequest()` |
