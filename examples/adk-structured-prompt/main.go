// Example: Structured system prompt with per-message context.
//
// This demonstrates modular system prompt composition and ChatWithContext.
//
// Usage:
//
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/divmora/localharness/adk"
	"github.com/divmora/localharness/adk/policy"
)

func main() {

	cfg := adk.NewLocalAgentConfig()
	cfg.Policies = []policy.Policy{policy.AllowAll()}

	// Use structured prompt instead of raw string
	cfg.StructuredPrompt = &adk.StructuredPrompt{
		Identity:           "You are ProjectBot, an expert Go developer assistant.",
		Guidelines:         "Always write tests. Keep functions small. Use descriptive names.",
		CommunicationStyle: "Be concise. Use bullet points for lists. Format code with Go syntax highlighting.",
		Sections: []adk.PromptSection{
			{
				Tag: "project_context",
				Content: `This is a Go microservice using:
- gRPC for API
- PostgreSQL for storage
- Redis for caching
The project follows clean architecture patterns.`,
				Priority: 50,
			},
			{
				Tag: "security_rules",
				Content: `- Never expose API keys or secrets in code or logs
- Always validate user input
- Use parameterized queries for database access`,
				Priority: 30,
			},
		},
	}

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
	defer agent.Close()

	// Chat with host context (simulating an IDE)
	resp, err := agent.ChatWithContext(ctx, "What best practices should I follow?", &adk.MessageContext{
		ActiveFile: &adk.FileEntry{Path: "/home/user/project/internal/handler/user.go", Language: "LANGUAGE_GO"},
		CursorLine: 42,
		OpenFiles: []adk.FileEntry{
			{Path: "/home/user/project/internal/handler/user.go", Language: "LANGUAGE_GO"},
			{Path: "/home/user/project/internal/handler/user_test.go", Language: "LANGUAGE_GO"},
		},
	})
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}

	fmt.Println(resp.Text)
}
