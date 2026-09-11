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

func TestDesktopSubagentDeclaration(t *testing.T) {
	decl := desktopSubagentDeclaration()
	if decl.Name != "desktop_subagent" {
		t.Errorf("expected name 'desktop_subagent', got %q", decl.Name)
	}
	if _, ok := decl.Parameters["properties"]; !ok {
		t.Error("expected parameters properties")
	}
}

func TestDesktopToolDeclarations(t *testing.T) {
	decls := desktopToolDeclarations()
	if len(decls) == 0 {
		t.Fatal("expected non-empty desktop tool declarations")
	}

	names := make(map[string]bool)
	for _, d := range decls {
		names[d.Name] = true
	}

	expected := []string{
		"desktop_screenshot",
		"desktop_list_windows",
		"desktop_focus_window",
		"desktop_click",
		"desktop_type",
		"desktop_shortcut",
	}

	for _, exp := range expected {
		if !names[exp] {
			t.Errorf("expected tool %q in desktop tool declarations", exp)
		}
	}
}

func TestExecuteDesktopSubagent(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Desktop task completed successfully", FinishReason: "stop"},
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
		HasDesktopConfig: true,
	})

	ctx := context.Background()
	tc := llm.ToolCall{
		ID:   "call_desktop_1",
		Name: "desktop_subagent",
		Args: map[string]interface{}{
			"TaskName":    "Test Task",
			"Task":        "Interact with window",
			"TaskSummary": "desktop test",
		},
	}

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_DesktopSubagent{
			DesktopSubagent: &pb.ActionDesktopSubagent{},
		},
	}

	err := eng.executeDesktopSubagent(ctx, tc, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if step.State != pb.StepUpdate_STATE_DONE {
		t.Errorf("expected state DONE, got %v", step.State)
	}

	if act := step.GetDesktopSubagent(); act != nil {
		if act.ConversationId == "" {
			t.Error("expected ConversationId to be set")
		}
	} else {
		t.Error("expected desktop subagent action on step")
	}

	// Wait for subagent goroutine to finish
	deadline := time.Now().Add(2 * time.Second)
	for len(eng.subagentTracker.List()) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	active := eng.subagentTracker.List()
	if len(active) > 0 {
		t.Errorf("expected subagent to finish, but found %d active", len(active))
	}
}

func TestParseShortcutKeys(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected []string
	}{
		{
			input:    []interface{}{"cmd", "l"},
			expected: []string{"cmd", "l"},
		},
		{
			input:    "cmd+l",
			expected: []string{"cmd", "l"},
		},
		{
			input:    "ctrl+shift+p",
			expected: []string{"ctrl", "shift", "p"},
		},
		{
			input:    `["cmd", "option", "i"]`,
			expected: []string{"cmd", "option", "i"},
		},
	}

	for _, tt := range tests {
		got := parseShortcutKeys(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("parseShortcutKeys(%v): expected %v, got %v", tt.input, tt.expected, got)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parseShortcutKeys(%v)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], got[i])
			}
		}
	}
}
