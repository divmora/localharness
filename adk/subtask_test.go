package adk

import (
	"log/slog"
	"os"
	"testing"

	"github.com/divmora/localharness/adk/middleware"
	"github.com/divmora/localharness/adk/policy"
)

func defaultTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestSubtaskConfig_Defaults(t *testing.T) {
	cfg := SubtaskConfig{
		Prompt: "test task",
	}

	if cfg.Prompt != "test task" {
		t.Fatal("expected prompt")
	}
	if cfg.ReadOnly != nil {
		t.Fatal("ReadOnly should default to nil (means true)")
	}
	if cfg.Timeout != 0 {
		t.Fatal("Timeout should default to 0 (means 5m)")
	}
}

func TestBool_Helper(t *testing.T) {
	truePtr := Bool(true)
	falsePtr := Bool(false)

	if truePtr == nil || *truePtr != true {
		t.Fatal("Bool(true) should return *true")
	}
	if falsePtr == nil || *falsePtr != false {
		t.Fatal("Bool(false) should return *false")
	}
}

func TestCreateChildAgent_ReadOnly(t *testing.T) {
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     policy.ConfirmRunCommand(),
			Capabilities: DefaultCapabilities(),
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt: "analyze files",
		// ReadOnly defaults to true
	})
	if err != nil {
		t.Fatalf("createChildAgent: %v", err)
	}

	// Read-only: write tools should be disabled
	childCfg := child.config
	if childCfg.Capabilities.CreateFile {
		t.Fatal("CreateFile should be disabled in read-only mode")
	}
	if childCfg.Capabilities.EditFile {
		t.Fatal("EditFile should be disabled in read-only mode")
	}
	if childCfg.Capabilities.RunCommand {
		t.Fatal("RunCommand should be disabled in read-only mode")
	}
	if childCfg.Capabilities.ManageTask {
		t.Fatal("ManageTask should be disabled in read-only mode")
	}
	if childCfg.Capabilities.InvokeSubagent {
		t.Fatal("InvokeSubagent should be disabled in read-only mode")
	}

	// Read tools should stay enabled
	if !childCfg.Capabilities.ViewFile {
		t.Fatal("ViewFile should be enabled in read-only mode")
	}
	if !childCfg.Capabilities.ListDir {
		t.Fatal("ListDir should be enabled in read-only mode")
	}

	// Policy should be AllowAll (no write tools to guard)
	if len(childCfg.Policies) != 1 {
		t.Fatalf("expected 1 policy (AllowAll), got %d", len(childCfg.Policies))
	}
}

func TestCreateChildAgent_WriteEnabled(t *testing.T) {
	parentPolicies := policy.ConfirmRunCommand()
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     parentPolicies,
			Capabilities: DefaultCapabilities(),
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt:   "fix the bug",
		ReadOnly: Bool(false),
	})
	if err != nil {
		t.Fatalf("createChildAgent: %v", err)
	}

	// Write-enabled: should inherit parent's policies
	if len(child.config.Policies) != len(parentPolicies) {
		t.Fatalf("expected %d policies (inherited), got %d",
			len(parentPolicies), len(child.config.Policies))
	}

	// Write tools should be enabled (from DefaultCapabilities)
	if !child.config.Capabilities.CreateFile {
		t.Fatal("CreateFile should be enabled in write mode")
	}
}

func TestCreateChildAgent_InheritsProvider(t *testing.T) {
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
						Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     policy.ConfirmRunCommand(),
			Capabilities: DefaultCapabilities(),
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if child.config.LitellmEndpoint != "test" {
		t.Fatal("should inherit parent's API key")
	}

}


func TestCreateChildAgent_CustomSystemPrompt(t *testing.T) {
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     policy.ConfirmRunCommand(),
			Capabilities: DefaultCapabilities(),
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt:       "test",
		SystemPrompt: "You are a security auditor.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if child.config.SystemInstructions != "You are a security auditor." {
		t.Fatal("should use custom system prompt")
	}
}

func TestCreateChildAgent_DefaultSystemPrompt(t *testing.T) {
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     policy.ConfirmRunCommand(),
			Capabilities: DefaultCapabilities(),
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if child.config.SystemInstructions != defaultSubtaskSystemPrompt {
		t.Fatal("should use default subtask system prompt")
	}
}

func TestCreateChildAgent_InheritsMiddlewares(t *testing.T) {
	mw := middleware.NewPatchToolArgs(nil)
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     policy.ConfirmRunCommand(),
			Capabilities: DefaultCapabilities(),
			Middlewares:  []middleware.Middleware{mw},
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(child.config.Middlewares) != 1 {
		t.Fatal("should inherit parent's middlewares")
	}
}

func TestCreateChildAgent_CustomMiddlewares(t *testing.T) {
	parentMw := middleware.NewPatchToolArgs(nil)
	childMw := middleware.NewTokenGuard(10000, 0.5, nil)

	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:   []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:     policy.ConfirmRunCommand(),
			Capabilities: DefaultCapabilities(),
			Middlewares:  []middleware.Middleware{parentMw},
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt:      "test",
		Middlewares: []middleware.Middleware{childMw},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(child.config.Middlewares) != 1 || child.config.Middlewares[0].Name() != "token_guard" {
		t.Fatal("should use custom middlewares, not parent's")
	}
}

func TestCreateChildAgent_NoSubagents(t *testing.T) {
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Workspaces:             []WorkspaceDef{{Directory: "/tmp/test"}},
			Policies:               policy.ConfirmRunCommand(),
			Capabilities:           DefaultCapabilities(),
			MaxSubagentDepth:       3,
			MaxConcurrentSubagents: 5,
		},
		logger: defaultTestLogger(),
	}

	child, err := parent.createChildAgent(SubtaskConfig{
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Subtask children shouldn't spawn sub-subagents
	if child.config.MaxSubagentDepth != 0 {
		t.Fatalf("expected 0 depth, got %d", child.config.MaxSubagentDepth)
	}
	if child.config.MaxConcurrentSubagents != 0 {
		t.Fatalf("expected 0 concurrent, got %d", child.config.MaxConcurrentSubagents)
	}
}


func TestRunSubtask_EmptyPrompt(t *testing.T) {
	parent := &Agent{
		config: &LocalAgentConfig{
			LitellmEndpoint: "test",
			Capabilities: DefaultCapabilities(),
		},
		logger: defaultTestLogger(),
	}

	_, err := parent.RunSubtask(nil, SubtaskConfig{})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestSubtaskHandle_Fields(t *testing.T) {
	h := &SubtaskHandle{
		ConversationID: "test-id",
		resultCh:       make(chan subtaskOutcome, 1),
	}

	if h.ConversationID != "test-id" {
		t.Fatal("expected conversation ID")
	}
}
