package engine

import (
	"log/slog"
	"os"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func TestToolCallRecovery_OpenAIStyle(t *testing.T) {
	knownTools := map[string]bool{"view_file": true, "grep_search": true}
	content := `{"type": "function", "name": "view_file", "parameters": {"path": "/foo/bar.go"}}`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "view_file" {
		t.Errorf("expected name 'view_file', got %q", calls[0].Name)
	}
	if calls[0].Args["path"] != "/foo/bar.go" {
		t.Errorf("expected path '/foo/bar.go', got %v", calls[0].Args["path"])
	}
	if calls[0].ID != "recovered_call_0" {
		t.Errorf("expected ID 'recovered_call_0', got %q", calls[0].ID)
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
}

func TestToolCallRecovery_SimplifiedStyle(t *testing.T) {
	knownTools := map[string]bool{"grep_search": true}
	content := `{"name": "grep_search", "args": {"query": "TODO", "path": "/src"}}`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "grep_search" {
		t.Errorf("expected name 'grep_search', got %q", calls[0].Name)
	}
	if calls[0].Args["query"] != "TODO" {
		t.Errorf("expected query 'TODO', got %v", calls[0].Args["query"])
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
}

func TestToolCallRecovery_MultipleCalls(t *testing.T) {
	knownTools := map[string]bool{"view_file": true, "list_dir": true}
	content := `I'll look at these files:
{"name": "view_file", "parameters": {"path": "/a.go"}}
{"name": "list_dir", "parameters": {"path": "/src"}}
Let me check.`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "view_file" {
		t.Errorf("expected first call 'view_file', got %q", calls[0].Name)
	}
	if calls[1].Name != "list_dir" {
		t.Errorf("expected second call 'list_dir', got %q", calls[1].Name)
	}
	if calls[0].ID != "recovered_call_0" || calls[1].ID != "recovered_call_1" {
		t.Errorf("unexpected IDs: %q, %q", calls[0].ID, calls[1].ID)
	}
	// Remaining should have the surrounding text
	if remaining == "" {
		t.Error("expected non-empty remaining text")
	}
}

func TestToolCallRecovery_UnknownTool(t *testing.T) {
	knownTools := map[string]bool{"view_file": true}
	content := `{"name": "hack_system", "parameters": {"target": "root"}}`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls for unknown tool, got %d", len(calls))
	}
	if remaining != content {
		t.Errorf("expected content unchanged, got %q", remaining)
	}
}

func TestToolCallRecovery_MixedTextAndToolCall(t *testing.T) {
	knownTools := map[string]bool{"view_file": true}
	content := `Let me check the file first.
{"name": "view_file", "parameters": {"path": "/main.go"}}
I'll review the implementation.`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "view_file" {
		t.Errorf("expected name 'view_file', got %q", calls[0].Name)
	}
	// Remaining should have the surrounding text (cleaned up)
	if remaining == "" {
		t.Error("expected non-empty remaining text")
	}
	// The extracted JSON should not be in remaining
	if contains(remaining, `"view_file"`) {
		t.Errorf("remaining should not contain extracted tool call JSON: %q", remaining)
	}
}

func TestToolCallRecovery_NonJSONContent(t *testing.T) {
	knownTools := map[string]bool{"view_file": true}
	content := "This is just regular text with no JSON at all."

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(calls))
	}
	if remaining != content {
		t.Errorf("expected content unchanged, got %q", remaining)
	}
}

func TestToolCallRecovery_MalformedJSON(t *testing.T) {
	knownTools := map[string]bool{"view_file": true}
	content := `{"name": "view_file", "parameters": {"path": "/main.go`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls for malformed JSON, got %d", len(calls))
	}
	if remaining != content {
		t.Errorf("expected content unchanged, got %q", remaining)
	}
}

func TestToolCallRecovery_EmptyContent(t *testing.T) {
	knownTools := map[string]bool{"view_file": true}
	calls, remaining := tryExtractToolCallsFromText("", knownTools, testLogger)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(calls))
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
}

func TestToolCallRecovery_NoKnownTools(t *testing.T) {
	content := `{"name": "view_file", "parameters": {"path": "/main.go"}}`
	calls, remaining := tryExtractToolCallsFromText(content, nil, testLogger)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls with nil known tools, got %d", len(calls))
	}
	if remaining != content {
		t.Errorf("expected content unchanged, got %q", remaining)
	}
}

func TestToolCallRecovery_NestedJSON(t *testing.T) {
	knownTools := map[string]bool{"run_command": true}
	content := `{"name": "run_command", "parameters": {"command": "echo '{\"key\": \"value\"}'", "cwd": "/tmp"}}`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "run_command" {
		t.Errorf("expected name 'run_command', got %q", calls[0].Name)
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
}

func TestToolCallRecovery_InvokeSubagentFromText(t *testing.T) {
	// This is the actual pattern we observed from Llama 3.3 70B
	knownTools := map[string]bool{"invoke_subagent": true, "view_file": true}
	content := `{"type": "function", "name": "invoke_subagent", "parameters": {"Subagents": "[{\"TypeName\": \"research\", \"Role\": \"Researcher\", \"Prompt\": \"Find issues\"}]"}}`

	calls, remaining := tryExtractToolCallsFromText(content, knownTools, testLogger)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "invoke_subagent" {
		t.Errorf("expected name 'invoke_subagent', got %q", calls[0].Name)
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
}

func TestFindJSONObjects(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"no json", "hello world", 0},
		{"single object", `{"key": "value"}`, 1},
		{"two objects", `{"a": 1} and {"b": 2}`, 2},
		{"nested", `{"a": {"b": "c"}}`, 1},
		{"unmatched brace", `{"a": "b"`, 0},
		{"brace in string", `{"a": "}{"}`, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := findJSONObjects(tt.input)
			if len(objects) != tt.expected {
				t.Errorf("expected %d objects, got %d", tt.expected, len(objects))
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
