package hooks

import (
	"fmt"
	"testing"
)

// --- Test helpers ---

// mockDecideHook is a PreToolCallDecideHook for testing.
type mockDecideHook struct {
	result   HookResult
	received []ToolCall
}

func (m *mockDecideHook) Run(ctx *HookContext, tc ToolCall) HookResult {
	m.received = append(m.received, tc)
	return m.result
}

// mockPostToolHook is a PostToolCallHook for testing.
type mockPostToolHook struct {
	received []ToolResult
}

func (m *mockPostToolHook) Run(ctx *HookContext, result ToolResult) {
	m.received = append(m.received, result)
}

// mockPreTurnHook is a PreTurnHook for testing.
type mockPreTurnHook struct {
	result   HookResult
	received []string
}

func (m *mockPreTurnHook) Run(ctx *HookContext, prompt string) HookResult {
	m.received = append(m.received, prompt)
	return m.result
}

// mockSessionHook implements both OnSessionStartHook and OnSessionEndHook.
type mockSessionHook struct {
	startCalled bool
	endCalled   bool
}

func (m *mockSessionHook) Run(ctx *HookContext) {
	// This is called by both start and end — differentiated by test assertions.
	m.startCalled = true
}

// --- Tests ---

func TestHookRunner_RegisterAndDispatch(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockDecideHook{result: HookResult{Allow: true}}
	if err := runner.RegisterHook(hook); err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	if !runner.HasHooks() {
		t.Fatal("expected HasHooks to be true")
	}

	tc := ToolCall{Name: "view_file", Args: map[string]any{"path": "/tmp/test.go"}}
	result := runner.DispatchPreToolCall(tc)

	if !result.Allow {
		t.Fatal("expected Allow=true")
	}
	if len(hook.received) != 1 {
		t.Fatalf("expected 1 call, got %d", len(hook.received))
	}
	if hook.received[0].Name != "view_file" {
		t.Fatalf("expected tool name 'view_file', got %q", hook.received[0].Name)
	}
}

func TestHookRunner_FirstDenyWins(t *testing.T) {
	runner := NewHookRunner()

	allowHook := &mockDecideHook{result: HookResult{Allow: true}}
	denyHook := &mockDecideHook{result: HookResult{Allow: false, Message: "denied by test"}}
	neverReachedHook := &mockDecideHook{result: HookResult{Allow: true}}

	runner.RegisterHook(allowHook)
	runner.RegisterHook(denyHook)
	runner.RegisterHook(neverReachedHook)

	tc := ToolCall{Name: "run_command"}
	result := runner.DispatchPreToolCall(tc)

	if result.Allow {
		t.Fatal("expected denial")
	}
	if result.Message != "denied by test" {
		t.Fatalf("expected denial message, got %q", result.Message)
	}
	// The third hook should never have been called (short-circuit)
	if len(neverReachedHook.received) != 0 {
		t.Fatal("expected short-circuit — third hook should not be called")
	}
}

func TestHookRunner_PostToolCall(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockPostToolHook{}
	runner.RegisterHook(hook)

	result := ToolResult{Name: "view_file", Content: "file contents", CallID: "c1"}
	runner.DispatchPostToolCall(result)

	if len(hook.received) != 1 {
		t.Fatalf("expected 1 result, got %d", len(hook.received))
	}
	if hook.received[0].Name != "view_file" {
		t.Fatalf("expected tool name 'view_file', got %q", hook.received[0].Name)
	}
}

func TestHookRunner_PreTurn_Deny(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockPreTurnHook{result: HookResult{Allow: false, Message: "rate limited"}}
	runner.RegisterHook(hook)

	result := runner.DispatchPreTurn("hello")
	if result.Allow {
		t.Fatal("expected denial")
	}
	if len(hook.received) != 1 || hook.received[0] != "hello" {
		t.Fatalf("expected prompt 'hello', got %v", hook.received)
	}
}

func TestHookRunner_NoHooks(t *testing.T) {
	runner := NewHookRunner()

	if runner.HasHooks() {
		t.Fatal("expected HasHooks to be false with no hooks")
	}

	// All dispatches should return allow by default
	result := runner.DispatchPreToolCall(ToolCall{Name: "anything"})
	if !result.Allow {
		t.Fatal("expected default allow with no hooks")
	}

	turnResult := runner.DispatchPreTurn("anything")
	if !turnResult.Allow {
		t.Fatal("expected default allow with no hooks")
	}
}

