# Conversation Summaries (Cross-Conversation Context)

**Status**: Future (not yet implemented)  
**Priority**: Low — P2 parity item with Antigravity  
**Depends on**: `ProjectRegistry` (shipped), `conversation.Manager` (shipped)  
**Version bump**: `feat:` → MINOR  
**Files to modify**: `conversation.go`, `message_context.go`, `engine.go`, `session.go`  
**Estimated effort**: Medium (2-3 hours)

---

## Antigravity Reference (Exact Format)

This is the exact output Antigravity injects into every user message, observed
from a live session on 2026-05-27. It appears **after** `<ADDITIONAL_METADATA>`
and **before** `<USER_REQUEST>`:

```markdown
# Conversation History
Here are the conversation IDs, titles, and summaries of your most recent 1 conversations, in reverse chronological order:

<conversation_summaries>
## Conversation 0ae66d50-34c5-4861-aff1-1a86889bed58: Testing Bolt Performance Harness
- Created: 2026-05-27T09:10:29Z
- Last modified: 2026-05-27T13:33:19Z

</conversation_summaries>
```

### Format Details

| Element | Exact format | Notes |
|:---|:---|:---|
| **Heading** | `# Conversation History` (markdown H1) | Always present when summaries exist |
| **Preamble** | `Here are the conversation IDs, titles, and summaries of your most recent N conversations, in reverse chronological order:` | `N` matches actual count |
| **Tag** | `<conversation_summaries>` / `</conversation_summaries>` | XML-style wrapping |
| **Entry heading** | `## Conversation <full-uuid>: <title>` | Full UUID, not truncated |
| **Created** | `- Created: <RFC3339>` | UTC timestamp |
| **Last modified** | `- Last modified: <RFC3339>` | UTC timestamp |
| **Separator** | Empty line between entries | Blank line after each entry |
| **Title style** | Title-case, descriptive | Appears LLM-generated, not raw user text |

### Ordering in Per-Message Context

Based on the system prompt analysis, conversation summaries appear in this
position within the enriched user message:

```
<user_information>...</user_information>
<user_rules>...</user_rules>
<skills>...</skills>
<plugins>...</plugins>
<artifacts>...</artifacts>
<slash_commands>...</slash_commands>
<knowledge_items>...</knowledge_items>       (LH extra)
<subagents>...</subagents>                   (LH extra)
<ADDITIONAL_METADATA>...</ADDITIONAL_METADATA>
<USER_SETTINGS_CHANGE>...</USER_SETTINGS_CHANGE>
# Conversation History                       ← HERE
<conversation_summaries>...</conversation_summaries>
<EPHEMERAL_MESSAGE>...</EPHEMERAL_MESSAGE>
<SYSTEM_MESSAGE>...</SYSTEM_MESSAGE>         (LH extra)
<USER_REQUEST>...</USER_REQUEST>
```

### Key Behavioral Observations

1. **Project-scoped** — only shows conversations from the same project/workspace
2. **Limited window** — "most recent N" (observed N=1 and N=2 in sessions)
3. **Lightweight** — only title + timestamps, no actual content/summary text
4. **Titles are likely LLM-generated** — "Testing Bolt Performance Harness" and
   "Refining Conversation Transcript Hierarchy" read like generated summaries,
   not raw first-message text
5. **Always present** — appears even with just 1 prior conversation
6. **Current conversation excluded** — only shows *other* conversations
7. **No content body** — despite the word "summaries" in the preamble, only
   title + timestamps are shown (no multi-line summary)

---

## Motivation

Without conversation summaries, the agent has no awareness of previous sessions:

| Use Case | Without Summaries | With Summaries |
|:---|:---|:---|
| Continue work | Agent doesn't know prior context exists | "Continue from conversation X" |
| Avoid repetition | May redo analysis from a prior session | Sees what was already done |
| Cross-reference | Cannot link related conversations | Can reference by ID/title |
| Project continuity | Each session is isolated | Agent sees project history |

---

## Existing Infrastructure (What's Already Built)

### ProjectRegistry (`internal/engine/project_registry.go`)

Maps workspace paths to stable project UUIDs:

```go
type Project struct {
    ID         string   `json:"id"`
    Name       string   `json:"name"`
    Workspaces []string `json:"workspaces"`
    CreatedAt  time.Time `json:"created_at"`
}
```

