package policy

import (
	"fmt"
	"testing"

	"github.com/divmora/localharness/adk/hooks"
)

// alwaysApprove is a test AskUserHandler that always approves.
func alwaysApprove(toolName string, args map[string]any) (bool, error) {
	return true, nil
}

// alwaysDeny is a test AskUserHandler that always denies.
func alwaysDeny(toolName string, args map[string]any) (bool, error) {
	return false, nil
}

// failHandler is a test AskUserHandler that returns an error.
func failHandler(toolName string, args map[string]any) (bool, error) {
	return false, fmt.Errorf("handler error")
}

func runPolicy(t *testing.T, policies []Policy, tc hooks.ToolCall) hooks.HookResult {
	t.Helper()
	hook, err := Enforce(policies)
	if err != nil {
		t.Fatalf("Enforce failed: %v", err)
	}
	ctx := hooks.NewHookContext()
	return hook.Run(ctx, tc)
}

// --- Priority Tests ---

func TestSpecificDenyBeatsWildcardAllow(t *testing.T) {
	policies := []Policy{
		AllowAll(),                    // Wildcard allow (lowest priority)
		DenyRule("run_command"),        // Specific deny (highest priority)
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("specific deny should beat wildcard allow")
	}
}

func TestSpecificAllowBeatsWildcardDeny(t *testing.T) {
	policies := []Policy{
		DenyAll(),                 // Wildcard deny
		Allow("view_file"),       // Specific allow (higher priority)
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "view_file"})
	if !result.Allow {
		t.Fatal("specific allow should beat wildcard deny")
	}

	// But other tools should be denied
	result = runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("wildcard deny should apply to unmatched tools")
	}
}

func TestSpecificDenyBeatsSpecificAllow(t *testing.T) {
	policies := []Policy{
		Allow("run_command"),       // Specific allow
		DenyRule("run_command"),    // Specific deny (higher priority)
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("specific deny should beat specific allow")
	}
}

func TestSpecificAskBeatsSpecificAllow(t *testing.T) {
	policies := []Policy{
		Allow("run_command"),
		Ask("run_command", alwaysDeny),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("specific ask (denied) should beat specific allow")
	}
}

func TestWildcardDenyBeatsWildcardAllow(t *testing.T) {
	policies := []Policy{
		AllowAll(),
		DenyAll(),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "anything"})
	if result.Allow {
		t.Fatal("wildcard deny should beat wildcard allow")
	}
}

// --- Predicate Tests ---

func TestConditionalPredicate(t *testing.T) {
	policies := []Policy{
		DenyRule("run_command",
			WithName("deny_rm"),
			WithPredicate(func(toolName string, args map[string]any) bool {
				cmd, _ := args["command"].(string)
				return cmd == "rm -rf /"
			}),
		),
		AllowAll(),
	}

	// Dangerous command denied
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "run_command",
		Args: map[string]any{"command": "rm -rf /"},
	})
	if result.Allow {
		t.Fatal("dangerous command should be denied")
	}

	// Safe command allowed
	result = runPolicy(t, policies, hooks.ToolCall{
		Name: "run_command",
		Args: map[string]any{"command": "ls -la"},
	})
	if !result.Allow {
		t.Fatal("safe command should be allowed")
	}
}

func TestPredicateNotMatching(t *testing.T) {
	policies := []Policy{
		DenyRule("run_command",
			WithPredicate(func(toolName string, args map[string]any) bool {
				return false // Never matches
			}),
		),
		AllowAll(),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if !result.Allow {
		t.Fatal("predicate returned false — deny should not apply")
	}
}

// --- Preset Tests ---

func TestConfirmRunCommand(t *testing.T) {
	policies := ConfirmRunCommand()

	// run_command denied
	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("run_command should be denied by ConfirmRunCommand")
	}

	// manage_task denied
	result = runPolicy(t, policies, hooks.ToolCall{Name: "manage_task"})
	if result.Allow {
		t.Fatal("manage_task should be denied by ConfirmRunCommand")
	}

	// view_file allowed
	result = runPolicy(t, policies, hooks.ToolCall{Name: "view_file"})
	if !result.Allow {
		t.Fatal("view_file should be allowed by ConfirmRunCommand")
	}

	// create_file allowed
	result = runPolicy(t, policies, hooks.ToolCall{Name: "write_to_file"})
	if !result.Allow {
		t.Fatal("create_file should be allowed by ConfirmRunCommand")
	}
}

