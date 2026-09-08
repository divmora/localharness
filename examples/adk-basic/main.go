// Example: Basic Go ADK usage with structured error handling.
//
// Creates an agent with default settings (ConfirmRunCommand policy),
// sends a prompt, and prints the response.
//
// Prerequisites:
//   - LITELLM_API_KEY environment variable set
//   - Binary auto-resolved (make build, PATH, or auto-download)
//
// Usage:
//
//	go run ./examples/adk-basic/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/divmora/localharness/adk"
)

func main() {

	// Create agent with safe defaults:
	// - All tools enabled except run_command (denied by policy)
	// - Current directory as workspace
	cfg := adk.NewLocalAgentConfig()

	agent, err := adk.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start agent: %v", err)
	}

	fmt.Println("Agent started. Conversation ID:", agent.ConversationID())

	// Chat with the agent with structured error handling
	// Note: Chat() will internally evaluate policies before each tool call.
	// run_command calls will be denied; file reads/writes will be allowed.
	resp, err := agent.Chat(ctx, "List the files in the current directory")
	if err != nil {
		// Demonstrate structured error handling
		handleError(err)
		return
	}

	fmt.Println("\n--- Response ---")
	fmt.Println(resp.Text)
	fmt.Printf("\nSteps taken: %d\n", len(resp.Steps))

	// Demonstrate error handling from step updates
	fmt.Println("\n--- Step Error Handling ---")
	handleStepErrors(resp.Steps)
}

// handleError demonstrates generic error processing
func handleError(err error) {
	log.Printf("Error occurred: %v", err)
	// In a real application, you would implement specific error recovery
	// based on the error type and context from the SDK
}

// handleStepErrors demonstrates processing errors from step updates
func handleStepErrors(steps []adk.Step) {
	for _, step := range steps {
		if step.State == adk.StateError && step.ErrorMessage != "" {
			fmt.Printf("Step %d error: %s\n", step.Index, step.ErrorMessage)

			// In a real application, you would parse the error message
			// to extract error codes and context for programmatic handling
			// The full ErrorInfo with code and metadata is available
			// in the underlying protobuf StepUpdate if needed

			// Use structured error codes for programmatic handling
			if step.ErrorCode != "" {
				fmt.Printf("  Error code: %s\n", step.ErrorCode)
			}

			// Display error metadata for debugging
			if len(step.ErrorMetadata) > 0 {
				fmt.Println("  Error metadata:")
				for key, value := range step.ErrorMetadata {
					fmt.Printf("    %s: %s\n", key, value)
				}
			}

			// Example error recovery based on error code
			switch step.ErrorCode {
			case "TOOL_TIMEOUT":
				fmt.Println("  → Tool timed out - could retry with longer timeout")
			case "FILE_NOT_FOUND":
				fmt.Println("  → Resource not found - check path or create resource")
			case "LLM_RATE_LIMIT":
				fmt.Println("  → API rate limit exceeded - implement retry with backoff")
			case "MAX_TURNS_EXCEEDED":
				fmt.Println("  → Maximum turns exceeded - request may need refinement")
			case "PERMISSION_DENIED":
				fmt.Println("  → Permission denied - check policies or add approval")
			default:
				// Fallback to message-based handling for backward compatibility
				switch {
				case strings.Contains(step.ErrorMessage, "timeout"):
					fmt.Println("  → Tool timed out - could retry with longer timeout")
				case strings.Contains(step.ErrorMessage, "not found"):
					fmt.Println("  → Resource not found - check path or create resource")
				case strings.Contains(step.ErrorMessage, "rate limit"):
					fmt.Println("  → API rate limit exceeded - implement retry with backoff")
				case strings.Contains(step.ErrorMessage, "maximum turns"):
					fmt.Println("  → Maximum turns exceeded - request may need refinement")
				default:
					fmt.Println("  → Non-recoverable error or unknown code")
				}
			}
		}
	}
}
