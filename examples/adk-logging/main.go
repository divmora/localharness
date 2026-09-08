// Example: Go ADK usage with Custom Logging and Token Limits.
//
// Demonstrates how to:
//  1. Enable verbose debug logging.
//  2. Pass a custom slog.Logger to control where logs go.
//  3. Configure a maximum session-level token limit safety guardrail.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-logging/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/divmora/localharness/adk"
)

func main() {

	// 1. Create a custom JSON logger that writes to stdout
	customLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // verbose logging
	}))

	// 2. Configure the agent with custom logger and token safety limits
	cfg := adk.NewLocalAgentConfig()
	cfg.Logger = customLogger
	cfg.MaxTotalTokens = 1500 // Limit session to 1500 total tokens max

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Println("Agent started. Conversation ID:", agent.ConversationID())

	// Chat with the agent. The logs will output in JSON format with debug details.
	resp, err := agent.Chat(ctx, "Hello! Tell me a very short 1-sentence joke.")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println("\n--- Response ---")
	fmt.Println(resp.Text)
	if resp.Usage != nil {
		fmt.Printf("\nToken usage for this turn: %d (cumulative total: %d)\n", resp.Usage.TotalTokens, resp.Usage.TotalTokens)
	}
}
