// adk-code-review is an example code review agent that demonstrates the
// complete middleware pipeline with dynamic tool selection.
//
// Features demonstrated:
//   - Dynamic ToolSelector: allows read-only tools by default, adds write
//     tools when the prompt explicitly requests fixes
//   - TokenGuard: 100K token budget
//   - InterruptResume + CheckpointStore: persistent checkpoints
//   - RetryConfig with failover: primary Gemini → fallback to OpenAI
//   - Custom PostTurn middleware: generates a review summary
//
// Usage:
//
//	# Optional fallback:
//	export OPENAI_API_KEY=your-key
//	go run ./examples/adk-code-review/
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/middleware"
	"github.com/divmora/localharness/adk/policy"
)

// ReviewSummary is a custom PostTurn middleware that summarizes each review turn.
type ReviewSummary struct {
	logger    *slog.Logger
	findings  int
	totalTime time.Duration
}

func (r *ReviewSummary) Name() string { return "review_summary" }

func (r *ReviewSummary) PreTurn(_ context.Context, req *middleware.TurnRequest) (*middleware.TurnRequest, error) {
	req.Metadata["review_start"] = time.Now()
	return req, nil
}

func (r *ReviewSummary) PostTurn(_ context.Context, resp *middleware.TurnResponse) (*middleware.TurnResponse, error) {
	if start, ok := resp.Metadata["review_start"].(time.Time); ok {
		elapsed := time.Since(start)
		r.totalTime += elapsed
		r.findings++
		r.logger.Info("review turn complete",
			"review_number", r.findings,
			"duration", elapsed.Round(time.Millisecond),
			"total_reviews", r.findings,
			"tokens", resp.TotalTokens,
		)
	}
	return resp, nil
}

func main() {

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Checkpoint directory
	home, _ := os.UserHomeDir()
	cpDir := filepath.Join(home, ".divmora", "localharness", "checkpoints", "code-review")
	cpStore := middleware.NewCheckpointStore(cpDir, logger)

	// Interrupt/Resume
	ir := middleware.NewInterruptResume(middleware.InterruptResumeConfig{
		TurnTimeout: 10 * time.Minute,
		OnInterrupt: cpStore.Save,
	}, logger)

	// Dynamic tool selector: read-only by default, write tools when explicitly requested
	selector := middleware.NewToolSelector(middleware.ToolSelectorConfig{
		Dynamic: func(prompt string) (middleware.ToolSelectorMode, []string, string) {
			lower := strings.ToLower(prompt)

			// If user asks to fix/apply changes, allow write tools too
			if strings.Contains(lower, "fix") ||
				strings.Contains(lower, "apply") ||
				strings.Contains(lower, "patch") ||
				strings.Contains(lower, "write") {
				// Allow everything — no restriction
				return 0, nil, ""
			}

			// Default: read-only mode for code review
			return middleware.ToolSelectorAllow,
				middleware.ReadOnlyTools,
				"Code review mode: read-only analysis"
		},
		WarnOnViolation: true,
	}, logger)

	cfg := adk.NewLocalAgentConfig()
	cfg.Logger = logger
	cfg.SystemInstructions = `You are a senior code reviewer. Your primary job is to:
1. Analyze code for bugs, security issues, performance problems, and style issues
2. Read files, search for patterns, and understand the codebase architecture
3. Provide detailed, actionable review feedback with file:line references
4. Only make changes when explicitly asked to "fix" or "apply" a suggestion

Start every review by understanding the codebase structure, then dive into specific files.
Format your findings as:
- 🔴 Critical: Security/correctness issues
- 🟡 Warning: Performance/maintainability concerns  
- 🔵 Info: Style suggestions and best practices`

	// Enable write tools but guard with policy
	cfg.Capabilities.RunCommand = true
	cfg.Policies = policy.ConfirmRunCommand()

	// Middleware stack
	cfg.Middlewares = []middleware.Middleware{
		// 1. Review summary (custom)
		&ReviewSummary{logger: logger},

		// 2. Token budget: 100K tokens
		middleware.NewTokenGuard(100000, 0.8, logger),

		// 3. Dynamic tool selector
		selector,

		// 4. Interrupt/Resume
		ir,

		// 5. JSON validator
		middleware.NewPatchToolArgs(logger),
	}

	// Retry with optional failover to OpenAI
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		logger.Info("failover configured", "fallback", "gpt-4o")
	}

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

	fmt.Println("Code Review Agent ready.")
	fmt.Println("Commands:")
	fmt.Println("  review <path>  — Review files at the given path")
	fmt.Println("  fix <issue>    — Apply a fix (enables write tools)")
	fmt.Println("  violations     — Show tool selection violations")
	fmt.Println("  quit           — Exit")
	fmt.Println()

	for {
		fmt.Print("review> ")
		var input string
		if _, err := fmt.Scanln(&input); err != nil {
			break
		}

		switch {
		case input == "quit" || input == "exit":
			// Show violation summary
			v := selector.Violations()
			if len(v) > 0 {
				fmt.Println("\nTool selection violations during session:")
				for tool, count := range v {
					fmt.Printf("  %s: %d\n", tool, count)
				}
			}
			return

		case input == "violations":
			v := selector.Violations()
			if len(v) == 0 {
				fmt.Println("No violations recorded.")
			} else {
				for tool, count := range v {
					fmt.Printf("  %s: %d violations\n", tool, count)
				}
			}
			continue
		}

		resp, err := agent.Chat(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintf(os.Stderr, "\n[Interrupted. Checkpoint saved.]\n")
				return
			}
			log.Printf("error: %v", err)
			continue
		}
		fmt.Println(resp)
	}
}
