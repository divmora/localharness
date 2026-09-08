# MCP Bridge

LocalHarness supports the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) for connecting to external tool servers. MCP tools are discovered at session startup and treated identically to built-in tools — they get the same policy enforcement, hooks, and step pipeline.

## Architecture

```
SDK Client                    LocalHarness Binary                  MCP Server(s)
    │                              │                                    │
    ├── InitRequest ──────────────►│                                    │
    │   mcp_servers: [...]         │                                    │
    │                              │── Load global mcp_config.json     │
    │                              │── Merge global + agent config     │
    │                              │── McpManager.Connect() ──────────►│
    │                              │◄── tools/list ────────────────────┤
    │                              │                                    │
    ├── UserMessage ──────────────►│── Generate(builtin + mcp tools) ──►│
    │                              │◄── ToolCall(mcp_tool) ────────────┤
    │                              │── McpManager.CallTool() ─────────►│
    │                              │◄── result ────────────────────────┤
    │◄── StepUpdate(mcp_tool) ────┤                                    │
```

MCP runs **inside the engine binary**, not the SDK. The SDK only passes configuration.

## Configuration

### Global Config (shared across all agents)

Create `~/.divmora/config/mcp_config.json`:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp/workspace"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      }
    },
    "postgres": {
      "url": "http://localhost:3001/sse",
      "headers": {
        "Authorization": "Bearer ${DB_TOKEN}"
      }
    }
  }
}
```

- **`${ENV_VAR}` expansion**: Environment variables in values are expanded at load time
- **Missing file**: Not an error — returns empty config
- **Invalid JSON**: Logged as warning, returns empty config

### Agent-Level Config (per session)

```go
cfg := adk.NewLocalAgentConfig()
cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")
cfg.McpServers = []sdk.McpServer{
    {
        Name:    "filesystem",
        Command: "npx",
        Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
    },
    {
        Name:    "database",
        URL:     "http://localhost:3001/sse",
        Headers: map[string]string{"Authorization": "Bearer token"},
    },
}
```

### Config Merge Rules

| Scenario | Result |
|:---|:---|
| Global has "fs", agent has "db" | Both connected |
| Global has "fs", agent has "fs" | Agent's "fs" overrides global |
| Global has "fs", agent empty | Global "fs" used |
| Global empty, agent has "fs" | Agent "fs" used |

## Supported Transports

| Transport | Config Field | Description |
|:---|:---|:---|
| **Stdio** | `command` + `args` | Launches MCP server as subprocess, communicates via stdin/stdout |
| **SSE / Streamable HTTP** | `url` | Connects to a running MCP server via HTTP |

## Tool Discovery

On connection, LocalHarness calls `tools/list` on each MCP server to discover available tools. These are then:

1. Filtered by `enabledTools` whitelist (if configured)
2. Checked for name conflicts with built-in tools (errors on conflict)
3. Registered alongside built-in tools for LLM function calling

## Tool Whitelisting

Restrict which tools are exposed from an MCP server:

```go
sdk.McpServer{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
    Tools:   []string{"read_file", "list_directory"},  // Only these tools
}
```

Or in `mcp_config.json`:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "enabledTools": ["read_file", "list_directory"]
    }
  }
}
```

## Safety

MCP servers are treated as **write-capable** for the mandatory safety validation. If MCP servers are configured, you must have policies or decide hooks:

```go
cfg.McpServers = []sdk.McpServer{{Name: "fs", Command: "npx", Args: []string{...}}}
cfg.Policies = policy.AllowAll()  // Required when MCP servers are configured
```

## Step Updates

MCP tool calls emit `StepUpdate` with `ActionMcpTool`:

```protobuf
message ActionMcpTool {
  string server_name = 1;   // Which MCP server owns this tool
  string tool_name = 2;     // MCP tool name
  string args_json = 3;     // JSON-encoded arguments
  string call_id = 4;       // LLM-assigned call ID
  string result_json = 10;  // JSON-encoded result (on STATE_DONE)
  bool is_error = 11;       // Whether the call failed
}
```

## Error Handling

- **Connection failure**: Logged, session continues without MCP tools (non-fatal)
- **Tool call error**: Error fed back to LLM for self-correction (doesn't break loop)
- **Tool name conflict**: Fatal error during `Connect()` — the conflicting server is not connected

## Key Paths

| Path | Purpose |
|:---|:---|
| `~/.divmora/config/mcp_config.json` | Global MCP server config (shared across all agents) |
| `~/.divmora/config/settings.json` | Global settings (telemetry, trusted workspaces) |
