// adk-research is an example read-only research agent that uses the middleware
// stack to restrict tool access and provide observability.
//
// Features demonstrated:
//   - ToolSelector middleware: restricts to read-only tools
//   - TokenGuard middleware: enforces a 50K token budget
//   - InterruptResume: saves checkpoints on timeout or user cancel
//   - CheckpointStore: persists checkpoints to disk
//   - RetryConfig: retries transient LLM errors
//
// Usage:
//
//	go run ./examples/adk-research/
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/middleware"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Checkpoint directory: ~/.divmora/localharness/checkpoints/research
	home, _ := os.UserHomeDir()
	cpDir := filepath.Join(home, ".divmora", "localharness", "checkpoints", "research")
	cpStore := middleware.NewCheckpointStore(cpDir, logger)

	// Interrupt/Resume with 5-minute turn timeout
	ir := middleware.NewInterruptResume(middleware.InterruptResumeConfig{
		TurnTimeout:          5 * time.Minute,
		OnInterrupt:          cpStore.Save,
		ResumePromptTemplate: "Continue the research task. Original request: {original_prompt}\nYou had completed {step_count} steps. Reason for interruption: {reason} ({reason_detail})",
	}, logger)

	cfg := adk.NewLocalAgentConfig()
	cfg.Logger = logger
	cfg.SystemInstructions = `You are a research assistant. Your job is to investigate codebases, 
documentation, and answer technical questions. You should ONLY read and analyze — never modify files 
or run commands. Provide thorough, well-structured answers with file references.`

	// Read-only capabilities — disable all write tools
	cfg.Capabilities.CreateFile = false
	cfg.Capabilities.EditFile = false
	cfg.Capabilities.RunCommand = false
	cfg.Capabilities.ManageTask = false

	// No write tools = no policy needed
	cfg.Policies = []policy.Policy{policy.AllowAll()}

	// Middleware stack
	cfg.Middlewares = []middleware.Middleware{
		// 1. Token budget: 50K tokens, warn at 80%
		middleware.NewTokenGuard(50000, 0.8, logger),

		// 2. Tool selector: restrict to read-only tools (defense-in-depth)
		middleware.NewToolSelector(middleware.ToolSelectorConfig{
			Mode:            middleware.ToolSelectorAllow,
			Tools:           middleware.ReadOnlyTools,
			Reason:          "This is a read-only research agent.",
			WarnOnViolation: true,
		}, logger),

		// 3. Interrupt/Resume for long research tasks
		ir,

		// 4. JSON validator
		middleware.NewPatchToolArgs(logger),
	}

	// Retry on transient errors

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	// Check for resumable checkpoint
	if cp, err := cpStore.Latest(); err == nil {
		prompt := ir.BuildResumePrompt(*cp)
		fmt.Fprintf(os.Stderr, "[Resuming from checkpoint: turn %d, reason: %s]\n",
			cp.TurnIndex, cp.Reason)
		resp, err := agent.Chat(ctx, prompt)
		if err != nil {
			log.Fatalf("resume error: %v", err)
		}
		fmt.Println(resp)
	}

	// Interactive loop
	fmt.Println("Research Agent ready. Ask questions about the codebase.")
	fmt.Println("Ctrl+C to interrupt (checkpoint saved automatically).")
	fmt.Println()

	for {
		fmt.Print("> ")
		var input string
		if _, err := fmt.Scanln(&input); err != nil {
			break
		}
		if input == "quit" || input == "exit" {
			break
		}

		resp, err := agent.Chat(ctx, input)
		if err != nil {
			// On interrupt, the middleware saved a checkpoint
			if ctx.Err() != nil {
				fmt.Fprintf(os.Stderr, "\n[Interrupted. Checkpoint saved. Run again to resume.]\n")
				return
			}
			log.Printf("error: %v", err)
			continue
		}
		fmt.Println(resp)
	}
}