Key methods:
- `FindOrCreate(workspacePaths []string) (*Project, error)` — used at session init
- `FindByWorkspace(workspacePath string) *Project`

Storage: `<appDataDir>/projects.json`

### Conversation Manager (`internal/conversation/conversation.go`)

Manages conversation lifecycle:

```go
type Manager struct {
    appDataDir       string
    conversationsDir string // appDataDir/conversations/
    brainDir         string // appDataDir/brain/
}
```

Key methods:
- `Create(cfg) (*Conversation, error)` — creates UUID + .pb + brain dirs
- `Resume(id string) (*Conversation, error)` — loads from .pb
- `List() ([]string, error)` — lists all conversation UUIDs (scans .pb files)

### ConversationState Proto (`proto/localharness/v1/localharness.proto` L978-1013)

```proto
message ConversationState {
    string conversation_id = 1;
    string created_at = 2;            // RFC3339
    string updated_at = 3;            // RFC3339
    string harness_version = 4;
    HarnessConfig config = 5;         // Contains workspaces for project resolution
    repeated ConversationMessage messages = 6;  // First user message = title source
    UsageMetadata total_usage = 7;
    int32 trajectory_count = 8;
    int32 step_count = 9;
    int32 compaction_count = 11;
    ConversationStatus status = 10;
}
```

### Session Wiring (`internal/server/session.go` L405-446)

Project resolution already happens at init:

```go
// session.go L405-409
projectRegistry := engine.NewProjectRegistry(appDataDir)
if err := projectRegistry.Load(); err != nil {
    s.logger.Warn("failed to load project registry", "error", err)
}
```

The `projectID` is resolved via `projectRegistry.FindOrCreate(workspaceDirs)`.

### MessageContextConfig (`internal/engine/message_context.go` L17-77)

The config struct where `ConversationSummaries` will be added. Current fields
include `ConversationID`, `AppDataDir`, `BrainDir`, `ProjectID`, `Workspaces`,
`UserRules`, `HostContext`, `PendingMessages`, `EphemeralMessages`, etc.

### EnrichUserMessage (`internal/engine/message_context.go` L124-380)

The rendering function. The new conversation summaries block should be inserted
**after** `<ADDITIONAL_METADATA>` / `<USER_SETTINGS_CHANGE>` and **before**
`<EPHEMERAL_MESSAGE>`.

---

## Proposed Architecture

### Per-Project Index Files

```
~/.divmora/localharness/
├── conversations/
│   ├── <uuid>.pb                          # Full ConversationState (heavy, ~5-50KB)
│   └── index/
│       ├── <project-uuid-A>.json          # [{id, title, created_at, updated_at}, ...]
│       └── <project-uuid-B>.json          # Typically <50KB per project
```

### Why Per-Project Index

| Approach | Perf at 100K | Concurrent Safety | Complexity |
|:---|:---|:---|:---|
| **Per-project index** ✅ | O(1) read, <1ms | Atomic rename | Low |
| `.meta.json` sidecars | O(N) stat(), 10-30s | Perfect (atomic) | Low |
| Single `index.json` | O(1) read, <1ms | Write conflicts | Low |
| SQLite | O(1) query | Built-in locking | High (new dep) |
| Scan `.pb` files | O(N) deserialize, minutes | N/A | None |

### Performance at Scale

| Total conversations | Per-project (typical) | Index file size | Read + parse time |
|:---|:---|:---|:---|
| 1K | ~50 entries | ~5KB | <1ms |
| 10K | ~200 entries | ~20KB | <1ms |
| 100K | ~500 entries | ~50KB | <1ms |
| 1M | ~1000 entries | ~100KB | ~2ms |

---

## Data Schema

### ConversationMeta (Go struct — `conversation.go`)

```go
// ConversationMeta is a lightweight index entry for a conversation.
// Stored in conversations/index/<project-id>.json as a JSON array.
type ConversationMeta struct {
    ID        string `json:"id"`          // Conversation UUID
    Title     string `json:"title"`       // Derived from first user message (truncated)
    ProjectID string `json:"project_id"`  // Owning project UUID
    CreatedAt string `json:"created_at"`  // RFC3339 UTC
    UpdatedAt string `json:"updated_at"`  // RFC3339 UTC
}
```

