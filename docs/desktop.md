# Desktop Computer Use & Application Automation

LocalHarness provides cross-platform native desktop computer use capabilities without requiring external heavyweight CGo dependencies. When enabled, agents and specialized `desktop_subagent` instances can interact directly with native GUI applications and windows on the host operating system.

## Supported Platforms

Desktop automation works out-of-the-box across all three major desktop OSes using native OS subprocesses:

| Platform | Window Management | Screen Capture | Input Simulation | Dependencies |
|:---|:---|:---|:---|:---|
| **macOS** | JXA / AppleScript (`osascript`) | `/usr/sbin/screencapture` | System Events / `cliclick` | Built-in macOS utilities |
| **Linux (X11 & Wayland)** | `wmctrl` / `xdotool` | `grim` (Wayland), `maim` / `scrot` (X11) | `xdotool` (X11), `ydotool` (Wayland) | Standard distro packages |
| **Windows** | PowerShell Win32 / `AppActivate` | .NET `System.Drawing` / `CopyFromScreen` | `user32.dll` `mouse_event` / `SendKeys` | Built-in Windows PowerShell |

The implementation is 100% CGo-free, preserving static cross-compilation across all 6 target architectures.

## Quick Start

### CLI (`lhctl`)

```bash
# Enable native desktop automation tools
lhctl run --desktop --prompt "Take a screenshot of Slack and check for unread alerts"

# Run an interactive session with both browser and desktop capabilities
lhctl --browser --desktop
```

### SDK (Go)

```go
agent, _ := adk.NewAgent(&sdk.LocalAgentConfig{
    LitellmAPIKey: os.Getenv("LITELLM_API_KEY"),
    Capabilities: sdk.CapabilitiesConfig{
        Desktop: true,
    },
})
```

## Available Desktop Tools

When desktop capability is enabled, the agent has access to the following atomic tools and subagent:

| Tool | Type | Description |
|:---|:---|:---|
| `desktop_subagent` | Subagent | Spawns an autonomous background desktop agent with before/after visual screenshot artifacts and completion notifications |
| `desktop_screenshot` | Observation | Captures the screen or focused app window to `<brainDir>/scratch/` |
| `desktop_list_windows` | Inspection | Lists all visible application windows with process IDs and titles |
| `desktop_focus_window` | Action | Brings a target application or window to the foreground |
| `desktop_click` | Action | Simulates mouse clicks at specific `(x, y)` pixel coordinates |
| `desktop_type` | Action | Types text into the currently focused window with optional `Enter` key |
| `desktop_shortcut` | Action | Triggers modifier key combinations (e.g. `["cmd", "c"]`, `["ctrl", "v"]`, `["alt", "tab"]`) |

## Desktop Subagent Lifecycle

```
Parent Agent calls desktop_subagent(TaskName, Task, TargetApplication)
  → Engine verifies desktop capability on platform
  → Parent registers subagent in SubagentTracker
  → Background goroutine captures initial screen state
  → Child engine executes prompt with "Desktop Agent" role
  → Child verifies actions with intermediate/final screenshots
  → Subagent saves conversation state and emits completion notification
  → Parent engine receives notification via NotifySendCh and auto-wakes
```

## Security & Boundary Safeguards

- Mutating desktop tools (`desktop_click`, `desktop_type`, `desktop_shortcut`) are subject to the harness permission policy unless running in `--yolo` mode.
- Screenshots are automatically stored in the session's flat brain directory (`~/.divmora/localharness/brain/<conv-id>/scratch/`), allowing the agent to cite and embed them in markdown artifacts.
