// Example: Auto-Discovery of skills and plugins.
//
// This example demonstrates filesystem-based auto-discovery. Instead of
// explicitly passing skills/plugins in config, they are placed in:
//
//   - Global:    <appDataDir>/skills/<name>/SKILL.md
//   - Global:    <appDataDir>/plugins/<name>/plugin.json
//   - Workspace: <workspace>/.agents/skills/<name>/SKILL.md
//   - Workspace: <workspace>/.agents/plugins/<name>/plugin.json
//
// The engine discovers them at session init and merges with any
// ADK-injected definitions. Priority: SDK > workspace > global.
//
// This example sets up a temporary workspace directory with sample
// skills and plugins to demonstrate the auto-discovery flow.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/auto-discovery --api-key $LITELLM_API_KEY
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	prompt := flag.String("prompt", "What skills and plugins do you have? List them all.", "Prompt for the agent")
	flag.Parse()

	// --- Set up a temporary workspace with .agents/ structure ---
	workspaceDir, err := os.MkdirTemp("", "harness-autodiscovery-*")
	if err != nil {
		log.Fatalf("Failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(workspaceDir)

	// Create a workspace skill: .agents/skills/code-review/SKILL.md
	skillDir := filepath.Join(workspaceDir, ".agents", "skills", "code-review")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		log.Fatal(err)
	}
	skillMD := `---
name: code-review
description: >
  Review code for bugs, performance issues, and best practice violations.
  Provides actionable feedback with severity ratings.
---

# Code Review Skill

When asked to review code, follow these steps:

1. Read the target file(s)
2. Identify issues in these categories:
   - **Bugs**: Logic errors, race conditions, null checks
   - **Performance**: Inefficient algorithms, unnecessary allocations
   - **Style**: Naming, formatting, documentation
3. Rate each issue: Critical / Warning / Info
4. Provide fix suggestions with code snippets
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		log.Fatal(err)
	}

	// Create a workspace plugin: .agents/plugins/testing/plugin.json + skills/
	pluginDir := filepath.Join(workspaceDir, ".agents", "plugins", "testing")
	pluginSkillDir := filepath.Join(pluginDir, "skills", "generate-tests")
	if err := os.MkdirAll(pluginSkillDir, 0755); err != nil {
		log.Fatal(err)
	}
	pluginJSON := `{"name":"testing","description":"Test generation and coverage analysis tools."}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		log.Fatal(err)
	}
	pluginSkillMD := `---
name: generate-tests
description: Generate unit tests for Go functions with table-driven test patterns.
---

# Generate Tests

Generate comprehensive unit tests following Go conventions:
- Use table-driven tests
- Cover edge cases and error paths
- Use t.Helper() for helper functions
`
	if err := os.WriteFile(filepath.Join(pluginSkillDir, "SKILL.md"), []byte(pluginSkillMD), 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Workspace directory: %s\n", workspaceDir)
	fmt.Println("Created:")
	fmt.Println("  .agents/skills/code-review/SKILL.md")
	fmt.Println("  .agents/plugins/testing/plugin.json")
	fmt.Println("  .agents/plugins/testing/skills/generate-tests/SKILL.md")
	fmt.Println()

	// --- Configure agent with workspace ---
	cfg := adk.NewLocalAgentConfig()
	cfg.Verbose = true
	cfg.Workspaces = []adk.WorkspaceDef{{Directory: workspaceDir}}

	// No explicit Skills/Plugins in config —
	// the engine auto-discovers from .agents/ at session init!

	cfg.Policies = []policy.Policy{policy.AllowAll()}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Println("Agent started — skills/plugins were auto-discovered!")
	fmt.Println("Conversation ID:", agent.ConversationID())
	fmt.Println("---")

	resp, err := agent.Chat(ctx, *prompt)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println("\n--- Response ---")
	fmt.Println(resp.Text)
	fmt.Printf("\nSteps taken: %d\n", len(resp.Steps))

	if resp.Usage != nil {
		fmt.Printf("Tokens: prompt=%d, completion=%d, total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
}