### Index File Format (`conversations/index/<project-id>.json`)

```json
[
  {
    "id": "cba73c8d-66d0-48f1-ac9c-00549e97270c",
    "title": "System prompt parity analysis and refactor",
    "project_id": "a1b2c3d4-...",
    "created_at": "2026-05-27T14:38:46Z",
    "updated_at": "2026-05-27T19:41:00Z"
  },
  {
    "id": "0ae66d50-34c5-4861-aff1-1a86889bed58",
    "title": "Testing Bolt Performance Harness",
    "project_id": "a1b2c3d4-...",
    "created_at": "2026-05-27T09:10:29Z",
    "updated_at": "2026-05-27T13:33:19Z"
  }
]
```

### ConversationSummary (Go struct — `message_context.go`)

```go
// ConversationSummary is a lightweight summary of a past conversation,
// injected into per-message context for cross-conversation awareness.
type ConversationSummary struct {
    ID        string // Conversation UUID
    Title     string // Human-readable title
    CreatedAt string // RFC3339
    UpdatedAt string // RFC3339
}
```

---

## Title Extraction Strategy

### Phase 1: Truncated First User Message (Ship First)

```go
func extractTitle(messages []*pb.ConversationMessage) string {
    for _, m := range messages {
        if m.Role == "user" && m.Content != "" {
            title := m.Content
            // Remove XML tags that EnrichUserMessage prepends
            // (the stored message may contain <USER_REQUEST> wrapping)
            if idx := strings.Index(title, "<USER_REQUEST>"); idx != -1 {
                // Extract just the user's text from inside the tag
                start := idx + len("<USER_REQUEST>\n")
                end := strings.Index(title, "</USER_REQUEST>")
                if end > start {
                    title = title[start:end]
                }
            }
            title = strings.TrimSpace(title)
            if len(title) > 80 {
                // Truncate at word boundary
                title = title[:77]
                if lastSpace := strings.LastIndex(title, " "); lastSpace > 40 {
                    title = title[:lastSpace]
                }
                title += "..."
            }
            return title
        }
    }
    return "Untitled conversation"
}
```

### Phase 2: LLM-Generated Titles (Future Enhancement)

Antigravity titles like "Testing Bolt Performance Harness" appear to be
LLM-generated. This could be done as a background post-processing step
using a cheap/fast model (e.g., Gemini Flash) after the first exchange:

```
Prompt: "Generate a concise 3-6 word title for this conversation.
First user message: {message}
Title:"
```

This is a separate enhancement and NOT required for parity.

---

## Implementation Plan

### Step 1: Index Infrastructure (`internal/conversation/conversation.go`)

Add after the existing `List()` method (~L150):

