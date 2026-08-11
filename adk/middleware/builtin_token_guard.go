package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// TokenGuard is a PostTurnMiddleware and StepMiddleware that tracks cumulative
// token usage and enforces budget limits with configurable warning thresholds.
//
// When the cumulative token count exceeds a warning threshold, a warning is
// logged. When it exceeds the hard limit, PostTurn returns an error and
// subsequent PreTurn calls are rejected.
//
// Usage:
//
//	guard := middleware.NewTokenGuard(100000, 0.8) // 100K limit, warn at 80%
//	cfg.Middlewares = append(cfg.Middlewares, guard)
//
//	// Later: check usage
//	fmt.Println(guard.TotalTokens())     // cumulative tokens used
//	fmt.Println(guard.BudgetExhausted()) // true if over limit
type TokenGuard struct {
	maxTokens     int
	warnThreshold float64 // fraction (0.0-1.0) at which to warn
	logger        *slog.Logger

	mu          sync.Mutex
	totalTokens int
	warned      bool
	exhausted   bool
}

// NewTokenGuard creates a token budget middleware.
//
// Parameters:
//   - maxTokens: hard limit on cumulative tokens. 0 = unlimited (guard only logs).
//   - warnThreshold: fraction (0.0-1.0) at which to emit a warning log.
//     Use 0.0 to disable warnings. Default recommendation: 0.8 (80%).
func NewTokenGuard(maxTokens int, warnThreshold float64, logger *slog.Logger) *TokenGuard {
	if logger == nil {
		logger = slog.Default()
	}
	if warnThreshold < 0 {
		warnThreshold = 0
	}
	if warnThreshold > 1 {
		warnThreshold = 1
	}
	return &TokenGuard{
		maxTokens:     maxTokens,
		warnThreshold: warnThreshold,
		logger:        logger,
	}
}

func (t *TokenGuard) Name() string { return "token_guard" }

// PreTurn rejects turns if the token budget is exhausted.
func (t *TokenGuard) PreTurn(ctx context.Context, req *TurnRequest) (*TurnRequest, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.exhausted {
		return nil, fmt.Errorf("token budget exhausted: %d / %d tokens used", t.totalTokens, t.maxTokens)
	}

	return req, nil
}

// PostTurn checks the cumulative token usage after a turn completes.
// Token counts are extracted from the TurnResponse.TotalTokens field.
func (t *TokenGuard) PostTurn(ctx context.Context, resp *TurnResponse) (*TurnResponse, error) {
	if resp.TotalTokens <= 0 {
		return resp, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalTokens += resp.TotalTokens

	if t.maxTokens <= 0 {
		// No limit set — just track
		t.logger.Debug("token usage update",
			"turn_tokens", resp.TotalTokens,
			"cumulative", t.totalTokens,
		)
		return resp, nil
	}

	usage := float64(t.totalTokens) / float64(t.maxTokens)

	// Check warning threshold
	if !t.warned && t.warnThreshold > 0 && usage >= t.warnThreshold {
		t.warned = true
		t.logger.Warn("token budget warning threshold reached",
			"cumulative", t.totalTokens,
			"max", t.maxTokens,
			"usage_pct", fmt.Sprintf("%.1f%%", usage*100),
			"threshold", fmt.Sprintf("%.0f%%", t.warnThreshold*100),
		)
	}

	// Check hard limit
	if t.totalTokens >= t.maxTokens {
		t.exhausted = true
		t.logger.Error("token budget exhausted",
			"cumulative", t.totalTokens,
			"max", t.maxTokens,
		)
		// Don't return an error here — let this turn's response through.
		// The NEXT PreTurn will reject.
		resp.Metadata["token_budget_exhausted"] = true
	}

	return resp, nil
}

// TotalTokens returns the cumulative token count across all turns.
func (t *TokenGuard) TotalTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalTokens
}

// BudgetExhausted returns true if the token budget has been exceeded.
func (t *TokenGuard) BudgetExhausted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exhausted
}

// Reset clears the token count and resets the exhaustion state.
func (t *TokenGuard) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalTokens = 0
	t.warned = false
	t.exhausted = false
}
