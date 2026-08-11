package adk

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/divmora/localharness/adk/hooks"
	"github.com/divmora/localharness/adk/middleware"
	"github.com/divmora/localharness/adk/policy"
)

// LocalAgentConfig configures a local harness agent.
//
// Default policy: ConfirmRunCommand() — denies run_command and manage_task,
// allows all other tools.
type LocalAgentConfig struct {
	// SystemInstructions is the raw system prompt sent to the LLM.
	// If StructuredPrompt is set, it takes priority over this.
	SystemInstructions string

	// StructuredPrompt enables modular system prompt composition.
	// If set, takes priority over SystemInstructions.
	// See StructuredPrompt type for details.
	StructuredPrompt *StructuredPrompt

	// Capabilities controls which built-in tools are available.
	Capabilities CapabilitiesConfig

	// HostTools defines custom tools that execute on the SDK side.
	// When the LLM calls a host tool, the harness forwards it to the
	// registered handler and feeds the result back to the LLM.
	// Tool names must be unique and must not collide with built-in harness tools.
	HostTools []HostToolDef

	// Policies is the list of tool call policies. Evaluated in priority order.
	// Default: policy.ConfirmRunCommand()
	Policies []policy.Policy

	// Hooks is a list of hooks to register with the agent.
	Hooks []hooks.Hook

	// Middlewares is an ordered list of middlewares for the turn pipeline.
	// PreTurn middlewares transform the prompt before it reaches the harness.
	// PostTurn middlewares post-process the response after the turn completes.
	// StepMiddlewares intercept individual streaming events.
	// Middlewares run in registration order for PreTurn/Step, reverse for PostTurn.
	Middlewares []middleware.Middleware

	// Workspaces is a list of workspace definitions the agent can operate in.
	// Default: current working directory.
	Workspaces []WorkspaceDef

	// ConversationID resumes an existing conversation. Empty = new conversation.
	ConversationID string

	// --- LLM Provider Configuration ---

	// LitellmEndpoint is the name of a LiteLLM proxy endpoint defined in ~/.divmora/config/litellm.json.
	// If set, this overrides other provider configs and routes traffic through the specified LiteLLM endpoint.
	LitellmEndpoint string

	// Inline LiteLLM configuration. Takes priority over LitellmEndpoint and ~/.divmora/config/litellm.json.
	LitellmAPIKey  string
	LitellmBaseURL string
	LitellmModel   string

	// --- Advanced ---

	// BinaryPath is the path to the localharness binary.
	// Default: auto-detected from PATH.
	BinaryPath string

	// CompactionThreshold controls context window compaction.
	//   0  = use harness default (100K tokens).
	//   -1 = disable compaction entirely.
	//   >0 = custom threshold (compact when token count exceeds this value).
	CompactionThreshold int

	// MaxSubagentDepth is the maximum nesting depth for subagents.
	// Default: 3. Set to 0 to disable subagents.
	MaxSubagentDepth int

	// MaxConcurrentSubagents is the maximum number of concurrent child agents.
	// Default: 5.
	MaxConcurrentSubagents int

	// McpServers configures connections to external MCP servers.
	// MCP tools are discovered at session init and treated identically to built-in tools.
	// These are merged with global config from ~/.divmora/config/mcp_config.json.
	McpServers []McpServer

	// Logger is an optional custom logger for the SDK client.
	Logger *slog.Logger

	// Verbose enables verbose debug logging to standard error if Logger is not set.
	Verbose bool

	// MaxTotalTokens enforces a session-level token usage limit. 0 = unlimited.
	MaxTotalTokens int

	// MaxAutoWakeTurns caps how many synthetic turns the agent can take
	// when background tasks or timers fire while the engine is idle.
	// 0 = auto-wake disabled (default). Set to e.g. 10 for autonomous agents.
	// The counter resets when a real user message arrives.
	MaxAutoWakeTurns int

	// --- Prompt Modules ---

	// EnablePlanningMode enables the plan-before-act workflow.
	// When enabled, the agent will research → plan → seek approval → execute → verify
	// for complex requests. OFF by default.
	EnablePlanningMode bool

	// EnableWebDevelopment enables the web development design system section.
	// Adds design aesthetics, CSS/JS patterns, and SEO guidance. OFF by default.
	EnableWebDevelopment bool

	// EnableSlashCommands enables the slash commands module in the system prompt.
	// When enabled, the agent can recommend slash commands to users. OFF by default.
	// Requires SlashCommands to be populated to be useful.
	EnableSlashCommands bool

	// SlashCommands defines the available slash commands the agent can recommend.
	// Only relevant when EnableSlashCommands is true.
	SlashCommands []SlashCommand

	// EnableKnowledgeItems enables the knowledge items module in the system prompt.
	// Teaches the agent to check curated KI summaries before starting research.
	// OFF by default. Set to true for IDE agents with a KI store.
	EnableKnowledgeItems bool

	// Skills defines available standalone skills. If non-empty, the <skills>
	// section is emitted in both the system prompt and per-message context.
	// Data-driven: no toggle needed — presence of data IS the toggle.
	Skills []SkillDef

	// Plugins defines installed plugin bundles. If non-empty, the <plugins>
	// section is emitted in both the system prompt and per-message context.
	// Data-driven: no toggle needed — presence of data IS the toggle.
	Plugins []PluginDef

	// SubagentTypes defines additional subagent types the agent can invoke.
	// These are merged with built-in types (research, self). SDK-registered
	// types override built-ins with the same name.
	// Data-driven: no toggle needed — presence of data IS the toggle.
	SubagentTypes []SubagentTypeDef

	// ExcludeBuiltinSubagents is a list of built-in subagent type names to exclude.
	// Use this to remove built-in types you don't want the agent to use.
	// Example: []string{"self"} to remove the self-clone type.
	ExcludeBuiltinSubagents []string

	// UserRules are ADK-injected user rules. Each rule carries a label (display name)
	// and inline content. These are merged with auto-discovered AGENTS.md files
	// and rendered in the <user_rules> section of every user message.
	// Data-driven: no toggle needed — presence of data IS the toggle.
	UserRules []UserRule

	// QuestionHandler is called when the agent needs user input via ask_question.
	// If nil, questions are auto-skipped.
	QuestionHandler QuestionHandlerFunc
}