func TestHookRunner_RegisterUnknownType(t *testing.T) {
	runner := NewHookRunner()

	err := runner.RegisterHook("not a hook")
	if err == nil {
		t.Fatal("expected error for unknown hook type")
	}
}

func TestHookContext_Hierarchy(t *testing.T) {
	parent := NewHookContext()
	parent.Set("session_id", "s1")

	child := parent.Child()
	child.Set("turn_id", "t1")

	// Child can see parent values
	if v, ok := child.Get("session_id"); !ok || v != "s1" {
		t.Fatalf("expected to inherit 'session_id' from parent, got %v", v)
	}

	// Child can see own values
	if v, ok := child.Get("turn_id"); !ok || v != "t1" {
		t.Fatalf("expected 'turn_id' in child, got %v", v)
	}

	// Parent cannot see child values
	if _, ok := parent.Get("turn_id"); ok {
		t.Fatal("parent should not see child values")
	}
}

// --- OnToolErrorHook tests ---

type mockToolErrorHook struct {
	result   ToolErrorResult
	received []ToolError
}

func (m *mockToolErrorHook) Run(ctx *HookContext, te ToolError) ToolErrorResult {
	m.received = append(m.received, te)
	return m.result
}

func TestHookRunner_ToolError_Handled(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockToolErrorHook{result: ToolErrorResult{
		Handled:         true,
		RecoveryContent: "recovered: using fallback",
	}}
	runner.RegisterHook(hook)

	te := ToolError{
		ToolName: "run_command",
		Error:    fmt.Errorf("command not found"),
		Args:     map[string]any{"command": "ls"},
	}

	result := runner.DispatchToolError(te)
	if !result.Handled {
		t.Fatal("expected error to be handled")
	}
	if result.RecoveryContent != "recovered: using fallback" {
		t.Errorf("expected recovery content, got %q", result.RecoveryContent)
	}
	if len(hook.received) != 1 || hook.received[0].ToolName != "run_command" {
		t.Fatalf("expected tool error for run_command, got %v", hook.received)
	}
}

func TestHookRunner_ToolError_Unhandled(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockToolErrorHook{result: ToolErrorResult{Handled: false}}
	runner.RegisterHook(hook)

	te := ToolError{ToolName: "view_file", Error: fmt.Errorf("file not found")}
	result := runner.DispatchToolError(te)

	if result.Handled {
		t.Fatal("expected error to NOT be handled")
	}
}

func TestHookRunner_ToolError_NoHooks(t *testing.T) {
	runner := NewHookRunner()

	te := ToolError{ToolName: "view_file", Error: fmt.Errorf("fail")}
	result := runner.DispatchToolError(te)

	if result.Handled {
		t.Fatal("expected unhandled with no hooks")
	}
}

// --- OnInteractionHook tests ---

type mockInteractionHook struct {
	result   InteractionResult
	received []Interaction
}

func (m *mockInteractionHook) Run(ctx *HookContext, interaction Interaction) InteractionResult {
	m.received = append(m.received, interaction)
	return m.result
}

func TestHookRunner_Interaction_Responded(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockInteractionHook{result: InteractionResult{
		Response: "option A",
	}}
	runner.RegisterHook(hook)

	interaction := Interaction{
		Question:  "Which option?",
		Options:   []string{"A", "B"},
		StepIndex: 5,
	}

	result := runner.DispatchInteraction(interaction)
	if result.Dismissed {
		t.Fatal("expected non-dismissed response")
	}
	if result.Response != "option A" {
		t.Errorf("expected 'option A', got %q", result.Response)
	}
	if len(hook.received) != 1 || hook.received[0].Question != "Which option?" {
		t.Fatalf("expected interaction, got %v", hook.received)
	}
}

func TestHookRunner_Interaction_Dismissed(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockInteractionHook{result: InteractionResult{Dismissed: true}}
	runner.RegisterHook(hook)

	result := runner.DispatchInteraction(Interaction{Question: "test?"})
	if !result.Dismissed {
		t.Fatal("expected dismissed")
	}
}

func TestHookRunner_Interaction_NoHooks(t *testing.T) {
	runner := NewHookRunner()

	result := runner.DispatchInteraction(Interaction{Question: "test?"})
	if !result.Dismissed {
		t.Fatal("expected dismissed with no hooks")
	}
}

