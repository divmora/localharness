package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/divmora/localharness/internal/llm"
)

// --- Deduplication Tests ---

func TestDeduplicateViewFile_SamePath_SupersetRange(t *testing.T) {
	// Turn 3: view_file("auth.go", L1-L200) → old content
	// Turn 7: view_file("auth.go", L1-L200) → new content
	// Result: Turn 3 result should be replaced with pointer
	messages := []llm.Message{
		{Role: "user", Content: "check auth.go"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 1.0, "end_line": 200.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: strings.Repeat("old line\n", 200)}},
		{Role: "model", Content: "I see the file"},
		{Role: "user", Content: "check again"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 1.0, "end_line": 200.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "view_file", Content: strings.Repeat("new line\n", 200)}},
		{Role: "model", Content: "updated content"},
		// Fresh window below (8 messages, but we have 8 total — so freshWindow=4 for this test)
	}

	result, stats := ReduceHistory(messages, 4)

	if stats.DeduplicatedFiles != 1 {
		t.Fatalf("expected 1 dedup, got %d", stats.DeduplicatedFiles)
	}
	if stats.TokensSaved <= 0 {
		t.Fatal("expected tokens saved > 0")
	}

	// Old result should be replaced
	oldResult := result[2].ToolResult.Content
	if !strings.Contains(oldResult, "Re-read") {
		t.Fatalf("expected pointer message, got: %s", oldResult[:100])
	}

	// New result should be preserved
	newResult := result[6].ToolResult.Content
	if !strings.Contains(newResult, "new line") {
		t.Fatal("newer result should be preserved")
	}
}

func TestDeduplicateViewFile_DifferentRanges_NoOverlap(t *testing.T) {
	// Turn 3: view_file("auth.go", L1-L200)
	// Turn 5: view_file("auth.go", L300-L400)
	// Result: Both kept — non-overlapping ranges
	messages := []llm.Message{
		{Role: "user", Content: "check auth"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 1.0, "end_line": 200.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: "lines 1-200"}},
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "check bottom"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 300.0, "end_line": 400.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "view_file", Content: "lines 300-400"}},
		{Role: "model", Content: "done"},
	}

	_, stats := ReduceHistory(messages, 3)

	if stats.DeduplicatedFiles != 0 {
		t.Fatalf("expected 0 dedup for non-overlapping ranges, got %d", stats.DeduplicatedFiles)
	}
}

func TestDeduplicateViewFile_OlderHasMoreLines(t *testing.T) {
	// Turn 3: view_file("auth.go", L1-L200) — broader range
	// Turn 5: view_file("auth.go", L50-L100) — narrower range
	// Result: Both kept — older has MORE info (not a superset)
	messages := []llm.Message{
		{Role: "user", Content: "check auth"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 1.0, "end_line": 200.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: "lines 1-200"}},
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "focus"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 50.0, "end_line": 100.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "view_file", Content: "lines 50-100"}},
		{Role: "model", Content: "done"},
	}

	_, stats := ReduceHistory(messages, 3)

	if stats.DeduplicatedFiles != 0 {
		t.Fatalf("expected 0 dedup when older has broader range, got %d", stats.DeduplicatedFiles)
	}
}

func TestDeduplicateViewFile_DifferentPaths(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "check files"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/auth.go", "start_line": 1.0, "end_line": 100.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: "auth content"}},
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "check db"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "view_file", Args: map[string]interface{}{"path": "/path/db.go", "start_line": 1.0, "end_line": 100.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "view_file", Content: "db content"}},
		{Role: "model", Content: "done"},
	}

	_, stats := ReduceHistory(messages, 3)

	if stats.DeduplicatedFiles != 0 {
		t.Fatalf("expected 0 dedup for different files, got %d", stats.DeduplicatedFiles)
	}
}

func TestDeduplicateViewFile_FullFileReread(t *testing.T) {
	// No start_line/end_line = full file (normalized to 1-999999)
	// Second full read supersedes the first
	messages := []llm.Message{
		{Role: "user", Content: "check"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/main.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: "full file old"}},
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "again"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "view_file", Args: map[string]interface{}{"path": "/path/main.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "view_file", Content: "full file new"}},
		{Role: "model", Content: "done"},
	}

	result, stats := ReduceHistory(messages, 3)

	if stats.DeduplicatedFiles != 1 {
		t.Fatalf("expected 1 dedup for full file re-read, got %d", stats.DeduplicatedFiles)
	}
	if !strings.Contains(result[2].ToolResult.Content, "Re-read") {
		t.Fatal("older full-file read should be replaced")
	}
}

