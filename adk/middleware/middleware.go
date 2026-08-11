// Package middleware provides a composable pipeline for intercepting agent turns.
//
// Middlewares run in the SDK process (not in the harness binary). They can
// transform prompts before they reach the harness (PreTurn), process streaming
// events as they arrive (ProcessStep), and post-process the final response
// after a turn completes (PostTurn).
//
// Middlewares are executed in registration order for PreTurn/ProcessStep, and
// in reverse order for PostTurn (like HTTP middleware stacks).
//
// Usage:
//
//	cfg := adk.NewLocalAgentConfig()
//	cfg.Middlewares = []middleware.Middleware{
//	    middleware.NewPatchToolArgs(),
//	    middleware.NewTokenGuard(100000, 0.8),
//	}
package middleware

import (
	"context"
)

// Middleware is the base interface for all middleware types.
// A middleware must implement at least one of the phase interfaces
// (PreTurnMiddleware, PostTurnMiddleware, StepMiddleware) to be useful.
type Middleware interface {
	// Name returns a unique, human-readable identifier for this middleware.
	Name() string
}

// PreTurnMiddleware intercepts a turn before the prompt is sent to the harness.
// Use this for prompt transformation, injection, validation, or logging.
type PreTurnMiddleware interface {
	Middleware
	// PreTurn is called before the prompt is sent to the harness.
	// Return a modified TurnRequest to transform the prompt, or return
	// the original request unchanged for observability-only middleware.
	// Returning an error aborts the turn.
	PreTurn(ctx context.Context, req *TurnRequest) (*TurnRequest, error)
}

// PostTurnMiddleware intercepts the final response after a turn completes.
// Use this for response transformation, metrics collection, or post-processing.
type PostTurnMiddleware interface {
	Middleware
	// PostTurn is called after the turn completes but before the response
	// is delivered to the caller. Return a modified TurnResponse or the
	// original unchanged. Returning an error replaces the response with
	// an error event.
	PostTurn(ctx context.Context, resp *TurnResponse) (*TurnResponse, error)
}

// StepMiddleware intercepts individual streaming events during a turn.
// Use this for event filtering, transformation, or real-time monitoring.
type StepMiddleware interface {
	Middleware
	// ProcessStep is called for each streaming event before it reaches
	// the caller's event channel. Set StepEvent.ShouldFilter = true
	// to suppress the event. Return a modified event or the original.
	// Returning an error emits an error event to the caller.
	ProcessStep(ctx context.Context, event *StepEvent) (*StepEvent, error)
}

// TurnRequest is the pre-processed prompt before it reaches the harness.
type TurnRequest struct {
	// Prompt is the user's message text.
	Prompt string

	// Metadata is extensible key-value data that middlewares can read/write.
	// Downstream middlewares and the caller can inspect this for cross-cutting
	// concerns (e.g., "request_id", "trace_id").
	Metadata map[string]any
}

// TurnResponse is the post-processed response after the harness turn completes.
type TurnResponse struct {
	// Text is the model's final text response.
	Text string

	// Thinking is the model's reasoning trace (if using a thinking model).
	Thinking string

	// TotalTokens is the cumulative token count for this turn.
	TotalTokens int

	// StepCount is the number of steps in this turn.
	StepCount int

	// Error is set if the turn ended with an error.
	Error error

	// Metadata is extensible key-value data that middlewares can read/write.
	Metadata map[string]any
}

// StepEvent wraps a streaming event for middleware processing.
type StepEvent struct {
	// EventType is the type identifier (maps to adk.StreamEventType).
	EventType int

	// TextDelta is the streaming text chunk (for text delta events).
	TextDelta string

	// ThinkingDelta is the streaming thinking chunk (for thinking delta events).
	ThinkingDelta string

	// ToolName is the tool being called (for tool events).
	ToolName string

	// ToolState is "active", "done", or "error" (for tool events).
	ToolState string

	// ErrorMessage is set for error events.
	ErrorMessage string

	// ShouldFilter suppresses this event from reaching the caller.
	// Set to true in ProcessStep() to drop the event.
	ShouldFilter bool

	// Metadata is extensible key-value data for cross-cutting concerns.
	Metadata map[string]any
}