func TestSafeDefaults(t *testing.T) {
	policies := SafeDefaults(alwaysDeny)

	// Read-only tools allowed
	for tool := range ReadOnlyTools {
		result := runPolicy(t, policies, hooks.ToolCall{Name: tool})
		if !result.Allow {
			t.Fatalf("read-only tool %q should be allowed by SafeDefaults", tool)
		}
	}

	// Write tools denied (handler denies)
	result := runPolicy(t, policies, hooks.ToolCall{Name: "write_to_file"})
	if result.Allow {
		t.Fatal("create_file should be denied by SafeDefaults with alwaysDeny handler")
	}

	result = runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("run_command should be denied by SafeDefaults with alwaysDeny handler")
	}
}

func TestSafeDefaults_Approved(t *testing.T) {
	policies := SafeDefaults(alwaysApprove)

	// Write tools allowed when handler approves
	result := runPolicy(t, policies, hooks.ToolCall{Name: "write_to_file"})
	if !result.Allow {
		t.Fatal("create_file should be allowed by SafeDefaults with alwaysApprove handler")
	}
}

// --- AskUser Tests ---

func TestAskUser_Approved(t *testing.T) {
	policies := []Policy{
		Ask("run_command", alwaysApprove),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if !result.Allow {
		t.Fatal("approved ask should allow")
	}
}

func TestAskUser_Denied(t *testing.T) {
	policies := []Policy{
		Ask("run_command", alwaysDeny),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("denied ask should deny")
	}
}

func TestAskUser_HandlerError_FailsClosed(t *testing.T) {
	policies := []Policy{
		Ask("run_command", failHandler),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("handler error should fail closed (deny)")
	}
}

// --- Enforce Validation Tests ---

func TestEnforce_AskUserWithoutHandler_Error(t *testing.T) {
	policies := []Policy{
		{Tool: "run_command", Decision: AskUser, Handler: nil},
	}

	_, err := Enforce(policies)
	if err == nil {
		t.Fatal("expected error for AskUser without handler")
	}
}

func TestEnforce_ValidPolicies(t *testing.T) {
	policies := []Policy{
		Allow("view_file"),
		DenyRule("run_command"),
		Ask("write_to_file", alwaysApprove),
	}

	hook, err := Enforce(policies)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if hook == nil {
		t.Fatal("expected non-nil hook")
	}
}

// --- Edge Cases ---

func TestNoPolicies_DefaultAllow(t *testing.T) {
	result := runPolicy(t, nil, hooks.ToolCall{Name: "anything"})
	if !result.Allow {
		t.Fatal("no policies should default to allow")
	}
}

func TestEmptyArgs(t *testing.T) {
	policies := ConfirmRunCommand()

	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "view_file",
		Args: nil,
	})
	if !result.Allow {
		t.Fatal("view_file with nil args should be allowed")
	}
}

func TestMustEnforce_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from MustEnforce with invalid policy")
		}
	}()

	MustEnforce([]Policy{
		{Tool: "run_command", Decision: AskUser, Handler: nil},
	})
}

func TestDenyMessage_IncludesPolicyName(t *testing.T) {
	policies := []Policy{
		DenyRule("run_command", WithName("my_custom_rule")),
	}

	result := runPolicy(t, policies, hooks.ToolCall{Name: "run_command"})
	if result.Allow {
		t.Fatal("expected denial")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty denial message")
	}
	// Should include the policy name
	if !contains(result.Message, "my_custom_rule") {
		t.Fatalf("denial message should contain policy name, got: %q", result.Message)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- WorkspaceOnly + WithAllowedPaths Tests ---

func TestWorkspaceOnly_DeniesOutsideWorkspace(t *testing.T) {
	policies := WorkspaceOnly([]string{"/home/user/project"})

	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "write_to_file",
		Args: map[string]any{"path": "/home/user/.divmora/brain/conv-123/plan.md"},
	})
	if result.Allow {
		t.Fatal("create_file outside workspace should be denied")
	}
}

func TestWorkspaceOnly_AllowsInsideWorkspace(t *testing.T) {
	policies := WorkspaceOnly([]string{"/home/user/project"})

	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "write_to_file",
		Args: map[string]any{"path": "/home/user/project/main.go"},
	})
	if !result.Allow {
		t.Fatal("create_file inside workspace should be allowed")
	}
}

