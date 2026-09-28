package conversation

import (
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestCleanPromptSummary(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "plain question",
			raw:  "can we include cloudflare argo tunnel so that user can access from remote control?",
			want: "Can we include cloudflare argo tunnel so that user can access...",
		},
		{
			name: "plan boilerplate prompt",
			raw:  "Please create a comprehensive implementation plan for: Add Cloudflare Tunnel.\n\nFirst, research the codebase using read tools...",
			want: "Plan: Add Cloudflare Tunnel",
		},
		{
			name: "plan slash command",
			raw:  "/plan refactor authentication middleware",
			want: "Plan: Refactor authentication middleware",
		},
		{
			name: "teamwork boilerplate prompt",
			raw:  "Please coordinate a team of autonomous specialized subagents to accomplish: Security Audit.\n\nDefine any specialized subagent types needed...",
			want: "Teamwork: Security Audit",
		},
		{
			name: "multiline markdown",
			raw:  "### Goal\nFix failing unit tests in internal/server/session_test.go\nEnsure lint passes.",
			want: "Goal",
		},
		{
			name: "empty prompt",
			raw:  "   ",
			want: "New Session",
		},
		{
			name: "short command",
			raw:  "fix typos",
			want: "Fix typos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanPromptSummary(tt.raw)
			if got != tt.want {
				t.Errorf("CleanPromptSummary(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExtractDescription(t *testing.T) {
	// Empty messages
	if desc := ExtractDescription(nil); desc != "New Session" {
		t.Errorf("expected 'New Session' for nil messages, got %q", desc)
	}

	// Only compact context marker
	msgs := []*pb.ConversationMessage{
		{Role: "user", Content: "[Compact Context]"},
		{Role: "user", Content: "Run tests and verify build"},
	}
	if desc := ExtractDescription(msgs); desc != "Run tests and verify build" {
		t.Errorf("expected 'Run tests and verify build', got %q", desc)
	}

	// Model message fallback if no user message
	modelOnly := []*pb.ConversationMessage{
		{Role: "model", Content: "Hello, how can I assist you with your project today?"},
	}
	if desc := ExtractDescription(modelOnly); desc != "Hello, how can I assist you with your project today?" {
		t.Errorf("expected model fallback, got %q", desc)
	}
}
