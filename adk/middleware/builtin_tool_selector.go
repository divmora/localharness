package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// ToolSelectorMode determines how the tool selector operates.
type ToolSelectorMode int

const (
	// ToolSelectorAllow means only the listed tools are recommended.
	// The LLM is instructed to prefer these tools and avoid others.
	ToolSelectorAllow ToolSelectorMode = iota

	// ToolSelectorDeny means the listed tools should be avoided.
	// The LLM is instructed not to use these tools.
	ToolSelectorDeny
)

// ToolSelectorConfig configures the dynamic tool selection middleware.
type ToolSelectorConfig struct {
	// Mode determines whether the tool list is an allow-list or deny-list.
	Mode ToolSelectorMode

	// Tools is the list of tool names to allow or deny.
	// Examples: "view_file", "run_command", "write_to_file", "grep_search".
	Tools []string

	// Reason is an optional explanation injected into the prompt.
	// Example: "This is a read-only research task."
	Reason string

	// Dynamic is an optional callback invoked per-turn to determine the
	// tool set dynamically based on the prompt content.
	// If non-nil, the static Tools/Mode fields are ignored and this
	// function's return value is used instead.
	// Return (nil, "", "") to skip tool guidance for this turn.
	Dynamic func(prompt string) (mode ToolSelectorMode, tools []string, reason string)

	// WarnOnViolation logs a warning when the LLM uses a discouraged tool.
	// Does NOT block execution — the policy layer handles that.
	WarnOnViolation bool
}

// ToolSelector is a middleware that injects tool usage guidance into the
// prompt via a structured directive. This approach works without modifying
// the tool schema — the LLM sees all tools but receives clear instructions
// about which to prefer or avoid.
//
// This is inspired by CloudWeGo Eino's tool selection patterns but adapted
// for the process-isolated harness architecture where the SDK cannot
// directly filter server-side tool schemas.
//
// Usage:
//
//	// Static: only allow read tools for research tasks
//	selector := middleware.NewToolSelector(middleware.ToolSelectorConfig{
//	    Mode:  middleware.ToolSelectorAllow,
//	    Tools: []string{"view_file", "list_dir", "grep_search", "find_file"},
//	    Reason: "This is a read-only research task.",
//	})
//
//	// Dynamic: adjust tools per-turn based on prompt analysis
//	selector := middleware.NewToolSelector(middleware.ToolSelectorConfig{
//	    Dynamic: func(prompt string) (middleware.ToolSelectorMode, []string, string) {
//	        if strings.Contains(prompt, "read only") {
//	            return middleware.ToolSelectorAllow,
//	                []string{"view_file", "list_dir", "grep_search"},
//	                "User requested read-only mode"
//	        }
//	        return 0, nil, "" // No guidance
//	    },
//	    WarnOnViolation: true,
//	})
type ToolSelector struct {
	config ToolSelectorConfig
	logger *slog.Logger

	// Track violations for observability
	violations map[string]int

	// Per-turn cached resolved config (set in PreTurn, read in ProcessStep).
	// This ensures dynamic mode violations use the same config that was
	// resolved for the current turn's prompt, not a fallback.
	resolvedMode  ToolSelectorMode
	resolvedTools []string
}

// NewToolSelector creates a new ToolSelector middleware.
func NewToolSelector(cfg ToolSelectorConfig, logger *slog.Logger) *ToolSelector {
	if logger == nil {
		logger = slog.Default()
	}
	return &ToolSelector{
		config:     cfg,
		logger:     logger,
		violations: make(map[string]int),
	}
}

func (t *ToolSelector) Name() string { return "tool_selector" }

// PreTurn injects tool guidance into the prompt.
func (t *ToolSelector) PreTurn(ctx context.Context, req *TurnRequest) (*TurnRequest, error) {
	mode, tools, reason := t.resolveConfig(req.Prompt)

	// Cache resolved config for ProcessStep (so dynamic mode works correctly)
	t.resolvedMode = mode
	t.resolvedTools = tools

	if len(tools) == 0 {
		return req, nil
	}

	directive := buildToolDirective(mode, tools, reason)
	req.Prompt = directive + "\n\n" + req.Prompt

	// Store in metadata for downstream consumers
	req.Metadata["tool_selector_mode"] = mode
	req.Metadata["tool_selector_tools"] = tools
	req.Metadata["tool_selector_reason"] = reason

	t.logger.Debug("tool guidance injected",
		"mode", modeString(mode),
		"tools", len(tools),
		"reason", reason,
	)

	return req, nil
}

