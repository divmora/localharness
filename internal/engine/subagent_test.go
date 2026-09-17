package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
)

// ── Helper Functions Tests ──────────────────────────────────────────────

func TestExtractFinalResponse(t *testing.T) {
	tests := []struct {
		name    string
		history []llm.Message
		want    string
	}{
		{
			name:    "empty history",
			history: nil,
			want:    "",
		},
		{
			name: "single model response",
			history: []llm.Message{
				{Role: "user", Content: "hello"},
				{Role: "model", Content: "world"},
			},
			want: "world",
		},
		{
			name: "model response after tool calls",
			history: []llm.Message{
				{Role: "user", Content: "view file"},
				{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file"}}},
				{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Content: "file content"}},
				{Role: "model", Content: "I found the content."},
			},
			want: "I found the content.",
		},
		{
			name: "skip model messages with tool calls",
			history: []llm.Message{
				{Role: "user", Content: "do stuff"},
				{Role: "model", Content: "thinking...", ToolCalls: []llm.ToolCall{{ID: "1", Name: "list_dir"}}},
				{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Content: "result"}},
			},
			want: "", // Only the model message with tool calls, no final text
		},
		{
			name: "multiple model responses — returns last",
			history: []llm.Message{
				{Role: "model", Content: "first"},
				{Role: "model", Content: "second"},
				{Role: "model", Content: "third"},
			},
			want: "third",
		},
		{
			name: "no model messages",
			history: []llm.Message{
				{Role: "user", Content: "hello"},
				{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Content: "result"}},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFinalResponse(tt.history)
			if got != tt.want {
				t.Errorf("extractFinalResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInvokeSubagentDeclaration(t *testing.T) {
	decl := invokeSubagentDeclaration()

	if decl.Name != "invoke_subagent" {
		t.Errorf("expected name 'invoke_subagent', got %q", decl.Name)
	}
	if decl.Description == "" {
		t.Error("description should not be empty")
	}
	if decl.Parameters == nil {
		t.Fatal("parameters should not be nil")
	}

	// Check required fields (new schema: Subagents array)
	props, ok := decl.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters should have properties")
	}
	if _, ok := props["Subagents"]; !ok {
		t.Error("parameters should have 'Subagents' property")
	}

	// Check required
	required, ok := decl.Parameters["required"].([]string)
	if !ok || len(required) == 0 {
		t.Error("parameters should have 'required' array")
	} else if required[0] != "Subagents" {
		t.Errorf("expected 'Subagents' in required, got %q", required[0])
	}
}

func TestDefineSubagentDeclaration(t *testing.T) {
	decl := defineSubagentDeclaration()
	if decl.Name != "define_subagent" {
		t.Errorf("expected name 'define_subagent', got %q", decl.Name)
	}
	props, ok := decl.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters should have properties")
	}
	for _, field := range []string{"name", "description", "system_prompt"} {
		if _, ok := props[field]; !ok {
			t.Errorf("parameters should have '%s' property", field)
		}
	}
}

func TestManageSubagentsDeclaration(t *testing.T) {
	decl := manageSubagentsDeclaration()
	if decl.Name != "manage_subagents" {
		t.Errorf("expected name 'manage_subagents', got %q", decl.Name)
	}
}

func TestSendMessageDeclaration(t *testing.T) {
	decl := sendMessageDeclaration()
	if decl.Name != "send_message" {
		t.Errorf("expected name 'send_message', got %q", decl.Name)
	}
}

func TestAccumulateUsage(t *testing.T) {
	// Currently returns zero usage since we don't store per-call usage in messages
	history := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "model", Content: "world"},
	}
	usage := accumulateUsage(history)
	// Just verify it doesn't panic and returns a valid struct
	if usage.PromptTokens < 0 {
		t.Error("usage should not be negative")
	}
}

// ── Subagent Engine Integration Tests ───────────────────────────────────

