package adk

import (
	"context"
	"fmt"
	"time"

	"github.com/divmora/localharness/adk/middleware"
	"github.com/divmora/localharness/adk/policy"
)

// SubtaskConfig configures a subtask to run as a child agent.
// This provides an ergonomic wrapper around the subagent system —
// instead of the define→invoke→wait ceremony, you call agent.RunSubtask()
// and get a result back synchronously.
//
// Under the hood, RunSubtask creates a new harness process with an
// independent conversation context. The child has:
//   - Fresh context (no parent history pollution)
//   - Its own conversation ID and brain directory
//   - Configurable tool access (read-only by default)
//   - Process isolation (child crash doesn't affect parent)
type SubtaskConfig struct {
	// Prompt is the task description for the subtask. Required.
	Prompt string

	// SystemPrompt is the system instructions for the subtask.
	// Default: "You are a focused coding assistant working on a specific
	// subtask. Complete the task and provide a clear, concise summary."
	SystemPrompt string

	// ReadOnly restricts the subtask to read-only tools (view_file,
	// list_dir, grep_search, find_file, search_web, read_url_content).
	// Write tools (create/edit/run_command) are disabled.
	// Default: true (safe by default).
	ReadOnly *bool

	// Timeout is the maximum duration for the subtask.
	// Default: 5 minutes. 0 = no timeout.
	Timeout time.Duration

	// Middlewares is an optional middleware stack for the subtask.
	// If nil, inherits the parent's middlewares.
	Middlewares []middleware.Middleware

	// Model overrides the parent's model for this subtask.
	// Useful for running cheaper/faster models on simple subtasks.
	// Empty = use parent's model.
	Model string
}

// SubtaskResult is the outcome of a subtask execution.
type SubtaskResult struct {
	// Text is the subtask's final text response.
	Text string

	// Thinking is the model's reasoning trace (if available).
	Thinking string

	// TotalTokens is the token count for this subtask.
	TotalTokens int

	// StepCount is the number of agentic steps taken.
	StepCount int

	// ConversationID is the child conversation's unique identifier.
	// Can be used to inspect the subtask's full history via lhctl.
	ConversationID string
}

// SubtaskHandle is a handle to an async subtask for non-blocking execution.
type SubtaskHandle struct {
	// ConversationID is the child conversation's unique identifier.
	ConversationID string

	resultCh chan subtaskOutcome
}

type subtaskOutcome struct {
	result *SubtaskResult
	err    error
}

// Wait blocks until the subtask completes and returns the result.
func (h *SubtaskHandle) Wait(ctx context.Context) (*SubtaskResult, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("subtask wait cancelled: %w", ctx.Err())
	case outcome := <-h.resultCh:
		return outcome.result, outcome.err
	}
}

const defaultSubtaskSystemPrompt = `You are a focused coding assistant working on a specific subtask.
You have access to the tools needed for your task. Complete it thoroughly and provide a clear,
concise summary of what you did and found. Be efficient — your response will be used as context
by the parent agent.`

