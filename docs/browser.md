# Browser Automation (Playwright)

LocalHarness provides built-in browser automation via [Playwright](https://playwright.dev/). When enabled, the agent can navigate websites, interact with UI elements, take screenshots, and verify web applications — powered by the official `@playwright/mcp` server.

## Prerequisites

- **Node.js 18+** installed on the host
- **npx** available in PATH (comes with Node.js)

Playwright browsers are auto-downloaded on first use by `@playwright/mcp`.

## Quick Start

### SDK (Go)

```go
agent, _ := adk.NewAgent(&sdk.LocalAgentConfig{
    LitellmAPIKey: os.Getenv("LITELLM_API_KEY"),
    Capabilities: sdk.CapabilitiesConfig{
        // ... other capabilities ...
        Browser: true,
    },
})
```

That's it. The engine auto-injects `@playwright/mcp` as an MCP server.

### Manual MCP Config (Alternative)

If you need custom Playwright configuration, you can configure it manually via MCP:

```go
cfg.McpServers = []sdk.McpServer{
    {
        Name:    "playwright",
        Command: "npx",
        Args:    []string{"-y", "@playwright/mcp@latest", "--headless"},
    },
}
```

When a manual `playwright` MCP server is configured, the auto-injection is skipped.

## Available Browser Tools

The `@playwright/mcp` server exposes these tools:

### Navigation & Tabs

| Tool | Description |
|:---|:---|
| `browser_navigate` | Navigate to a URL |
| `browser_go_back` | Go back in browser history |
| `browser_go_forward` | Go forward in browser history |
| `browser_tab_list` | List all open tabs |
| `browser_tab_new` | Open a new tab |
| `browser_tab_select` | Switch to a specific tab |
| `browser_tab_close` | Close a tab |

### Interaction

| Tool | Description |
|:---|:---|
| `browser_click` | Click an element (by accessibility ref) |
| `browser_type` | Type text into an input field |
| `browser_select_option` | Select an option from a dropdown |
| `browser_hover` | Hover over an element |
| `browser_drag` | Drag an element to a target |

### Observation

| Tool | Description |
|:---|:---|
| `browser_snapshot` | Get the page's accessibility tree (text representation) |
| `browser_screenshot` | Capture a screenshot of the current page |
| `browser_console_messages` | Get browser console output |
| `browser_network_requests` | Get network request log |

### Forms & Input

| Tool | Description |
|:---|:---|
| `browser_fill` | Fill a form field |
| `browser_press_key` | Press a keyboard key |
| `browser_file_upload` | Upload a file |

## How It Works

```
Agent calls browser_navigate("https://example.com")
  → Engine recognizes it as an MCP tool
  → MCP Manager routes to the "playwright" server session
  → @playwright/mcp subprocess opens Chromium, navigates
  → Returns accessibility snapshot (structured text)
  → Result flows back to LLM as tool output
```

### Accessibility-First

The Playwright MCP server uses the browser's **accessibility tree** rather than screenshots or vision models. This is:

- **Efficient** — small text payloads instead of large images
- **Deterministic** — no vision model hallucinations
- **Fast** — no image encoding/decoding overhead
- **Cost-effective** — uses text tokens, not image tokens

The agent "sees" the page as structured elements (buttons, links, inputs, headings) with ref IDs, then interacts by referencing those IDs.

## Error Handling

| Scenario | Behavior |
|:---|:---|
| Node.js not installed | Warning logged, browser tools not available |
| `npx` not in PATH | Warning logged, agent continues without browser |
| Playwright install fails | MCP connection error (non-fatal), logged as warning |
| User has manual `playwright` MCP config | Auto-injection skipped, user config used |

## Proto Reference

```protobuf
message BuiltinToolsConfig {
  // ... other fields ...
  bool browser = 14;  // default: false (requires Node.js + npx)
}
```

## Key Files

| File | Purpose |
|:---|:---|
| `internal/server/session.go` | Auto-injection logic for Playwright MCP server |
| `proto/localharness/v1/localharness.proto` | `browser` field in `BuiltinToolsConfig` |
| `sdk/types.go` | `Browser` field in `CapabilitiesConfig` |
| `sdk/agent.go` | Maps `Browser` to proto in `buildHarnessConfig()` |
