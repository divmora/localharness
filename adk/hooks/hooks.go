// Package hooks provides the lifecycle hook interfaces for the LocalHarness Go ADK.
//
// Hooks allow SDK consumers to observe and intercept agent lifecycle events
// such as tool calls, turns, and session boundaries.
//
// Hook categories:
//   - InspectHook: Read-only, non-blocking (observability)
//   - DecideHook: Read-only, blocking (policy decisions)
//   - TransformHook: Modifying, blocking (data transformation)
package hooks

// HookResult is the outcome of a DecideHook evaluation.
type HookResult struct {
	// Allow indicates whether the operation should proceed.
	Allow bool

	// Message provides a human-readable explanation when Allow is false.
	// This message is fed back to the LLM so it can adapt.
	Message string
}

// HookContext provides shared state across hook invocations.
// Contexts are hierarchical: Session → Turn → Operation.
type HookContext struct {
	parent *HookContext
	store  map[string]any
}

// NewHookContext creates a new root HookContext.
func NewHookContext() *HookContext {
	return &HookContext{store: make(map[string]any)}
}

// Child creates a child context that inherits from this context.
func (c *HookContext) Child() *HookContext {
	return &HookContext{parent: c, store: make(map[string]any)}
}

// Get retrieves a value from this context or its parents.
func (c *HookContext) Get(key string) (any, bool) {
	if v, ok := c.store[key]; ok {
		return v, true
	}
	if c.parent != nil {
		return c.parent.Get(key)
	}
	return nil, false
}

// Set stores a value in this context (local scope only).
func (c *HookContext) Set(key string, value any) {
	c.store[key] = value
}

// ToolCall represents a tool invocation as seen by hooks.
type ToolCall struct {
	// Name is the tool name (e.g., "run_command", "view_file").
	Name string

	// Args is the tool call's argument map.
	Args map[string]any

	// CallID is the LLM-assigned call identifier.
	CallID string

	// ArgsJSON is the raw JSON representation of the arguments.
	ArgsJSON string
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	// Name is the tool name.
	Name string

	// Content is the result content.
	Content string

	// IsError indicates whether the tool call failed.
	IsError bool

	// CallID is the LLM-assigned call identifier.
	CallID string
}

// ToolError represents a tool execution failure with recovery options.
type ToolError struct {
	// ToolName is the tool that failed.
	ToolName string

	// Error is the error that occurred.
	Error error

	// Args is the tool call arguments that caused the failure.
	Args map[string]any

	// CallID is the LLM-assigned call identifier.
	CallID string
}

// ToolErrorResult is the outcome of an OnToolErrorHook.
type ToolErrorResult struct {
	// Handled indicates whether the hook handled the error.
	// If true, RecoveryContent is used as the tool result instead of the error.
	Handled bool

	// RecoveryContent is the substitute result when Handled is true.
	RecoveryContent string
}

// Interaction represents a question or prompt from the agent to the user.
type Interaction struct {
	// Question is the text of the agent's question.
	Question string

	// Options is the list of suggested options (may be empty for free-text).
	Options []string

	// StepIndex is the step that triggered this interaction.
	StepIndex int32
}

// InteractionResult is the response to an agent interaction.
type InteractionResult struct {
	// Response is the user's answer.
	Response string

	// Dismissed indicates the user chose not to respond.
	Dismissed bool
}

// CompactionEvent is emitted when the engine compacts conversation history.
type CompactionEvent struct {
	// OriginalTokens is the token count before compaction.
	OriginalTokens int

	// CompactedTokens is the token count after compaction.
	CompactedTokens int

	// MessagesRemoved is the number of messages removed.
	MessagesRemoved int

	// Summary is the LLM-generated summary of removed messages.
	Summary string
}

// ArtifactFeedbackEvent is emitted when the agent creates or updates an artifact
// with RequestFeedback=true, signaling that the artifact needs user review.
// SDKs can use this to surface the artifact prominently (e.g., show a plan
// review dialog) or pause execution until the user responds.
type ArtifactFeedbackEvent struct {
	// Path is the absolute path to the artifact file.
	Path string

	// Filename is the base name of the artifact (e.g., "implementation_plan.md").
	Filename string

	// ArtifactType identifies the kind of artifact (e.g., "implementation_plan",
	// "walkthrough", "task", "other").
	ArtifactType string

	// Summary is a description of the artifact contents.
	Summary string
}

// --- Hook Interfaces ---

// PreToolCallDecideHook is called before each tool execution.
// Returning HookResult{Allow: false} prevents the tool from executing.
// This is the primary hook for policy enforcement.
type PreToolCallDecideHook interface {
	Run(ctx *HookContext, tc ToolCall) HookResult
}

// PostToolCallHook is called after a tool completes (observability only).
type PostToolCallHook interface {
	Run(ctx *HookContext, result ToolResult)
}

// PreTurnHook is called before a turn starts.
// Returning HookResult{Allow: false} prevents the turn from executing.
type PreTurnHook interface {
	Run(ctx *HookContext, prompt string) HookResult
}

// PostTurnHook is called after a turn completes (observability only).
type PostTurnHook interface {
	Run(ctx *HookContext, response string)
}

// OnSessionStartHook is called when the agent session starts.
type OnSessionStartHook interface {
	Run(ctx *HookContext)
}

// OnSessionEndHook is called when the agent session ends.
type OnSessionEndHook interface {
	Run(ctx *HookContext)
}

// OnToolErrorHook is called when a tool execution fails.
// Returning ToolErrorResult{Handled: true} suppresses the error and
// provides a recovery value to the LLM instead.
type OnToolErrorHook interface {
	Run(ctx *HookContext, te ToolError) ToolErrorResult
}

// OnInteractionHook is called when the agent asks the user a question.
// The hook can provide an answer programmatically without human intervention.
type OnInteractionHook interface {
	Run(ctx *HookContext, interaction Interaction) InteractionResult
}

// OnCompactionHook is called when conversation history is compacted.
// This is an observability hook — it cannot prevent compaction.
type OnCompactionHook interface {
	Run(ctx *HookContext, event CompactionEvent)
}

// OnArtifactFeedbackHook is called when the agent creates an artifact with
// RequestFeedback=true. This is an observability hook that allows SDKs to
// detect when the agent wants user review (e.g., on an implementation plan)
// and surface it to the user.
type OnArtifactFeedbackHook interface {
	Run(ctx *HookContext, event ArtifactFeedbackEvent)
}

// Hook is the union type for all hook interfaces.
// Used for type-agnostic registration.
type Hook interface{}
