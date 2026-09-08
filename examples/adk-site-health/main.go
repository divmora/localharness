package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run ./examples/adk-site-health <url>")
		fmt.Println("Example: go run ./examples/adk-site-health https://example.com")
		os.Exit(1)
	}
	targetURL := os.Args[1]

	// Configure the Site Health agent
	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()} // Allow file writing & commands

	// Enable necessary tools: Web Fetch, File operations, and Browser
	caps := adk.DefaultCapabilities()
	caps.WebFetch = true
	caps.WebSearch = true
	caps.CreateFile = true
	caps.EditFile = true
	caps.RunCommand = true // for ping if needed
	caps.Browser = true    // enables Playwright MCP for visual checks
	cfg.Capabilities = caps

	// Customize the agent's identity
	cfg.SystemInstructions = `You are a Site Health Monitor agent.
Your job is to investigate a target website and produce a detailed Markdown report.
You must:
1. Use your host tool 'dns_lookup' to get the IP address of the target.
2. Use 'read_url_content' to quickly fetch the page source.
3. Use the 'browser' tool (Playwright MCP) to navigate to the site and verify it renders correctly (take a screenshot if possible).
4. Run 'ping -c 3 <ip>' using 'run_command' to check latency.
5. Search the web for recent news about this domain.
6. Compile all this into a report file named 'site_health_report.md'.
7. Say "DONE" when the report is saved.`

	// Register a custom host tool for DNS lookup
	cfg.HostTools = []adk.HostToolDef{
		{
			Name:        "dns_lookup",
			Description: "Looks up the IP addresses for a given hostname.",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				hostname, ok := args["hostname"].(string)
				if !ok {
					return nil, fmt.Errorf("hostname argument is required and must be a string")
				}
				ips, err := net.LookupIP(hostname)
				if err != nil {
					return nil, err
				}
				var ipStrings []string
				for _, ip := range ips {
					ipStrings = append(ipStrings, ip.String())
				}
				return map[string]any{"ips": ipStrings}, nil
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

	fmt.Printf("🔍 Starting health check for %s\n", targetURL)
	prompt := fmt.Sprintf("Please run a full health check on %s and generate the report.", targetURL)

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
	fmt.Println("\n\nHealth check finished!")
}
