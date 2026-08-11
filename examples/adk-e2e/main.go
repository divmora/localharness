package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	// 1. Agent creation with AllTools + AllowAll policy
	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()}
	cfg.Capabilities = adk.AllTools() // explicitly request all tools

	// Configure subagents to enable testing them
	cfg.SubagentTypes = []adk.SubagentTypeDef{
		{
			Name:         "file_lister",
			Description:  "A subagent that can list files and directories.",
			SystemPrompt: "You are a subagent that lists files. You have access to list_dir.",
		},
	}

	// 3. Register a host tool via config
	cfg.HostTools = []adk.HostToolDef{
		{
			Name:        "get_time",
			Description: "Returns the current time",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return map[string]string{"time": time.Now().Format(time.RFC3339)}, nil
			},
		},
	}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	if err := agent.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Printf("Agent started. Conversation ID: %s\n", agent.ConversationID())
	fmt.Println("Running E2E test prompt...")

	// The prompt exercises:
	// - host tools (get_time)
	// - subtasks (delegate to file_lister subagent)
	// - web fetch (read_url_content)
	// - file ops (write_to_file)
	prompt := `Please do the following steps in order, and say "DONE" when finished:
1. Call the 'get_time' tool to get the current time.
2. Spawn a subagent (using invoke_subagent or similar) to list the contents of the current directory.
3. Fetch the content of "https://example.com".
4. Create a file called "e2e_test_output.txt" in the current directory containing the time you got, the first 3 files you saw, and a brief summary of the example.com website.`

	// 2. Streaming execution
	events, err := agent.ChatStream(context.Background(), prompt)
	if err != nil {
		log.Fatalf("ChatStream failed: %v", err)
	}

	for event := range events {
		switch event.Type {
		case adk.EventTextDelta:
			fmt.Print(event.TextDelta)
		case adk.EventToolCallStart:
			fmt.Printf("\n🔧 Tool call: %s\n", event.Step.ToolName)
		case adk.EventToolCallDone:
			fmt.Printf("   ✅ Tool done\n")
		case adk.EventError:
			fmt.Printf("\n❌ Error: %s\n", event.Step.ErrorMessage)
		case adk.EventTurnComplete:
			fmt.Printf("\n\nTurn complete. Total steps: %d\n", len(event.Response.Steps))
		}
	}

	// Verify the file was created
	if _, err := os.Stat("e2e_test_output.txt"); err == nil {
		fmt.Println("✅ Success: e2e_test_output.txt was created.")
		os.Remove("e2e_test_output.txt") // Cleanup
	} else {
		log.Fatalf("❌ Failure: e2e_test_output.txt was not created!")
	}
}
