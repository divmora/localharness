package engine

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
)

func TestBrowserSubagentDeclaration(t *testing.T) {
	decl := browserSubagentDeclaration()
	if decl.Name != "browser_subagent" {
		t.Errorf("expected name 'browser_subagent', got %q", decl.Name)
	}
	if _, ok := decl.Parameters["properties"]; !ok {
		t.Error("expected parameters properties")
	}
}

func TestExecuteBrowserSubagent(t *testing.T) {
	// Setup simple provider that just finishes
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Done", FinishReason: "stop"},
		},
	}

	logger := slog.Default()
	toolRegistry := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     toolRegistry,
		SystemPrompt:     "Test",
		SubagentsEnabled: true,
		MaxSubagents:     1,
		MaxDepth:         2,
		Logger:           logger,
		HasBrowserConfig: true,
	})

	ctx := context.Background()
	tc := llm.ToolCall{
		ID:   "call_1",
		Name: "browser_subagent",
		Args: map[string]interface{}{
			"TaskName":      "Test Task",
			"Task":          "Go to example.com",
			"TaskSummary":   "test",
			"RecordingName": "test_rec",
		},
	}

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_BrowserSubagent{
			BrowserSubagent: &pb.ActionBrowserSubagent{},
		},
	}

	err := eng.executeBrowserSubagent(ctx, tc, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if step.State != pb.StepUpdate_STATE_DONE {
		t.Errorf("expected state DONE, got %v", step.State)
	}

	if act := step.GetBrowserSubagent(); act != nil {
		if act.ConversationId == "" {
			t.Error("expected ConversationId to be set")
		}
	} else {
		t.Error("expected browser subagent action on step")
	}

	// Give subagent a moment to finish in background
	time.Sleep(100 * time.Millisecond)

	active := eng.subagentTracker.List()
	if len(active) > 0 {
		t.Errorf("expected subagent to finish, but found %d active", len(active))
	}
}

func TestExecuteBrowserSubagent_NoCapability(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.Default()
	toolRegistry := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     toolRegistry,
		SystemPrompt:     "Test",
		SubagentsEnabled: true,
		Logger:           logger,
		HasBrowserConfig: false, // Disabled!
	})

	ctx := context.Background()
	tc := llm.ToolCall{
		ID:   "call_2",
		Name: "browser_subagent",
		Args: map[string]interface{}{
			"TaskName": "Task",
			"Task":     "Navigate somewhere",
		},
	}
	step := &pb.StepUpdate{}

	err := eng.executeBrowserSubagent(ctx, tc, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if step.State != pb.StepUpdate_STATE_ERROR {
		t.Errorf("expected step state ERROR for missing browser capability, got %v", step.State)
	}
}

func TestBrowserSubagentDeclaration_JetskiFeatures(t *testing.T) {
	decl := browserSubagentDeclaration()
	props, ok := decl.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["ProfileDir"]; !ok {
		t.Error("expected ProfileDir in declaration properties")
	}
	if _, ok := props["Isolated"]; !ok {
		t.Error("expected Isolated in declaration properties")
	}
}

func TestExecuteBrowserSubagent_PersistentProfileAndIsolated(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Done", FinishReason: "stop"},
		},
	}
	logger := slog.Default()
	toolRegistry := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     toolRegistry,
		SystemPrompt:     "Test",
		SubagentsEnabled: true,
		MaxSubagents:     1,
		MaxDepth:         2,
		Logger:           logger,
		HasBrowserConfig: true,
	})

	ctx := context.Background()
	tc := llm.ToolCall{
		ID:   "call_profile",
		Name: "browser_subagent",
		Args: map[string]interface{}{
			"TaskName":      "Test Task",
			"Task":          "Log in to portal",
			"TaskSummary":   "test login",
			"RecordingName": "login_rec",
			"ProfileDir":    "/custom/browser/profile",
			"Isolated":      true,
		},
	}

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_BrowserSubagent{
			BrowserSubagent: &pb.ActionBrowserSubagent{},
		},
	}

	err := eng.executeBrowserSubagent(ctx, tc, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if step.State != pb.StepUpdate_STATE_DONE {
		t.Errorf("expected state DONE, got %v", step.State)
	}

	act := step.GetBrowserSubagent()
	if act == nil {
		t.Fatal("expected browser subagent action on step")
	}
	if act.ProfileDir != "/custom/browser/profile" {
		t.Errorf("expected ProfileDir '/custom/browser/profile', got %q", act.ProfileDir)
	}
	if !act.Isolated {
		t.Error("expected Isolated to be true")
	}

	time.Sleep(100 * time.Millisecond)
}

func TestExecuteBrowserSubagent_IsolatedEnv_NoGlobalMutation(t *testing.T) {
	origVal, hadVal := os.LookupEnv("PLAYWRIGHT_RECORD_VIDEO_DIR")
	_ = os.Unsetenv("PLAYWRIGHT_RECORD_VIDEO_DIR")
	defer func() {
		if hadVal {
			_ = os.Setenv("PLAYWRIGHT_RECORD_VIDEO_DIR", origVal)
		} else {
			_ = os.Unsetenv("PLAYWRIGHT_RECORD_VIDEO_DIR")
		}
	}()

	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Browser subagent done", FinishReason: "stop"},
		},
	}
	logger := slog.Default()
	toolRegistry := tools.NewRegistry(nil, logger)
	appDataDir := t.TempDir()

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     toolRegistry,
		SystemPrompt:     "Test",
		SubagentsEnabled: true,
		MaxSubagents:     5,
		MaxDepth:         2,
		Logger:           logger,
		HasBrowserConfig: true,
		AppDataDir:       appDataDir,
		Env: map[string]string{
			"PARENT_CUSTOM_ENV": "parent_val",
		},
	})

	ctx := context.Background()
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_BrowserSubagent{
			BrowserSubagent: &pb.ActionBrowserSubagent{},
		},
	}
	tc := llm.ToolCall{
		ID:   "call_env_test",
		Name: "browser_subagent",
		Args: map[string]interface{}{
			"TaskName":      "Env Test Task",
			"Task":          "Test env isolation",
			"TaskSummary":   "testing env",
			"RecordingName": "rec_env",
		},
	}

	err := eng.executeBrowserSubagent(ctx, tc, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Process-wide environment must NOT be mutated
	if val, set := os.LookupEnv("PLAYWRIGHT_RECORD_VIDEO_DIR"); set {
		t.Fatalf("PLAYWRIGHT_RECORD_VIDEO_DIR was mutated in process-wide environment: %q", val)
	}

	// 2. Child engine must have inherited the isolated environment
	active := eng.subagentTracker.List()
	if len(active) == 0 {
		// If already finished, that's fine, but let's check step
		if step.State != pb.StepUpdate_STATE_DONE {
			t.Errorf("expected state DONE, got %v", step.State)
		}
	} else {
		inst := active[0]
		childEnv := inst.Engine.Env()
		if childEnv["PARENT_CUSTOM_ENV"] != "parent_val" {
			t.Errorf("expected PARENT_CUSTOM_ENV in child engine, got %v", childEnv["PARENT_CUSTOM_ENV"])
		}
		if childEnv["PLAYWRIGHT_RECORD_VIDEO_DIR"] == "" {
			t.Error("expected PLAYWRIGHT_RECORD_VIDEO_DIR in child engine isolated env")
		}
	}

	// Give subagent a moment to finish in background
	deadline := time.Now().Add(1 * time.Second)
	for len(eng.subagentTracker.List()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
