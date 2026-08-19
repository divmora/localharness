package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// InterruptReason describes why a turn was interrupted.
type InterruptReason string

const (
	// InterruptUser indicates the user requested an interrupt.
	InterruptUser InterruptReason = "user"

	// InterruptLimit indicates a token limit was exceeded.
	InterruptLimit InterruptReason = "limit"

	// InterruptTimeout indicates a turn exceeded the time limit.
	InterruptTimeout InterruptReason = "timeout"

	// InterruptCustom indicates a custom interrupt condition was met.
	InterruptCustom InterruptReason = "custom"
)

// TurnCheckpoint captures the state at a turn boundary for resume.
type TurnCheckpoint struct {
	// TurnIndex is the 0-based index of the turn that was interrupted.
	TurnIndex int

	// Prompt is the original prompt that was being processed.
	Prompt string

	// PartialResponse is any partial response text received before interrupt.
	PartialResponse string

	// TotalTokens is the cumulative token count at the point of interrupt.
	TotalTokens int

	// StepCount is the number of steps completed before interrupt.
	StepCount int

	// Reason explains why the turn was interrupted.
	Reason InterruptReason

	// ReasonDetail provides additional context for the interrupt.
	ReasonDetail string

	// Timestamp is when the checkpoint was created.
	Timestamp time.Time

	// Metadata carries arbitrary key-value data for custom resume logic.
	Metadata map[string]any
}

// InterruptResumeConfig configures the interrupt/resume middleware.
type InterruptResumeConfig struct {
	// TurnTimeout is the maximum duration for a single turn.
	// When exceeded, the turn is interrupted and a checkpoint is saved.
	// 0 = no timeout.
	TurnTimeout time.Duration

	// OnInterrupt is called when a turn is interrupted.
	// The callback receives the checkpoint for external persistence.
	// If nil, checkpoints are only stored in memory.
	OnInterrupt func(checkpoint TurnCheckpoint)

	// ResumePromptTemplate is the prompt template used when resuming.
	// Use {original_prompt} as a placeholder for the original prompt, and
	// {partial_response} for any partial response received.
	// Default: "Continue from where you left off. Original task: {original_prompt}"
	ResumePromptTemplate string
}

// InterruptResume is a middleware that enables stateless interrupt and resume
// at turn boundaries. When a turn is interrupted (by user, limit, or timeout),
// it saves a checkpoint that can be used to construct a resume prompt.
//
// This implements the "stateless approach" — no engine state serialization.
// The resume is done by sending a new prompt that includes context about the
// interrupted turn.
//
// Usage:
//
//	ir := middleware.NewInterruptResume(middleware.InterruptResumeConfig{
//	    TurnTimeout: 5 * time.Minute,
//	    OnInterrupt: func(cp middleware.TurnCheckpoint) {
//	        // Save checkpoint to disk or database
//	        log.Printf("Turn interrupted: %s", cp.Reason)
//	    },
//	})
//	cfg.Middlewares = append(cfg.Middlewares, ir)
//
//	// Later: resume from checkpoint
//	resumePrompt := ir.BuildResumePrompt(checkpoint)
//	agent.Chat(ctx, resumePrompt)
type InterruptResume struct {
	config InterruptResumeConfig
	logger *slog.Logger

	mu              sync.Mutex
	turnIndex       int
	currentPrompt   string
	turnStart       time.Time
	partialText     string
	stepCount       int
	lastCheckpoint  *TurnCheckpoint
}

// NewInterruptResume creates a new interrupt/resume middleware.
func NewInterruptResume(cfg InterruptResumeConfig, logger *slog.Logger) *InterruptResume {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ResumePromptTemplate == "" {
		cfg.ResumePromptTemplate = "Continue from where you left off. Original task: {original_prompt}"
	}
	return &InterruptResume{
		config: cfg,
		logger: logger,
	}
}

func (ir *InterruptResume) Name() string { return "interrupt_resume" }

