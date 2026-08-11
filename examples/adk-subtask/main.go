// adk-subtask demonstrates the ergonomic subtask API — the "Agent-as-Tool"
// pattern stolen from Eino, but built on top of our process-isolated
// subagent architecture.
//
// Instead of the define→invoke→wait ceremony, you call agent.RunSubtask()
// and get a result back synchronously. For parallel work, use RunSubtaskAsync().
//
// Usage:
//
//	go run ./examples/adk-subtask/
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := adk.NewLocalAgentConfig()
	cfg.Logger = logger
	cfg.Policies = policy.ConfirmRunCommand()

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()

	if err := agent.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	// ─── Example 1: Simple synchronous subtask ───
	fmt.Println("═══ Example 1: Synchronous subtask (read-only) ═══")
	result, err := agent.RunSubtask(ctx, adk.SubtaskConfig{
		Prompt:  "List the top-level files in the current directory and describe what each one does.",
		Timeout: 2 * time.Minute,
		// ReadOnly defaults to true — safe by default
	})
	if err != nil {
		log.Fatalf("subtask error: %v", err)
	}
	fmt.Printf("Result (%d tokens, %d steps):\n%s\n\n", result.TotalTokens, result.StepCount, result.Text)

	// ─── Example 2: Subtask with custom system prompt ───
	fmt.Println("═══ Example 2: Custom system prompt ═══")
	result, err = agent.RunSubtask(ctx, adk.SubtaskConfig{
		Prompt:       "Analyze the Go module dependencies and identify any that might be outdated.",
		SystemPrompt: "You are a dependency auditor. Focus on Go module versions and report findings concisely.",
		Timeout:      2 * time.Minute,
	})
	if err != nil {
		log.Fatalf("subtask error: %v", err)
	}
	fmt.Printf("Result:\n%s\n\n", result.Text)

	// ─── Example 3: Parallel subtasks ───
	fmt.Println("═══ Example 3: Parallel subtasks ═══")
	files := []string{"go.mod", "Makefile", "README.md"}
	handles := make([]*adk.SubtaskHandle, len(files))

	for i, file := range files {
		handles[i] = agent.RunSubtaskAsync(ctx, adk.SubtaskConfig{
			Prompt:  fmt.Sprintf("Summarize the contents of %s in 2-3 sentences.", file),
			Timeout: 1 * time.Minute,
		})
		fmt.Printf("  Launched subtask for %s\n", file)
	}

	fmt.Println("  Waiting for results...")
	for i, h := range handles {
		r, err := h.Wait(ctx)
		if err != nil {
			fmt.Printf("  %s: ERROR: %v\n", files[i], err)
			continue
		}
		fmt.Printf("\n  %s (%d tokens):\n  %s\n", files[i], r.TotalTokens, r.Text)
	}

	// ─── Example 4: Write-enabled subtask ───
	fmt.Println("\n═══ Example 4: Write-enabled subtask ═══")
	result, err = agent.RunSubtask(ctx, adk.SubtaskConfig{
		Prompt:   "Create a file called SUBTASK_TEST.md with the text 'This file was created by a subtask agent.'",
		ReadOnly: adk.Bool(false), // Enable write tools
		Timeout:  1 * time.Minute,
	})
	if err != nil {
		log.Fatalf("write subtask error: %v", err)
	}
	fmt.Printf("Result:\n%s\n", result.Text)

	// ─── Example 5: Cheaper model for simple tasks ───
	fmt.Println("\n═══ Example 5: Model override ═══")
	result, err = agent.RunSubtask(ctx, adk.SubtaskConfig{
		Prompt:  "What is 2 + 2?",
		Model:   "gemini-2.0-flash-lite", // Cheaper model for trivial tasks
		Timeout: 30 * time.Second,
	})
	if err != nil {
		fmt.Printf("Model override: %v (expected if model not available)\n", err)
	} else {
		fmt.Printf("Result: %s\n", result.Text)
	}
}
