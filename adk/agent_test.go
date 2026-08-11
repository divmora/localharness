package adk

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/adk/connection"
	"github.com/divmora/localharness/adk/policy"
)

type mockConnection struct {
	sentPrompt     string
	steps          []connection.Step
	permissionResp map[string]bool
	closeCalled    bool
	conversationID string
}

func (m *mockConnection) Send(ctx context.Context, prompt string) error {
	m.sentPrompt = prompt
	return nil
}

func (m *mockConnection) SendWithContext(ctx context.Context, prompt string, userCtx *pb.UserContext, ephemeralMsgs []string) error {
	m.sentPrompt = prompt
	return nil
}

func (m *mockConnection) ReceiveSteps(ctx context.Context) (<-chan connection.Step, error) {
	ch := make(chan connection.Step, len(m.steps))
	for _, s := range m.steps {
		ch <- s
	}
	close(ch)
	return ch, nil
}

func (m *mockConnection) SendPermissionResponse(ctx context.Context, requestID string, approved bool, reason string) error {
	if m.permissionResp == nil {
		m.permissionResp = make(map[string]bool)
	}
	m.permissionResp[requestID] = approved
	return nil
}

func (m *mockConnection) SendToolResult(ctx context.Context, stepID, toolName, resultJSON string, isError bool) error {
	return nil
}

func (m *mockConnection) SendQuestionResponse(ctx context.Context, requestID string, answers []*connection.QuestionAnswer, skipped bool) error {
	return nil
}

func (m *mockConnection) Close() error {
	m.closeCalled = true
	return nil
}

func (m *mockConnection) ConversationID() string {
	return m.conversationID
}

func (m *mockConnection) FetchAgentCard(ctx context.Context) (*connection.AgentCard, error) {
	return &connection.AgentCard{
		Name:    "mock-agent",
		Version: "0.0.0-test",
	}, nil
}

func TestAgent_ChatBasic(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-123",
		steps: []connection.Step{
			{
				Index:   1,
				Text:    "Hello! I am ready.",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
			},
		},
	}
	agent.conn = mock
	agent.started = true

	resp, err := agent.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if mock.sentPrompt != "hi" {
		t.Errorf("expected sent prompt 'hi', got %q", mock.sentPrompt)
	}

	if resp.Text != "Hello! I am ready." {
		t.Errorf("expected response 'Hello! I am ready.', got %q", resp.Text)
	}

	if len(resp.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(resp.Steps))
	}
}

func TestAgent_PermissionRequest(t *testing.T) {
	policies := []policy.Policy{
		policy.DenyRule("run_command", policy.WithName("deny_run_command")),
	}

	cfg := &LocalAgentConfig{
		Policies:     policies,
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-123",
		steps: []connection.Step{
			{
				Index:               1,
				PermissionRequestID: "req-1",
				PermissionToolName:  "run_command",
				State:               connection.StateWaiting,
			},
			{
				Index:     2,
				Text:      "Permission was denied, so I stopped.",
				State:     connection.StateDone,
				Source:    connection.SourceModel,
				IsFinal:   true,
			},
		},
	}
	agent.conn = mock
	agent.started = true

	_, err = agent.Chat(context.Background(), "run command")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if mock.permissionResp == nil {
		t.Fatal("expected permission response to be sent")
	}

	approved, sent := mock.permissionResp["req-1"]
	if !sent {
		t.Error("expected permission response to be sent for req-1")
	}
	if approved {
		t.Error("expected permission for run_command to be denied")
	}
}

func TestAgent_CustomLoggerAndVerbose(t *testing.T) {
	// 1. Check Verbose turns on level debug
	cfg := &LocalAgentConfig{
		Verbose:      true,
		Policies:     []policy.Policy{},
	}
	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	if agent.logger == nil {
		t.Error("expected logger to be initialized")
	}

	// 2. Check Custom Logger is preserved
	customLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg2 := &LocalAgentConfig{
		Logger:       customLogger,
		Policies:     []policy.Policy{},
	}
	agent2, err := NewAgent(cfg2)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	if agent2.logger != customLogger {
		t.Error("expected custom logger to be used")
	}
}

