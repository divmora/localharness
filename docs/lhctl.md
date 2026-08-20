# lhctl — LocalHarness CLI & Interactive TUI

`lhctl` is the official CLI and Interactive Terminal User Interface (TUI) for LocalHarness.
Powered by [Charm Bubbletea](https://github.com/charmbracelet/bubbletea), [Lipgloss](https://github.com/charmbracelet/lipgloss), and [Cobra](https://github.com/spf13/cobra), it supports interactive multi-agent chat with live token streaming, animated tool spinners, syntax highlighting, unified diff approvals, operational modes, background daemon management, multi-client attach/detach, and offline conversation inspection.

## Installation

```bash
# Build binary
make build-lhctl
# Binary created at bin/lhctl
```

---

## Interactive Terminal UI (TUI)

Launch the interactive chat interface:

```bash
# Start a fresh new interactive session (default)
lhctl

# Resume the most recent conversation (like agy -c)
lhctl -c

# Resume a specific conversation by full or partial ID
lhctl -c 0192a5b6-7c8d-7ef0-9123-456789abcdef
lhctl -c 0192a5b6

# Run explicitly with flags
lhctl run --model=gpt-4o --workspace=/path/to/project --yolo

# Launch background task and detach immediately
lhctl run --prompt="Analyze codebase for performance bottlenecks" --detach

# Attach to a running background daemon session
lhctl attach <session-id>
```

> **Resume on Exit**: Whenever you exit an interactive session (`/exit`, `/quit`, `Ctrl+C`, `Ctrl+D`), `lhctl` automatically displays the exact command to resume your conversation session (e.g. `lhctl -c <id>`).

### Command Flags (`lhctl` & `lhctl run`)

| Flag | Short | Description | Default |
|:---|:---|:---|:---|
| `--conversation` | `-c` | Resume an existing conversation by ID (or most recent if omitted) | Start fresh conversation |
| `--continue` | | Alias for `--conversation` | |
| `--resume` | | Alias for `--conversation` | |
| `--model` | `-m` | Target LLM model (e.g. `gpt-4o`, `claude-3-5-sonnet`) | Harness default |
| `--workspace` | `-w` | Attach workspace directory (repeatable) | Current working directory |
| `--yolo` | `-y` | Enable YOLO Mode (skip all permission prompts) | `false` |
| `--detach` | `-d` | Launch prompt in background daemon without blocking | `false` |
| `--prompt` | `-p` | Initial prompt to execute immediately | `""` |
| `--data-dir` | | Global data directory override | `~/.divmora/localharness/` |

---

## TUI Modes & Keybindings

### Operational Modes (`Shift+Tab` / `/mode`)

Press **`Shift+Tab`** (or type `/mode`) to cycle through the 3 operational modes:

| Mode | Status Badge | Description |
|:---|:---|:---|
| **`DEFAULT`** | `🛡️ DEFAULT` | **Safe Mode**: Reads inside workspaces/`~/.divmora` are auto-approved. File edits and shell commands require user approval with unified diff previews. |
| **`ACCEPT-EDITS`** | `⚡ ACCEPT-EDITS` | **Auto-Accept Edits**: All file modifications (`write_to_file`, `replace_file_content`) are auto-approved without prompts. Shell commands (`run_command`) still require confirmation. |
| **`PLAN`** | `📋 PLAN` | **Plan-Before-Act**: Instructs the agent to research and write `implementation_plan.md` in the brain directory before modifying code. Planning guard blocks direct code modifications until a plan exists. |

### Keyboard Shortcuts

| Shortcut | Action |
|:---|:---|
| **`Shift+Tab`** | Cycle operational modes (`DEFAULT` ➔ `ACCEPT-EDITS` ➔ `PLAN`) |
| **`/`** | Open instant **Slash Command Autocomplete** menu with description tooltips |
| **`@`** | Open **Workspace File Autocomplete** matching files across attached workspaces |
| **`↑` / `↓`** | Navigate autocomplete candidates (with wrap-around) |
| **`Tab` / `Enter`** | Accept autocomplete selection / Send prompt / Confirm approval |
| **`Ctrl+C`** | Interrupt/pause running turn (press twice within 2s to exit) |
| **`Ctrl+D`** | Detach TUI cleanly (agents continue in background daemon) |
| **`PgUp` / `PgDn`** | Scroll conversation viewport |
| **`Esc`** | Close modal overlay / dismiss autocomplete menu |

---

## Inline Approvals & Permission Scoping

When the agent executes a mutating tool (file edit or shell command) in `DEFAULT` mode, `lhctl` renders a non-blocking **inline approval card** directly in the interaction area with unified diff previews and scoped permission options:

| Key | Option | Description |
|:---|:---|:---|
| **`1` - `9`** | **Toggle Sub-command** | When a chained/piped command (`&&`, `||`, `;`, `\|`) has multiple parts, press the digit key to toggle individual sub-commands between `[✓]` (approved) and `[✗]` (denied). |
| **`y`** or **`Enter`** | **Allow selected** | Approves the selected sub-commands for single execution. |
| **`c`** or **`a`** | **Allow in conversation** | Grants permission for the selected sub-commands for the remainder of the active conversation session. |
| **`g`** | **Always allow globally** | Permanently saves the selected sub-commands to `~/.divmora/config/settings.json` (`allowedCommands`), auto-approving them across all future conversations. |
| **`n`** or **`Esc`** | **Deny** | Denies the tool call and feeds denial/partial approval feedback back to the model. |
| **`yolo`** | **YOLO Mode** | Auto-approves all pending and future tool calls in the session. |

When you partially approve (e.g. approving `go test ./...` while denying `go run main.go`), the approved sub-commands are registered in your session permissions, and the model receives clear feedback:
`User approved sub-commands: [go test ./...], but denied: [go run main.go]. Approved sub-commands have been granted permission; please execute approved commands individually if needed.`
The agent can then immediately execute the approved sub-command on its next step without prompting again!

---

## Interactive Question Cards (`ask_question`)

When the model calls the `ask_question` tool to clarify ambiguous requirements, `lhctl` renders a focused, sequential step-by-step interactive card in the bottom interaction area:

| Key | Action |
|:---|:---|
| **`1` - `9`** | Directly select or toggle the corresponding predefined option |
| **`o`** | Enter **Write-in Mode** to type a custom response not in the predefined choices |
| **`↑` / `k`**, **`↓` / `j`** | Move focus cursor (`❯`) up or down across options |
| **`Space`** | Toggle or select the focused option (`(●)` for single-select, `[✓]` for multi-select) |
| **`Enter`** | Log the current answer in chat history and advance to the next question (or submit on final question) |
| **`b`** or **`p`** | Go back to the previous question |
| **`s`** or **`Esc`** | Skip the question prompt |

---

## Slash Commands in TUI

| Command | Description |
|:---|:---|
| `/help` | Display command catalog and keyboard shortcuts |
| `/mode [name]` | Switch mode: `default`, `accept-edits`, `plan` (or `Shift+Tab`) |
| `/plan [goal]` | Research codebase and create `implementation_plan.md` before coding |
| `/teamwork [goal]` | Coordinate a team of parallel autonomous subagents to solve a complex task |
| `/pause` | Gracefully pause the active agent turn (`Ctrl+C`) |
| `/resume [msg]` | Resume execution with optional updated instructions |
| `/model [name]` | View or switch active LLM model target |
| `/compact` | Trigger context window compaction |
| `/status` | Show daemon status, active subagents, running tasks, and token counters |
| `/subagents` | View subagent hierarchy & drill down into subagent transcripts |
| `/tasks`, `/ps` | View running background tasks, shell commands, timers, and live output |
| `/workspace list` | List all attached workspace roots |
| `/workspace add <dir>` | Dynamically attach a workspace directory with trust check |
| `/workspace remove <dir>` | Detach a workspace directory |
| `/yolo` | Toggle YOLO Mode on/off (bypass all approval queues) |
| `/detach` | Detach TUI while agent runs in background |
| `/clear` | Clear the chat history viewport |
| `/exit`, `/quit` | Exit the TUI session |

---

## Custom User Slash Commands (`.agents/commands/*.md`)

You can create project-level or global custom slash commands using Markdown template files:

### Discovery Paths:
- **Workspace**: `<workspace>/.agents/commands/*.md` (or `.agent/commands/*.md`)
- **Global**: `~/.divmora/commands/*.md` (or `~/.divmora/localharness/commands/*.md`)

### Example: `.agents/commands/review.md`
```markdown
---
description: Perform a comprehensive code review
argument_placeholder: "[branch-or-file]"
---
Please perform a thorough code review on {{args}}.
Focus on:
1. Potential bugs and edge cases
2. Performance bottlenecks
3. Adherence to project architecture and coding rules in AGENTS.md
```

### Features:
- **Autocomplete**: As soon as you type `/`, all custom commands automatically appear in the completion menu tagged with `[workspace]` or `[custom]`.
- **Parameter Interpolation**:
  - `{{args}}` or `$*` is replaced with all passed arguments.
  - Positional variables `$1`, `$2`, `{{1}}`, `{{2}}` are supported.
  - If no placeholder exists in the template, trailing arguments are automatically appended.
- **Help Menu**: All discovered custom commands are listed under the **🛠️ CUSTOM COMMANDS** section in `/help`.

---

## Daemon Runtime Management

Manage the persistent background daemon runtime:

```bash
# Start background daemon
lhctl daemon start

# Check daemon process status (PID, port, uptime)
lhctl daemon status

# Stop background daemon
lhctl daemon stop
```

---

## Offline Conversation Debugger

`lhctl` functions offline to inspect `.pb` state files directly from disk without connecting to a running daemon.

### `conversation list` (alias: `conv list`)

List all recorded conversations:

```bash
lhctl conversation list
lhctl conv list --recent=5    # Last 5 only
```

### `conversation inspect` (alias: `conv inspect`)

Detailed message-level analysis of a conversation:

```bash
lhctl conv inspect <id>                  # Overview with size analysis
lhctl conv inspect <id> --steps          # Full step trace with paths
lhctl conv inspect <id> --errors         # Only errors and policy denials
lhctl conv inspect <id> --step=1         # Deep-dive into a single step
lhctl conv inspect <id> --json           # JSON output for scripting
lhctl conv inspect <id> --top=5          # Show top 5 largest messages
```

### `conversation tree` (alias: `conv tree`)

Visualize the subagent hierarchy tree:

```bash
lhctl conv tree <id>
```

### `conversation trace` (alias: `conv trace`)

Real-time tool call timeline:

```bash
lhctl conv trace <id>                    # Show tool call timeline
lhctl conv trace <id> --watch            # Live tail (updates as agent runs)
lhctl conv trace <id> --commands         # Show full command lines
```

---

## Shell Autocompletion

Generate autocompletion scripts for your shell:

```bash
# Bash
lhctl completion bash > /etc/bash_completion.d/lhctl

# Zsh
lhctl completion zsh > ~/.zshrc.d/lhctl.zsh

# Fish
lhctl completion fish > ~/.config/fish/completions/lhctl.fish

# PowerShell
lhctl completion powershell | Out-String | Invoke-Expression
```
