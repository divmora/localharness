package engine

import (
	"context"
	"log/slog"
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