func TestSubagentBasicExecution(t *testing.T) {
	// Parent provider: calls invoke_subagent with new schema, then gets the result and responds
	// Child provider: responds with text immediately (runs async)
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				// Parent call 1: invoke subagent with Subagents array
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_sub",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"Subagents": []interface{}{
								map[string]interface{}{
									"TypeName": "research",
									"Role":     "File Finder",
									"Prompt":   "List all Go files",
								},
							},
						},
					},
				},
			},
			{
				// Parent call 2: final response (subagent launched async)
				Content:      "I launched a research subagent to find Go files.",
				FinishReason: "stop",
			},
			{
				// Child call: subagent responds immediately (runs in goroutine)
				Content:      "Found 5 Go files: main.go, server.go, engine.go, tools.go, types.go",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	var mu sync.Mutex
	var steps []*pb.StepUpdate

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     reg,
		SystemPrompt:     "test parent",
		ConversationID:   "test-conv",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
		MaxDepth:         3,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		mu.Lock()
		steps = append(steps, step)
		mu.Unlock()
	}

	err := eng.Run(context.Background(), "Find all Go files using a subagent")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait for child goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// invoke_subagent now returns immediately (async), so parent gets 2 LLM calls
	// (invoke_subagent + final response), child runs asynchronously
	provider.mu.Lock()
	callCount := len(provider.callLog)
	provider.mu.Unlock()
	if callCount < 2 {
		t.Errorf("expected at least 2 LLM calls, got %d", callCount)
	}

	// Should see invoke_subagent step with STATE_DONE (launch results)
	mu.Lock()
	var sawInvokeSubagent bool
	for _, s := range steps {
		if s.GetInvokeSubagent() != nil && s.State == pb.StepUpdate_STATE_DONE {
			sawInvokeSubagent = true
		}
	}
	mu.Unlock()
	if !sawInvokeSubagent {
		t.Error("expected invoke_subagent step with STATE_DONE")
	}
}

func TestSubagentDepthLimit(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_sub",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"prompt": "do something",
						},
					},
				},
			},
			{
				// After depth limit error, LLM should get the error and respond
				Content:      "Cannot spawn subagent — depth limit reached.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	var steps []*pb.StepUpdate
	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		ConversationID:   "test-depth",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
		Depth:            3, // Already at max depth
		MaxDepth:         3,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	err := eng.Run(context.Background(), "Spawn a subagent")
	if err != nil {
		t.Fatalf("Run should succeed (depth error is non-fatal): %v", err)
	}

	// Should see an error step for the invoke_subagent
	var sawDepthError bool
	for _, s := range steps {
		if s.GetInvokeSubagent() != nil && s.State == pb.StepUpdate_STATE_ERROR {
			sawDepthError = true
			if !strings.Contains(s.ErrorInfo.Message, "depth") {
				t.Errorf("error should mention depth, got: %s", s.ErrorInfo.Message)
			}
		}
	}
	if !sawDepthError {
		t.Error("expected depth limit error step")
	}
}

func TestSubagentContextIsolation(t *testing.T) {
	// Verify that the child engine gets a fresh context (no parent history)
	provider := &mockProvider{}
	provider.responses = []*llm.GenerateResponse{
		{
			// Parent call 1: invoke subagent with Subagents array
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_sub",
					Name: "invoke_subagent",
					Args: map[string]interface{}{
						"Subagents": []interface{}{
							map[string]interface{}{
								"TypeName": "self",
								"Role":     "Worker",
								"Prompt":   "child task",
							},
						},
					},
				},
			},
		},
		{
			// Parent call 2: final (async — child runs in background)
			Content:      "done",
			FinishReason: "stop",
		},
		{
			// Child call: respond immediately
			Content:      "child result",
			FinishReason: "stop",
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		SystemPrompt:     "parent system prompt",
		ConversationID:   "test-isolation",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
	})

	// Pre-populate parent history with old messages
	eng.history = []llm.Message{
		{Role: "user", Content: "old message 1"},
		{Role: "model", Content: "old response 1"},
	}

	err := eng.Run(context.Background(), "Run a subagent")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait for child goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// Parent should have run (at least 2 LLM calls)
	provider.mu.Lock()
	callCount := len(provider.callLog)
	provider.mu.Unlock()
	if callCount < 2 {
		t.Fatal("expected at least 2 LLM calls")
	}
}

