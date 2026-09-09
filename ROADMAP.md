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

---

## 2. Protocol & Wire Format

- [ ] **JSON Wire Format Debug Option**
  - Provide a toggle for JSON wire format alongside Protobuf for simplified debugging, curl-friendly testing, and third-party WebSocket integrations.
- [ ] **Multimedia User Messages**
  - Support multi-part user messages including image, audio, and binary inputs across pipe and WebSocket transports.
- [ ] **ToolContext Injection**
  - Pass structured `ToolContext` into host tool executors to give tools direct access to current session ID, turn counters, and conversation state.

---

## 3. Tooling & Extensions

- [ ] **Image Generation Tool**
  - Add native image generation tool integrating with Imagen / Gemini multimodal capabilities.
- [ ] **MCP Resource Access**
  - Implement `list_resources` and `read_resource` tools to expose Model Context Protocol (MCP) server resources directly to LLM agents.
- [ ] **Jupyter Notebook Editing Tool (`notebook_edit`)**
  - Add cell-level read, write, and execute capabilities for `.ipynb` notebook files.
- [ ] **Programmatic Workspace Management Tool**
  - Add tool for dynamically creating, switching, and pruning workspace contexts within long-running sessions.

---

## 4. Developer Experience & Security

- [ ] **Shell Injection & Dangerous Command Detection**
  - Static pattern analysis (`isDangerousBinary()`, `isDangerousSubcommand()`) before executing shell commands via `run_command`.
- [ ] **Auto-Run Trust Policy Engine**
  - Implement configurable auto-run policies based on workspace trust state in `~/.divmora/config/settings.json`.
- [ ] **Interactive REPL Auto-Upgrade Policies**
  - Dynamic permission escalation workflows for interactive TUI / CLI sessions.

---

## 5. Desktop GUI (`gui/`) & Visual Tooling

- [ ] **Deep Link URL Protocol Handler**
  - Register `localharness://` custom URI scheme to launch sessions and attach to background daemons directly from browsers.
- [ ] **Interactive Subagent Execution Graph**
  - Real-time DAG visualizer in the desktop GUI displaying active subagents, message flows, and token consumption metrics.
- [ ] **Desktop Bundle Optimization**
  - Slim down bundled Tauri assets and reduce startup latency across macOS, Linux, and Windows.

---

## 6. Agent Integrations

- [ ] **Agent Auto-Discovery**
  - Automatically discover and build agent submodules under `agents/` without manual `go build` commands.
- [ ] **Agent Version Matrix**
  - Track and validate ADK version compatibility for each integrated agent submodule.
