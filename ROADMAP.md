# LocalHarness Product Roadmap

This document serves as the **living product roadmap** for LocalHarness.
- **Adding Items**: Whenever a new capability, enhancement, or architectural improvement is identified for the future, add it here under the appropriate category.
- **Removing Items**: Once a feature is fully implemented, verified with tests, and committed, **remove it from this roadmap**.

---

## 1. Triggers & Reactive Execution

- [ ] **Filesystem Watcher Triggers (`OnFileChange`)**
  - Implement real-time filesystem watching to allow auto-reactive agent loops triggered on file creation, deletion, or modification within watched directories.
- [ ] **Interval & Cron Triggers (`Every(duration, fn)`)**
  - Add native interval and recurring cron triggers within the harness daemon for scheduled audits, periodic syncs, and background agent routines.
- [ ] **Crash-Resilient Subagent Reconciliation**
  - Sweep and reconcile orphaned subagent processes on daemon restart, marking interrupted subagents cleanly in conversation state and enabling checkpoint-based resumption.

---

## 2. Protocol & Wire Format

- [ ] **JSON Wire Format Debug Option**
  - Provide a toggle for JSON wire format alongside Protobuf for simplified debugging, curl-friendly testing, and third-party WebSocket integrations.
- [ ] **Multimedia User Messages & Multimodal Vision Pipeline**
  - Support multi-part user messages including image, audio, and binary inputs across pipe and WebSocket transports.
  - Convert screenshot artifacts from `desktop_subagent`, `desktop_screenshot`, and `browser_subagent` into base64 `image_url` data parts in `llm.Message` so vision models (GPT-4o, Claude 3.5 Sonnet, Gemini 2.0 Flash) can directly inspect screen layout and visual UI elements.
- [ ] **ToolContext Injection**
  - Pass structured `ToolContext` into host tool executors to give tools direct access to current session ID, turn counters, and conversation state.
- [ ] **Pre-Flight Context Budgeting & Token Overflow Protection**
  - Implement proactive context window estimation before invoking `Generate()`. If estimated tokens exceed the model context window minus safety margin, trigger immediate emergency compaction to prevent unrecoverable `400 context_length_exceeded` API errors.

---

## 3. Tooling & Extensions

- [ ] **Image Generation Tool**
  - Add native image generation tool integrating with Imagen / Gemini multimodal capabilities.
- [ ] **Full MCP Protocol Coverage (Resources & Prompts)**
  - Implement `resources/list`, `resources/read`, and `prompts/get` in `internal/mcp/manager.go` to expose external context sources (database schemas, PR templates, tickets) directly to agents.
- [ ] **Fault-Tolerant MCP Server Connections**
  - Support `optional: true` configurations and per-server connection timeouts in the MCP manager, ensuring a single slow or unreachable external MCP server does not abort session initialization.
- [ ] **Jupyter Notebook Editing Tool (`notebook_edit`)**
  - Add cell-level read, write, and execute capabilities for `.ipynb` notebook files.
- [ ] **Visual Grounding & Accessibility Tree Targeting for Desktop**
  - Expose native accessibility APIs (macOS Accessibility, Windows UI Automation, Linux AT-SPI) to enable coordinate-free semantic desktop targeting by label, icon description, or role.
- [ ] **HiDPI / Retina Coordinate Normalization**
  - Automatically detect OS display scaling factor (macOS 2x Retina, Windows 125%/150% DPI) and normalize visual click coordinates between screenshot pixels and OS mouse point space.
- [ ] **Desktop Session Video Recording**
  - Continuous lightweight screen and window recording saved as MP4/WebM artifacts for desktop subagent trajectories.
- [ ] **Active Window & Browser Control Indicators (Agent HUD)**
  - **Browser In-Page HUD & Glow Perimeter**: Inject a non-intrusive floating badge (`● 🤖 LocalHarness Agent Active`) and glowing viewport perimeter border via Playwright `--init-script` so agent-controlled browser windows are immediately identifiable without obstructing user/agent interactions (`pointer-events: none`).
  - **Desktop Window OS Notifications & TUI Indicators**: Native OS notification banners (macOS `display notification`, Linux `notify-send`, Windows toast) and terminal HUD badges indicating when the agent focuses or takes control of an application window.

---

## 4. Developer Experience & Security

