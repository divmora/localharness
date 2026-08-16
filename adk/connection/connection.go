// Package connection defines the connection interface for communicating with
// a localharness agent backend.
package connection

import (
	"context"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// Connection is the interface for a live session with a localharness backend.
// The local implementation communicates over WebSocket + Protobuf.
type Connection interface {
	// Send sends a user prompt to the agent.
	Send(ctx context.Context, prompt string) error

	// SendWithContext sends a user prompt with per-message host context and optional ephemeral messages.
	SendWithContext(ctx context.Context, prompt string, userCtx *pb.UserContext, ephemeralMsgs []string) error

	// ReceiveSteps returns a channel that receives steps as they arrive.
	// The channel is closed when the turn completes.
	ReceiveSteps(ctx context.Context) (<-chan Step, error)

	// SendPermissionResponse sends a permission decision back to the harness.
	SendPermissionResponse(ctx context.Context, requestID string, approved bool, reason string) error

	// SendQuestionResponse sends the user's answers back to the harness.
	SendQuestionResponse(ctx context.Context, requestID string, answers []*QuestionAnswer, skipped bool) error

	// SendToolResult sends a host tool result back to the harness.
	SendToolResult(ctx context.Context, stepID, toolName, resultJSON string, isError bool) error

	// Close shuts down the connection and releases resources.
	Close() error

	// ConversationID returns the active conversation identifier.
	ConversationID() string

	// FetchAgentCard fetches the A2A agent card from the harness.
	// Returns nil if the harness does not support agent cards.
	FetchAgentCard(ctx context.Context) (*AgentCard, error)
}

// AgentCard is the A2A discovery document served at /.well-known/agent.json.
type AgentCard struct {
	Name               string              `json:"name"`
	Description        string              `json:"description,omitempty"`
	Version            string              `json:"version"`
	URL                string              `json:"url,omitempty"`
	DocumentationURL   string              `json:"documentationUrl,omitempty"`
	Provider           *AgentCardProvider  `json:"provider,omitempty"`
	Capabilities       AgentCardCapabilities `json:"capabilities"`
	DefaultInputModes  []string            `json:"defaultInputModes"`
	DefaultOutputModes []string            `json:"defaultOutputModes"`
	Skills             []AgentCardSkill    `json:"skills,omitempty"`
}

// AgentCardProvider describes the entity providing the agent.
type AgentCardProvider struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// AgentCardCapabilities advertises supported protocol features.
type AgentCardCapabilities struct {
	Streaming              bool   `json:"streaming"`
	PushNotifications      bool   `json:"pushNotifications"`
	StateTransitionHistory bool   `json:"stateTransitionHistory"`
	A2AVersion             string `json:"a2aVersion,omitempty"`
}

// AgentCardSkill describes a specific task the agent can perform.
type AgentCardSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// Step represents a step received from the harness.
type Step struct {
	// Index is the step's position in the trajectory.
	Index int32

	// Text is the step content (for model responses).
	Text string

	// TextDelta is a streaming text chunk.
	TextDelta string

	// Thinking is the model's reasoning trace.
	Thinking string

	// ThinkingDelta is a streaming thinking chunk.
	ThinkingDelta string

	// ToolName is the name of the tool being called (empty for text steps).
	ToolName string

	// ToolArgsJSON is the raw JSON arguments for the tool call.
	ToolArgsJSON string

	// ToolArgs is the parsed arguments map.
	ToolArgs map[string]any

	// PermissionRequestID is set when the harness is asking for permission.
	PermissionRequestID string

	// PermissionToolName is the tool name in a permission request.
	PermissionToolName string

	// PermissionArgsSummary is the human-readable summary in a permission request.
	PermissionArgsSummary string

	// State is the step's lifecycle state.
	State StepState

	// Source indicates who produced this step.
	Source StepSource

	// ErrorMessage is set when State is StateError.
	ErrorMessage string

	// ErrorCode is the error code when State is StateError.
	ErrorCode string

	// ErrorMetadata contains structured error context when State is StateError.
	ErrorMetadata map[string]string

	// IsFinal indicates this is the last step in the turn.
	IsFinal bool

	// QuestionRequestID is set when the harness is asking the user a question.
	QuestionRequestID string

	// Questions is the list of questions being asked.
	Questions []UserQuestion

	// Usage contains token usage metadata for this step (if populated by model).
	Usage *UsageMetadata

	// Compaction fields (set when engine compacts context window)
	CompactionOriginalTokens  int
	CompactionCompactedTokens int
	CompactionMessagesRemoved int
	CompactionSummary         string

	// ToolResultContent is the tool's output content when a tool completes.
	ToolResultContent string

	// ToolResultIsError indicates the tool returned an error result.
	ToolResultIsError bool

	// IsHostToolCall is true when this step is a host-side (SDK-registered)
	// tool call that requires the SDK to execute and return a result.
	IsHostToolCall bool
}

// UsageMetadata tracks token consumption for a model call.
type UsageMetadata struct {
	PromptTokens     int
	CompletionTokens int
	ThinkingTokens   int
	TotalTokens      int
	CachedTokens     int
}

// StepState represents the lifecycle state of a step.
type StepState int

const (
	StateUnspecified StepState = iota
	StateActive
	StateDone
	StateWaiting
	StateError
	StateStreaming
)

// StepSource indicates who produced a step.
type StepSource int

const (
	SourceUnspecified StepSource = iota
	SourceSystem
	SourceUser
	SourceModel
)

// IsToolCall returns true if this step represents a tool call.
func (s *Step) IsToolCall() bool {
	return s.ToolName != "" && s.State == StateActive
}

// IsPermissionRequest returns true if this step is asking for permission.
func (s *Step) IsPermissionRequest() bool {
	return s.PermissionRequestID != "" && s.State == StateWaiting
}

// IsTextResponse returns true if this step contains a model text response.
func (s *Step) IsTextResponse() bool {
	return s.Text != "" && s.ToolName == "" && s.State == StateDone
}

// IsStreamingDelta returns true if this step contains a streaming delta.
func (s *Step) IsStreamingDelta() bool {
	return s.State == StateStreaming
}

// IsQuestionRequest returns true if this step is asking the user a question.
func (s Step) IsQuestionRequest() bool {
	return s.QuestionRequestID != ""
}

// IsCompaction returns true if this step is a context compaction event.
func (s Step) IsCompaction() bool {
	return s.CompactionOriginalTokens > 0
}

// IsToolComplete returns true if this step is a completed tool call.
func (s Step) IsToolComplete() bool {
	return s.ToolName != "" && s.State == StateDone && s.ToolResultContent != ""
}

// UserQuestion represents a question being asked to the user.
type UserQuestion struct {
	Question      string
	Options       []string
	IsMultiSelect bool
}

// QuestionAnswer represents the user's response to a question.
type QuestionAnswer struct {
	SelectedIndices []int32
	SelectedOptions []string
	Text            string
}
