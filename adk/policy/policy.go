// Package policy provides a declarative policy system for controlling tool call
// permissions in the LocalHarness Go ADK.
//
// Policies are evaluated using a priority-based model where specificity and
// safety determine precedence:
//
//	Specific Deny > Specific Ask > Specific Allow >
//	Wildcard Deny > Wildcard Ask > Wildcard Allow
//
// Within each priority group, first match wins (short-circuit evaluation).
//
// Usage:
//
//	policies := []policy.Policy{
//	    policy.Deny("run_command"),            // Always deny shell commands
//	    policy.Allow("view_file"),             // Always allow file reads
//	    policy.Ask("write_to_file", myHandler),  // Ask user for file creation
//	    policy.AllowAll(),                     // Allow everything else
//	}
//	hook := policy.Enforce(policies)
//	// Register hook with HookRunner
package policy

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/divmora/localharness/adk/hooks"
)

const wildcard = "*"

// Decision is the outcome a policy can produce.
type Decision int

const (
	// Approve allows the tool call to proceed.
	Approve Decision = iota
	// Deny blocks the tool call and returns a denial message to the LLM.
	Deny
	// AskUser invokes the handler to obtain user approval.
	AskUser
)

// Predicate tests tool call arguments. Returns true if the policy applies.
// If nil, the policy always applies.
type Predicate func(toolName string, args map[string]any) bool

// AskUserHandler is called when a policy decision is AskUser.
// Returns true if the user approves the tool call.
type AskUserHandler func(toolName string, args map[string]any) (approved bool, err error)

// Option configures optional Policy fields.
type Option func(*Policy)

// WithName sets a human-readable name for the policy (used in logs and deny messages).
func WithName(name string) Option {
	return func(p *Policy) { p.Name = name }
}

// WithPredicate sets a conditional predicate on the policy.
func WithPredicate(pred Predicate) Option {
	return func(p *Policy) { p.When = pred }
}

// Policy is a single declarative rule for a tool call.
type Policy struct {
	// Tool is the tool name to match, or "*" for all tools.
	Tool string

	// Decision is the outcome when this policy matches.
	Decision Decision

	// When is an optional predicate on the tool call's arguments.
	// If nil, the policy matches any call to the named tool.
	When Predicate

	// Handler is invoked when Decision is AskUser. Must be provided for
	// AskUser policies (validated at Enforce() time).
	Handler AskUserHandler

	// Name is a human-readable label used in logging and deny reasons.
	Name string
}

// --- Builder Helpers ---

