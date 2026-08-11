package adk

import "context"

// StructuredPrompt composes a system prompt from modular XML-tagged sections.
// The engine auto-generates tool and workspace sections and assembles them
// with user-provided identity, guidelines, and custom sections.
//
// Example:
//
//	prompt := &adk.StructuredPrompt{
//	    Identity:   "You are DevBot, a DevOps assistant.",
//	    Guidelines: "Always write tests. Keep functions small.",
//	    Sections: []adk.PromptSection{
//	        {Tag: "project_context", Content: "This is a Go microservice using gRPC."},
//	        {Tag: "security_rules", Content: "Never expose secrets in logs.", Priority: 30},
//	    },
//	}
type StructuredPrompt struct {
	// Identity sets who the agent is.
	// Default: "You are a helpful AI coding assistant."
	Identity string

	// Guidelines are behavioral rules the agent should always follow.
	Guidelines string

	// CommunicationStyle controls how the agent formats responses.
	CommunicationStyle string

	// Sections are custom tagged content blocks.
	// Each section is rendered as <tag>content</tag> in the final prompt.
	Sections []PromptSection
}

// PromptSection is a tagged content block in the system prompt.
type PromptSection struct {
	// Tag is the XML tag name (e.g., "rules", "project_context").
	Tag string

	// Content is the section body.
	Content string

	// Priority controls ordering (lower = earlier in prompt). Default: 100.
	Priority int
}

// SlashCommand defines a user-facing chat shortcut the agent can recommend.
type SlashCommand struct {
	// Name is the slash command (e.g., "/goal", "/schedule").
	Name string

	// Description is a human-readable explanation of what the command does.
	Description string
}

// WorkspaceDef describes a workspace directory the agent can operate in.
type WorkspaceDef struct {
	// Directory is the absolute path to the workspace directory.
	Directory string

	// CorpusName is an optional semantic search corpus identifier
	// (e.g. "divmora/localharness"). When set, the agent's per-message
	// context renders a URI→CorpusName mapping matching Antigravity's format.
	CorpusName string
}

// ToolHandlerFunc is the callback invoked when the LLM calls a host-registered tool.
// It receives the request context and the parsed tool arguments.
// Return a JSON-serializable result, or an error to report a tool failure to the LLM.
type ToolHandlerFunc func(ctx context.Context, args map[string]any) (result any, err error)

// HostToolDef defines a custom tool registered by the SDK.
// The harness forwards these to your handler when the LLM calls them.
//
// Example:
//
//	adk.HostToolDef{
//	    Name:        "get_weather",
//	    Description: "Get current weather for a city",
//	    Parameters: map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "city": map[string]any{
//	                "type":        "string",
//	                "description": "City name, e.g. 'Tokyo'",
//	            },
//	        },
//	        "required": []string{"city"},
//	    },
//	    Handler: func(ctx context.Context, args map[string]any) (any, error) {
//	        city, _ := args["city"].(string)
//	        return map[string]string{"city": city, "temp": "22°C"}, nil
//	    },
//	}
type HostToolDef struct {
	// Name is the tool name the LLM will use to call it.
	// Must not collide with built-in harness tool names.
	Name string

	// Description tells the LLM what this tool does.
	Description string

	// Parameters is the JSON Schema for the tool's arguments.
	// This is passed directly to the LLM's function calling interface.
	Parameters map[string]any

	// Handler is the function called when the LLM invokes this tool.
	// Must not be nil.
	Handler ToolHandlerFunc
}

// SkillDef describes an available skill the agent can use.
// Skills are folders containing a SKILL.md instruction file and optional
// scripts, examples, and resources that extend the agent's capabilities.
type SkillDef struct {
	// Name is the skill identifier (e.g., "run-security-scanner").
	Name string

	// Description is a human-readable explanation of what the skill does.
	Description string

	// SkillPath is the absolute path to the SKILL.md file.
	SkillPath string
}

// PluginDef describes an installed plugin bundle.
// Plugins group skills, subagents, and configuration together for a
// specific feature or domain.
type PluginDef struct {
	// Name is the plugin identifier (e.g., "securecoder").
	Name string

	// Description is an optional human-readable explanation.
	Description string

	// Path is the path to the plugin directory.
	Path string

	// Skills are the skills exposed by this plugin.
	Skills []SkillDef
}

// SubagentTypeDef describes a subagent type that the agent can invoke.
// Each type defines a specialized agent with its own system prompt and
// configurable tool access. Use SDK-registered types to extend built-in
// types (research, self) with custom domain-specific agents.
type SubagentTypeDef struct {
	// Name is the unique identifier for this subagent type.
	Name string

	// Description is a human-readable explanation of what this subagent does.
	Description string

	// SystemPrompt defines the subagent's role, capabilities, and behavior.
	SystemPrompt string

	// EnableWriteTools allows the subagent to create/edit files and run commands.
	// Default: false (read-only).
	EnableWriteTools bool

	// EnableMCPTools allows the subagent to call MCP tools.
	// Default: false.
	EnableMCPTools bool

	// EnableSubagentTools allows the subagent to define and invoke its own subagents.
	// Default: false.
	EnableSubagentTools bool
}

