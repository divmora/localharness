// Package main demonstrates the ADK middleware pipeline.
//
// This example shows how to use built-in and custom middlewares to intercept
// and transform agent turns.
//
// Usage:
//
//	go run ./examples/adk-middleware --api-key $LITELLM_API_KEY --prompt "List files"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/middleware"
)

// LoggingMiddleware is a custom middleware that logs every turn.
type LoggingMiddleware struct {
	logger *slog.Logger
}

func (m *LoggingMiddleware) Name() string { return "logging" }

func (m *LoggingMiddleware) PreTurn(_ context.Context, req *middleware.TurnRequest) (*middleware.TurnRequest, error) {
	m.logger.Info("📤 PreTurn", "prompt_len", len(req.Prompt))
	req.Metadata["turn_start"] = time.Now()
	return req, nil
}

func (m *LoggingMiddleware) PostTurn(_ context.Context, resp *middleware.TurnResponse) (*middleware.TurnResponse, error) {
	if start, ok := resp.Metadata["turn_start"].(time.Time); ok {
		m.logger.Info("📥 PostTurn",
			"response_len", len(resp.Text),
			"tokens", resp.TotalTokens,
			"steps", resp.StepCount,
			"duration", time.Since(start).Round(time.Millisecond),
		)
	}
	return resp, nil
}

func main() {
	prompt := flag.String("prompt", "List the files in the current directory", "Prompt to send")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Create config with middleware stack
	cfg := adk.NewLocalAgentConfig()
	cfg.Logger = logger

	// Register middlewares — executed in order for PreTurn, reverse for PostTurn
	cfg.Middlewares = []middleware.Middleware{
		// 1. Custom logging (runs first in PreTurn, last in PostTurn)
		&LoggingMiddleware{logger: logger},

		// 2. Token budget guard (100K tokens, warn at 80%)
		middleware.NewTokenGuard(100000, 0.8, logger),

		// 3. Tool selector — restrict to read-only tools for this example
		middleware.NewToolSelector(middleware.ToolSelectorConfig{
			Mode:            middleware.ToolSelectorAllow,
			Tools:           middleware.ReadOnlyTools,
			Reason:          "This is a read-only research task.",
			WarnOnViolation: true,
		}, logger),

		// 4. Tool args JSON validator (observability — logs malformed tool args)
		middleware.NewPatchToolArgs(logger),
	}

	// Enable automatic retry on transient LLM errors (rate limits, timeouts)

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()

	if err := agent.Start(ctx); err != nil {
		log.Fatalf("start agent: %v", err)
	}

	fmt.Printf("🤖 Sending: %s\n\n", *prompt)

	resp, err := agent.Chat(ctx, *prompt)
	if err != nil {
		log.Fatalf("chat: %v", err)
	}

	fmt.Printf("📝 Response:\n%s\n", resp.Text)

	if resp.Usage != nil {
		fmt.Printf("\n📊 Tokens: %d prompt, %d completion, %d total\n",
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
			resp.Usage.TotalTokens,
		)
	}
}