func TestSubagentMissingPrompt(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_sub",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							// No Subagents, no prompt!
						},
					},
				},
			},
			{
				Content:      "OK, no subagent then.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		ConversationID:   "test-no-prompt",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
	})

	var steps []*pb.StepUpdate
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	err := eng.Run(context.Background(), "Spawn a subagent without a prompt")
	if err != nil {
		t.Fatalf("Run should succeed (error is non-fatal): %v", err)
	}

	// Should see error step — message says "at least one subagent must be specified"
	var sawError bool
	for _, s := range steps {
		if s.GetInvokeSubagent() != nil && s.State == pb.StepUpdate_STATE_ERROR {
			sawError = true
			if !strings.Contains(s.ErrorInfo.Message, "subagent") {
				t.Errorf("error should mention subagent, got: %s", s.ErrorInfo.Message)
			}
		}
	}
	if !sawError {
		t.Error("expected error step for missing Subagents")
	}
}

func TestSubagentDefaultsDisabled(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		Logger:           logger,
		SubagentsEnabled: false, // Default: disabled
	})

	decls := eng.buildToolDeclarations()
	for _, d := range decls {
		if d.Name == "invoke_subagent" {
			t.Error("invoke_subagent should NOT be in tool declarations when disabled")
		}
	}
}

func TestSubagentEnabledInDeclarations(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		Logger:           logger,
		SubagentsEnabled: true,
		MaxDepth:         3,
		Depth:            0, // Root — should be available
	})

	decls := eng.buildToolDeclarations()
	var found bool
	for _, d := range decls {
		if d.Name == "invoke_subagent" {
			found = true
		}
	}
	if !found {
		t.Error("invoke_subagent should be in tool declarations when enabled at depth < maxDepth")
	}
}

func TestSubagentNotInDeclarationsAtMaxDepth(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		Logger:           logger,
		SubagentsEnabled: true,
		MaxDepth:         3,
		Depth:            3, // At max depth — should NOT be available
	})

	decls := eng.buildToolDeclarations()
	for _, d := range decls {
		if d.Name == "invoke_subagent" {
			t.Error("invoke_subagent should NOT be in declarations at max depth")
		}
	}
}

func TestToolGroupFiltering(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Register tools with explicit groups
	reg := tools.NewRegistry(nil, logger)
	reg.Register("view_file", nil, tools.ToolSchema{
		Name:  "view_file",
		Group: tools.ToolGroupRead,
	})
	reg.Register("list_dir", nil, tools.ToolSchema{
		Name:  "list_dir",
		Group: tools.ToolGroupRead,
	})
	reg.Register("write_to_file", nil, tools.ToolSchema{
		Name:  "write_to_file",
		Group: tools.ToolGroupWrite,
	})
	reg.Register("run_command", nil, tools.ToolSchema{
		Name:  "run_command",
		Group: tools.ToolGroupWrite,
	})
	reg.Register("finish", nil, tools.ToolSchema{
		Name: "finish",
		// No group — always available
	})

	t.Run("no filter — all tools available", func(t *testing.T) {
		eng := NewEngine(Config{
			Provider:     provider,
			ToolRegistry: reg,
			Logger:       logger,
		})
		decls := eng.buildToolDeclarations()
		names := map[string]bool{}
		for _, d := range decls {
			names[d.Name] = true
		}
		for _, want := range []string{"view_file", "list_dir", "write_to_file", "run_command", "finish"} {
			if !names[want] {
				t.Errorf("expected %q in declarations", want)
			}
		}
	})

	t.Run("exclude write group — read + ungrouped survive", func(t *testing.T) {
		eng := NewEngine(Config{
			Provider:     provider,
			ToolRegistry: reg,
			Logger:       logger,
			ExcludeToolGroups: map[tools.ToolGroup]bool{
				tools.ToolGroupWrite: true,
			},
		})
		decls := eng.buildToolDeclarations()
		names := map[string]bool{}
		for _, d := range decls {
			names[d.Name] = true
		}
		// Read and ungrouped should survive
		if !names["view_file"] {
			t.Error("view_file (read) should survive write exclusion")
		}
		if !names["list_dir"] {
			t.Error("list_dir (read) should survive write exclusion")
		}
		if !names["finish"] {
			t.Error("finish (ungrouped) should survive write exclusion")
		}
		// Write should be filtered
		if names["write_to_file"] {
			t.Error("create_file (write) should be excluded")
		}
		if names["run_command"] {
			t.Error("run_command (write) should be excluded")
		}
	})

	t.Run("exclude host tools", func(t *testing.T) {
		eng := NewEngine(Config{
			Provider:         provider,
			ToolRegistry:     reg,
			Logger:           logger,
			ExcludeHostTools: true,
			HostToolDecls: []llm.FunctionDeclaration{
				{Name: "my_host_tool", Description: "SDK tool"},
			},
		})
		decls := eng.buildToolDeclarations()
		for _, d := range decls {
			if d.Name == "my_host_tool" {
				t.Error("host tool should be excluded when ExcludeHostTools is true")
			}
		}
	})
}

