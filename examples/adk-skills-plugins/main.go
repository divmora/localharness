// Example: Skills and Plugins agent.
//
// Demonstrates two approaches to providing skills and plugins:
//
// 1. ADK-injected: Explicitly pass skills/plugins via config (shown in this example)
// 2. Auto-discovered: Place skills/plugins in filesystem directories (see below)
//
// Auto-discovery directories:
//
//	Global:    ~/.divmora/localharness/skills/<name>/SKILL.md
//	Global:    ~/.divmora/localharness/plugins/<name>/plugin.json
//	Workspace: <workspace>/.agents/skills/<name>/SKILL.md
//	Workspace: <workspace>/.agents/plugins/<name>/plugin.json
//
// The engine merges all sources (SDK > workspace > global, dedup by name).
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-skills-plugins --api-key $LITELLM_API_KEY
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	prompt := flag.String("prompt", "What skills and plugins do you have available?", "Prompt for the agent")
	flag.Parse()

	cfg := adk.NewLocalAgentConfig()
	cfg.Verbose = true

	// === ADK-injected Skills ===
	// These are standalone skills the agent can use. Each skill has a SKILL.md
	// that the agent reads via view_file to learn the full instructions.
	cfg.Skills = []adk.SkillDef{
		{
			Name:        "code-review",
			Description: "Review code for bugs, performance issues, and best practices.",
			SkillPath:   "/path/to/skills/code-review/SKILL.md",
		},
		{
			Name:        "db-migration",
			Description: "Generate database migration scripts from schema changes.",
			SkillPath:   "/path/to/skills/db-migration/SKILL.md",
		},
	}

	// === ADK-injected Plugins ===
	// Plugins are bundles that group skills, subagents, and config together.
	// Each plugin has a plugin.json metadata file and a skills/ directory.
	cfg.Plugins = []adk.PluginDef{
		{
			Name:        "securecoder",
			Description: "Security analysis, vulnerability scanning, and remediation.",
			Path:        "/path/to/plugins/securecoder",
			Skills: []adk.SkillDef{
				{
					Name:        "run-security-scanner",
					Description: "Scan source files for common security vulnerabilities.",
					SkillPath:   "/path/to/plugins/securecoder/skills/run-scanner/SKILL.md",
				},
				{
					Name:        "generate-audit-report",
					Description: "Generate a security audit report after code generation.",
					SkillPath:   "/path/to/plugins/securecoder/skills/audit-report/SKILL.md",
				},
			},
		},
	}

	// Allow all tools so the agent can read SKILL.md files.
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

	fmt.Println("Agent started with skills and plugins.")
	fmt.Println("Conversation ID:", agent.ConversationID())
	fmt.Println()
	fmt.Println("Skills:")
	for _, s := range cfg.Skills {
		fmt.Printf("  - %s: %s\n", s.Name, s.Description)
	}
	fmt.Println("Plugins:")
	for _, p := range cfg.Plugins {
		fmt.Printf("  - %s: %s (%d skills)\n", p.Name, p.Description, len(p.Skills))
	}
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
