// Example: Ephemeral messages — per-turn directives the agent follows silently.
//
// This demonstrates how to inject ephemeral messages that steer the agent's
// behavior for a single turn without the agent acknowledging them to the user.
//
// Usage:
//
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
	defer agent.Close()

	// ─── Example 1: Security context directive ─────────────────────────
	// The IDE detects the user has the security scanner panel open.
	// Inject an ephemeral message so the agent prioritizes security.
	fmt.Println("=== Turn 1: With security directive ===")
	resp, err := agent.ChatWithContext(ctx, "Review this handler function", &adk.MessageContext{
		ActiveFile: &adk.FileEntry{Path: "/home/user/project/internal/handler/auth.go", Language: "LANGUAGE_GO"},
		CursorLine: 15,
		EphemeralMessages: []string{
			"The user has the security scanner panel open. Prioritize security best practices and highlight potential vulnerabilities.",
		},
	})
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println(resp.Text)

	// ─── Example 2: Language override ──────────────────────────────────
	// The IDE detects the user's locale preference.
	fmt.Println("\n=== Turn 2: With language override ===")
	resp, err = agent.ChatWithContext(ctx, "Explain how this works", &adk.MessageContext{
		ActiveFile: &adk.FileEntry{Path: "/home/user/project/internal/handler/auth.go", Language: "LANGUAGE_GO"},
		EphemeralMessages: []string{
			"Respond in Spanish.",
		},
	})
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println(resp.Text)

	// ─── Example 3: Multiple directives ────────────────────────────────
	// Combine multiple behavioral constraints for a single turn.
	fmt.Println("\n=== Turn 3: Multiple directives ===")
	resp, err = agent.ChatWithContext(ctx, "Refactor the database layer", &adk.MessageContext{
		ActiveFile: &adk.FileEntry{Path: "/home/user/project/internal/db/queries.go", Language: "LANGUAGE_GO"},
		EphemeralMessages: []string{
			"The user is on the free tier. Do not suggest features that require the premium plan.",
			"The project uses PostgreSQL 15. Use PG15-specific features when beneficial.",
			"Keep explanations brief — the user prefers concise responses this session.",
		},
	})
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println(resp.Text)

	// ─── Example 4: No ephemeral messages (normal turn) ────────────────
	// Ephemeral messages are single-use. This turn has none.
	fmt.Println("\n=== Turn 4: Normal turn (no ephemeral messages) ===")
	resp, err = agent.Chat(ctx, "What did we discuss?")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println(resp.Text)
}
