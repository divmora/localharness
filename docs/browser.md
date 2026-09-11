# Browser Automation (Playwright)

LocalHarness provides zero-config, built-in browser automation via [Playwright](https://playwright.dev/).
Whenever Node.js/`npx` is installed in PATH, browser capability (`browser_subagent` and `@playwright/mcp`) is **automatically enabled by default**. Furthermore, LocalHarness **autonomously decides headed vs. headless mode**:
- **Interactive Desktop Sessions** (macOS, Windows, or Linux with `$DISPLAY`/`$WAYLAND_DISPLAY`): The browser launches in **headed** mode with a visible browser window, allowing you to observe agent actions and solve logins/CAPTCHAs.
- **Detached Daemons / Headless Environments** (`--detach`, CI, headless servers): LocalHarness runs the browser in **headless** mode invisibly.
- **Explicit Overrides**: Pass `--headless` or `--headed` to force a specific mode, or `--no-browser` to disable browser tools entirely.

## Prerequisites

- **Node.js 18+** installed on the host
- **npx** available in PATH (bundled with Node.js)

Playwright browsers are auto-downloaded on first use by `@playwright/mcp`.

## Quick Start

### CLI (`lhctl`)

```bash
# Just run lhctl — browser capability and headed mode are automatically active!
lhctl run --prompt "Navigate to http://localhost:8080 and test the signup flow"

# Force headless execution on a desktop machine
lhctl run --headless --prompt "Run headless audit of https://example.com"

# Force disable browser tools
lhctl run --no-browser --prompt "Refactor internal Go code"

# Custom browser profile directory
lhctl run --browser-profile=~/.my-browser-profile --prompt "Access staging console"

# Ephemeral isolated mode (no state saved between sessions)
lhctl run --isolated --prompt "Run clean end-to-end checkout test"

# Connect to an existing running browser via Chrome DevTools Protocol (CDP)
lhctl run --connect-browser=http://localhost:9222 --prompt "Inspect current tab"
```

### Persistent Browser Profiles

By default, LocalHarness provisions a persistent Chromium user profile at `~/.divmora/localharness/browser_profile`.
- **Session Continuity**: Cookies, local storage, indexedDB, and session tokens persist across runs. Once you log in to an internal service, GitHub, or an authenticated portal, subsequent runs remain authenticated.
- **Custom Profile**: Use `--browser-profile <path>` to target an existing Chrome/Chromium profile or project-specific directory.
- **Clean-Slate Isolation**: Pass `--isolated` to run in memory without saving cookies or state across runs (ideal for automated CI tests).

### Vision & Element Bounding Boxes

The browser agent operates with full multimodal and visual grounding capabilities:
- **`--snapshot-boxes`**: Element snapshots include exact bounding box coordinates `[box=x,y,width,height]`.
- **`--caps=vision`**: Multimodal vision models can observe rendered page snapshots alongside accessibility tree refs.
- **Session Recordings**: Traces and action artifacts are automatically saved into the conversation brain artifacts folder (`~/.divmora/localharness/brain/<session-id>/artifacts/`).

### Human-in-the-Loop Handoff (2FA / CAPTCHA)

When navigating web applications protected by Cloudflare bot checks, CAPTCHAs, or 2FA/MFA:
1. The browser subagent detects the authentication barrier.
2. Instead of failing or endlessly looping, it triggers an `ask_question` prompt asking the user to solve the verification.
3. In headed mode (`--headed`), the user completes the challenge in the visible browser window, confirms in the terminal, and the subagent automatically resumes with the authenticated session intact.

### Browser Modes

The `browser_subagent` supports 4 operational modes:
- **`auto`** (default): Starts headless for speed; automatically escalates to headed or prompts user if captcha/auth challenges are encountered.
- **`headed`**: Launches a visible browser window so user can view interactions and intervene for 2FA/logins.
- **`headless`**: Runs entirely invisibly in the background.
- **`connect`**: Attaches to an already running Chromium/Chrome browser via CDP URL.

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
