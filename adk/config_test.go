package adk

import (
	"context"
	"strings"
	"testing"

	"github.com/divmora/localharness/adk/hooks"
	"github.com/divmora/localharness/adk/policy"
)



func TestValidate_WriteToolsWithoutPolicy_Rejected(t *testing.T) {
	// Each write/side-effect tool should trigger the safety check
	writeTools := []struct {
		name string
		caps CapabilitiesConfig
	}{
		{"CreateFile", CapabilitiesConfig{CreateFile: true}},
		{"EditFile", CapabilitiesConfig{EditFile: true}},
		{"RunCommand", CapabilitiesConfig{RunCommand: true}},
		{"ManageTask", CapabilitiesConfig{ManageTask: true}},
		{"InvokeSubagent", CapabilitiesConfig{InvokeSubagent: true}},
	}

	for _, tc := range writeTools {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &LocalAgentConfig{
				Capabilities: tc.caps,
				Policies:     nil, // No policies!
			}
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("%s enabled without policy should fail validation", tc.name)
			}
			if !strings.Contains(err.Error(), "write tools are enabled without a safety policy") {
				t.Errorf("expected safety policy error, got: %v", err)
			}
		})
	}
}

func TestValidate_WriteToolsWithPolicy_Accepted(t *testing.T) {
	cfg := &LocalAgentConfig{
		Capabilities: CapabilitiesConfig{
			CreateFile: true,
			EditFile:   true,
			RunCommand: true,
		},
		Policies: []policy.Policy{policy.AllowAll()},
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error with write tools + policy, got: %v", err)
	}
}

func TestValidate_WriteToolsWithDecideHook_Accepted(t *testing.T) {
	cfg := &LocalAgentConfig{
		Capabilities: CapabilitiesConfig{
			CreateFile: true,
			EditFile:   true,
		},
		Hooks: []hooks.Hook{&testDecideHook{}},
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error with write tools + decide hook, got: %v", err)
	}
}

func TestValidate_ReadOnlyTools_NoPolicyRequired(t *testing.T) {
	cfg := &LocalAgentConfig{
		Capabilities: ReadOnlyCapabilities(),
		// No policies — should be fine for read-only
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for read-only tools without policy, got: %v", err)
	}
}

func TestValidate_DefaultConfig_Accepted(t *testing.T) {
	cfg := NewLocalAgentConfig()
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("default config should pass validation, got: %v", err)
	}
}

func TestValidate_AskUserWithoutHandler_Rejected(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies: []policy.Policy{
			{
				Tool:     "run_command",
				Decision: policy.AskUser,
				Handler:  nil, // Missing handler!
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for AskUser policy without handler")
	}
	if !strings.Contains(err.Error(), "missing a handler") {
		t.Errorf("expected 'missing a handler' error, got: %v", err)
	}
}

func TestValidate_EmptyPolicies_NoWriteTools_Accepted(t *testing.T) {
	cfg := &LocalAgentConfig{
		Capabilities: CapabilitiesConfig{
			ViewFile:  true,
			ListDir:   true,
			SearchDir: true,
			WebSearch: true,
		},
		Policies: []policy.Policy{}, // Empty slice
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for empty policies without write tools, got: %v", err)
	}
}

// --- Host Tool Validation Tests ---

func dummyHandler(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil
}

func TestValidate_HostTools_Valid(t *testing.T) {
	cfg := &LocalAgentConfig{
		HostTools: []HostToolDef{
			{Name: "get_weather", Description: "Weather", Handler: dummyHandler},
			{Name: "query_db", Description: "Database", Handler: dummyHandler},
		},
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected valid host tools to pass, got: %v", err)
	}
}

func TestValidate_HostTools_DuplicateName(t *testing.T) {
	cfg := &LocalAgentConfig{
		HostTools: []HostToolDef{
			{Name: "get_weather", Description: "Weather", Handler: dummyHandler},
			{Name: "get_weather", Description: "Weather dupe", Handler: dummyHandler},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate host tool name")
	}
	if !strings.Contains(err.Error(), "duplicate host tool name") {
		t.Errorf("expected 'duplicate host tool name' error, got: %v", err)
	}
}

func TestValidate_HostTools_BuiltinCollision(t *testing.T) {
	builtins := []string{"view_file", "run_command", "grep_search", "finish", "ask_question", "schedule"}
	for _, name := range builtins {
		t.Run(name, func(t *testing.T) {
			cfg := &LocalAgentConfig{
				HostTools: []HostToolDef{
					{Name: name, Description: "Collision", Handler: dummyHandler},
				},
			}
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for built-in name collision: %s", name)
			}
			if !strings.Contains(err.Error(), "conflicts with a built-in harness tool") {
				t.Errorf("expected 'conflicts with a built-in harness tool' error, got: %v", err)
			}
		})
	}
}

func TestValidate_HostTools_NilHandler(t *testing.T) {
	cfg := &LocalAgentConfig{
		HostTools: []HostToolDef{
			{Name: "broken_tool", Description: "No handler"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
	if !strings.Contains(err.Error(), "nil handler") {
		t.Errorf("expected 'nil handler' error, got: %v", err)
	}
}

func TestValidate_HostTools_EmptyName(t *testing.T) {
	cfg := &LocalAgentConfig{
		HostTools: []HostToolDef{
			{Name: "", Description: "No name", Handler: dummyHandler},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "empty name") {
		t.Errorf("expected 'empty name' error, got: %v", err)
	}
}

// testDecideHook implements PreToolCallDecideHook for testing.
type testDecideHook struct{}

func (h *testDecideHook) Run(ctx *hooks.HookContext, tc hooks.ToolCall) hooks.HookResult {
	return hooks.HookResult{Allow: true}
}

// Verify it implements the interface
var _ hooks.PreToolCallDecideHook = (*testDecideHook)(nil)