// PreTurn records the turn start time and prompt.
func (ir *InterruptResume) PreTurn(ctx context.Context, req *TurnRequest) (*TurnRequest, error) {
	ir.mu.Lock()
	ir.turnIndex++
	ir.currentPrompt = req.Prompt
	ir.turnStart = time.Now()
	ir.partialText = ""
	ir.stepCount = 0
	ir.mu.Unlock()

	// If a timeout is configured, inject a deadline into the metadata
	if ir.config.TurnTimeout > 0 {
		req.Metadata["turn_deadline"] = time.Now().Add(ir.config.TurnTimeout)
	}

	return req, nil
}

// PostTurn checks for timeout-based interrupts.
func (ir *InterruptResume) PostTurn(ctx context.Context, resp *TurnResponse) (*TurnResponse, error) {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	// Check if the turn exceeded the timeout
	if ir.config.TurnTimeout > 0 && time.Since(ir.turnStart) > ir.config.TurnTimeout {
		cp := ir.createCheckpointLocked(InterruptTimeout, "turn exceeded timeout")
		cp.PartialResponse = resp.Text
		cp.TotalTokens = resp.TotalTokens
		cp.StepCount = resp.StepCount
		ir.lastCheckpoint = &cp

		if ir.config.OnInterrupt != nil {
			ir.config.OnInterrupt(cp)
		}

		ir.logger.Warn("turn timed out — checkpoint saved",
			"turn", ir.turnIndex,
			"duration", time.Since(ir.turnStart),
			"timeout", ir.config.TurnTimeout,
		)
	}

	return resp, nil
}

// ProcessStep accumulates partial text from streaming events.
func (ir *InterruptResume) ProcessStep(ctx context.Context, event *StepEvent) (*StepEvent, error) {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	ir.stepCount++

	if event.TextDelta != "" {
		ir.partialText += event.TextDelta
	}

	return event, nil
}

// Interrupt manually creates a checkpoint at the current point.
// Call this from a user cancellation handler or limit exceeded hook.
func (ir *InterruptResume) Interrupt(reason InterruptReason, detail string) TurnCheckpoint {
	ir.mu.Lock()
	defer ir.mu.Unlock()

	cp := ir.createCheckpointLocked(reason, detail)
	ir.lastCheckpoint = &cp

	if ir.config.OnInterrupt != nil {
		ir.config.OnInterrupt(cp)
	}

	ir.logger.Info("turn interrupted — checkpoint saved",
		"turn", ir.turnIndex,
		"reason", reason,
		"detail", detail,
		"steps", ir.stepCount,
	)

	return cp
}

// LastCheckpoint returns the most recent checkpoint, or nil if none.
func (ir *InterruptResume) LastCheckpoint() *TurnCheckpoint {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	return ir.lastCheckpoint
}

// BuildResumePrompt constructs a prompt for resuming from a checkpoint.
func (ir *InterruptResume) BuildResumePrompt(cp TurnCheckpoint) string {
	prompt := ir.config.ResumePromptTemplate

	// Replace placeholders
	prompt = replaceAll(prompt, "{original_prompt}", cp.Prompt)
	prompt = replaceAll(prompt, "{partial_response}", cp.PartialResponse)
	prompt = replaceAll(prompt, "{reason}", string(cp.Reason))
	prompt = replaceAll(prompt, "{reason_detail}", cp.ReasonDetail)
	prompt = replaceAll(prompt, "{step_count}", fmt.Sprintf("%d", cp.StepCount))
	prompt = replaceAll(prompt, "{token_count}", fmt.Sprintf("%d", cp.TotalTokens))

	return prompt
}

// createCheckpointLocked creates a checkpoint from current state. Caller must hold mu.
func (ir *InterruptResume) createCheckpointLocked(reason InterruptReason, detail string) TurnCheckpoint {
	return TurnCheckpoint{
		TurnIndex:       ir.turnIndex,
		Prompt:          ir.currentPrompt,
		PartialResponse: ir.partialText,
		StepCount:       ir.stepCount,
		Reason:          reason,
		ReasonDetail:    detail,
		Timestamp:       time.Now(),
		Metadata:        make(map[string]any),
	}
}

// replaceAll is a simple string replacement helper.
func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