// Allow creates an APPROVE policy for the given tool.
func Allow(tool string, opts ...Option) Policy {
	p := Policy{Tool: tool, Decision: Approve}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// DenyRule creates a DENY policy for the given tool.
func DenyRule(tool string, opts ...Option) Policy {
	p := Policy{Tool: tool, Decision: Deny}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// Ask creates an ASK_USER policy for the given tool. handler is required.
func Ask(tool string, handler AskUserHandler, opts ...Option) Policy {
	p := Policy{Tool: tool, Decision: AskUser, Handler: handler}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// AllowAll creates a wildcard APPROVE policy. Equivalent to Allow("*").
func AllowAll() Policy {
	return Allow(wildcard, WithName("allow_all"))
}

// DenyAll creates a wildcard DENY policy.
func DenyAll() Policy {
	return DenyRule(wildcard, WithName("deny_all"))
}

// --- Presets ---

// ReadOnlyTools is the set of tools that are considered read-only / safe.

var ReadOnlyTools = map[string]bool{
	"view_file":  true,
	"list_dir":   true,
	"grep_search": true,
	"find_file":  true,
	"finish":     true,
}

// FileTools is the set of tools that operate on file paths.
var FileTools = map[string]bool{
	"view_file":                  true,
	"write_to_file":              true,
	"replace_file_content":       true,
	"multi_replace_file_content": true,
	"list_dir":                   true,
	"grep_search":                 true,
	"find_file":                  true,
}

// ConfirmRunCommand is the default policy: deny run_command and manage_task,
// allow everything else.
func ConfirmRunCommand() []Policy {
	return []Policy{
		DenyRule("run_command", WithName("confirm_run_command")),
		DenyRule("manage_task", WithName("confirm_run_command")),
		AllowAll(),
	}
}

// SafeDefaults allows read-only tools and asks for everything else.

func SafeDefaults(handler AskUserHandler) []Policy {
	policies := make([]Policy, 0, len(ReadOnlyTools)+1)
	for tool := range ReadOnlyTools {
		policies = append(policies, Allow(tool, WithName("safe_defaults")))
	}
	policies = append(policies, Ask(wildcard, handler, WithName("safe_defaults")))
	return policies
}

// WorkspaceOnlyOption configures optional behavior for WorkspaceOnly.
type WorkspaceOnlyOption func(*workspaceOnlyConfig)

type workspaceOnlyConfig struct {
	allowedPaths []string // Additional directories to allow beyond workspace roots
}

// WithAllowedPaths adds additional directories that are allowed beyond workspace roots.
// Useful for allowing writes to the brain/artifacts directory (AppDataDir).
func WithAllowedPaths(paths ...string) WorkspaceOnlyOption {
	return func(c *workspaceOnlyConfig) {
		c.allowedPaths = append(c.allowedPaths, paths...)
	}
}

// WorkspaceOnly denies file tools targeting paths outside the given workspace
// directories (and any additional allowed paths).
func WorkspaceOnly(workspaces []string, opts ...WorkspaceOnlyOption) []Policy {
	var cfg workspaceOnlyConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	// Normalize workspace paths
	absWorkspaces := make([]string, len(workspaces))
	for i, ws := range workspaces {
		abs, err := filepath.Abs(ws)
		if err != nil {
			absWorkspaces[i] = ws
		} else {
			absWorkspaces[i] = abs
		}
	}

	// Normalize additional allowed paths
	absAllowed := make([]string, 0, len(cfg.allowedPaths))
	for _, p := range cfg.allowedPaths {
		abs, err := filepath.Abs(p)
		if err != nil {
			absAllowed = append(absAllowed, p)
		} else {
			absAllowed = append(absAllowed, abs)
		}
	}

	outsideWorkspace := func(toolName string, args map[string]any) bool {
		path, _ := args["path"].(string)
		if path == "" {
			return false // No path argument — allow
		}

		// Resolve the path to an absolute path for comparison.
		// For relative paths, resolve against the first workspace root
		// (matching the engine's workspace.Manager.ValidatePath behavior).
		// Using filepath.Abs() here would resolve against the SDK process
		// CWD, which may differ from the workspace directory.
		var absPath string
		if filepath.IsAbs(path) {
			absPath = filepath.Clean(path)
		} else if len(absWorkspaces) > 0 {
			absPath = filepath.Join(absWorkspaces[0], path)
		} else {
			var err error
			absPath, err = filepath.Abs(path)
			if err != nil {
				return true // Can't resolve — deny for safety
			}
		}
		for _, ws := range absWorkspaces {
			if absPath == ws || strings.HasPrefix(absPath, ws+string(filepath.Separator)) {
				return false // Inside a workspace
			}
		}
		for _, ap := range absAllowed {
			if absPath == ap || strings.HasPrefix(absPath, ap+string(filepath.Separator)) {
				return false // Inside an allowed path
			}
		}
		return true // Outside all workspaces and allowed paths
	}

	var policies []Policy
	for tool := range FileTools {
		policies = append(policies, DenyRule(tool,
			WithName("workspace_only"),
			WithPredicate(outsideWorkspace),
		))
	}
	return policies
}

// --- Priority Buckets ---

const (
	levelSpecificDeny  = 0
	levelSpecificAsk   = 1
	levelSpecificAllow = 2
	levelWildcardDeny  = 3
	levelWildcardAsk   = 4
	levelWildcardAllow = 5
	numLevels          = 6
)

var decisionToSpecificLevel = map[Decision]int{
	Deny:    levelSpecificDeny,
	AskUser: levelSpecificAsk,
	Approve: levelSpecificAllow,
}

var decisionToWildcardLevel = map[Decision]int{
	Deny:    levelWildcardDeny,
	AskUser: levelWildcardAsk,
	Approve: levelWildcardAllow,
}

func bucketIndex(p Policy) int {
	if p.Tool == wildcard {
		return decisionToWildcardLevel[p.Decision]
	}
	return decisionToSpecificLevel[p.Decision]
}

// --- PolicyDecideHook ---

// EnforceOption configures optional behavior for Enforce.
type EnforceOption func(*policyDecideHook)

// WithLogger configures policyDecideHook to log decisions to the given logger.
func WithLogger(logger *slog.Logger) EnforceOption {
	return func(h *policyDecideHook) {
		h.logger = logger
	}
}

// policyDecideHook implements hooks.PreToolCallDecideHook.
// Created by Enforce(). Policies are pre-sorted into priority buckets at
// construction time; evaluation walks buckets high-to-low and short-circuits
// on the first matching policy.
type policyDecideHook struct {
	buckets [numLevels][]Policy
	logger  *slog.Logger
}

// Run evaluates all policies against the tool call.
func (h *policyDecideHook) Run(ctx *hooks.HookContext, tc hooks.ToolCall) hooks.HookResult {
	for _, bucket := range h.buckets {
		for _, p := range bucket {
			// Check tool name match
			if p.Tool != wildcard && p.Tool != tc.Name {
				continue
			}

			// Check predicate
			if p.When != nil && !p.When(tc.Name, tc.Args) {
				continue
			}

			// First match in this bucket wins
			return h.apply(p, tc)
		}
	}

	// No policy matched — default open
	return hooks.HookResult{Allow: true}
}

func (h *policyDecideHook) apply(p Policy, tc hooks.ToolCall) hooks.HookResult {
	label := p.Name
	if label == "" {
		label = p.Tool
	}

	logger := h.logger
	if logger == nil {
		logger = slog.Default()
	}

	switch p.Decision {
	case Deny:
		logger.Info("policy denied tool", "policy", label, "tool", tc.Name)
		return hooks.HookResult{
			Allow:   false,
			Message: fmt.Sprintf("Denied by policy '%s'.", label),
		}

	case Approve:
		logger.Debug("policy approved tool", "policy", label, "tool", tc.Name)
		return hooks.HookResult{Allow: true}

	case AskUser:
		logger.Info("policy requesting user approval", "policy", label, "tool", tc.Name)
		approved, err := p.Handler(tc.Name, tc.Args)
		if err != nil {
			logger.Error("ask_user handler error — failing closed", "policy", label, "error", err)
			return hooks.HookResult{
				Allow:   false,
				Message: fmt.Sprintf("Policy handler error for '%s': %v", label, err),
			}
		}
		if approved {
			return hooks.HookResult{Allow: true}
		}
		return hooks.HookResult{
			Allow:   false,
			Message: fmt.Sprintf("User denied tool '%s' (policy '%s').", tc.Name, label),
		}
	}

	return hooks.HookResult{Allow: true}
}

// --- Public Factory ---

// Enforce creates a PreToolCallDecideHook that enforces the given policies.
//
// Validates policies at construction time:
//   - Every AskUser policy must have a handler.
//
// Policies are bucketed by priority so evaluation can short-circuit.
// Returns an error if validation fails.
func Enforce(policies []Policy, opts ...EnforceOption) (hooks.PreToolCallDecideHook, error) {
	// Validation
	for _, p := range policies {
		if p.Decision == AskUser && p.Handler == nil {
			name := p.Name
			if name == "" {
				name = p.Tool
			}
			return nil, fmt.Errorf(
				"AskUser policy '%s' is missing a handler. Provide one via policy.Ask(tool, handler)",
				name,
			)
		}
	}

	// Build priority buckets, preserving registration order within each
	hook := &policyDecideHook{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(hook)
	}

	for _, p := range policies {
		idx := bucketIndex(p)
		hook.buckets[idx] = append(hook.buckets[idx], p)
	}

	return hook, nil
}

// MustEnforce is like Enforce but panics on validation errors.
func MustEnforce(policies []Policy, opts ...EnforceOption) hooks.PreToolCallDecideHook {
	hook, err := Enforce(policies, opts...)
	if err != nil {
		panic(err)
	}
	return hook
}