```go
// indexDir returns the path to the conversation index directory.
func (m *Manager) indexDir() string {
    return filepath.Join(m.conversationsDir, "index")
}

// indexPath returns the path to a project's conversation index file.
func (m *Manager) indexPath(projectID string) string {
    return filepath.Join(m.indexDir(), projectID+".json")
}

// SaveMeta updates the per-project conversation index with metadata
// for the given conversation. Creates the index directory if needed.
func (m *Manager) SaveMeta(conv *Conversation, projectID string) error {
    if projectID == "" {
        return nil // No project context — skip indexing
    }

    // Ensure index directory exists
    if err := os.MkdirAll(m.indexDir(), 0755); err != nil {
        return fmt.Errorf("conversation index: mkdir: %w", err)
    }

    // Load existing index
    indexPath := m.indexPath(projectID)
    var entries []ConversationMeta
    if data, err := os.ReadFile(indexPath); err == nil {
        json.Unmarshal(data, &entries) // Ignore parse errors — rebuild
    }

    // Build updated entry
    meta := ConversationMeta{
        ID:        conv.ID,
        Title:     extractTitle(conv.State.Messages),
        ProjectID: projectID,
        CreatedAt: conv.State.CreatedAt,
        UpdatedAt: conv.State.UpdatedAt,
    }

    // Update or append
    found := false
    for i, e := range entries {
        if e.ID == conv.ID {
            entries[i] = meta
            found = true
            break
        }
    }
    if !found {
        entries = append(entries, meta)
    }

    // Atomic write
    data, err := json.MarshalIndent(entries, "", "  ")
    if err != nil {
        return fmt.Errorf("conversation index: marshal: %w", err)
    }
    tmpPath := indexPath + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0644); err != nil {
        return fmt.Errorf("conversation index: write: %w", err)
    }
    return os.Rename(tmpPath, indexPath)
}

// ListRecentByProject returns the most recent conversations for a project,
// excluding the given conversation ID (typically the current one).
// Results are sorted by updated_at descending.
func (m *Manager) ListRecentByProject(projectID, excludeConvID string, limit int) ([]ConversationMeta, error) {
    if projectID == "" {
        return nil, nil
    }

    data, err := os.ReadFile(m.indexPath(projectID))
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // No index yet
        }
        return nil, err
    }

    var entries []ConversationMeta
    if err := json.Unmarshal(data, &entries); err != nil {
        return nil, nil // Corrupt index — return empty, will be rebuilt
    }

    // Filter out current conversation
    var filtered []ConversationMeta
    for _, e := range entries {
        if e.ID != excludeConvID {
            filtered = append(filtered, e)
        }
    }

    // Sort by updated_at descending
    sort.Slice(filtered, func(i, j int) bool {
        return filtered[i].UpdatedAt > filtered[j].UpdatedAt
    })

    // Limit
    if len(filtered) > limit {
        filtered = filtered[:limit]
    }

    return filtered, nil
}

// RebuildProjectIndex rebuilds the index for a specific project by scanning
// all .pb files. Used for recovery and migration.
func (m *Manager) RebuildProjectIndex(projectID string, workspacePaths []string) error {
    ids, err := m.List()
    if err != nil {
        return err
    }

    var entries []ConversationMeta
    for _, id := range ids {
        conv, err := m.Resume(id)
        if err != nil {
            continue // Skip corrupt conversations
        }
        // Check if conversation belongs to this project (workspace overlap)
        if conv.State.Config != nil && hasWorkspaceOverlap(conv.State.Config.Workspaces, workspacePaths) {
            entries = append(entries, ConversationMeta{
                ID:        conv.ID,
                Title:     extractTitle(conv.State.Messages),
                ProjectID: projectID,
                CreatedAt: conv.State.CreatedAt,
                UpdatedAt: conv.State.UpdatedAt,
            })
        }
    }

    // Write index
    if err := os.MkdirAll(m.indexDir(), 0755); err != nil {
        return err
    }
    data, err := json.MarshalIndent(entries, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(m.indexPath(projectID), data, 0644)
}
```

### Step 2: Message Context Types (`internal/engine/message_context.go`)

Add to `MessageContextConfig` (after `SettingsChanges` field, ~L76):

```go
// ConversationSummaries are recent conversations from the same project,
// injected as a # Conversation History block for cross-conversation awareness.
// Populated at session init from the per-project conversation index.
ConversationSummaries []ConversationSummary
```

Add struct definition (after `SettingsChange` struct):

```go
// ConversationSummary is a lightweight summary of a past conversation.
type ConversationSummary struct {
    ID        string
    Title     string
    CreatedAt string // RFC3339
    UpdatedAt string // RFC3339
}
```

### Step 3: Rendering (`internal/engine/message_context.go`)

Insert in `EnrichUserMessage()` **after** the `<USER_SETTINGS_CHANGE>` loop
and **before** the `<EPHEMERAL_MESSAGE>` loop:

```go
// # Conversation History + <conversation_summaries> — recent project conversations
if len(cfg.ConversationSummaries) > 0 {
    fmt.Fprintf(&b, "# Conversation History\nHere are the conversation IDs, titles, and summaries of your most recent %d conversations, in reverse chronological order:\n\n", len(cfg.ConversationSummaries))
    b.WriteString("<conversation_summaries>\n")
    for _, cs := range cfg.ConversationSummaries {
        fmt.Fprintf(&b, "## Conversation %s: %s\n", cs.ID, cs.Title)
        fmt.Fprintf(&b, "- Created: %s\n", cs.CreatedAt)
        fmt.Fprintf(&b, "- Last modified: %s\n", cs.UpdatedAt)
        b.WriteString("\n")
    }
    b.WriteString("</conversation_summaries>\n")
}
```

### Step 4: Engine Config (`internal/engine/engine.go`)

Add to `Config` struct (after `WorkspaceInfos`, ~L123):