func TestWorkspaceOnly_WithAllowedPaths_AllowsBrainDir(t *testing.T) {
	policies := WorkspaceOnly(
		[]string{"/home/user/project"},
		WithAllowedPaths("/home/user/.divmora"),
	)

	// File in brain dir should be allowed
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "write_to_file",
		Args: map[string]any{"path": "/home/user/.divmora/brain/conv-123/implementation_plan.md"},
	})
	if !result.Allow {
		t.Fatal("create_file in allowed path (brain dir) should be allowed")
	}

	// File in workspace should still be allowed
	result = runPolicy(t, policies, hooks.ToolCall{
		Name: "replace_file_content",
		Args: map[string]any{"path": "/home/user/project/src/main.go"},
	})
	if !result.Allow {
		t.Fatal("edit_file in workspace should still be allowed")
	}

	// File outside both should still be denied
	result = runPolicy(t, policies, hooks.ToolCall{
		Name: "write_to_file",
		Args: map[string]any{"path": "/etc/passwd"},
	})
	if result.Allow {
		t.Fatal("create_file outside all allowed areas should be denied")
	}
}

func TestWorkspaceOnly_WithAllowedPaths_NoPathArg(t *testing.T) {
	policies := WorkspaceOnly(
		[]string{"/home/user/project"},
		WithAllowedPaths("/home/user/.divmora"),
	)

	// Tool call without path arg should be allowed (no path to check)
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "write_to_file",
		Args: map[string]any{"content": "hello"},
	})
	if !result.Allow {
		t.Fatal("tool call without path arg should be allowed")
	}
}

func TestWorkspaceOnly_WithoutAllowedPaths_BackwardCompatible(t *testing.T) {
	// Calling WorkspaceOnly without options should behave identically to before
	policies := WorkspaceOnly([]string{"/home/user/project"})

	// Inside workspace — allowed
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "view_file",
		Args: map[string]any{"path": "/home/user/project/file.go"},
	})
	if !result.Allow {
		t.Fatal("view_file inside workspace should be allowed")
	}

	// Outside workspace — denied
	result = runPolicy(t, policies, hooks.ToolCall{
		Name: "view_file",
		Args: map[string]any{"path": "/tmp/secret.txt"},
	})
	if result.Allow {
		t.Fatal("view_file outside workspace should be denied")
	}
}

// --- Relative Path Resolution Tests ---

func TestWorkspaceOnly_RelativePathInsideWorkspace(t *testing.T) {
	policies := WorkspaceOnly([]string{"/home/user/project"})

	// Relative path inside workspace should be allowed
	// (resolved as /home/user/project/.zenith/bolt.md)
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "view_file",
		Args: map[string]any{"path": ".zenith/bolt.md"},
	})
	if !result.Allow {
		t.Fatal("relative path inside workspace should be allowed")
	}
}

func TestWorkspaceOnly_RelativePathSubdir(t *testing.T) {
	policies := WorkspaceOnly([]string{"/home/user/project"})

	// Relative path to a subdirectory should be allowed
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "write_to_file",
		Args: map[string]any{"path": "src/main.go"},
	})
	if !result.Allow {
		t.Fatal("relative path to subdirectory should be allowed")
	}
}

func TestWorkspaceOnly_RelativePathEscape(t *testing.T) {
	policies := WorkspaceOnly([]string{"/home/user/project"})

	// Relative path escaping workspace via ../ should be denied
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "view_file",
		Args: map[string]any{"path": "../../etc/passwd"},
	})
	if result.Allow {
		t.Fatal("relative path escaping workspace via ../ should be denied")
	}
}

func TestWorkspaceOnly_RelativePathWithAllowedPaths(t *testing.T) {
	policies := WorkspaceOnly(
		[]string{"/home/user/project"},
		WithAllowedPaths("/home/user/.divmora"),
	)

	// Relative path inside workspace should still work
	result := runPolicy(t, policies, hooks.ToolCall{
		Name: "replace_file_content",
		Args: map[string]any{"path": "internal/config.go"},
	})
	if !result.Allow {
		t.Fatal("relative path inside workspace should be allowed with allowed paths configured")
	}
}