func TestAgent_TokenBudgetLimit(t *testing.T) {
	cfg := &LocalAgentConfig{
		MaxTotalTokens: 100,
		Policies:       []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-123",
		steps: []connection.Step{
			{
				Index:   1,
				Text:    "Hello! I am ready.",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
				Usage: &connection.UsageMetadata{
					TotalTokens: 60,
				},
			},
		},
	}
	agent.conn = mock
	agent.started = true

	// First call consumes 60 tokens (60 total < 100 limit, so it should succeed)
	resp, err := agent.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("first Chat failed: %v", err)
	}
	if agent.totalTokens != 60 {
		t.Errorf("expected total tokens 60, got %d", agent.totalTokens)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 60 {
		t.Errorf("expected response usage 60, got %v", resp.Usage)
	}

	// Next call should fail if cumulative total exceeds limit, but 60 is < 100.
	// So second chat is allowed to run.
	mock.steps = []connection.Step{
		{
			Index:   2,
			Text:    "Hello again.",
			State:   connection.StateDone,
			Source:  connection.SourceModel,
			IsFinal: true,
			Usage: &connection.UsageMetadata{
				TotalTokens: 50,
			},
		},
	}

	// Run second chat (succeeds and total becomes 110)
	_, err = agent.Chat(context.Background(), "hi again")
	if err != nil {
		t.Fatalf("second Chat failed: %v", err)
	}
	if agent.totalTokens != 110 {
		t.Errorf("expected total tokens 110, got %d", agent.totalTokens)
	}

	// Third chat should fail immediately because totalTokens (110) >= limit (100)
	_, err = agent.Chat(context.Background(), "third prompt")
	if err == nil {
		t.Fatal("expected Chat to fail due to token budget limit, but it succeeded")
	}
	if !strings.Contains(err.Error(), "token budget exceeded") {
		t.Errorf("expected token budget exceeded error, got: %v", err)
	}
}

// --- ChatStream tests ---

func TestAgent_ChatStreamBasic(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-stream-1",
		steps: []connection.Step{
			{
				Index:   1,
				Text:    "Here is the result.",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
				Usage: &connection.UsageMetadata{
					TotalTokens: 42,
				},
			},
		},
	}
	agent.conn = mock
	agent.started = true

	events, err := agent.ChatStream(context.Background(), "what's the answer?")
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var gotTurnComplete bool
	var resp *ChatResponse
	eventCount := 0
	for event := range events {
		eventCount++
		if event.Type == EventTurnComplete {
			gotTurnComplete = true
			resp = event.Response
		}
	}

	if !gotTurnComplete {
		t.Fatal("expected EventTurnComplete, never received")
	}
	if resp == nil {
		t.Fatal("expected non-nil response in TurnComplete")
	}
	if resp.Text != "Here is the result." {
		t.Errorf("expected response text, got %q", resp.Text)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 42 {
		t.Errorf("expected 42 total tokens, got %v", resp.Usage)
	}
}

func TestAgent_ChatStreamToolCall(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-stream-tool",
		steps: []connection.Step{
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateActive,
				Source:   connection.SourceModel,
			},
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateDone,
				Source:   connection.SourceModel,
			},
			{
				Index:   2,
				Text:    "Done!",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
			},
		},
	}
	agent.conn = mock
	agent.started = true

	events, err := agent.ChatStream(context.Background(), "read the file")
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var types []StreamEventType
	for event := range events {
		types = append(types, event.Type)
	}

	// Expect: ToolCallStart, ToolCallDone, TurnComplete
	if len(types) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(types), types)
	}

	// Find tool events
	hasStart := false
	hasDone := false
	hasComplete := false
	for _, et := range types {
		switch et {
		case EventToolCallStart:
			hasStart = true
		case EventToolCallDone:
			hasDone = true
		case EventTurnComplete:
			hasComplete = true
		}
	}

	if !hasStart {
		t.Error("missing EventToolCallStart")
	}
	if !hasDone {
		t.Error("missing EventToolCallDone")
	}
	if !hasComplete {
		t.Error("missing EventTurnComplete")
	}
}

func TestAgent_ChatStreamDelta(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-stream-delta",
		steps: []connection.Step{
			{
				Index:     1,
				TextDelta: "Hello ",
				State:     connection.StateStreaming,
				Source:    connection.SourceModel,
			},
			{
				Index:         1,
				ThinkingDelta: "I should greet the user.",
				State:         connection.StateStreaming,
				Source:        connection.SourceModel,
			},
			{
				Index:     1,
				TextDelta: "World!",
				State:     connection.StateStreaming,
				Source:    connection.SourceModel,
			},
			{
				Index:   1,
				Text:    "Hello World!",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
			},
		},
	}
	agent.conn = mock
	agent.started = true

	events, err := agent.ChatStream(context.Background(), "greet me")
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var textDeltas []string
	var thinkingDeltas []string
	for event := range events {
		switch event.Type {
		case EventTextDelta:
			textDeltas = append(textDeltas, event.TextDelta)
		case EventThinkingDelta:
			thinkingDeltas = append(thinkingDeltas, event.ThinkingDelta)
		}
	}

	if len(textDeltas) != 2 {
		t.Fatalf("expected 2 text deltas, got %d: %v", len(textDeltas), textDeltas)
	}
	if textDeltas[0] != "Hello " || textDeltas[1] != "World!" {
		t.Errorf("unexpected text deltas: %v", textDeltas)
	}
	if len(thinkingDeltas) != 1 || thinkingDeltas[0] != "I should greet the user." {
		t.Errorf("unexpected thinking deltas: %v", thinkingDeltas)
	}
}