// UserRule is a labeled user rule to inject into the agent's per-message context.
// ADK-injected rules are merged with auto-discovered AGENTS.md files and rendered
// in the <user_rules> section of every user message with <RULE[label]> wrapping.
type UserRule struct {
	// Label identifies the rule source (e.g. "settings.json", "team-standards").
	// Appears as the tag name: <RULE[label]>...</RULE[label]>.
	Label string

	// Content is the rule text, injected verbatim.
	Content string
}

// FileEntry describes a file with optional language annotation.
// Used in MessageContext to provide the agent with file-level metadata.
type FileEntry struct {
	// Path is the absolute file path.
	Path string

	// Language is an optional language identifier (e.g. "LANGUAGE_GO", "LANGUAGE_PYTHON").
	Language string
}

// MessageContext provides per-message metadata from the host (IDE, CLI, etc.).
// Pass to ChatWithContext() to give the LLM awareness of the user's current state.
type MessageContext struct {
	// ActiveFile is the currently open/focused file.
	ActiveFile *FileEntry

	// CursorLine is the cursor line position (1-indexed).
	CursorLine int

	// OpenFiles is a list of other open files.
	OpenFiles []FileEntry

	// Extra is extensible key-value metadata.
	Extra map[string]string

	// EphemeralMessages are system directives injected per-message.
	// The agent follows these strictly but does not acknowledge them to the user.
	// Use for contextual instructions like "focus on security" or "respond in French".
	EphemeralMessages []string
}

// ChatResponse is the final response from an Agent.Chat() call.
type ChatResponse struct {
	// Text is the model's final text response.
	Text string

	// Thinking is the model's reasoning trace (if using a thinking model).
	Thinking string

	// Usage contains token usage metadata for the turn.
	Usage *UsageMetadata

	// Steps is the list of steps that occurred during this turn.
	Steps []Step
}

// UsageMetadata tracks token consumption for a model call.
type UsageMetadata struct {
	PromptTokens     int
	CompletionTokens int
	ThinkingTokens   int
	TotalTokens      int
	CachedTokens     int
}