// ProcessStep checks for tool selection violations (using a discouraged tool).
// Uses the cached config from PreTurn to ensure dynamic mode violations are
// checked against the correct per-turn tool set.
func (t *ToolSelector) ProcessStep(ctx context.Context, event *StepEvent) (*StepEvent, error) {
	if !t.config.WarnOnViolation || event.ToolName == "" || event.ToolState != "active" {
		return event, nil
	}

	// Use cached config from PreTurn (correct for dynamic mode).
	// Fall back to static config if PreTurn hasn't been called yet.
	mode := t.resolvedMode
	tools := t.resolvedTools
	if len(tools) == 0 {
		mode = t.config.Mode
		tools = t.config.Tools
	}
	if len(tools) == 0 {
		return event, nil
	}

	violated := false
	switch mode {
	case ToolSelectorAllow:
		// Violation if tool is NOT in allow list
		if !contains(tools, event.ToolName) {
			violated = true
		}
	case ToolSelectorDeny:
		// Violation if tool IS in deny list
		if contains(tools, event.ToolName) {
			violated = true
		}
	}

	if violated {
		t.violations[event.ToolName]++
		t.logger.Warn("tool selection violation",
			"tool", event.ToolName,
			"mode", modeString(mode),
			"count", t.violations[event.ToolName],
		)
		event.Metadata["tool_selection_violation"] = true
	}

	return event, nil
}

// Violations returns the cumulative count of tool selection violations by tool name.
func (t *ToolSelector) Violations() map[string]int {
	result := make(map[string]int, len(t.violations))
	for k, v := range t.violations {
		result[k] = v
	}
	return result
}

// resolveConfig returns the effective mode, tools, and reason for this turn.
func (t *ToolSelector) resolveConfig(prompt string) (ToolSelectorMode, []string, string) {
	if t.config.Dynamic != nil && prompt != "" {
		return t.config.Dynamic(prompt)
	}
	return t.config.Mode, t.config.Tools, t.config.Reason
}

// Predefined tool groups for convenience.
var (
	// ReadOnlyTools is the set of tools that only read data.
	ReadOnlyTools = []string{
		"view_file", "list_dir", "grep_search", "find_file",
		"search_web", "read_url_content",
	}

	// WriteTools is the set of tools that modify the filesystem or run commands.
	WriteTools = []string{
		"write_to_file", "replace_file_content", "multi_replace_file_content",
		"run_command", "manage_task",
	}

	// DangerousTools is the subset of write tools with high risk.
	DangerousTools = []string{
		"run_command", "manage_task",
	}
)

// buildToolDirective creates the prompt injection text.
func buildToolDirective(mode ToolSelectorMode, tools []string, reason string) string {
	var b strings.Builder
	b.WriteString("<tool_guidance>\n")

	switch mode {
	case ToolSelectorAllow:
		b.WriteString("For this task, you should ONLY use the following tools:\n")
		for _, tool := range tools {
			fmt.Fprintf(&b, "- %s\n", tool)
		}
		b.WriteString("\nDo NOT use any other tools unless absolutely necessary. ")
		b.WriteString("If you need a tool not in this list, explain why before using it.")
	case ToolSelectorDeny:
		b.WriteString("For this task, you must NOT use the following tools:\n")
		for _, tool := range tools {
			fmt.Fprintf(&b, "- %s\n", tool)
		}
		b.WriteString("\nUse alternative approaches instead of these tools.")
	}

	if reason != "" {
		fmt.Fprintf(&b, "\nReason: %s", reason)
	}

	b.WriteString("\n</tool_guidance>")
	return b.String()
}

func modeString(mode ToolSelectorMode) string {
	switch mode {
	case ToolSelectorAllow:
		return "allow"
	case ToolSelectorDeny:
		return "deny"
	default:
		return "unknown"
	}
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}