// --- Command Collapse Tests ---

func TestCollapseCommands_SameCommand(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "build"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "run_command", Content: "FAIL: 3 errors\ndetails..."}},
		{Role: "model", Content: "fixing"},
		{Role: "user", Content: "try again"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "run_command", Content: "ok - all pass"}},
		{Role: "model", Content: "all good"},
	}

	result, stats := ReduceHistory(messages, 3)

	if stats.CollapsedCommands != 1 {
		t.Fatalf("expected 1 collapsed, got %d", stats.CollapsedCommands)
	}

	oldResult := result[2].ToolResult.Content
	if !strings.Contains(oldResult, "Command re-run") {
		t.Fatalf("expected collapse pointer, got: %s", oldResult)
	}

	// Latest result preserved
	if result[6].ToolResult.Content != "ok - all pass" {
		t.Fatal("latest result should be preserved")
	}
}

func TestCollapseCommands_DifferentCommands(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "build"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "run_command", Args: map[string]interface{}{"command": "go build ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "run_command", Content: "build ok"}},
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "test"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "run_command", Content: "test ok"}},
		{Role: "model", Content: "done"},
	}

	_, stats := ReduceHistory(messages, 3)

	if stats.CollapsedCommands != 0 {
		t.Fatalf("expected 0 collapsed for different commands, got %d", stats.CollapsedCommands)
	}
}

func TestCollapseCommands_ErrorPreserved(t *testing.T) {
	// Error results should NOT be collapsed — different failure modes matter
	messages := []llm.Message{
		{Role: "user", Content: "build"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "run_command", Content: "panic: nil pointer", IsError: true}},
		{Role: "model", Content: "fixing"},
		{Role: "user", Content: "retry"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "run_command", Content: "all pass"}},
		{Role: "model", Content: "done"},
	}

	_, stats := ReduceHistory(messages, 3)

	if stats.CollapsedCommands != 0 {
		t.Fatalf("expected 0 collapsed for error results, got %d", stats.CollapsedCommands)
	}
}

// --- Trim Tests ---

func TestTrimLargeResults_OldLargeFile(t *testing.T) {
	// Create a 200-line view_file result in the stale zone
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("%d: line %d content", i, i))
	}
	largeContent := strings.Join(lines, "\n")

	messages := []llm.Message{
		{Role: "user", Content: "check"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/big.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: largeContent}},
		// 8 fresh messages
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "m1"},
		{Role: "model", Content: "m2"},
		{Role: "user", Content: "m3"},
		{Role: "model", Content: "m4"},
		{Role: "user", Content: "m5"},
		{Role: "model", Content: "m6"},
		{Role: "user", Content: "m7"},
	}

	result, stats := ReduceHistory(messages, 8)

	if stats.TrimmedResults != 1 {
		t.Fatalf("expected 1 trimmed, got %d", stats.TrimmedResults)
	}
	if stats.TokensSaved <= 0 {
		t.Fatal("expected tokens saved > 0")
	}

	trimmedContent := result[2].ToolResult.Content
	if !strings.Contains(trimmedContent, "trimmed") {
		t.Fatalf("expected trimmed marker, got: %s", trimmedContent[:200])
	}
	// Should contain first line
	if !strings.Contains(trimmedContent, "1: line 1") {
		t.Fatal("should contain first line")
	}
	// Should contain last line
	if !strings.Contains(trimmedContent, "200: line 200") {
		t.Fatal("should contain last line")
	}
	// Should NOT contain middle content
	if strings.Contains(trimmedContent, "100: line 100") {
		t.Fatal("should NOT contain middle lines")
	}
}

func TestTrimLargeResults_FreshLargeFile(t *testing.T) {
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	largeContent := strings.Join(lines, "\n")

	messages := []llm.Message{
		{Role: "user", Content: "check"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/big.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: largeContent}},
		{Role: "model", Content: "ok"},
	}

	_, stats := ReduceHistory(messages, 8) // Everything is within fresh window

	if stats.TrimmedResults != 0 {
		t.Fatalf("expected 0 trimmed for fresh content, got %d", stats.TrimmedResults)
	}
}

func TestTrimLargeResults_SmallFile(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "check"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/path/small.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: "only 5 lines\n1\n2\n3\n4"}},
		// 8 fresh messages
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "m1"},
		{Role: "model", Content: "m2"},
		{Role: "user", Content: "m3"},
		{Role: "model", Content: "m4"},
		{Role: "user", Content: "m5"},
		{Role: "model", Content: "m6"},
		{Role: "user", Content: "m7"},
	}

	_, stats := ReduceHistory(messages, 8)

	if stats.TrimmedResults != 0 {
		t.Fatalf("expected 0 trimmed for small file, got %d", stats.TrimmedResults)
	}
}

