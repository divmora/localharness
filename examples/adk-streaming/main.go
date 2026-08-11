// Example: Streaming ChatResponse
//
// This example demonstrates the ChatStream() API for real-time step delivery.
// Text appears character-by-character as the model generates it, and tool calls
// are displayed as they happen.
//
// Usage:
//
//	go run . --prompt "List the files in the current directory"
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
	prompt := flag.String("prompt", "What is 2+2? Explain your reasoning.", "Prompt to send to the agent")
	flag.Parse()

	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	if err := agent.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Printf("💬 Prompt: %s\n\n", *prompt)

	// Use ChatStream for real-time delivery
	events, err := agent.ChatStream(context.Background(), *prompt)
	if err != nil {
		log.Fatalf("ChatStream failed: %v", err)
	}

	for event := range events {
		switch event.Type {
		case adk.EventTextDelta:
			// Print text as it streams in
			fmt.Print(event.TextDelta)

		case adk.EventThinkingDelta:
			// Show thinking in gray (ANSI escape)
			fmt.Printf("\033[90m%s\033[0m", event.ThinkingDelta)

		case adk.EventToolCallStart:
			fmt.Printf("\n🔧 [%s] starting...\n", event.Step.ToolName)

		case adk.EventToolCallDone:
			fmt.Printf("   ✅ [%s] done\n", event.Step.ToolName)

		case adk.EventError:
			fmt.Printf("   ❌ [%s] error: %s\n", event.Step.ToolName, event.Step.ErrorMessage)

		case adk.EventTurnComplete:
			resp := event.Response
			fmt.Printf("\n\n📊 Steps: %d", len(resp.Steps))
			if resp.Usage != nil {
				fmt.Printf(", Tokens: %d", resp.Usage.TotalTokens)
			}
			fmt.Println()
		}
	}
}
