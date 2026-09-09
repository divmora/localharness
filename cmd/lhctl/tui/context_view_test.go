package tui

import (
	"strings"
	"testing"
)

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{50, "50"},
		{999, "999"},
		{1000, "1.0k"},
		{29900, "29.9k"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
	}

	for _, tt := range tests {
		got := FormatTokenCount(tt.input)
		if got != tt.expected {
			t.Errorf("FormatTokenCount(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDefaultMaxTokens(t *testing.T) {
	if got := DefaultMaxTokens("gemini-2.5-pro"); got != 1048576 {
		t.Errorf("expected 1048576 for gemini, got %d", got)
	}
	if got := DefaultMaxTokens("claude-3-5-sonnet"); got != 200000 {
		t.Errorf("expected 200000 for claude, got %d", got)
	}
	if got := DefaultMaxTokens("gpt-4o"); got != 128000 {
		t.Errorf("expected 128000 for gpt-4o, got %d", got)
	}
}

func TestRenderContextView(t *testing.T) {
	info := ContextInfo{
		ModelName:        "gemini-2.5-pro",
		PromptTokens:     15000,
		CompletionTokens: 5000,
		TotalTokens:      20000,
		Workspaces:       []string{"/Users/dev/project"},
		SessionID:        "test-session-123",
	}

	rendered := RenderContextView(info, 90)

	if !strings.Contains(rendered, "Context Usage") {
		t.Errorf("expected header in context view: %s", rendered)
	}
	if !strings.Contains(rendered, "◉") || !strings.Contains(rendered, "□") {
		t.Errorf("expected 2D block grid with dots: %s", rendered)
	}
	if !strings.Contains(rendered, "gemini-2.5-pro") {
		t.Errorf("expected model name in context view: %s", rendered)
	}
	if !strings.Contains(rendered, "/Users/dev/project") {
		t.Errorf("expected workspace in context view: %s", rendered)
	}
}
