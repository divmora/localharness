package hooks

import "fmt"

// HookRunner manages hook registration and dispatches lifecycle events.
type HookRunner struct {
	preToolCallDecideHooks   []PreToolCallDecideHook
	postToolCallHooks        []PostToolCallHook
	preTurnHooks             []PreTurnHook
	postTurnHooks            []PostTurnHook
	onSessionStartHooks      []OnSessionStartHook
	onSessionEndHooks        []OnSessionEndHook
	onToolErrorHooks         []OnToolErrorHook
	onInteractionHooks       []OnInteractionHook
	onCompactionHooks        []OnCompactionHook
	onArtifactFeedbackHooks  []OnArtifactFeedbackHook

	sessionContext *HookContext
}

// NewHookRunner creates a new HookRunner.
func NewHookRunner() *HookRunner {
	return &HookRunner{
		sessionContext: NewHookContext(),
	}
}

// RegisterHook registers a hook by inferring its type from the interface it implements.
// A single hook can implement multiple interfaces and will be registered for all of them.
func (r *HookRunner) RegisterHook(hook Hook) error {
	registered := false

	if h, ok := hook.(PreToolCallDecideHook); ok {
		r.preToolCallDecideHooks = append(r.preToolCallDecideHooks, h)
		registered = true
	}
	if h, ok := hook.(PostToolCallHook); ok {
		r.postToolCallHooks = append(r.postToolCallHooks, h)
		registered = true
	}
	if h, ok := hook.(PreTurnHook); ok {
		r.preTurnHooks = append(r.preTurnHooks, h)
		registered = true
	}
	if h, ok := hook.(PostTurnHook); ok {
		r.postTurnHooks = append(r.postTurnHooks, h)
		registered = true
	}
	if h, ok := hook.(OnSessionStartHook); ok {
		r.onSessionStartHooks = append(r.onSessionStartHooks, h)
		registered = true
	}
	if h, ok := hook.(OnSessionEndHook); ok {
		r.onSessionEndHooks = append(r.onSessionEndHooks, h)
		registered = true
	}
	if h, ok := hook.(OnToolErrorHook); ok {
		r.onToolErrorHooks = append(r.onToolErrorHooks, h)
		registered = true
	}
	if h, ok := hook.(OnInteractionHook); ok {
		r.onInteractionHooks = append(r.onInteractionHooks, h)
		registered = true
	}
	if h, ok := hook.(OnCompactionHook); ok {
		r.onCompactionHooks = append(r.onCompactionHooks, h)
		registered = true
	}
	if h, ok := hook.(OnArtifactFeedbackHook); ok {
		r.onArtifactFeedbackHooks = append(r.onArtifactFeedbackHooks, h)
		registered = true
	}

	if !registered {
		return fmt.Errorf("unknown hook type: %T", hook)
	}
	return nil
}

// HasHooks returns true if any hooks are registered.
func (r *HookRunner) HasHooks() bool {
	return len(r.preToolCallDecideHooks) > 0 ||
		len(r.postToolCallHooks) > 0 ||
		len(r.preTurnHooks) > 0 ||
		len(r.postTurnHooks) > 0 ||
		len(r.onSessionStartHooks) > 0 ||
		len(r.onSessionEndHooks) > 0 ||
		len(r.onToolErrorHooks) > 0 ||
		len(r.onInteractionHooks) > 0 ||
		len(r.onCompactionHooks) > 0 ||
		len(r.onArtifactFeedbackHooks) > 0
}

// HasPreToolCallDecideHooks returns true if any pre-tool-call decide hooks are registered.
func (r *HookRunner) HasPreToolCallDecideHooks() bool {
	return len(r.preToolCallDecideHooks) > 0
}