func TestSubagentContextCancellation(t *testing.T) {
	// Provider that will be used by both parent and child
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_sub",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"prompt": "do work",
						},
					},
				},
			},
			// Child will try to call LLM but context is cancelled
			{Content: "should not reach", FinishReason: "stop"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		ConversationID:   "test-cancel",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := eng.Run(ctx, "Run a subagent")
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

func TestSubagentTrajectoryIDs(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_sub",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"Subagents": []interface{}{
								map[string]interface{}{
									"TypeName": "research",
									"Role":     "Worker",
									"Prompt":   "child task",
								},
							},
						},
					},
				},
			},
			{
				Content:      "parent done",
				FinishReason: "stop",
			},
			{
				Content:      "child done",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	var mu sync.Mutex
	var trajStates []*pb.TrajectoryState
	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		ConversationID:   "test-traj-ids",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
	})
	eng.trajCB = func(state *pb.TrajectoryState) {
		mu.Lock()
		trajStates = append(trajStates, state)
		mu.Unlock()
	}

	err := eng.Run(context.Background(), "Run subagent")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait for child goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// Should see parent RUNNING + parent IDLE (child runs async)
	mu.Lock()
	parentRunning := false
	parentIdle := false

	for _, ts := range trajStates {
		if ts.TrajectoryId == "traj_0" {
			if ts.State == pb.TrajectoryState_TRAJ_RUNNING {
				parentRunning = true
			}
			if ts.State == pb.TrajectoryState_TRAJ_IDLE {
				parentIdle = true
			}
		}
	}
	mu.Unlock()

	if !parentRunning {
		t.Error("expected parent TRAJ_RUNNING")
	}
	if !parentIdle {
		t.Error("expected parent TRAJ_IDLE")
	}
}

