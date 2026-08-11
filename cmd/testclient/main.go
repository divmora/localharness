// Test client for LocalHarness — uses the Go ADK.
//
// Connects to the harness via the SDK's pipe-based handshake, sends a prompt,
// and prints all steps received.
//
// Usage:
//
//	go run ./cmd/testclient --api-key=YOUR_GEMINI_KEY --prompt "List files in the current directory"
//	go run ./cmd/testclient --api-key=YOUR_GEMINI_KEY --prompt "Create a file called hello.txt" --enable-commands
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func main() {
	var (
		endpoint    = flag.String("endpoint", "", "LiteLLM endpoint name (from ~/.divmora/config/litellm.json)")
		apiKey      = flag.String("api-key", "", "Inline API key override")
		baseURL     = flag.String("base-url", "", "Inline API base URL override")
		model       = flag.String("model", "", "Inline model name override")
		prompt      = flag.String("prompt", "", "User prompt to send")
		workspace   = flag.String("workspace", "", "Workspace directory (defaults to cwd)")
		sysInstr    = flag.String("system", "", "System instructions")
		enableCmd   = flag.Bool("enable-commands", false, "Enable run_command tool")
		enableSub   = flag.Bool("enable-subagents", false, "Enable invoke_subagent tool")
		autoApprove = flag.Bool("auto-approve", false, "Auto-approve all permission requests")
		planning    = flag.Bool("planning", false, "Enable planning mode (research → plan → execute)")
		compThreshold = flag.Int("compaction-threshold", 0, "Token threshold for context compaction (0=default 100K, -1=disabled, >0=custom)")
		convID      = flag.String("conversation", "", "Resume an existing conversation by ID")
		verbose     = flag.Bool("verbose", false, "Enable verbose debug logging")
	)
	flag.Parse()

	if *prompt == "" {
		log.Fatal("Error: --prompt is required")
	}

	// Build SDK config
	cfg := adk.NewLocalAgentConfig()

	// LiteLLM configuration
	cfg.LitellmEndpoint = *endpoint
	cfg.LitellmAPIKey = *apiKey
	cfg.LitellmBaseURL = *baseURL
	cfg.LitellmModel = *model

	// System instructions
	if *sysInstr != "" {
		cfg.SystemInstructions = *sysInstr
	}

	// Workspace
	if *workspace != "" {
		cfg.Workspaces = []adk.WorkspaceDef{{Directory: *workspace}}
	}

	// Capabilities
	cfg.Capabilities.RunCommand = *enableCmd
	cfg.Capabilities.ManageTask = *enableCmd
	cfg.Capabilities.InvokeSubagent = *enableSub

	// Resume conversation
	if *convID != "" {
		cfg.ConversationID = *convID
	}

	// Compaction
	cfg.CompactionThreshold = *compThreshold

	// Verbose
	cfg.Verbose = *verbose

	// Planning mode
	cfg.EnablePlanningMode = *planning

	// Policies
	if *autoApprove {
		cfg.Policies = []policy.Policy{policy.AllowAll()}
	}

	// Create agent
	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	// Handle Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	go func() {
		<-interrupt
		fmt.Println("\n⚠️  Interrupted — shutting down...")
		cancel()
		agent.Close()
		os.Exit(0)
	}()

	// Start agent (pipe handshake happens here)
	fmt.Printf("🔌 Starting localharness agent (endpoint=%s)...\n", *endpoint)
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}
	fmt.Printf("✅ Connected (conversation=%s)\n", agent.ConversationID())

	// Send prompt
	fmt.Printf("📤 Sending: %q\n", *prompt)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	resp, err := agent.Chat(ctx, *prompt)
	if err != nil {
		log.Fatalf("Chat error: %v", err)
	}

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("✅ Done — %d steps\n", len(resp.Steps))
	if resp.Usage != nil {
		fmt.Printf("📊 Tokens: %d prompt, %d completion, %d total (cached: %d)\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, resp.Usage.CachedTokens)
	}
	if resp.Text != "" {
		fmt.Printf("\n🤖 Final response:\n%s\n", resp.Text)
	}
}