func TestAgent_ChatStreamError(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-stream-error",
		steps: []connection.Step{
			{
				Index:        1,
				ToolName:     "run_command",
				State:        connection.StateError,
				Source:       connection.SourceModel,
				ErrorMessage: "permission denied",
			},
			{
				Index:   2,
				Text:    "Sorry, I cannot run commands.",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
			},
		},
	}
	agent.conn = mock
	agent.started = true

	events, err := agent.ChatStream(context.Background(), "run ls")
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var hasError bool
	var hasComplete bool
	var resp *ChatResponse
	for event := range events {
		switch event.Type {
		case EventError:
			hasError = true
			if event.Step.ErrorMessage != "permission denied" {
				t.Errorf("expected error message 'permission denied', got %q", event.Step.ErrorMessage)
			}
		case EventTurnComplete:
			hasComplete = true
			resp = event.Response
		}
	}

	if !hasError {
		t.Error("missing EventError for failed tool")
	}
	if !hasComplete {
		t.Fatal("missing EventTurnComplete")
	}
	if resp.Text != "Sorry, I cannot run commands." {
		t.Errorf("unexpected response: %q", resp.Text)
	}
}

// TestAgent_TokenUsageDeduplication verifies that the SDK only counts Usage
// once per step index, even when the harness sends it on multiple sub-step
// updates (Active → Permission → Done) for the same tool call.
func TestAgent_TokenUsageDeduplication(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	// Simulate a tool call that sends Usage on 3 sub-step updates
	// (Active, Permission/Waiting, Done) — all with the same Index.
	usage := &connection.UsageMetadata{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		CachedTokens:     50,
	}

	mock := &mockConnection{
		conversationID: "conv-dedup",
		steps: []connection.Step{
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateActive,
				Source:   connection.SourceModel,
				Usage:    usage,
			},
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateWaiting,
				Source:   connection.SourceModel,
				Usage:    usage, // Same usage, same index
			},
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateDone,
				Source:   connection.SourceModel,
				Usage:    usage, // Same usage, same index
			},
			{
				Index:   2,
				Text:    "Done!",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
			},
		},
	}
	agent.conn = mock
	agent.started = true

	_, err = agent.Chat(context.Background(), "test dedup")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Should be 120, NOT 360 (120 × 3)
	if agent.totalTokens != 120 {
		t.Errorf("expected totalTokens 120 (counted once), got %d", agent.totalTokens)
	}
}

// TestAgent_TokenUsageMultipleSteps verifies that Usage from different step
// indices is correctly accumulated (each distinct step counted once).
func TestAgent_TokenUsageMultipleSteps(t *testing.T) {
	cfg := &LocalAgentConfig{
		Policies:     []policy.Policy{},
	}

	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	mock := &mockConnection{
		conversationID: "conv-multi",
		steps: []connection.Step{
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateActive,
				Source:   connection.SourceModel,
				Usage:    &connection.UsageMetadata{TotalTokens: 50},
			},
			{
				Index:    1,
				ToolName: "view_file",
				State:    connection.StateDone,
				Source:   connection.SourceModel,
				Usage:    &connection.UsageMetadata{TotalTokens: 50}, // Duplicate — should be ignored
			},
			{
				Index:    3,
				ToolName: "grep_search",
				State:    connection.StateActive,
				Source:   connection.SourceModel,
				Usage:    &connection.UsageMetadata{TotalTokens: 80},
			},
			{
				Index:    3,
				ToolName: "grep_search",
				State:    connection.StateDone,
				Source:   connection.SourceModel,
				Usage:    &connection.UsageMetadata{TotalTokens: 80}, // Duplicate — should be ignored
			},
			{
				Index:   5,
				Text:    "Here are the results.",
				State:   connection.StateDone,
				Source:  connection.SourceModel,
				IsFinal: true,
				Usage:   &connection.UsageMetadata{TotalTokens: 100},
			},
		},
	}
	agent.conn = mock
	agent.started = true

	_, err = agent.Chat(context.Background(), "test multi")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// 50 (step 1) + 80 (step 3) + 100 (step 5) = 230
	expected := 230
	if agent.totalTokens != expected {
		t.Errorf("expected totalTokens %d, got %d", expected, agent.totalTokens)
	}
}
