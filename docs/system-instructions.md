# Structured System Instructions

The localharness supports modular system prompt composition through **Structured System Instructions**. Instead of providing a single raw system prompt string, you can compose the prompt from tagged sections that the engine assembles automatically.

## How It Works

There are two layers:

### Layer 1: System Prompt (set at init, cacheable)

The engine builds the system prompt from XML-tagged sections ordered by priority:

```
<identity>
You are DevBot, a DevOps assistant.
</identity>
<ephemeral_message>
...how to handle system-injected messages...
</ephemeral_message>
<artifacts>
...artifact conventions, naming, formatting...
</artifacts>
<planning_mode>
...research → plan → approve → execute → verify...
</planning_mode>
<planning_mode_artifacts>
...task.md, implementation_plan.md, walkthrough.md templates...
</planning_mode_artifacts>
<guidelines>
...behavioral rules for file operations...
</guidelines>
<tools>
You have access to the following tools:
- view_file: View file contents
- edit_file: Edit an existing file
...
</tools>
<security_rules>
Never expose secrets in logs.
</security_rules>
<user_guidelines>
Always write tests. Keep functions small.
</user_guidelines>
```

**Section priority table** (lower = earlier in prompt):

| Priority | Tag | Source | Always Present? |
|:---|:---|:---|:---|
| 0 | `<identity>` | Default or `StructuredPrompt.Identity` | Yes |
| 3 | `<web_application_development>` | `EnableWebDevelopment` | Opt-in |
| 5 | `<ephemeral_message>` | Built-in | Yes |
| 6 | `<skills>` | `Skills` data | Data-driven |
| 7 | `<plugins>` | `Plugins` data | Data-driven |
| 8 | `<subagents>` | `SubagentsEnabled` + `SubagentTypes` | Opt-in |
| 9 | `<messaging>` | Built-in | Yes |
| 10 | `<conversation_transcript>` | Built-in | Yes |
| 11 | `<artifacts>` | Built-in | Yes |
| 12 | `<slash_commands>` | `EnableSlashCommands` + `SlashCommands` | Opt-in |
| 13 | `<knowledge_items>` | `EnableKnowledgeItems` | Opt-in |
| 14 | `<planning_mode>` | `EnablePlanningMode` | Opt-in |
| 15 | `<planning_mode_artifacts>` | `EnablePlanningMode` | Opt-in |
| 16 | `<guidelines>` | Built-in safety rules | Yes |
| 17 | `<communication_style_defaults>` | Built-in formatting rules | Yes |
| 40 | `<tools>` | Auto-generated from tool schemas | Yes (if tools exist) |
| 60 | `<user_instructions>` | `SystemInstructions` or `StructuredPrompt` | If provided |
| 70 | `<user_guidelines>` | `StructuredPrompt.Guidelines` | If provided |
| 80 | `<communication_style>` | `StructuredPrompt.CommunicationStyle` | If provided |
| 100+ | Custom sections | `StructuredPrompt.Sections[]` | If provided |

### Layer 2: Per-Message Context (enriched per user message)

The engine automatically prepends dynamic context to each user message:

```
<user_information>
The USER's OS version is linux.
The user has 1 active workspaces, each defined by a URI and a CorpusName. Multiple URIs
potentially map to the same CorpusName. The mapping is shown as follows in the format
[URI] -> [CorpusName]:
/home/user/project -> org/project
Code relating to the user's requests should be written in the locations listed above.
Avoid writing project code files to tmp, in the .gemini dir, or directly to the Desktop
and similar folders unless explicitly asked.
App Data Directory: /home/user/.divmora/localharness
Conversation ID: abc-123
Brain Directory: /home/user/.divmora/localharness/brain/abc-123
</user_information>
<user_rules>
The following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any following instructions.
Review them carefully and always take them into account when you generate responses and code:
<RULE[AGENTS.md]>
# AGENTS.md
Keep docs in sync.
</RULE[AGENTS.md]>
</user_rules>
<skills>
Available skills:
- run-scanner (/path/to/SKILL.md): Scan files for vulnerabilities
</skills>
<plugins>
Available plugins:
# securecoder (/path/to/plugin)
Skills:
- scan (/path/to/scan/SKILL.md): Run security scanner

</plugins>
<artifacts>
Artifact Directory Path: /home/user/.divmora/localharness/brain/abc-123
Existing artifacts:
- plan.md
- task.md
</artifacts>
<knowledge_items>
# Knowledge Items (KI) Summaries

The following KIs are available. Read relevant artifacts with view_file before starting research.

## error-handling-patterns
Summary: Established patterns for error wrapping and sentinel errors.
Artifacts:
- /data/knowledge/uuid/error-handling-patterns/artifacts/overview.md

## api-conventions ⚠️ Potentially outdated (references changed since last update)
Summary: Public API naming conventions.
Artifacts:
- /data/knowledge/uuid/api-conventions/artifacts/api_surface.md

</knowledge_items>
<ADDITIONAL_METADATA>
Active Document: /home/user/project/main.go
Cursor Line: 42
Open Files:
- /home/user/project/main.go
- /home/user/project/test.go
</ADDITIONAL_METADATA>
<USER_REQUEST>
fix the bug in the handler
</USER_REQUEST>
```

