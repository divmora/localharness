# Integrated Agents

The `agents/` directory contains specialized AI agent submodules built on the LocalHarness ADK. Each agent is a standalone Go program that uses the [LocalHarness ADK](../adk/) to run a localharness agent session with a domain-specific system prompt and tool configuration.

## Available Agents

### Jules — Codebase Analysis

| Property | Value |
|:---|:---|
| **Path** | `agents/jules` |
| **Repo** | [divmora/jules-ai-agent](https://github.com/divmora/jules-ai-agent) |
| **Focus** | General codebase analysis — performance, design, security, maintainability |
| **ADK Version** | `v0.2.1` |

Jules provides four specialized agents that run a single analysis pass against a workspace:

| Agent | Flag | Domain |
|:---|:---|:---|
| ⚡ Bolt | `--agent bolt` | Performance issues |
| 🎨 Palette | `--agent palette` | Design & UX issues |
| 🛡️ Sentinel | `--agent sentinel` | Security vulnerabilities |
| 🧹 Sweeper | `--agent sweeper` | Technical debt & maintainability |

**Build & Run:**
```bash
cd agents/jules && go build -o ../../bin/jules . && cd ../..
./bin/jules --agent sentinel --workspace /path/to/project --prompt "Audit for security vulnerabilities"
```

Each agent maintains a memory journal in `.jules/<agent>.md` within the target workspace to recall past findings and avoid repeating pitfalls.

See the [Jules README](https://github.com/divmora/jules-ai-agent#readme) for full documentation.

---

## Working with Submodules

### Initial Checkout

Agent submodules are checked out automatically with:
```bash
git clone --recursive https://github.com/divmora/localharness.git
```

If you already cloned without submodules:
```bash
git submodule update --init --recursive
```

### Updating an Agent Submodule

To pull the latest changes for a specific agent:
```bash
cd agents/jules && git pull origin main && cd ../..
# or
git submodule update --remote agents/jules
```

Then commit the updated submodule pointer:
```bash
git add agents/jules
git commit -m "chore: update jules submodule to latest main"
```

### Adding a New Agent Submodule

To integrate a new ADK-powered agent:
```bash
git submodule add -b main https://github.com/divmora/<new-agent> agents/<new-agent>
```

Then:
1. Add the agent to the table in [README.md](../README.md#integrated-agents)
2. Add a section to this doc page
3. Commit with a `feat:` prefix (triggers MINOR bump via release-please)
