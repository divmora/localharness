# Zenith

AI-powered codebase improvement agent built on [LocalHarness](../../README.md).

Zenith runs focused, single-purpose agents (called **personas**) that scan your codebase, find one improvement, fix it, verify it, and present the results — all autonomously.

## Personas

| Persona | Domain | Output | Description |
|:---|:---|:---|:---|
| `bolt` | ⚡ Performance | `.zenith/bolt.md` | Finds and fixes performance bottlenecks |
| `sentinel` | 🛡️ Security | `.zenith/sentinel.md` | Finds and fixes security vulnerabilities |
| `palette` | 🎨 UX/A11y | `.zenith/palette.md` | Finds and fixes accessibility/UX issues |
| `hawk` | 🦅 Code Review | `.zenith/hawk/` | Produces actionable code review reports |

## Quick Start

```bash
# Set your API key
export LITELLM_API_KEY=your-key

# Build
go build -o bin/zenith ./agents/zenith/

# Run
bin/zenith bolt                                    # Performance scan (cwd)
bin/zenith --workspace=/path/to/project sentinel   # Security scan
bin/zenith palette "Scan the login page"           # Custom prompt
bin/zenith hawk "Review src/services/auth.go"      # Code review report
```

## How It Works

Each persona follows the same workflow:

```
Read Journal → Replay Known Patterns → Scan for New Issues → Fix One → Verify → Present → Update Journal
```

### Journal System

Zenith maintains a `.zenith/` directory in each workspace with per-persona journals:

```
your-project/
├── .zenith/
│   ├── bolt.md       # Performance learnings
│   ├── sentinel.md   # Security learnings
│   ├── palette.md    # UX/a11y learnings
│   ├── hawk.md       # Review ledger (what was reviewed & when)
│   └── hawk/         # Review reports
│       ├── src--services--auth_review.md
│       └── src--handlers--payment_review.md
└── ...
```

**Journals persist learnings across runs.** Each run:
1. Reads the journal for known anti-patterns
2. Scans the codebase for remaining instances of those patterns
3. Fixes the first match found (or hunts for new issues if all are resolved)
4. Writes new learnings back to the journal

**SKIP entries** let you mark intentional patterns that Zenith should not try to fix:

```markdown
## SKIP - get()->count()
**Reason:** Intentional — needed for legacy API compatibility
**Files:**
- api/app/Http/Controllers/LoginCtrl.php
- api/app/Http/Controllers/OldApiCtrl.php
```

## Performance

Typical bolt run on a production codebase:

| Metric | Value |
|:---|---:|
| API Calls | ~26-38 |
| Duration | ~2-8 min |
| Total Tokens | ~1-2.4M |
| Cache Hit | ~78% |
| Estimated Cost | ~$0.03-$0.07 |

## CLI Reference

```
zenith [flags] <persona> [prompt]

  Note: Flags must come BEFORE the persona name.

Flags:
  --workspace=<path>   Workspace directory (default: cwd)
  --endpoint=<name>    LiteLLM endpoint name (default: from litellm.json)
  --model=<name>       Model name (default: endpoint-specific)
  --base-url=<url>     Base URL override
  --api-key=<key>      API key override
  --config=<path>      Path to YAML config file (overrides auto-discovery)
  --verbose, -v        Enable verbose debug logging

Environment:
  LITELLM_API_KEY      Global LiteLLM API key if not defined in endpoint config
  LOCALHARNESS_BIN     Optional. Path to localharness binary.
```

## Configuration

Zenith loads config from YAML files with the following priority:

```
CLI flags > env vars > --config file > .zenith/config.yml (workspace) > ~/.divmora/agents/zenith/config.yml (global)
```

Copy `config.example.yml` to get started:

```bash
# Per-workspace config
cp agents/zenith/config.example.yml /path/to/project/.zenith/config.yml

# Or global config
mkdir -p ~/.divmora/agents/zenith
cp agents/zenith/config.example.yml ~/.divmora/agents/zenith/config.yml
```

### Config Schema

```yaml
# ── Global Defaults ──────────────────────────────────────────────
endpoint: cloudflare-llama           # Endpoint name defined in ~/.divmora/config/litellm.json
model: @cf/meta/llama-3-8b-instruct  # Model override
base_url: https://gateway.ai.cloudflare.com/... # Optional base url override
api_key: sk-...                      # Optional api key override
workspace: /path/to/project          # Default workspace directory

# ── Per-Persona Overrides ────────────────────────────────────────
# Each persona inherits global defaults. Override only what differs.
personas:
  bolt:
    model: gemini-2.5-flash         # Fast model for performance scanning
    endpoint: google-vertex         # Switch endpoint
  sentinel:
    endpoint: openai-prod           # Use OpenAI endpoint for security analysis
    model: gpt-4o
  palette:
    model: gemini-2.5-pro           # Bigger model for UX reasoning
```

> **Note:** API keys are always read from environment variables — never put keys in config files.

### Examples

```bash
# Use defaults from config
zenith bolt

# Override model for one run
zenith --model=gemini-2.5-pro bolt

# Use a completely different endpoint
zenith --endpoint=openai-prod --model=gpt-4o sentinel

# Use local Ollama
zenith --endpoint=local-ollama --model=llama3 bolt
```

## Development

```bash
# Build from source
go build -o bin/zenith ./agents/zenith/

# Run with local harness binary
LOCALHARNESS_BIN=./bin/localharness bin/zenith --workspace=/path/to/project bolt

# Monitor with lhctl
bin/lhctl conv trace <conversation-id>
```
