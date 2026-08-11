// Example: Basic Go ADK usage.
//
// Creates an agent with default settings (ConfirmRunCommand policy),
// sends a prompt, and prints the response.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-basic/main.go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/divmora/localharness/adk"
)

func main() {

	// Create agent with safe defaults:
	// - All tools enabled except run_command (denied by policy)
	// - Current directory as workspace
	cfg := adk.NewLocalAgentConfig()

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

	// Chat with the agent
	// Note: Chat() will internally evaluate policies before each tool call.
	// run_command calls will be denied; file reads/writes will be allowed.
	resp, err := agent.Chat(ctx, "List the files in the current directory")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println("\n--- Response ---")
	fmt.Println(resp.Text)
	fmt.Printf("\nSteps taken: %d\n", len(resp.Steps))
}
