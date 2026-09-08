// Example: Read-only agent with interactive approval for writes.
//
// Uses SafeDefaults to create an agent that can freely read files
// but asks the user (via a callback) before any write operation.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-safe-agent/main.go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

// promptUser asks the user for approval via stdin.
// This is the AskUserHandler callback invoked by the policy system
// when a tool call requires user consent.
func promptUser(toolName string, args map[string]any) (bool, error) {
	fmt.Printf("\n🔒 Permission requested for: %s\n", toolName)
	fmt.Printf("   Args: %v\n", args)
	fmt.Print("   Allow? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes", nil
}

func main() {

	// SafeDefaults:
	// - Read-only tools (view_file, list_dir, search_dir, find_file) → always allowed
	// - Everything else → calls promptUser() for approval
	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = policy.SafeDefaults(promptUser)
	cfg.Capabilities = adk.DefaultCapabilities()
	cfg.Capabilities.RunCommand = true // Enable but require approval via policy

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	fmt.Println("🤖 Safe agent started. Read operations are auto-approved.")
	fmt.Println("   Write operations will ask for your approval.")
	fmt.Println()

	// Interactive chat loop
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" || prompt == "exit" || prompt == "quit" {
			break
		}

		resp, err := agent.Chat(ctx, prompt)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n🤖 %s\n\n", resp.Text)
	}

	fmt.Println("\nGoodbye!")
}