## Prompt Modules

Optional domain-specific system prompt sections. All are **OFF by default**.

### Planning Mode (`EnablePlanningMode`)

Teaches the agent a plan-before-act workflow:

1. **Research** — investigate the codebase
2. **Plan** — create `implementation_plan.md` artifact
3. **Approve** — wait for user approval
4. **Execute** — work through `task.md` checklist
5. **Verify** — run tests, create `walkthrough.md`

Includes detailed templates for all three planning artifacts.

### Web Development (`EnableWebDevelopment`)

Adds design aesthetics, CSS/JS patterns, SEO guidance, and implementation workflow for web applications.

### Slash Commands (`EnableSlashCommands`)

Teaches the agent to recommend user-facing chat shortcuts (e.g., `/goal`, `/schedule`). The agent cannot execute slash commands itself — it suggests them when appropriate.

Requires `SlashCommands` definitions to be provided in the config.

### Knowledge Items (`EnableKnowledgeItems`)

Teaches the agent to check curated Knowledge Item (KI) summaries before starting research. Prevents redundant work and ensures adherence to established patterns.

Ideal for IDE agents with a KI store.

### Communication Style Defaults (always present)

Provides baseline response formatting rules (conciseness, markdown, file links, task references). Always present at priority 14. The user's `StructuredPrompt.CommunicationStyle` (priority 80) supplements these defaults with tone/personality preferences.

### Skills (data-driven)

If `Skills` definitions are provided, the `<skills>` section appears in both the system prompt (with guidance text + available list) and per-message context (available list only).

Teaches the agent what skills are (instruction folders with `SKILL.md`), how to discover them, and instructs it to `view_file` the SKILL.md before using any skill. **No toggle needed** — the presence of skill data IS the toggle.

### Plugins (data-driven)

If `Plugins` definitions are provided, the `<plugins>` section appears in both the system prompt (with guidance text + available list) and per-message context (available list only).

Teaches the agent what plugins are (bundles of skills + subagents + config, with `plugin.json`). Each plugin lists its exposed skills. **No toggle needed** — the presence of plugin data IS the toggle.

## Auto-Discovery

Skills and plugins are auto-discovered from the filesystem at session init. Three sources are scanned and merged:

### Discovery Sources

| Priority | Source | Skill Path | Plugin Path |
|:---|:---|:---|:---|
| 1 (highest) | **ADK-injected** | `config.Skills` | `config.Plugins` |
| 2 | **Workspace** | `<workspace>/.agents/skills/<name>/SKILL.md` | `<workspace>/.agents/plugins/<name>/plugin.json` |
| 3 (lowest) | **Global** | `<appDataDir>/skills/<name>/SKILL.md` | `<appDataDir>/plugins/<name>/plugin.json` |

Deduplication: if two skills (or plugins) share the same `name`, the higher-priority source wins.

### SKILL.md Format

```yaml
---
name: run-security-scanner
description: >
  Scan files for common security issues like XSS and SQL injection.
---

# Full instructions follow (read by agent via view_file)
```

### plugin.json Format

```json
{
  "name": "securecoder",
  "description": "Security analysis and remediation.",
  "disabled": false
}
```

If `disabled: true`, the plugin is skipped. Skills within a plugin are auto-discovered from its `skills/` subdirectory.

### Directory Layout Example

```
~/.divmora/localharness/           # appDataDir (global)
├── plugins/
│   └── securecoder/
│       ├── plugin.json
│       └── skills/
│           └── scan/SKILL.md
└── skills/
    └── custom-lint/SKILL.md

/path/to/project/                    # workspace
├── .agents/
│   ├── plugins/
│   │   └── project-plugin/
│   │       ├── plugin.json
│   │       └── skills/
│   └── skills/
│       └── project-lint/SKILL.md
└── AGENTS.md
```

## SDK Usage

### Basic (raw string — backward compatible)

```go
cfg := adk.NewLocalAgentConfig()
cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")
cfg.SystemInstructions = "You are a helpful assistant."
```

### Structured Prompt

```go
cfg := adk.NewLocalAgentConfig()
cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")
cfg.StructuredPrompt = &sdk.StructuredPrompt{
    Identity:           "You are DevBot, a DevOps assistant.",
    Guidelines:         "Always write tests. Keep functions small.",
    CommunicationStyle: "Be concise. Use bullet points.",
    Sections: []sdk.PromptSection{
        {Tag: "security_rules", Content: "Never expose secrets.", Priority: 30},
        {Tag: "project_context", Content: "Go microservice using gRPC.", Priority: 50},
    },
}
```

