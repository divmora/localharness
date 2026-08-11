# lhctl — LocalHarness CLI Debugger

`lhctl` is a lightweight, offline CLI tool for inspecting localharness conversation state.
It reads `.pb` files directly from disk — no WebSocket connection, API keys, or running harness needed.

## Installation

```bash
# From the repo
go build -o bin/lhctl ./cmd/lhctl/

# Or use the Makefile
make build-lhctl
```

## Commands

### `conversation list` (alias: `conv list`)

List all conversations sorted by most recent:

```bash
lhctl conversation list
lhctl conv list --recent=5    # Last 5 only
```

Output:
```
ID                                      Updated               Messages  Status    Agent             Size
──────────────────────────────────────────────────────────────────────────────────────────────────────
253aacfb-6bb2-42ad-86f8-a46da00bd159    2026-06-01T09:25:47          8  ACTIVE    root               17K
30aef7a0-0b0b-45ab-8966-dc302db27382    2026-06-01T09:25:38          4  ACTIVE    research (d1)       6K
cb94ce74-adae-4ae2-b8e6-e7c6f056bbaf    2026-05-30T07:33:14          8  ACTIVE    root               30K
```

The **Agent** column shows `root` for top-level conversations and `type (dN)` for subagents
(e.g., `research (d1)` = a research agent at depth 1).

### `conversation inspect` (alias: `conv inspect`)

Detailed message-level analysis of a conversation:

```bash
lhctl conv inspect e358                  # Overview with size analysis
lhctl conv inspect e358 --steps          # Full step trace with paths
lhctl conv inspect e358 --errors         # Only errors and policy denials
lhctl conv inspect e358 --step=1         # Deep-dive into a single step
lhctl conv inspect e358 --json           # JSON output for scripting
lhctl conv inspect e358 --top=5          # Show top 5 largest messages
```

For subagent conversations, inspect also shows a **🔗 Agent Lineage** section:

```
🔗 Agent Lineage:
  Parent:     253aacfb-6bb2-42ad-86f8-a46da00bd159
  Role:       File Analyzer
  Type:       research
  Depth:      1
```

#### Default output

- **Message table** with size, cumulative bytes, and tool call info
- **Large result warnings**: ⚠️ for >5KB, 🔴 LARGE for >20KB
- **Top N largest messages** — immediately shows which tool results bloat context
- **Context breakdown** — bytes by role (user/model/tool call/tool result)

#### `--steps` — Full tool args and error details

Shows full file paths (not basenames), error messages, and status indicators:

```
 #    Tool            Path/Args                                                   Size  Status
────────────────────────────────────────────────────────────────────────────────────────────────────
 0    (user prompt)                                                            1,934 B
 1    view_file       /home/user/.divmora/localharness/knowledge/bolt.md        133B
 2      └─ result                                                                  62B  ❌ ERROR
                      Error: Permission denied: Denied by policy 'workspace_only'.
 3    view_file       /home/user/project/.zenith/bolt.md                            68B
 4      └─ result                                                              3,860 B  ✅
```

#### `--step=N` — Deep-dive into a single step

Dumps full args and result content for one step:

```
Step 1 — model → view_file
────────────────────────────────────────────────────────────
  Role:      model
  Size:      133B
  Tool:      view_file
  Path:      /home/user/.divmora/localharness/knowledge/.../bolt.md

  Args:
    path:                /home/user/.divmora/localharness/knowledge/.../bolt.md
```

#### `--errors` — Error-only view

Quick filter for policy denials, tool failures, etc.:

```
 #    Tool            Path                                                Error
────────────────────────────────────────────────────────────────────────────────────────────────────
 2    view_file       /home/user/.divmora/localharness/knowledge/...    Permission denied: Denied by policy 'workspace_...'
 4    create_file     /home/user/project/.zenith/bolt.md                   Error: create_file: file already exists
```

#### `--json` — Machine-readable output

Each message includes `tool_args`, `error_text`, `is_error`, `full_path`, and `timestamp` fields:

```bash
# Find all errors
lhctl conv inspect <id> --json | jq '.messages[] | select(.is_error)'

# Find all file paths accessed
lhctl conv inspect <id> --json | jq '.messages[] | select(.full_path) | .full_path'

# Find messages over 10KB
lhctl conv inspect <id> --json | jq '.messages[] | select(.size_bytes > 10000)'
```

## Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--data-dir=<path>` | Override data directory | `~/.divmora/localharness/` |

## Debugging Workflows

### Finding token-heavy conversations

```bash
lhctl conv list
lhctl conv inspect <id>          # See size breakdown
lhctl conv inspect <id> --steps  # Find which tool calls are large
```

### Diagnosing policy denials

```bash
lhctl conv inspect <id> --errors    # See all denied/failed steps
lhctl conv inspect <id> --step=2    # Deep-dive into the denial
```

### Analyzing tool result bloat

The cumulative column in the inspect output shows how context grows message-by-message.
Look for sudden jumps — those are large tool results that could benefit from truncation.

## Data Sources

`lhctl` reads from these paths (all read-only):

| Path | Content |
|------|---------|
| `~/.divmora/localharness/conversations/<uuid>.pb` | `ConversationState` protobuf (root + subagents) |
| `~/.divmora/localharness/brain/<uuid>/` | Artifacts, traces, logs (flat — all agents at same level) |

### `conversation tree` (alias: `conv tree`)

Visualize the agent family tree for any conversation (works from root or child):

```bash
lhctl conv tree 253aacfb              # From root
lhctl conv tree 30aef7a0              # From child — walks up to find root
```

Output:
```
🌳 Agent Tree (root: 253aacfb)
────────────────────────────────────────────────────────────────────────────────
🤖 253aacfb [root] (8 msgs, ✅)
🔹 30aef7a0 [research: File Analyzer] (d1, 4 msgs, ✅) ◀
────────────────────────────────────────────────────────────────────────────────

Agents: 2 | Max depth: 1
```

The `◀` marker shows which conversation you queried. Multi-level trees render with box-drawing connectors.

## Debugging Workflows

### Inspecting subagent conversations

```bash
lhctl conv list --recent=10             # Spot subagents via Agent column
lhctl conv tree <any-id>                # See the full agent hierarchy
lhctl conv inspect <subagent-id>        # Inspect with lineage context
```

## Future Commands (Planned)

- `lhctl conversation traces <id>` — Show per-API-call trace breakdown
- `lhctl conversation export <id> --format=markdown` — Export as readable markdown

### `conversation trace` (alias: `conv trace`)

Real-time tool call timeline for a conversation:

```bash
lhctl conv trace <id>                    # Show tool call timeline
lhctl conv trace <id> --watch            # Live tail (updates as agent runs)
lhctl conv trace <id> --commands         # Show full command output
```

Output:
```
Conversation: 55084b1a-ad45-40cb-badb-c445358f9335
Steps: 26 | Duration: 2m 1s

 Step   Tool                       Detail                                              Latency
────────────────────────────────────────────────────────────────────────────────────────────────────
     1  🔎 find_file                pattern=*bolt.md                                    2.5s
     3  📄 view_file                …/fms/.zenith/bolt.md                               1.7s
     5  🔍 grep_search              "Cache::remember" in …/divmora/ifmists/fms        3.9s
    45  ✏️ replace_file_content     …/Console/Commands/GeneratePaoCheque…               5.5s
    47  ⚙️ run_command              php -l api/app/Console/Commands/Generate…           2.8s
    51  ✅ finish                   ✅                                                   3.1s
────────────────────────────────────────────────────────────────────────────────────────────────────

📊 26 API calls | 1 writes | total latency: 106.7s
   9× view_file, 7× grep_search, 5× run_command, 3× find_file, 1× replace_file_content, 1× finish
```

The trace reads from `~/.divmora/localharness/brain/<uuid>/.system_generated/traces/` which contains
per-step JSON files with request/response metadata, token usage, and latency.