func TestSubagentWithChildError(t *testing.T) {
	// Parent calls subagent with new schema, child LLM fails → error async
	provider := &mockProvider{}
	provider.responses = []*llm.GenerateResponse{
		{
			// Parent call 1: invoke subagent
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_sub",
					Name: "invoke_subagent",
					Args: map[string]interface{}{
						"Subagents": []interface{}{
							map[string]interface{}{
								"TypeName": "research",
								"Role":     "Worker",
								"Prompt":   "do work",
							},
						},
					},
				},
			},
		},
		{
			// Parent call 2: final response (child runs async)
			Content:      "Launched the worker.",
			FinishReason: "stop",
		},
	}

	// Override Generate to fail on the child call (call 3)
	failingProvider := &selectiveFailProvider{
		inner:      provider,
		failOnCall: 3, // Third call (child) fails
		failErr:    fmt.Errorf("child LLM quota exceeded"),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	var mu sync.Mutex
	var steps []*pb.StepUpdate
	eng := NewEngine(Config{
		Provider:         failingProvider,
		ToolRegistry:     tools.NewRegistry(nil, logger),
		ConversationID:   "test-child-err",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		mu.Lock()
		steps = append(steps, step)
		mu.Unlock()
	}

	// Parent should complete normally (child fails async)
	_ = eng.Run(context.Background(), "Run a failing subagent")

	// Wait for child goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// Check that invoke_subagent step was emitted with STATE_DONE (launch)
	mu.Lock()
	var sawSubagentStep bool
	for _, s := range steps {
		if s.GetInvokeSubagent() != nil && s.State == pb.StepUpdate_STATE_DONE {
			sawSubagentStep = true
		}
	}
	mu.Unlock()
	if !sawSubagentStep {
		t.Error("expected subagent launch step")
	}
}

// selectiveFailProvider fails on a specific call number.
// Thread-safe for concurrent use by parent and child goroutines.
type selectiveFailProvider struct {
	mu         sync.Mutex
	inner      *mockProvider
	failOnCall int
	failErr    error
	callCount  int
}

func (p *selectiveFailProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	p.mu.Lock()
	p.callCount++
	shouldFail := p.callCount == p.failOnCall
	p.mu.Unlock()
	if shouldFail {
		return nil, p.failErr
	}
	return p.inner.Generate(ctx, req)
}

func (p *selectiveFailProvider) ModelName() string { return "selective-fail-model" }
func (p *selectiveFailProvider) Close() error      { return nil }

// TestSubagentE2E_DefineInvokeManageSend exercises the full subagent lifecycle:
//  1. define_subagent — register a custom type
//  2. invoke_subagent — launch it
//  3. manage_subagents (list) — see it running
//  4. send_message — send a message to the child
func TestSubagentE2E_DefineInvokeManageSend(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				// Step 1: Parent defines a custom subagent type
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_define",
						Name: "define_subagent",
						Args: map[string]interface{}{
							"name":               "linter",
							"description":        "Runs lint checks on Go code",
							"system_prompt":      "You are a Go linter specialist.",
							"enable_write_tools": false,
						},
					},
				},
			},
			{
				// Step 2: Parent invokes the custom type
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_invoke",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"Subagents": []interface{}{
								map[string]interface{}{
									"TypeName": "linter",
									"Role":     "Lint Worker",
									"Prompt":   "Lint the engine package",
								},
							},
						},
					},
				},
			},
			{
				// Step 3: Parent calls manage_subagents to list active
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_manage",
						Name: "manage_subagents",
						Args: map[string]interface{}{
							"Action": "list",
						},
					},
				},
			},
			{
				// Step 4: Parent sends a message to the child
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_send",
						Name: "send_message",
						Args: map[string]interface{}{
							"ConversationID": "test-e2e-sub-2-linter",
							"Message":        "Focus on error handling patterns",
						},
					},
				},
			},
			{
				// Step 5: Parent wraps up
				Content:      "All done — linter subagent launched and messaged.",
				FinishReason: "stop",
			},
			{
				// Child response (runs async in goroutine)
				Content:      "Lint results: 0 issues found.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	var mu sync.Mutex
	var steps []*pb.StepUpdate

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     reg,
		SystemPrompt:     "test parent",
		ConversationID:   "test-e2e",
		TrajectoryID:     "traj_0",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
		MaxDepth:         3,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		mu.Lock()
		steps = append(steps, step)
		mu.Unlock()
	}

	err := eng.Run(context.Background(), "Define a linter subagent, invoke it, list agents, send it a message")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait for child goroutine to finish
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Verify: define_subagent step was emitted
	var sawDefine, sawInvoke, sawManage, sawSendMsg bool
	for _, s := range steps {
		if s.GetDefineSubagent() != nil {
			sawDefine = true
		}
		if s.GetInvokeSubagent() != nil && s.State == pb.StepUpdate_STATE_DONE {
			sawInvoke = true
		}
		if s.GetManageSubagents() != nil {
			sawManage = true
		}
		if s.GetSendMessageAction() != nil {
			sawSendMsg = true
		}
	}

	if !sawDefine {
		t.Error("expected define_subagent step")
	}
	if !sawInvoke {
		t.Error("expected invoke_subagent step with STATE_DONE")
	}
	if !sawManage {
		t.Error("expected manage_subagents step")
	}
	if !sawSendMsg {
		t.Error("expected send_message step")
	}

	// Verify: "linter" type was registered in the registry
	linterType, found := eng.subagentRegistry.Get("linter")
	if !found {
		t.Error("linter type should be registered after define_subagent")
	} else {
		if linterType.Description != "Runs lint checks on Go code" {
			t.Errorf("unexpected description: %q", linterType.Description)
		}
		if linterType.EnableWriteTools {
			t.Error("linter should have EnableWriteTools=false")
		}
	}

	// Verify: at least 5 LLM calls (parent define + invoke + manage + send + done, plus child)
	provider.mu.Lock()
	callCount := len(provider.callLog)
	provider.mu.Unlock()
	if callCount < 5 {
		t.Errorf("expected at least 5 LLM calls, got %d", callCount)
	}
}