// Step represents one action in the agent's trajectory.
type Step struct {
	// Index is the step's position in the trajectory.
	Index int32

	// Text is the step's text content (for model responses).
	Text string

	// ToolName is the tool being called (empty for text responses).
	ToolName string

	// ToolArgs is the tool call arguments (nil for text responses).
	ToolArgs map[string]any

	// State is the step's lifecycle state.
	State StepState

	// Source indicates who produced this step.
	Source StepSource

	// ErrorMessage is set when State is StateError.
	ErrorMessage string
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

// --- AskQuestion Types ---

// QuestionHandlerFunc is called when the agent asks the user a question.
// The handler should present the questions to the user and return their answers.
type QuestionHandlerFunc func(ctx context.Context, questions []Question) (*QuestionResponse, error)

// Question represents a question from the agent to the user.
type Question struct {
	// Text is the question text.
	Text string

	// Options are the selectable options (at least 2).
	Options []string

	// IsMultiSelect allows selecting multiple options.
	IsMultiSelect bool
}

// QuestionResponse holds the user's answers.
type QuestionResponse struct {
	// Answers contains one answer per question.
	Answers []Answer

	// Skipped is true if the user chose not to answer.
	Skipped bool
}

// Answer represents the user's response to a single question.
type Answer struct {
	// SelectedIndices are the 0-based indices of chosen options.
	SelectedIndices []int32

	// SelectedOptions are the text values of chosen options.
	SelectedOptions []string

	// Text is a free-form write-in response.
	Text string
}

// --- A2A Agent Card Types ---

// AgentCardInfo is the SDK-facing representation of an A2A agent card.
// Returned by Agent.FetchAgentCard().
type AgentCardInfo struct {
	Name               string
	Description        string
	Version            string
	URL                string
	DocumentationURL   string
	Provider           *AgentCardProviderInfo
	Capabilities       AgentCardCapabilities
	DefaultInputModes  []string
	DefaultOutputModes []string
	Skills             []AgentCardSkillInfo
}

// AgentCardProviderInfo describes who provides the agent.
type AgentCardProviderInfo struct {
	Name string
	URL  string
}

// AgentCardCapabilities describes protocol features the agent supports.
type AgentCardCapabilities struct {
	Streaming              bool
	PushNotifications      bool
	StateTransitionHistory bool
	A2AVersion             string
}

// AgentCardSkillInfo describes a specific skill the agent can perform.
type AgentCardSkillInfo struct {
	ID          string
	Name        string
	Description string
	Tags        []string
	Examples    []string
}

// CapabilitiesConfig controls which built-in tools are available.
type CapabilitiesConfig struct {
	ViewFile       bool
	CreateFile     bool
	EditFile       bool
	ListDir        bool
	SearchDir      bool
	FindFile       bool
	RunCommand     bool
	Finish         bool
	ManageTask     bool
	InvokeSubagent bool
	WebSearch      bool
	WebFetch       bool
	Schedule       bool
	Browser        bool // Requires Node.js + npx; auto-injects @playwright/mcp
}

// DefaultCapabilities returns capabilities with all safe tools enabled
// and run_command/manage_task disabled.
func DefaultCapabilities() CapabilitiesConfig {
	return CapabilitiesConfig{
		ViewFile:       true,
		CreateFile:     true,
		EditFile:       true,
		ListDir:        true,
		SearchDir:      true,
		FindFile:       true,
		RunCommand:     false,
		Finish:         true,
		ManageTask:     false,
		InvokeSubagent: false,
		WebSearch:      true,
		WebFetch:       true,
		Schedule:       true,
	}
}

// ReadOnlyCapabilities returns capabilities with only read-only tools enabled.
func ReadOnlyCapabilities() CapabilitiesConfig {
	return CapabilitiesConfig{
		ViewFile:       true,
		ListDir:        true,
		SearchDir:      true,
		FindFile:       true,
		Finish:         true,
		InvokeSubagent: false,
		WebSearch:      true,
		WebFetch:       true,
		Schedule:       true,
	}
}

// AllTools returns capabilities with every tool enabled.
// Must be paired with explicit policies (e.g., policy.AllowAll()) or
// the mandatory safety validation will reject the config.
func AllTools() CapabilitiesConfig {
	return CapabilitiesConfig{
		ViewFile:       true,
		CreateFile:     true,
		EditFile:       true,
		ListDir:        true,
		SearchDir:      true,
		FindFile:       true,
		RunCommand:     true,
		Finish:         true,
		ManageTask:     true,
		InvokeSubagent: true,
		WebSearch:      true,
		WebFetch:       true,
		Schedule:       true,
		Browser:        true,
	}
}

// NoTools returns capabilities with all tools disabled.
// The agent can only produce text responses.
func NoTools() CapabilitiesConfig {
	return CapabilitiesConfig{
		Finish: true, // Finish is always enabled so the agent can end gracefully
	}
}

// NondestructiveTools returns capabilities that allow reading and creating
// files but not editing existing files or running commands.
// Useful for documentation, scaffolding, and code generation tasks.
func NondestructiveTools() CapabilitiesConfig {
	return CapabilitiesConfig{
		ViewFile:   true,
		CreateFile: true,
		ListDir:    true,
		SearchDir:  true,
		FindFile:   true,
		Finish:     true,
		WebSearch:  true,
		WebFetch:   true,
		Schedule:   true,
	}
}

// --- Streaming API ---

// StreamEvent is emitted in real-time during ChatStream().
// Check the Type field to determine which data fields are populated.
type StreamEvent struct {
	// Type identifies the kind of event.
	Type StreamEventType

	// Step is the processed step (populated for ToolCallStart, ToolCallDone, Error events).
	Step *Step

	// TextDelta is an incremental text chunk from the model (for EventTextDelta).
	TextDelta string

	// ThinkingDelta is an incremental thinking chunk from the model (for EventThinkingDelta).
	ThinkingDelta string

	// Response is the final ChatResponse (for EventTurnComplete only).
	// Nil if the turn ended with an error.
	Response *ChatResponse

	// Error is set when the turn ends due to a transport or protocol error.
	// Only populated with EventTurnComplete when Response is nil.
	Error error
}

// StreamEventType identifies the kind of streaming event.
type StreamEventType int

const (
	// EventTextDelta delivers an incremental text chunk from the model.
	// StreamEvent.TextDelta is populated.
	EventTextDelta StreamEventType = iota + 1

	// EventThinkingDelta delivers an incremental thinking/reasoning chunk.
	// StreamEvent.ThinkingDelta is populated.
	EventThinkingDelta

	// EventToolCallStart signals that a tool call has begun (STATE_ACTIVE).
	// StreamEvent.Step is populated with the tool name and args.
	EventToolCallStart

	// EventToolCallDone signals that a tool call has completed (STATE_DONE).
	// StreamEvent.Step is populated with the final state.
	EventToolCallDone

	// EventError signals a non-fatal step error (tool failure, permission denied, etc.).
	// The agentic loop continues. StreamEvent.Step is populated.
	EventError

	// EventTurnComplete signals the end of the turn.
	// StreamEvent.Response contains the final ChatResponse.
	// If Error is non-nil, the turn ended abnormally and Response may be nil.
	EventTurnComplete

	// EventCompaction signals that the engine compacted conversation history.
	// StreamEvent.Step is populated with compaction metadata.
	EventCompaction
)

// String returns a human-readable name for the event type.
func (t StreamEventType) String() string {
	switch t {
	case EventTextDelta:
		return "TextDelta"
	case EventThinkingDelta:
		return "ThinkingDelta"
	case EventToolCallStart:
		return "ToolCallStart"
	case EventToolCallDone:
		return "ToolCallDone"
	case EventError:
		return "Error"
	case EventTurnComplete:
		return "TurnComplete"
	case EventCompaction:
		return "Compaction"
	default:
		return "Unknown"
	}
}