// DispatchPreToolCall evaluates all pre-tool-call decide hooks.
// Returns the first deny result, or an allow result if all hooks pass.
func (r *HookRunner) DispatchPreToolCall(tc ToolCall) HookResult {
	ctx := r.sessionContext.Child() // Turn-scoped context
	opCtx := ctx.Child()           // Operation-scoped context

	for _, hook := range r.preToolCallDecideHooks {
		result := hook.Run(opCtx, tc)
		if !result.Allow {
			return result
		}
	}
	return HookResult{Allow: true}
}

// DispatchPostToolCall notifies all post-tool-call hooks.
func (r *HookRunner) DispatchPostToolCall(result ToolResult) {
	ctx := r.sessionContext.Child().Child() // Operation context

	for _, hook := range r.postToolCallHooks {
		hook.Run(ctx, result)
	}
}

// DispatchPreTurn evaluates all pre-turn hooks.
// Returns the first deny result, or an allow result if all hooks pass.
func (r *HookRunner) DispatchPreTurn(prompt string) HookResult {
	ctx := r.sessionContext.Child() // Turn-scoped context

	for _, hook := range r.preTurnHooks {
		result := hook.Run(ctx, prompt)
		if !result.Allow {
			return result
		}
	}
	return HookResult{Allow: true}
}

// DispatchPostTurn notifies all post-turn hooks.
func (r *HookRunner) DispatchPostTurn(response string) {
	ctx := r.sessionContext.Child()

	for _, hook := range r.postTurnHooks {
		hook.Run(ctx, response)
	}
}

// DispatchSessionStart notifies all session-start hooks.
func (r *HookRunner) DispatchSessionStart() {
	for _, hook := range r.onSessionStartHooks {
		hook.Run(r.sessionContext)
	}
}

// DispatchSessionEnd notifies all session-end hooks.
func (r *HookRunner) DispatchSessionEnd() {
	for _, hook := range r.onSessionEndHooks {
		hook.Run(r.sessionContext)
	}
}

// DispatchToolError notifies all tool-error hooks.
// Returns the first handled result, or an unhandled result if no hook recovers.
func (r *HookRunner) DispatchToolError(te ToolError) ToolErrorResult {
	ctx := r.sessionContext.Child().Child() // Operation context

	for _, hook := range r.onToolErrorHooks {
		result := hook.Run(ctx, te)
		if result.Handled {
			return result
		}
	}
	return ToolErrorResult{Handled: false}
}

// DispatchInteraction notifies all interaction hooks.
// Returns the first non-dismissed result, or a dismissed result if no hook responds.
func (r *HookRunner) DispatchInteraction(interaction Interaction) InteractionResult {
	ctx := r.sessionContext.Child()

	for _, hook := range r.onInteractionHooks {
		result := hook.Run(ctx, interaction)
		if !result.Dismissed {
			return result
		}
	}
	return InteractionResult{Dismissed: true}
}

// DispatchCompaction notifies all compaction hooks (observability only).
func (r *HookRunner) DispatchCompaction(event CompactionEvent) {
	ctx := r.sessionContext.Child()

	for _, hook := range r.onCompactionHooks {
		hook.Run(ctx, event)
	}
}

// HasOnToolErrorHooks returns true if any tool-error hooks are registered.
func (r *HookRunner) HasOnToolErrorHooks() bool {
	return len(r.onToolErrorHooks) > 0
}

// HasOnInteractionHooks returns true if any interaction hooks are registered.
func (r *HookRunner) HasOnInteractionHooks() bool {
	return len(r.onInteractionHooks) > 0
}

// DispatchArtifactFeedback notifies all artifact feedback hooks (observability only).
// Called when the agent creates an artifact with RequestFeedback=true.
func (r *HookRunner) DispatchArtifactFeedback(event ArtifactFeedbackEvent) {
	ctx := r.sessionContext.Child()

	for _, hook := range r.onArtifactFeedbackHooks {
		hook.Run(ctx, event)
	}
}

// HasOnArtifactFeedbackHooks returns true if any artifact feedback hooks are registered.
func (r *HookRunner) HasOnArtifactFeedbackHooks() bool {
	return len(r.onArtifactFeedbackHooks) > 0
}
