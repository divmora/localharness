// Example: Planning Mode agent.
//
// Creates an agent with EnablePlanningMode = true. The agent will
// research → plan → seek approval → execute → verify for complex requests.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-planning-mode --api-key $LITELLM_API_KEY --prompt "Refactor the error handling in internal/tools/"
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
	prompt := flag.String("prompt", "Analyze this codebase and suggest improvements", "Prompt for the agent")
	flag.Parse()

	cfg := adk.NewLocalAgentConfig()
	cfg.Verbose = true

	// Enable planning mode — the agent will create implementation_plan.md,
	// wait for approval, then execute with task.md tracking and walkthrough.md summary.
	cfg.EnablePlanningMode = true

	// Allow all tools so the agent can research freely.
	// In production, use more restrictive policies.
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

	fmt.Println("Agent started with planning mode enabled.")
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