```go
ConversationSummaries []ConversationSummary // Recent project conversations for cross-session context
```

Pass through in `NewEngine()` where `msgCtx` is populated (~L259):

```go
ConversationSummaries: cfg.ConversationSummaries,
```

### Step 5: Session Wiring (`internal/server/session.go`)

At session init, after project resolution (~L409), before `engine.NewEngine()`:

```go
// Load recent conversation summaries for cross-session context
var convSummaries []engine.ConversationSummary
if projectID != "" {
    metas, err := convMgr.ListRecentByProject(projectID, s.conv.ID, 5)
    if err != nil {
        s.logger.Warn("failed to load conversation summaries", "error", err)
    }
    for _, m := range metas {
        convSummaries = append(convSummaries, engine.ConversationSummary{
            ID:        m.ID,
            Title:     m.Title,
            CreatedAt: m.CreatedAt,
            UpdatedAt: m.UpdatedAt,
        })
    }
}
```

Add `ConversationSummaries: convSummaries` to `engine.Config{...}` (~L446).

In `handleUserMessage()` and `cleanup()`, call `SaveMeta()` alongside `SaveAll()`:

```go
// session.go handleUserMessage() after SaveAll() (~L515)
if projectID != "" {
    convMgr.SaveMeta(s.conv, projectID)
}

// session.go cleanup() (~L196)
if s.conv != nil {
    s.conv.SaveAll()
    if projectID != "" {
        convMgr.SaveMeta(s.conv, projectID)
    }
}
```

**Note**: `projectID` and `convMgr` need to be stored on the `Session` struct
as fields so they are accessible in `cleanup()` and `handleUserMessage()`.

### Step 6: Migration & CLI

Add a `localharness index rebuild` subcommand (or auto-detect on first run):

```go
// If index directory doesn't exist but conversations do, auto-rebuild
if _, err := os.Stat(convMgr.IndexDir()); os.IsNotExist(err) {
    ids, _ := convMgr.List()
    if len(ids) > 0 {
        s.logger.Info("rebuilding conversation index", "conversations", len(ids))
        convMgr.RebuildProjectIndex(projectID, workspaceDirs)
    }
}
```

---

## Concurrent Write Safety

- **Atomic rename**: `os.WriteFile(tmp) + os.Rename(tmp, target)`
- **Worst case**: Two sessions for same project save simultaneously →
  one update "lost" (last writer wins, other entry is stale)
- **Recovery**: `.pb` files are source of truth. `RebuildProjectIndex()`
  reconciles from `.pb` state at any time
- **Acceptable**: This is a metadata cache. Stale entries cause at most
  a slightly outdated title or timestamp — no data loss

---

## Proto Changes

**None required.** The conversation index is a Go-only concern with no
wire protocol changes. All needed data already exists:

| Data needed | Source |
|:---|:---|
| Conversation ID | `ConversationState.conversation_id` |
| Created/Updated timestamps | `ConversationState.created_at` / `updated_at` |
| Title source (first user message) | `ConversationState.messages[0]` where `role == "user"` |
| Project scoping | `ConversationState.config.workspaces` → `ProjectRegistry` |

---

## Test Plan

### Unit Tests (`conversation_test.go`)

```go
func TestSaveMeta(t *testing.T)               // Creates index, writes entry
func TestSaveMeta_Update(t *testing.T)         // Updates existing entry on re-save
func TestListRecentByProject(t *testing.T)     // Returns sorted, limited results
func TestListRecentByProject_ExcludesSelf(t *testing.T) // Excludes current conv
func TestListRecentByProject_Empty(t *testing.T)         // No index file yet
func TestExtractTitle(t *testing.T)            // Truncation, word boundary, edge cases
func TestRebuildProjectIndex(t *testing.T)     // Rebuilds from .pb files
```

### Integration Test (`message_context_test.go`)

```go
func TestEnrichUserMessage_ConversationSummaries(t *testing.T)
// Verify:
// - "# Conversation History" header present
// - <conversation_summaries> tags present
// - Each entry has ## Conversation <id>: <title>
// - Created/Last modified lines present
// - Empty summaries = no block injected
```

### Manual Verification

```bash
go run ./cmd/testclient --api-key=$KEY --prompt "What conversations have I had recently?"
# Agent should reference the conversation summaries from the injected context
```