// RunSubtask executes a subtask synchronously in a child agent process.
// The child gets a fresh conversation context, independent tool access,
// and process-isolated execution.
//
// This is the ergonomic wrapper around the subagent system — one call
// instead of define→invoke→wait:
//
//	result, err := agent.RunSubtask(ctx, adk.SubtaskConfig{
//	    Prompt:   "Analyze the auth module for security issues",
//	    ReadOnly: adk.Bool(true),
//	})
//	fmt.Println(result.Text)
//
// For parallel subtasks, use RunSubtaskAsync:
//
//	h1 := agent.RunSubtaskAsync(ctx, adk.SubtaskConfig{Prompt: "Analyze auth.go"})
//	h2 := agent.RunSubtaskAsync(ctx, adk.SubtaskConfig{Prompt: "Analyze db.go"})
//	r1, _ := h1.Wait(ctx)
//	r2, _ := h2.Wait(ctx)
func (a *Agent) RunSubtask(ctx context.Context, cfg SubtaskConfig) (*SubtaskResult, error) {
	if cfg.Prompt == "" {
		return nil, fmt.Errorf("subtask prompt is required")
	}

	// Apply timeout
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	child, err := a.createChildAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("create subtask agent: %w", err)
	}
	defer child.Close()

	if err := child.Start(ctx); err != nil {
		return nil, fmt.Errorf("start subtask: %w", err)
	}

	resp, err := child.Chat(ctx, cfg.Prompt)
	if err != nil {
		return nil, fmt.Errorf("subtask failed: %w", err)
	}

	totalTokens := 0
	if resp.Usage != nil {
		totalTokens = resp.Usage.TotalTokens
	}

	return &SubtaskResult{
		Text:           resp.Text,
		Thinking:       resp.Thinking,
		TotalTokens:    totalTokens,
		StepCount:      len(resp.Steps),
		ConversationID: child.ConversationID(),
	}, nil
}

// RunSubtaskAsync launches a subtask in the background and returns a handle
// that can be waited on. Use this for parallel subtask execution:
//
//	handles := make([]*adk.SubtaskHandle, 3)
//	for i, file := range files {
//	    handles[i] = agent.RunSubtaskAsync(ctx, adk.SubtaskConfig{
//	        Prompt: fmt.Sprintf("Review %s for bugs", file),
//	    })
//	}
//	for _, h := range handles {
//	    result, err := h.Wait(ctx)
//	    // ...
//	}
func (a *Agent) RunSubtaskAsync(ctx context.Context, cfg SubtaskConfig) *SubtaskHandle {
	handle := &SubtaskHandle{
		resultCh: make(chan subtaskOutcome, 1),
	}

	go func() {
		result, err := a.RunSubtask(ctx, cfg)
		if result != nil {
			handle.ConversationID = result.ConversationID
		}
		handle.resultCh <- subtaskOutcome{result: result, err: err}
	}()

	return handle
}

// createChildAgent builds a child Agent with config derived from the parent.
func (a *Agent) createChildAgent(cfg SubtaskConfig) (*Agent, error) {
	child := NewLocalAgentConfig()

	// Inherit LLM provider from parent
	child.LitellmEndpoint = a.config.LitellmEndpoint
	child.BinaryPath = a.config.BinaryPath

	// System prompt
	child.SystemInstructions = cfg.SystemPrompt
	if child.SystemInstructions == "" {
		child.SystemInstructions = defaultSubtaskSystemPrompt
	}

	// Inherit workspaces
	child.Workspaces = a.config.Workspaces

	// Tool access: read-only by default
	readOnly := true
	if cfg.ReadOnly != nil {
		readOnly = *cfg.ReadOnly
	}

	if readOnly {
		child.Capabilities.CreateFile = false
		child.Capabilities.EditFile = false
		child.Capabilities.RunCommand = false
		child.Capabilities.ManageTask = false
		child.Capabilities.InvokeSubagent = false
		// Read tools stay enabled (ViewFile, ListDir, SearchDir, FindFile, etc.)
		child.Policies = []policy.Policy{policy.AllowAll()}
	} else {
		// Write-enabled: inherit parent's policies
		child.Policies = a.config.Policies
	}

	// Middlewares: use subtask-specific or inherit parent's
	if cfg.Middlewares != nil {
		child.Middlewares = cfg.Middlewares
	} else {
		child.Middlewares = a.config.Middlewares
	}



	// Subtask children don't spawn sub-subagents by default
	child.MaxSubagentDepth = 0
	child.MaxConcurrentSubagents = 0

	// Use parent's logger with subtask context
	child.Logger = a.logger.With("subtask", true)
	child.Verbose = a.config.Verbose

	return NewAgent(child)
}

// Bool is a helper that returns a pointer to a bool value.
// Useful for SubtaskConfig.ReadOnly where nil means "use default".
func Bool(v bool) *bool {
	return &v
}