// McpServer configures a connection to an MCP server.
type McpServer struct {
	// Name is a unique identifier for this server.
	Name string

	// --- Stdio transport (launch as subprocess) ---

	// Command is the binary to execute (e.g., "npx", "uvx", "node").
	// Mutually exclusive with URL.
	Command string

	// Args are command arguments (e.g., ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]).
	Args []string

	// --- HTTP/SSE transport (connect to endpoint) ---

	// URL is the SSE/Streamable HTTP endpoint. Mutually exclusive with Command.
	URL string

	// Headers are auth/custom headers for HTTP transport.
	Headers map[string]string

	// --- Options ---

	// Env sets extra environment variables for the server process.
	Env map[string]string

	// Tools is a whitelist of tool names to expose. Empty = all discovered tools.
	Tools []string
}



// NewLocalAgentConfig creates a config with safe defaults:
//   - ConfirmRunCommand policy (deny shell, allow rest)
//   - All safe tools enabled
//   - Current working directory as workspace
func NewLocalAgentConfig() *LocalAgentConfig {
	cwd, _ := os.Getwd()
	workspaces := []WorkspaceDef{}
	if cwd != "" {
		workspaces = []WorkspaceDef{{Directory: cwd}}
	}

	return &LocalAgentConfig{
		Capabilities: DefaultCapabilities(),
		Policies:     policy.ConfirmRunCommand(),
		Workspaces:   workspaces,
	}
}

// Validate checks the configuration for errors.
func (c *LocalAgentConfig) Validate() error {
	// We no longer validate LLM keys because everything runs through the global litellm.json
	// config on the backend. The LitellmEndpoint field is optional.

	// Safety: require policies when write/side-effect tools or MCP servers are enabled.
	// This prevents accidentally running an autonomous agent with full write access.
	// MCP servers are treated as write-capable since their tools can have side effects.
	hasWriteTools := c.Capabilities.CreateFile ||
		c.Capabilities.EditFile ||
		c.Capabilities.RunCommand ||
		c.Capabilities.ManageTask ||
		c.Capabilities.InvokeSubagent ||
		len(c.McpServers) > 0
	hasPolicies := len(c.Policies) > 0
	hasDecideHooks := false
	for _, h := range c.Hooks {
		if _, ok := h.(hooks.PreToolCallDecideHook); ok {
			hasDecideHooks = true
			break
		}
	}

	if hasWriteTools && !hasPolicies && !hasDecideHooks {
		return fmt.Errorf(
			"write tools are enabled without a safety policy. " +
				"Add Policies: policy.AllowAll() to approve all tool calls, " +
				"or Policies: policy.ConfirmRunCommand() to deny shell access",
		)
	}

	// Validate AskUser policies have handlers
	for _, p := range c.Policies {
		if p.Decision == policy.AskUser && p.Handler == nil {
			name := p.Name
			if name == "" {
				name = p.Tool
			}
			return fmt.Errorf("AskUser policy '%s' is missing a handler", name)
		}
	}

	// Validate host tools: no nil handlers, no duplicates, no built-in name collisions.
	if err := validateHostTools(c.HostTools); err != nil {
		return err
	}

	return nil
}

// reservedToolNames is the set of built-in harness tool names.
// Host tools cannot use these names — harness tools always have priority.
var reservedToolNames = map[string]bool{
	"view_file":                  true,
	"write_to_file":              true,
	"replace_file_content":       true,
	"multi_replace_file_content": true,
	"list_dir":                   true,
	"grep_search":                true,
	"find_file":                  true,
	"run_command":                true,
	"manage_task":                true,
	"finish":                     true,
	"ask_question":               true,
	"ask_permission":             true,
	"list_permissions":           true,
	"search_web":                 true,
	"read_url_content":           true,
	"schedule":                   true,
	"invoke_subagent":            true,
	"define_subagent":            true,
	"manage_subagents":           true,
	"send_message":               true,
	"knowledge_read":             true,
	"knowledge_write":            true,
	"publish":                    true,
}

// validateHostTools checks host tool definitions for errors:
//   - Each tool must have a non-empty name and a non-nil handler.
//   - Tool names must not collide with built-in harness tools.
//   - Tool names must be unique (no duplicates).
func validateHostTools(tools []HostToolDef) error {
	if len(tools) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(tools))
	for _, ht := range tools {
		if ht.Name == "" {
			return fmt.Errorf("host tool has empty name")
		}
		if ht.Handler == nil {
			return fmt.Errorf("host tool %q has nil handler", ht.Name)
		}
		if reservedToolNames[ht.Name] {
			return fmt.Errorf("host tool %q conflicts with a built-in harness tool", ht.Name)
		}
		if seen[ht.Name] {
			return fmt.Errorf("duplicate host tool name: %q", ht.Name)
		}
		seen[ht.Name] = true
	}
	return nil
}