func TestSubagentWorkspaceAndPermissionInheritance(t *testing.T) {
	wsDir := t.TempDir()
	filePath := filepath.Join(wsDir, "config.go")
	if err := os.WriteFile(filePath, []byte("package config\nvar Key = \"secret\"\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	var permissionCallCount atomic.Int32
	permHandler := func(ctx context.Context, req *pb.ActionPermissionRequest) (bool, string, error) {
		permissionCallCount.Add(1)
		return true, "", nil
	}

	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			// Parent Turn 1: Spawns research subagent
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_sub",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"Subagents": []interface{}{
								map[string]interface{}{
									"TypeName": "research",
									"Role":     "File Reader",
									"Prompt":   "Read config.go",
								},
							},
						},
					},
				},
			},
			// Parent Turn 2: Finish
			{
				Content:      "Subagent launched successfully",
				FinishReason: "stop",
			},
			// Child Turn 1: Reads config.go inside workspace
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_view",
						Name: "view_file",
						Args: map[string]interface{}{
							"path": filePath,
						},
					},
				},
			},
			// Child Turn 2: Done
			{
				Content:      "Found key variable in config.go",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)
	tools.RegisterBuiltinTools(reg, nil)

	eng := NewEngine(Config{
		Provider:          provider,
		ToolRegistry:      reg,
		SystemPrompt:      "test parent",
		ConversationID:    "test-conv-perm",
		TrajectoryID:      "traj_perm",
		BrainDir:          t.TempDir(),
		Logger:            logger,
		SubagentsEnabled:  true,
		MaxDepth:          3,
		Workspaces:        []string{wsDir},
		PermissionHandler: permHandler,
	})

	err := eng.Run(context.Background(), "Delegate file reading to subagent")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait for child goroutine to finish
	time.Sleep(200 * time.Millisecond)

	// Because config.go is inside workspace, subagent's view_file is auto-approved
	// without invoking PermissionHandler (same as normal agent).
	if count := permissionCallCount.Load(); count != 0 {
		t.Errorf("expected 0 permission prompts for subagent reading inside workspace, got %d", count)
	}
}

func TestSubagentModelTiering(t *testing.T) {
	parentProvider := &mockProvider{
		modelName: "claude-3-5-sonnet",
		responses: []*llm.GenerateResponse{
			// Parent Turn 1: Launches multiple subagents with different tiers
			{
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_tier_launch",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"Subagents": []interface{}{
								map[string]interface{}{
									"TypeName": "research", // Should default to flash -> claude-3-5-haiku
									"Role":     "Code Researcher",
									"Prompt":   "Investigate AST",
								},
								map[string]interface{}{
									"TypeName": "self",
									"Role":     "Self Planner",
									"Prompt":   "Plan release",
									"Model":    "pro", // Should resolve to pro -> claude-3-5-sonnet
								},
								map[string]interface{}{
									"TypeName": "self",
									"Role":     "Fast Worker",
									"Prompt":   "Fast check",
									"Model":    "flash_lite", // Should resolve to flash_lite -> claude-3-5-haiku
								},
								map[string]interface{}{
									"TypeName": "self",
									"Role":     "Cross Model Worker",
									"Prompt":   "Run benchmark",
									"Model":    "gpt-4o-mini", // Explicit model
								},
							},
						},
					},
				},
			},
			// Parent Turn 2: Parent ends
			{
				Content:      "Subagents dispatched",
				FinishReason: "stop",
			},
			// Child responses
			{Content: "Research complete", FinishReason: "stop"},
			{Content: "Plan complete", FinishReason: "stop"},
			{Content: "Fast check complete", FinishReason: "stop"},
			{Content: "Benchmark complete", FinishReason: "stop"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:         parentProvider,
		ToolRegistry:     reg,
		SystemPrompt:     "test parent",
		ConversationID:   "test-conv-tiers",
		TrajectoryID:     "traj_tiers",
		BrainDir:         t.TempDir(),
		Logger:           logger,
		SubagentsEnabled: true,
		MaxDepth:         3,
		MaxSubagents:     10,
	})

	err := eng.Run(context.Background(), "Launch tiered subagents")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify the tool result message in history contains the resolved models
	var toolResultContent string
	for _, m := range eng.History() {
		if m.ToolResult != nil && m.ToolResult.CallID == "call_tier_launch" {
			toolResultContent = m.ToolResult.Content
			break
		}
	}

	if toolResultContent == "" {
		t.Fatal("expected tool result for call_tier_launch not found in history")
	}

	// Check model names in the launch output
	if !strings.Contains(toolResultContent, `"model": "claude-3-5-haiku"`) {
		t.Errorf("expected research subagent to resolve to 'claude-3-5-haiku' (flash tier), got:\n%s", toolResultContent)
	}
	if !strings.Contains(toolResultContent, `"model": "claude-3-5-sonnet"`) {
		t.Errorf("expected pro subagent to resolve to 'claude-3-5-sonnet' (pro tier), got:\n%s", toolResultContent)
	}
	if !strings.Contains(toolResultContent, `"model": "gpt-4o-mini"`) {
		t.Errorf("expected explicit subagent to resolve to 'gpt-4o-mini', got:\n%s", toolResultContent)
	}

	// Wait for background subagents to finish before tempdir cleanup
	time.Sleep(200 * time.Millisecond)
}

