package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	targetURL := "https://example.com"
	if len(os.Args) > 1 {
		targetURL = os.Args[1]
	}

	// Configure the Browser Subagent example
	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()} // Allow all for demonstration

	// Enable browser capabilities (this auto-injects Playwright MCP)
	caps := adk.DefaultCapabilities()
	caps.Browser = true 
	caps.InvokeSubagent = true
	caps.CreateFile = true // So the agent can write the summary
	cfg.Capabilities = caps

	// The system instructions tell the parent agent how to use the subagent
	cfg.SystemInstructions = `You are a researcher agent.
Your job is to read information from a target website using a specialized browser subagent.
1. Use the 'browser_subagent' tool to assign a task to visit the target URL.
2. The subagent will run in the background. Stop acting and wait for it to finish.
3. Once the subagent returns a completion message with the page content, summarize it into a file named 'research_summary.md'.
4. Say "DONE" when the summary is saved.`

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	if err := agent.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Printf("🌐 Spawning agent to research %s...\n", targetURL)
	prompt := fmt.Sprintf("Please use browser_subagent to visit %s. Once the page loads, find a prominent link (like 'Admin Panel' or 'Model Hub') and click it. Then summarize the resulting page's heading and text.", targetURL)

	events, err := agent.ChatStream(context.Background(), prompt)
	if err != nil {
		log.Fatalf("ChatStream failed: %v", err)
	}

	for event := range events {
		switch event.Type {
		case adk.EventTextDelta:
			fmt.Print(event.TextDelta)
		case adk.EventToolCallStart:
			fmt.Printf("\n🔧 Executing tool: %s\n", event.Step.ToolName)
		case adk.EventToolCallDone:
			fmt.Printf("   ✅ Complete\n")
		case adk.EventError:
			fmt.Printf("\n❌ Error: %s\n", event.Step.ErrorMessage)
		}
	}
	fmt.Println("\n\nResearch finished!")
}
