// Example: Policy-based permission control.
//
// Demonstrates how to use the policy system to control which tools
// the agent can use. Shows deny, allow, conditional deny, and ask patterns.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-policy/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	// Build a custom policy set:
	//
	// 1. Deny run_command entirely
	// 2. Deny file creates outside the workspace (auto-handled by WorkspaceOnly)
	// 3. Deny replace_file_content for any .go files (conditional predicate)
	// 4. Allow everything else
	policies := []policy.Policy{
		// Block shell commands
		policy.DenyRule("run_command", policy.WithName("no_shell")),
		policy.DenyRule("manage_task", policy.WithName("no_tasks")),

		// Block editing Go source files (conditional predicate)
		policy.DenyRule("replace_file_content",
			policy.WithName("protect_go_files"),
			policy.WithPredicate(func(toolName string, args map[string]any) bool {
				path, _ := args["path"].(string)
				return strings.HasSuffix(path, ".go")
			}),
		),

		// Allow everything else
		policy.AllowAll(),
	}

	sandboxDir := "/tmp/agent-sandbox"
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}

	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = policies
	cfg.Workspaces = []adk.WorkspaceDef{{Directory: sandboxDir}} // Restrict to sandbox

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	// The agent will be able to read files and create non-.go files,
	// but cannot run shell commands or edit .go files.
	resp, err := agent.Chat(ctx, "Create a file called notes.txt with 'Hello from the agent!'")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println(resp.Text)

	// This will result in a policy denial (agent adapts):
	resp, err = agent.Chat(ctx, "Now edit main.go and add a comment")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println(resp.Text)
}
