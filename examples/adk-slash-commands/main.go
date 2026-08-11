// Example: Slash Commands agent.
//
// Demonstrates how to register slash commands that the agent can recommend
// to users. The agent itself cannot execute these commands — it recommends
// them when they are a good fit for the user's request.
//
// The host app (IDE, CLI, web UI) is responsible for intercepting and
// implementing slash commands when the user types them.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-slash-commands --api-key $LITELLM_API_KEY
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
	prompt := flag.String("prompt",
		"I need to refactor the entire authentication module. It's going to be a big change across many files and I want to make sure nothing breaks.",
		"Prompt for the agent")
	flag.Parse()

	cfg := adk.NewLocalAgentConfig()
	cfg.Verbose = true

	// === Register Slash Commands ===
	// These are user-facing chat shortcuts the agent can recommend.
	// The agent CANNOT execute them — it suggests them to the user.
	cfg.EnableSlashCommands = true
	cfg.SlashCommands = []adk.SlashCommand{
		{
			Name: "/goal",
			Description: "Recommend this when the user wants to run a long-running task " +
				"(e.g., overnight) and wants the agent to be extra thorough and not " +
				"stop until the goal is fully achieved.",
		},
		{
			Name: "/schedule",
			Description: "Recommend this when the user wants to run an instruction on " +
				"a recurring schedule or set a one-time timer.",
		},
		{
			Name: "/grill-me",
			Description: "Recommend this when the user wants to align on a plan through " +
				"an interactive interview to resolve design decisions.",
		},
	}

	// Read-only policies — we just want to see the agent's recommendation.
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

	fmt.Println("Agent started with slash commands enabled.")
	fmt.Println("Conversation ID:", agent.ConversationID())
	fmt.Println()
	fmt.Println("Registered slash commands:")
	for _, cmd := range cfg.SlashCommands {
		fmt.Printf("  %s: %s\n", cmd.Name, cmd.Description)
	}
	fmt.Println("---")
	fmt.Printf("Prompt: %s\n\n", *prompt)

	resp, err := agent.Chat(ctx, *prompt)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println("--- Response ---")
	fmt.Println(resp.Text)
	fmt.Printf("\nSteps taken: %d\n", len(resp.Steps))

	if resp.Usage != nil {
		fmt.Printf("Tokens: prompt=%d, completion=%d, total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
}