// --- OnCompactionHook tests ---

type mockCompactionHook struct {
	received []CompactionEvent
}

func (m *mockCompactionHook) Run(ctx *HookContext, event CompactionEvent) {
	m.received = append(m.received, event)
}

func TestHookRunner_Compaction(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockCompactionHook{}
	runner.RegisterHook(hook)

	event := CompactionEvent{
		OriginalTokens:  85000,
		CompactedTokens: 12000,
		MessagesRemoved: 45,
		Summary:         "User asked to refactor auth...",
	}

	runner.DispatchCompaction(event)

	if len(hook.received) != 1 {
		t.Fatalf("expected 1 compaction event, got %d", len(hook.received))
	}
	if hook.received[0].OriginalTokens != 85000 {
		t.Errorf("expected 85000 original tokens, got %d", hook.received[0].OriginalTokens)
	}
	if hook.received[0].MessagesRemoved != 45 {
		t.Errorf("expected 45 messages removed, got %d", hook.received[0].MessagesRemoved)
	}
}

func TestHookRunner_Compaction_NoHooks(t *testing.T) {
	// Just ensure no panic
	runner := NewHookRunner()
	runner.DispatchCompaction(CompactionEvent{OriginalTokens: 100})
}

// --- Has*Hooks tests ---

func TestHookRunner_HasNewHooks(t *testing.T) {
	runner := NewHookRunner()

	if runner.HasOnToolErrorHooks() {
		t.Fatal("expected no tool error hooks")
	}
	if runner.HasOnInteractionHooks() {
		t.Fatal("expected no interaction hooks")
	}
	if runner.HasOnArtifactFeedbackHooks() {
		t.Fatal("expected no artifact feedback hooks")
	}

	runner.RegisterHook(&mockToolErrorHook{})
	if !runner.HasOnToolErrorHooks() {
		t.Fatal("expected tool error hooks")
	}
	if !runner.HasHooks() {
		t.Fatal("expected HasHooks=true")
	}
}

// --- OnArtifactFeedbackHook tests ---

type mockArtifactFeedbackHook struct {
	received []ArtifactFeedbackEvent
}

func (m *mockArtifactFeedbackHook) Run(ctx *HookContext, event ArtifactFeedbackEvent) {
	m.received = append(m.received, event)
}

func TestHookRunner_ArtifactFeedback(t *testing.T) {
	runner := NewHookRunner()

	hook := &mockArtifactFeedbackHook{}
	runner.RegisterHook(hook)

	event := ArtifactFeedbackEvent{
		Path:         "/brain/conv-123/implementation_plan.md",
		Filename:     "implementation_plan.md",
		ArtifactType: "implementation_plan",
		Summary:      "Refactor auth module into service layer",
	}

	runner.DispatchArtifactFeedback(event)

	if len(hook.received) != 1 {
		t.Fatalf("expected 1 artifact feedback event, got %d", len(hook.received))
	}
	if hook.received[0].Filename != "implementation_plan.md" {
		t.Errorf("expected filename 'implementation_plan.md', got %q", hook.received[0].Filename)
	}
	if hook.received[0].ArtifactType != "implementation_plan" {
		t.Errorf("expected artifact type 'implementation_plan', got %q", hook.received[0].ArtifactType)
	}
	if hook.received[0].Summary != "Refactor auth module into service layer" {
		t.Errorf("expected summary, got %q", hook.received[0].Summary)
	}
}

func TestHookRunner_ArtifactFeedback_NoHooks(t *testing.T) {
	// Just ensure no panic
	runner := NewHookRunner()
	runner.DispatchArtifactFeedback(ArtifactFeedbackEvent{Filename: "plan.md"})
}

func TestHookRunner_ArtifactFeedback_HasHooks(t *testing.T) {
	runner := NewHookRunner()

	if runner.HasOnArtifactFeedbackHooks() {
		t.Fatal("expected no artifact feedback hooks initially")
	}

	runner.RegisterHook(&mockArtifactFeedbackHook{})
	if !runner.HasOnArtifactFeedbackHooks() {
		t.Fatal("expected artifact feedback hooks after registration")
	}
	if !runner.HasHooks() {
		t.Fatal("expected HasHooks=true after artifact feedback hook registration")
	}
}