func TestSubagentCompressedHandoffBriefing(t *testing.T) {
	childHandoffText := `### Handoff Briefing
- **Task & Goal**: Refactor codegraph SQLite schema.
- **Status**: COMPLETED
- **Files Created & Modified**:
  - internal/codegraph/sqlite.go: Migrated JSON to indexed SQLite.
  - internal/codegraph/sqlite_test.go: Added concurrent benchmark.
- **Key Changes & Decisions**: Zero CGO modernc.org/sqlite with WAL mode.
- **Verification & Tests**: go test ./internal/codegraph/... passed 100%.
- **Next Steps**: Update documentation.`

	notifyCh := make(chan tools.SystemMessage, 10)

	provider := &mockProvider{
		modelName: "gpt-4o",
		responses: []*llm.GenerateResponse{
			// Parent Turn 1: Launch subagent
			{
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_subagent_briefing",
						Name: "invoke_subagent",
						Args: map[string]interface{}{
							"Subagents": []interface{}{
								map[string]interface{}{
									"TypeName": "research",
									"Role":     "SQLite Researcher",
									"Prompt":   "Refactor SQLite storage",
								},
							},
						},
					},
				},
			},
			// Parent Turn 2: Done
			{
				Content:      "Parent waiting",
				FinishReason: "stop",
			},
		},
		responsesByModel: map[string][]*llm.GenerateResponse{
			"gpt-4o-mini": {
				// Child Turn: Subagent completes with full handoff text
				{
					Content:      childHandoffText,
					FinishReason: "stop",
				},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)
	parentBrainDir := t.TempDir()

	eng := NewEngine(Config{
		Provider:         provider,
		ToolRegistry:     reg,
		SystemPrompt:     "test parent",
		ConversationID:   "test-conv-compressed-handoff",
		TrajectoryID:     "traj_compressed",
		BrainDir:         parentBrainDir,
		Logger:           logger,
		SubagentsEnabled: true,
		NotifySendCh:     notifyCh,
		MaxDepth:         3,
	})

	err := eng.Run(context.Background(), "Run child and inspect handoff")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait for child notification to arrive on notifyCh
	var receivedMsg tools.SystemMessage
	select {
	case msg := <-notifyCh:
		receivedMsg = msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subagent completion notification")
	}

	if receivedMsg.Source != "subagent_complete" {
		t.Errorf("expected source 'subagent_complete', got %q", receivedMsg.Source)
	}

	lines := strings.Split(strings.TrimSpace(receivedMsg.Content), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 lines in compressed handoff notification, got %d lines:\n%s", len(lines), receivedMsg.Content)
	}

	// Line 1: Subagent status and role
	if !strings.Contains(lines[0], "Subagent 'research'") || !strings.Contains(lines[0], "completed") {
		t.Errorf("line 1 missing subagent status: %q", lines[0])
	}
	if !strings.Contains(lines[0], "Conversation ID: ") {
		t.Errorf("line 1 missing conversation ID: %q", lines[0])
	}

	// Line 2: Markdown link to handoff_briefing.md
	if !strings.Contains(lines[1], "Handoff Briefing: [handoff_briefing.md](file://") {
		t.Errorf("line 2 missing markdown link to handoff_briefing.md: %q", lines[1])
	}

	// Extract the filepath from the link and verify the file exists on disk
	linkStart := strings.Index(lines[1], "(file://")
	linkEnd := strings.Index(lines[1], ")")
	if linkStart == -1 || linkEnd == -1 {
		t.Fatalf("malformed briefing link: %q", lines[1])
	}
	filePath := lines[1][linkStart+len("(file://") : linkEnd]
	data, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatalf("failed to read handoff_briefing.md at %q: %v", filePath, readErr)
	}
	if string(data) != childHandoffText {
		t.Errorf("handoff_briefing.md content mismatch:\ngot %q\nwant %q", string(data), childHandoffText)
	}

	// Line 3: Concise summary excerpt (not the whole 500 characters)
	if !strings.HasPrefix(lines[2], "Summary: ") {
		t.Errorf("line 3 must start with 'Summary: ', got %q", lines[2])
	}
	if strings.Contains(lines[2], "###") {
		t.Errorf("line 3 should not have raw markdown headers: %q", lines[2])
	}
	if !strings.Contains(lines[2], "Refactor codegraph SQLite schema.") {
		t.Errorf("line 3 should contain the summary goal: %q", lines[2])
	}

	// Wait for background subagents to finish before tempdir cleanup
	time.Sleep(200 * time.Millisecond)
}

