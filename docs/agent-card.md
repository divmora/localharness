# A2A Agent Card

The LocalHarness implements the [Google A2A (Agent-to-Agent)](https://github.com/google/A2A) protocol's **Agent Card** — a machine-readable JSON discovery document that describes the agent's capabilities, skills, and interaction requirements.

## Endpoint

```
GET /.well-known/agent.json
```

The agent card is served as a standard HTTP endpoint on the same server that hosts the WebSocket connection. No authentication is required.

**Response headers:**
- `Content-Type: application/json`
- `Access-Control-Allow-Origin: *`

## Default Agent Card

When no custom card is configured, the harness returns a default card reflecting its built-in capabilities:

```json
{
  "name": "LocalHarness Agent",
  "description": "An agentic AI coding assistant...",
  "version": "0.3.0",
  "provider": {
    "name": "Divmora",
    "url": "https://github.com/divmora/localharness"
  },
  "capabilities": {
    "streaming": true,
    "pushNotifications": false,
    "stateTransitionHistory": true,
    "a2aVersion": "0.2.0"
  },
  "defaultInputModes": ["text/plain"],
  "defaultOutputModes": ["text/plain", "application/json"],
  "skills": [
    {
      "id": "code-editing",
      "name": "Code Editing",
      "description": "View, create, and edit files with targeted search-and-replace.",
      "tags": ["code", "files", "editing"],
      "examples": ["Fix the bug in handler.go"]
    },
    ...
  ],
  "authentication": {
    "schemes": ["apiKey"]
  }
}
```

### Default Skills

| Skill ID | Description |
|:---|:---|
| `code-editing` | View, create, and edit files |
| `code-search` | Search files by content or name pattern |
| `shell-execution` | Run shell commands (sync, background, persistent) |
| `web-research` | Search the web and fetch page contents |
| `task-management` | Background tasks, timers, and cron jobs |
| `agent-delegation` | Spawn child agents for parallel subtasks |

## Custom Agent Card

### Server-Side (Binary)

```go
s := server.NewServer(apiKey, logger)
s.SetAgentCard(&server.AgentCard{
    Name:        "MyCustomAgent",
    Description: "A specialized code review agent.",
    Version:     "1.0.0",
    Provider: &server.AgentCardProvider{
        Name: "MyCompany",
        URL:  "https://example.com",
    },
    Capabilities: server.AgentCardCapabilities{
        Streaming: true,
    },
    DefaultInputModes:  []string{"text/plain"},
    DefaultOutputModes: []string{"text/plain"},
    Skills: []server.AgentCardSkill{
        {
            ID:          "code-review",
            Name:        "Code Review",
            Description: "Review pull requests and suggest improvements.",
            Tags:        []string{"review", "code-quality"},
            Examples:    []string{"Review the latest PR"},
        },
    },
})
```

### SDK-Side (Fetching)

```go
agent, _ := adk.NewAgent(cfg)
agent.Start(ctx)

card, err := agent.FetchAgentCard(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Agent: %s v%s\n", card.Name, card.Version)
fmt.Printf("Streaming: %v\n", card.Capabilities.Streaming)
for _, skill := range card.Skills {
    fmt.Printf("  - %s: %s\n", skill.Name, skill.Description)
}
```

## A2A Protocol Compliance

The agent card follows the [A2A specification](https://github.com/google/A2A):

| Field | Status | Notes |
|:---|:---|:---|
| `name` | ✅ | Agent display name |
| `description` | ✅ | Agent purpose summary |
| `version` | ✅ | From `config.HarnessVersion` |
| `url` | ✅ | Service endpoint URL |
| `provider` | ✅ | Name + URL |
| `capabilities` | ✅ | streaming, pushNotifications, stateTransitionHistory, a2aVersion |
| `defaultInputModes` | ✅ | MIME types accepted |
| `defaultOutputModes` | ✅ | MIME types produced |
| `skills` | ✅ | ID, name, description, tags, examples |
| `authentication` | ✅ | apiKey scheme |

## Key Files

| File | Purpose |
|:---|:---|
| `internal/server/agent_card.go` | AgentCard types, default card, HTTP handler |
| `internal/server/server.go` | Route registration at `/.well-known/agent.json` |
| `internal/server/agent_card_test.go` | Tests for default card, custom card, CORS |
| `sdk/connection/connection.go` | `FetchAgentCard()` interface + SDK types |
| `sdk/connection/local_connection.go` | HTTP GET implementation |
| `sdk/types.go` | `AgentCardInfo` and related SDK types |
| `sdk/agent.go` | `Agent.FetchAgentCard()` convenience method |
