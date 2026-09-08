package middleware

import (
	"context"
	"fmt"
	"log/slog"
)

// Chain manages an ordered list of middlewares and executes them in sequence.
//
// Execution order:
//   - PreTurn middlewares run in registration order (first registered → first to run).
//   - PostTurn middlewares run in reverse order (last registered → first to run).
//   - StepMiddlewares run in registration order.
//
// This mirrors the standard HTTP middleware convention where the outermost
// middleware wraps the innermost, so pre-processing flows inward and
// post-processing flows outward.
type Chain struct {
	preTurn  []PreTurnMiddleware
	postTurn []PostTurnMiddleware
	step     []StepMiddleware
	logger   *slog.Logger
}

// NewChain creates a middleware chain from the given middlewares.
// Each middleware is inspected for which phase interfaces it implements
// and registered accordingly. A middleware can implement multiple phases.
func NewChain(logger *slog.Logger, middlewares ...Middleware) *Chain {
	c := &Chain{logger: logger}
	if c.logger == nil {
		c.logger = slog.Default()
	}

	for _, m := range middlewares {
		registered := false

		if pt, ok := m.(PreTurnMiddleware); ok {
			c.preTurn = append(c.preTurn, pt)
			registered = true
		}
		if pt, ok := m.(PostTurnMiddleware); ok {
			c.postTurn = append(c.postTurn, pt)
			registered = true
		}
		if sm, ok := m.(StepMiddleware); ok {
			c.step = append(c.step, sm)
			registered = true
		}

		if !registered {
			c.logger.Warn("middleware does not implement any phase interface",
				"name", m.Name(),
			)
		} else {
			c.logger.Debug("registered middleware", "name", m.Name())
		}
	}

	return c
}

// HasPreTurn returns true if any PreTurnMiddleware is registered.
func (c *Chain) HasPreTurn() bool {
	return len(c.preTurn) > 0
}

// HasPostTurn returns true if any PostTurnMiddleware is registered.
func (c *Chain) HasPostTurn() bool {
	return len(c.postTurn) > 0
}

// HasStep returns true if any StepMiddleware is registered.
func (c *Chain) HasStep() bool {
	return len(c.step) > 0
}

// IsEmpty returns true if no middlewares are registered.
func (c *Chain) IsEmpty() bool {
	return !c.HasPreTurn() && !c.HasPostTurn() && !c.HasStep()
}

// RunPreTurn executes all PreTurnMiddlewares in registration order.
// Each middleware receives the output of the previous one.
// If any middleware returns an error, the chain stops and the error is returned.
func (c *Chain) RunPreTurn(ctx context.Context, req *TurnRequest) (*TurnRequest, error) {
	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}

	current := req
	for _, m := range c.preTurn {
		c.logger.Debug("running PreTurn middleware", "name", m.Name())

		result, err := m.PreTurn(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("middleware %q PreTurn: %w", m.Name(), err)
		}
		if result == nil {
			return nil, fmt.Errorf("middleware %q PreTurn returned nil request", m.Name())
		}
		current = result
	}

	return current, nil
}

// RunPostTurn executes all PostTurnMiddlewares in reverse registration order.
// Each middleware receives the output of the previous one.
// If any middleware returns an error, the chain stops and the error is returned.
func (c *Chain) RunPostTurn(ctx context.Context, resp *TurnResponse) (*TurnResponse, error) {
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}

	current := resp
	// Reverse order for post-processing
	for i := len(c.postTurn) - 1; i >= 0; i-- {
		m := c.postTurn[i]
		c.logger.Debug("running PostTurn middleware", "name", m.Name())

		result, err := m.PostTurn(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("middleware %q PostTurn: %w", m.Name(), err)
		}
		if result == nil {
			return nil, fmt.Errorf("middleware %q PostTurn returned nil response", m.Name())
		}
		current = result
	}

	return current, nil
}

// RunStep executes all StepMiddlewares in registration order.
// Returns the (possibly modified) event. If ShouldFilter is set by any
// middleware, subsequent middlewares still run but the event is ultimately
// suppressed from the caller.
// If any middleware returns an error, the chain stops and the error is returned.
func (c *Chain) RunStep(ctx context.Context, event *StepEvent) (*StepEvent, error) {
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}

	current := event
	for _, m := range c.step {
		result, err := m.ProcessStep(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("middleware %q ProcessStep: %w", m.Name(), err)
		}
		if result == nil {
			return nil, fmt.Errorf("middleware %q ProcessStep returned nil event", m.Name())
		}
		current = result
	}

	return current, nil
}