- [ ] **Cross-Platform Shell Resolver for Windows**
  - Detect availability of `bash`, and gracefully fallback to `powershell.exe`, `pwsh`, or `cmd.exe` in `run_command` and `task_manager`, preventing execution failures on Windows environments where Git Bash is not in `%PATH%`.
- [ ] **URL & Domain Allowlist Policy Engine**
  - **Default-Deny Network Boundary**: Implement a pure allowlist security model (supporting wildcards like `*.github.com`) across all web-facing tools (`read_url_content`, web search, browser). Any unlisted domain triggers an interactive prompt or is blocked.
  - **Tiered Local Scoping**: Support global (`~/.divmora/config/settings.json`), project-committed (`<workspace>/.agents/settings.json`), and gitignored local approvals (`<workspace>/.agents/settings.local.json`) to prevent Git merge conflicts.
  - **Playwright Origin Enforcement**: Pass allowed domains directly to `@playwright/mcp` via `--allowed-origins` flag.
  - **Configuration & CLI**: Interactive prompts ("Always allow", "Allow once") automatically save to `settings.local.json`, with CLI flag `--allow-domain` and management command `lhctl config allow-domain`. Pre-initialized with `localhost` and `127.0.0.1`.
- [ ] **Auto-Run Trust Policy Engine**
  - Implement configurable auto-run policies based on workspace trust state in `~/.divmora/config/settings.json`.
- [ ] **Interactive REPL Auto-Upgrade Policies**
  - Dynamic permission escalation workflows for interactive TUI / CLI sessions.

---

## 5. Performance & Scalability

- [ ] **Adaptive AST & Knowledge Cache Eviction**
  - Implement LRU-based memory capping and eviction for in-memory AST codegraph nodes and persistent knowledge items in massive multi-workspace repositories.

---

## 6. Terminal User Interface (`lhctl` TUI) & Desktop GUI

- [ ] **Collapsible Streaming Tool Execution Cards**
  - Interactive foldable tool blocks with live spinner indicators, execution duration timers, and collapsible stdout/stderr stream viewers.
- [ ] **Rich Syntax-Highlighted Markdown & Inline Diffs**
  - Terminal-rendered markdown tables, syntax-highlighted code snippets via Chroma, and formatted red/green diff views for file modifications.
- [ ] **Dedicated Full-Screen Tabbed Views (F1–F5)**
  - Keyboard-driven tab switching between `Chat` (F1), `Subagents DAG Tree` (F2), `Background Tasks & Terminals` (F3), `Artifacts Viewer` (F4), and `Active Windows / Desktop & Browser HUD` (F5).
- [ ] **Interactive Permission & Domain Approval Modals**
  - Keyboard-navigable modal dialogs for tool approvals and domain allowlist additions with `[y] Allow Once`, `[A] Always Allow (Persist)`, and `[n] Deny`.
- [ ] **Prompt History & Fuzzy Reverse Search (`Ctrl+R`)**
  - Multi-session persistent prompt history with Up/Down navigation and interactive reverse history search.
- [ ] **Smooth Mouse Wheel & Touchpad Viewport Scrolling**
  - Native mouse event capture for effortless viewport scrolling without taking focus away from the input prompt.
- [ ] **Deep Link URL Protocol Handler**
  - Register `localharness://` custom URI scheme to launch sessions and attach to background daemons directly from browsers.
- [ ] **Interactive Subagent Execution Graph**
  - Real-time DAG visualizer in the desktop GUI displaying active subagents, message flows, and token consumption metrics.
- [ ] **Desktop Bundle Optimization**
  - Slim down bundled Tauri assets and reduce startup latency across macOS, Linux, and Windows.

---

## 7. Agent Integrations

- [ ] **Agent Auto-Discovery**
  - Automatically discover and build agent submodules under `agents/` without manual `go build` commands.
- [ ] **Agent Version Matrix**
  - Track and validate ADK version compatibility for each integrated agent submodule.

---

## 8. Voice & Realtime Audio Interaction

- [ ] **Full-Duplex Realtime Voice Agent Mode (`lhctl voice` / Live API)**
  - Bidirectional low-latency audio streaming via WebRTC / WebSocket connecting directly to Gemini 2.0 Flash Multimodal Live API or OpenAI Realtime API for natural, hands-free conversational pair-programming with interruption handling.
- [ ] **Wake-Word & Ambient Background Voice Trigger**
  - Optional lightweight local wake-word listener (e.g., "Hey Harness") running in the background daemon to trigger agent execution without manually focusing terminal windows.

