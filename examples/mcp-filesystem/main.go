// Example: Using MCP with LocalHarness
//
// This example connects a LocalHarness agent to an MCP filesystem server,
// giving it the ability to use tools provided by the MCP server alongside
// the built-in LocalHarness tools.
//
// Prerequisites:
//
//	npm install -g @modelcontextprotocol/server-filesystem  # or use npx
//
// Usage:
//
//	go run . --prompt "List the files in /tmp/workspace"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	prompt := flag.String("prompt", "List the files in the workspace", "Prompt to send to the agent")
	workspace := flag.String("workspace", "/tmp/mcp-demo", "Workspace directory for the MCP filesystem server")
	flag.Parse()

	// Ensure workspace directory exists
	os.MkdirAll(*workspace, 0755)

	// Create some demo files
	os.WriteFile(*workspace+"/hello.txt", []byte("Hello from MCP!"), 0644)
	os.WriteFile(*workspace+"/readme.md", []byte("# MCP Demo\nThis is a demo workspace."), 0644)

	// Configure the agent with MCP filesystem server
	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()} // Required when MCP servers are configured
	cfg.Verbose = true

	// Add an MCP server that provides filesystem tools
	cfg.McpServers = []adk.McpServer{
		{
			Name:    "filesystem",
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", *workspace},
		},
	}

	fmt.Printf("🔌 Connecting to MCP filesystem server (workspace: %s)\n", *workspace)
	fmt.Printf("💬 Prompt: %s\n\n", *prompt)

	// Create and run the agent
	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	// Chat with the agent — it can now use MCP filesystem tools
	resp, err := agent.Chat(ctx, *prompt)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Printf("\n🤖 Response:\n%s\n", resp.Text)
	fmt.Printf("\n📊 Steps: %d", len(resp.Steps))
	if resp.Usage != nil {
		fmt.Printf(", Tokens: %d", resp.Usage.TotalTokens)
	}
	fmt.Println()
}