### With Prompt Modules

```go
cfg := adk.NewLocalAgentConfig()
cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")
cfg.EnablePlanningMode = true    // plan-before-act workflow
cfg.EnableWebDevelopment = true  // web design aesthetics
cfg.EnableSlashCommands = true   // slash command recommendations
cfg.SlashCommands = []sdk.SlashCommand{
    {Name: "/goal", Description: "Run a long-running task with extra thoroughness"},
    {Name: "/schedule", Description: "Set up a recurring schedule or timer"},
}
cfg.EnableKnowledgeItems = true  // KI check-before-research
cfg.Skills = []sdk.SkillDef{
    {Name: "run-scanner", Description: "Scan files for vulnerabilities", SkillPath: "/skills/scanner/SKILL.md"},
}
cfg.Plugins = []sdk.PluginDef{
    {
        Name: "securecoder",
        Path: "/plugins/securecoder",
        Skills: []sdk.SkillDef{
            {Name: "scan", Description: "Run scanner", SkillPath: "/plugins/securecoder/skills/scan/SKILL.md"},
        },
    },
}
```

### Chat with Host Context

```go
resp, err := agent.ChatWithContext(ctx, "fix the bug", &adk.MessageContext{
    ActiveFile: "/path/to/main.go",
    CursorLine: 42,
    OpenFiles:  []string{"/path/to/main.go", "/path/to/test.go"},
    EphemeralMessages: []string{
        "The user has the security scanner active. Prioritize security fixes.",
    },
})
```

## User Rules

User rules are injected into the `<user_rules>` section of every user message. There are two sources:

### Auto-discovered (AGENTS.md)

The engine automatically discovers and loads `AGENTS.md` (or `.agents.md`) files from workspace root directories. Each file's content is wrapped in `<RULE[filename]>` tags.

When multiple workspaces provide the same filename (e.g. both have `AGENTS.md`), the tag includes the workspace basename for disambiguation:

```xml
<RULE[project-a/AGENTS.md]>
...rules from project-a...
</RULE[project-a/AGENTS.md]>
<RULE[project-b/AGENTS.md]>
...rules from project-b...
</RULE[project-b/AGENTS.md]>
```

### ADK-injected

SDKs can inject rules via `UserRules` in the config. These are rendered identically but use the `label` as the tag name:

```go
cfg.UserRules = []sdk.UserRule{
    {Label: "team-standards", Content: "Always write tests. Use conventional commits."},
    {Label: "settings.json", Content: "Respond concisely. Prefer Go."},
}
```

SDK rules appear first (higher priority), followed by auto-discovered AGENTS.md rules. All rules are wrapped with a preamble directing the model to treat them as mandatory.

## Proto Reference

```protobuf
message StructuredSystemInstructions {
  string identity = 1;
  string guidelines = 2;
  string communication_style = 3;
  repeated SystemSection sections = 4;
}

message SystemSection {
  string tag = 1;
  string content = 2;
  int32 priority = 3;
}

message PromptModules {
  bool enable_web_development = 1;
  bool enable_planning = 2;
  bool enable_slash_commands = 3;
  bool enable_knowledge_items = 4;
}

message SlashCommandDef {
  string name = 1;
  string description = 2;
}

message UserRuleConfig {
  string label = 1;
  string content = 2;
}

message UserMessage {
  string content = 1;
  string conversation_id = 2;
  UserContext context = 3;
  repeated string ephemeral_messages = 4;
}

message UserContext {
  string active_file = 1;
  int32 cursor_line = 2;
  repeated string open_files = 3;
  map<string, string> extra = 4;
}
```

## Ephemeral Messages

Ephemeral messages are per-turn directives injected by the SDK/host that the agent must follow strictly but never acknowledge to the user. They are rendered as `<EPHEMERAL_MESSAGE>` blocks in the enriched user prompt.

### Sources

Ephemeral messages can come from:

1. **SDK/Host** — passed via `MessageContext.EphemeralMessages` in the SDK, which maps to `UserMessage.ephemeral_messages` in the proto.
2. **Engine-internal** — generated by the engine itself (e.g., the planning guard injects "You are in Planning Mode" directives).

Both sources are merged before the enriched prompt is built. ADK-injected messages are consumed (cleared) after each turn.

### Examples

```go
// ADK-injected ephemeral message
resp, err := agent.ChatWithContext(ctx, "refactor the handler", &adk.MessageContext{
    EphemeralMessages: []string{
        "The user has the security scanner panel open. Prioritize security best practices.",
        "Respond in French.",
    },
})
```

Common use cases:
- **Context signals**: "The user is viewing the git diff panel" — nudges the agent toward reviewing recent changes.
- **Behavioral overrides**: "Respond in a specific language" or "Be extra verbose for this turn."
- **Feature gates**: "The user has the premium tier. Enable advanced analysis."
- **Safety directives**: "Do not modify files outside /src/ for this turn."