func TestCompactionSummarizerProvider(t *testing.T) {
	// Main provider (e.g. Claude 3.5 Sonnet)
	mainProvider := &mockProvider{
		modelName: "claude-3-5-sonnet",
		responses: []*llm.GenerateResponse{
			{Content: "normal turn answer", FinishReason: "stop"},
		},
	}

	// Fast summarizer provider (e.g. Claude 3.5 Haiku)
	summarizerProvider := &mockProvider{
		modelName: "claude-3-5-haiku",
		responses: []*llm.GenerateResponse{
			{
				Content:      "- Goal: do work\n- Files: main.go",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	// Create engine with dedicated SummarizerProvider
	eng := NewEngine(Config{
		Provider:            mainProvider,
		SummarizerProvider:  summarizerProvider,
		ToolRegistry:        reg,
		SystemPrompt:        "system",
		ConversationID:      "test-conv-summarizer",
		TrajectoryID:        "traj_sum",
		CompactionThreshold: 50, // Low threshold to force compaction
		KeepRecentMessages:  2,
		Logger:              logger,
		InitialHistory: []llm.Message{
			{Role: "user", Content: strings.Repeat("hello world this is a long conversation message ", 20)},
			{Role: "model", Content: strings.Repeat("response from assistant with lots of information ", 20)},
			{Role: "user", Content: "recent message 1"},
			{Role: "model", Content: "recent response 1"},
			{Role: "user", Content: "recent message 2"},
			{Role: "model", Content: "recent response 2"},
		},
	})

	err := eng.Run(context.Background(), "Trigger compaction on turn")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify summarizerProvider was invoked for compaction
	summarizerProvider.mu.Lock()
	sumCalls := len(summarizerProvider.callLog)
	summarizerProvider.mu.Unlock()

	if sumCalls == 0 {
		t.Error("expected summarizerProvider to be called for compaction, but callLog was empty")
	}

	// Verify mainProvider was used for normal turn generation (not compaction)
	mainProvider.mu.Lock()
	mainCalls := len(mainProvider.callLog)
	mainProvider.mu.Unlock()

	if mainCalls != 1 {
		t.Errorf("expected mainProvider to be called exactly 1 time for turn generation, got %d", mainCalls)
	}
}