// --- Combined Tests ---

func TestReduceHistory_NoOp(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "model", Content: "hi"},
	}

	result, stats := ReduceHistory(messages, 8)

	if stats.TokensSaved != 0 {
		t.Fatal("expected no tokens saved for simple messages")
	}
	if len(result) != len(messages) {
		t.Fatal("messages should not be modified")
	}
}

func TestReduceHistory_EmptyMessages(t *testing.T) {
	result, stats := ReduceHistory(nil, 8)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
	if stats.TokensSaved != 0 {
		t.Fatal("expected no savings")
	}
}

func TestReduceHistory_Combined(t *testing.T) {
	// Set up a scenario with both dedup and command collapse opportunities
	messages := []llm.Message{
		// Old: view_file auth.go
		{Role: "user", Content: "check auth"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/auth.go", "start_line": 1.0, "end_line": 100.0}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: strings.Repeat("old\n", 50)}},
		// Old: run_command go test
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "run_command", Content: "FAIL\nold errors"}},
		{Role: "model", Content: "fixing"},
		// Fresh window starts here
		{Role: "user", Content: "try again"},
		{Role: "model", ToolCalls: []llm.ToolCall{
			{ID: "3", Name: "view_file", Args: map[string]interface{}{"path": "/auth.go", "start_line": 1.0, "end_line": 100.0}},
		}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "3", Name: "view_file", Content: strings.Repeat("new\n", 50)}},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "4", Name: "run_command", Args: map[string]interface{}{"command": "go test ./..."}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "4", Name: "run_command", Content: "ok - all pass"}},
		{Role: "model", Content: "all good"},
		{Role: "user", Content: "great"},
	}

	result, stats := ReduceHistory(messages, 7)

	if stats.DeduplicatedFiles < 1 {
		t.Fatalf("expected >= 1 dedup, got %d", stats.DeduplicatedFiles)
	}
	if stats.CollapsedCommands < 1 {
		t.Fatalf("expected >= 1 collapsed, got %d", stats.CollapsedCommands)
	}
	if stats.TokensSaved <= 0 {
		t.Fatal("expected tokens saved > 0")
	}

	// Verify old results are replaced
	if !strings.Contains(result[2].ToolResult.Content, "Re-read") {
		t.Fatal("old view_file should be deduped")
	}
	if !strings.Contains(result[4].ToolResult.Content, "Command re-run") {
		t.Fatal("old run_command should be collapsed")
	}

	// Verify new results preserved
	if !strings.Contains(result[8].ToolResult.Content, "new") {
		t.Fatal("new view_file should be preserved")
	}
	if result[10].ToolResult.Content != "ok - all pass" {
		t.Fatal("new run_command should be preserved")
	}
}

func TestReduceHistory_DoesNotMutateOriginal(t *testing.T) {
	original := []llm.Message{
		{Role: "user", Content: "check"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "1", Name: "view_file", Args: map[string]interface{}{"path": "/auth.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "1", Name: "view_file", Content: "original content"}},
		{Role: "model", Content: "ok"},
		{Role: "user", Content: "again"},
		{Role: "model", ToolCalls: []llm.ToolCall{{ID: "2", Name: "view_file", Args: map[string]interface{}{"path": "/auth.go"}}}},
		{Role: "tool", ToolResult: &llm.ToolCallResult{CallID: "2", Name: "view_file", Content: "new content"}},
		{Role: "model", Content: "done"},
	}

	// Save the original content
	originalContent := original[2].ToolResult.Content

	ReduceHistory(original, 3)

	// Original should NOT be mutated (we work on a copy)
	if original[2].ToolResult.Content != originalContent {
		t.Fatal("original message should not be mutated")
	}
}

// --- Helper Tests ---

func TestToInt(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected int
	}{
		{float64(42), 42},
		{int(10), 10},
		{int32(5), 5},
		{int64(100), 100},
		{nil, 0},
		{"not a number", 0},
	}

	for _, tt := range tests {
		got := toInt(tt.input)
		if got != tt.expected {
			t.Errorf("toInt(%v) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestTruncateString(t *testing.T) {
	if truncateString("short", 10) != "short" {
		t.Error("short string should not be truncated")
	}
	if truncateString("this is a longer string", 10) != "this is a ..." {
		t.Errorf("got: %s", truncateString("this is a longer string", 10))
	}
}
