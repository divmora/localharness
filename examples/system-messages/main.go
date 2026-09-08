// Example: System message injection and auto-wake.
//
// This example demonstrates the system notification pipeline:
// 1. Agent starts a background task (short sleep + echo)
// 2. Task completes → notification pushed to unified channel
// 3. Auto-wake → engine starts a synthetic turn
// 4. Agent sees <SYSTEM_MESSAGE> with task completion details
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Run `make build` first, then set LOCALHARNESS_BIN=./bin/localharness
//
// Usage:
//
//	go run ./examples/system-messages/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	cfg := adk.NewLocalAgentConfig()
	cfg.Verbose = true

	// Enable all tools — schema sanitization now strips unsupported fields
	cfg.Capabilities = adk.AllTools()
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

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("  System Message E2E Test")
	fmt.Println("  Conversation ID:", agent.ConversationID())
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// --- Test 1: Background task completion notification ---
	fmt.Println("=== Test 1: Background Task Completion ===")
	fmt.Println("Asking agent to start a short background task...")
	fmt.Println()

	resp, err := agent.Chat(ctx, `Run this command as a background task (set WaitMsBeforeAsync to 500): echo "starting build..." && sleep 3 && echo "BUILD COMPLETE: all tests passed"

After starting the task, tell me the task ID and then stop. Do NOT poll or wait for it.`)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println("Response:", resp.Text)
	fmt.Printf("Steps: %d\n", len(resp.Steps))
	for _, s := range resp.Steps {
		fmt.Printf("  Step %d: tool=%q state=%d source=%d err=%q\n", s.Index, s.ToolName, s.State, s.Source, s.ErrorMessage)
	}
	fmt.Println()

	// Wait for the background task to complete (should trigger auto-wake)
	fmt.Println("⏳ Waiting 5 seconds for background task to complete and auto-wake...")
	time.Sleep(5 * time.Second)

	// --- Test 2: Timer notification ---
	fmt.Println()
	fmt.Println("=== Test 2: Timer Notification ===")
	fmt.Println("Asking agent to set a 3-second timer...")
	fmt.Println()

	resp, err = agent.Chat(ctx, `Set a one-shot timer for 3 seconds with the prompt "Timer test: check system health". Then stop and wait.`)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println("Response:", resp.Text)
	fmt.Printf("Steps: %d\n", len(resp.Steps))
	fmt.Println()

	// Wait for the timer to fire (should trigger auto-wake)
	fmt.Println("⏳ Waiting 5 seconds for timer to fire and auto-wake...")
	time.Sleep(5 * time.Second)

	// --- Test 3: Verify the agent received system messages ---
	fmt.Println()
	fmt.Println("=== Test 3: Verify Delivery ===")
	fmt.Println("Asking agent what notifications it received...")
	fmt.Println()

	resp, err = agent.Chat(ctx, "Did you receive any system notifications or messages from background tasks or timers? Summarize what happened.")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Println("Response:", resp.Text)
	fmt.Printf("\nTotal steps in final turn: %d\n", len(resp.Steps))
}
