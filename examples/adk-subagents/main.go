// Example: Subagent types and multi-agent orchestration.
//
// Demonstrates three subagent capabilities:
//
// 1. Built-in types: "research" (read-only) and "self" (full-power clone)
// 2. SDK-registered types: Custom domain-specific agents with controlled tool access
// 3. Agent-defined types: The LLM can define ephemeral types at runtime via define_subagent
//
// The agent uses four subagent tools:
//
//   - define_subagent: Register a new subagent type for the conversation
//   - invoke_subagent: Launch one or more subagents concurrently in the background
//   - manage_subagents: List active / kill specific / kill all subagents
//   - send_message: Send a message to another agent by conversation ID
//
// Type hierarchy (higher priority wins on name collision):
//
//	Agent-defined > SDK-registered > Built-in
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-subagents --api-key $LITELLM_API_KEY
//	go run ./examples/adk-subagents --api-key <litellm api key if required>$KEY --prompt "Research the engine package and write unit tests"
//	LOCALHARNESS_BIN=./bin/localharness go run ./examples/adk-subagents --api-key <litellm api key if required>$KEY
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {
	prompt := flag.String("prompt", "Research the codebase structure using a subagent, then summarize what you found.", "Prompt for the agent")
	maxTurns := flag.Int("turns", 3, "Maximum follow-up turns to wait for subagent completion")
	flag.Parse()

	cfg := adk.NewLocalAgentConfig()
	cfg.Verbose = true

	// === Subagent capability must be enabled ===
	cfg.Capabilities.InvokeSubagent = true

	// === SDK-registered Subagent Types ===
	// These extend the built-in types (research, self) with custom agents.
	// SDK types override built-ins with the same name.
	cfg.SubagentTypes = []adk.SubagentTypeDef{
		{
			// Custom test-writer agent: can read AND write (to create test files)
			Name:        "test-writer",
			Description: "A specialized agent that writes unit tests for Go packages.",
			SystemPrompt: `You are a Go test-writing specialist. Given a package path or file,
you analyze the code and write comprehensive unit tests following Go conventions.
Use table-driven tests, check edge cases, and aim for good coverage.
Write tests to *_test.go files in the same package.`,
			EnableWriteTools: true, // Can create/edit files
		},
		{
			// Custom code-reviewer agent: read-only, no write access
			Name:        "reviewer",
			Description: "Reviews code for bugs, performance issues, and Go best practices.",
			SystemPrompt: `You are a senior Go code reviewer. Analyze the given code for:
1. Correctness bugs and edge cases
2. Performance issues (unnecessary allocations, lock contention)
3. Go idiom violations (error handling, naming conventions)
4. Security concerns
Provide a structured review with severity levels (critical/warning/info).`,
			// EnableWriteTools defaults to false — read-only reviewer
		},
	}

	// === Optionally exclude built-in types ===
	// Uncomment to remove the "self" built-in (full-power clone):
	// cfg.ExcludeBuiltinSubagents = []string{"self"}

	// Allow all tools so subagents can work freely.
	cfg.Policies = []policy.Policy{policy.AllowAll()}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Println("Agent started with subagent support.")
	fmt.Println("Conversation ID:", agent.ConversationID())
	fmt.Println()
	fmt.Println("Built-in subagent types: research, self")
	fmt.Println("SDK-registered types:")
	for _, st := range cfg.SubagentTypes {
		writeAccess := "read-only"
		if st.EnableWriteTools {
			writeAccess = "read+write"
		}
		fmt.Printf("  - %s: %s (%s)\n", st.Name, st.Description, writeAccess)
	}
	fmt.Println("---")

	// === Turn 1: Send the initial prompt (LLM spawns subagent) ===
	fmt.Printf("\n📤 Turn 1: %s\n", *prompt)
	resp, err := agent.Chat(ctx, *prompt)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	printResponse(1, resp)

	// === Follow-up turns: Wait for subagent completion ===
	// The subagent runs async. When it finishes, a system message triggers
	// a new turn. We send follow-up prompts to let the parent process results.
	for turn := 2; turn <= *maxTurns+1; turn++ {
		// Give the subagent time to work.
		fmt.Printf("\n⏳ Waiting 10s for subagent(s) to complete...\n")
		time.Sleep(10 * time.Second)

		followUp := "Check if any subagents have completed. If yes, summarize their findings. If they're still running, tell me their status."
		fmt.Printf("📤 Turn %d: %s\n", turn, followUp)

		resp, err = agent.Chat(ctx, followUp)
		if err != nil {
			log.Fatalf("Chat turn %d failed: %v", turn, err)
		}
		printResponse(turn, resp)

		// If the response indicates completion (no more pending subagents), stop.
		if resp.Text != "" {
			// A simple heuristic: if the LLM gives a summary without mentioning
			// "still running" or "pending", we're probably done.
			fmt.Println("\n✅ Multi-turn subagent flow complete.")
			break
		}
	}

	if resp != nil && resp.Usage != nil {
		fmt.Printf("\n📊 Final tokens: prompt=%d, completion=%d, total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
}

func printResponse(turn int, resp *adk.ChatResponse) {
	fmt.Printf("\n--- Turn %d Response ---\n", turn)
	if resp.Text != "" {
		fmt.Println(resp.Text)
	}
	fmt.Printf("Steps: %d", len(resp.Steps))
	if resp.Usage != nil {
		fmt.Printf(" | Tokens: prompt=%d, completion=%d, total=%d",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
	fmt.Println()
}
