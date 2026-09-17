package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/divmora/localharness/internal/llm"
)

func TestDefaultModelTierResolver(t *testing.T) {
	tests := []struct {
		name          string
		parentModel   string
		requestedTier string
		want          string
	}{
		// Inherit / Empty
		{name: "empty requested tier", parentModel: "gpt-4o", requestedTier: "", want: "gpt-4o"},
		{name: "inherit tier", parentModel: "gpt-4o", requestedTier: "inherit", want: "gpt-4o"},
		{name: "case-insensitive inherit", parentModel: "claude-3-5-sonnet", requestedTier: "INHERIT", want: "claude-3-5-sonnet"},

		// Explicit model override
		{name: "explicit gpt-4o-mini", parentModel: "claude-3-5-sonnet", requestedTier: "gpt-4o-mini", want: "gpt-4o-mini"},
		{name: "explicit custom local", parentModel: "gpt-4o", requestedTier: "llama3.1:8b", want: "llama3.1:8b"},
		{name: "explicit gemini-2.0-flash", parentModel: "gpt-4o", requestedTier: "gemini-2.0-flash", want: "gemini-2.0-flash"},

		// Google Gemini family
		{name: "gemini flash_lite", parentModel: "gemini-2.5-pro", requestedTier: "flash_lite", want: "gemini-2.0-flash-lite"},
		{name: "gemini flash", parentModel: "gemini-2.5-pro", requestedTier: "flash", want: "gemini-2.5-flash"},
		{name: "gemini pro", parentModel: "gemini-2.5-flash", requestedTier: "pro", want: "gemini-2.5-pro"},

		// Anthropic Claude family
		{name: "claude flash_lite", parentModel: "claude-3-5-sonnet", requestedTier: "flash_lite", want: "claude-3-5-haiku"},
		{name: "claude flash", parentModel: "claude-3-5-sonnet-20241022", requestedTier: "flash", want: "claude-3-5-haiku"},
		{name: "claude pro", parentModel: "claude-3-5-haiku", requestedTier: "pro", want: "claude-3-5-sonnet"},

		// OpenAI family
		{name: "openai flash_lite", parentModel: "gpt-4o", requestedTier: "flash_lite", want: "gpt-4o-mini"},
		{name: "openai flash", parentModel: "gpt-4o", requestedTier: "flash", want: "gpt-4o-mini"},
		{name: "openai pro", parentModel: "gpt-4o-mini", requestedTier: "pro", want: "gpt-4o"},
		{name: "openai o1 flash", parentModel: "o1-preview", requestedTier: "flash", want: "gpt-4o-mini"},

		// DeepSeek family
		{name: "deepseek flash_lite", parentModel: "deepseek-reasoner", requestedTier: "flash_lite", want: "deepseek-chat"},
		{name: "deepseek flash", parentModel: "deepseek-reasoner", requestedTier: "flash", want: "deepseek-chat"},
		{name: "deepseek pro", parentModel: "deepseek-chat", requestedTier: "pro", want: "deepseek-reasoner"},

		// Empty parent fallback
		{name: "empty parent flash", parentModel: "", requestedTier: "flash", want: "gpt-4o-mini"},
		{name: "empty parent pro", parentModel: "", requestedTier: "pro", want: "gpt-4o"},

		// Unknown / local model fallback (preserves parentModel so custom names aren't broken)
		{name: "unknown model flash", parentModel: "mistral-7b-instruct", requestedTier: "flash", want: "mistral-7b-instruct"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultModelTierResolver(tt.parentModel, tt.requestedTier)
			if got != tt.want {
				t.Errorf("DefaultModelTierResolver(%q, %q) = %q, want %q",
					tt.parentModel, tt.requestedTier, got, tt.want)
			}
		})
	}
}

// nonClonerProvider is a dummy provider that does NOT implement llm.ModelCloner.
type nonClonerProvider struct {
	model string
}

func (p *nonClonerProvider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	return &llm.GenerateResponse{Content: "ok"}, nil
}
func (p *nonClonerProvider) ModelName() string { return p.model }
func (p *nonClonerProvider) Close() error      { return nil }

func TestResolveSubagentProvider(t *testing.T) {
	t.Run("nil parent returns nil", func(t *testing.T) {
		res := ResolveSubagentProvider(nil, "flash", nil)
		if res != nil {
			t.Errorf("expected nil provider, got %v", res)
		}
	})

	t.Run("cloner clones to tier model", func(t *testing.T) {
		parent := &mockProvider{modelName: "gpt-4o"}
		child := ResolveSubagentProvider(parent, "flash", nil)
		if child == nil {
			t.Fatal("child provider should not be nil")
		}
		if child.ModelName() != "gpt-4o-mini" {
			t.Errorf("expected 'gpt-4o-mini', got %q", child.ModelName())
		}
	})

	t.Run("cloner with explicit model name", func(t *testing.T) {
		parent := &mockProvider{modelName: "gpt-4o"}
		child := ResolveSubagentProvider(parent, "claude-3-5-haiku", nil)
		if child == nil {
			t.Fatal("child provider should not be nil")
		}
		if child.ModelName() != "claude-3-5-haiku" {
			t.Errorf("expected 'claude-3-5-haiku', got %q", child.ModelName())
		}
	})

	t.Run("inherit returns parent directly", func(t *testing.T) {
		parent := &mockProvider{modelName: "gpt-4o"}
		child := ResolveSubagentProvider(parent, "inherit", nil)
		if child != parent {
			t.Errorf("expected identical parent pointer for inherit, got %v", child)
		}
	})

	t.Run("non-cloner safely falls back to parent", func(t *testing.T) {
		parent := &nonClonerProvider{model: "custom-raw-model"}
		child := ResolveSubagentProvider(parent, "flash", nil)
		if child != parent {
			t.Errorf("expected fallback to parent for non-cloner, got %v", child)
		}
	})
}

func TestExtractHandoffSummary(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		if got := extractHandoffSummary("", 200); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("strips markdown headers and bullets", func(t *testing.T) {
		text := `### Handoff Briefing
- **Task & Goal**: Refactor sqlite database index.
- **Status**: COMPLETED
- **Files Created & Modified**:
  - internal/codegraph/sqlite.go: added WAL mode.
- **Verification**: go test ./... passed.`

		got := extractHandoffSummary(text, 250)
		if strings.Contains(got, "###") {
			t.Errorf("summary should not contain markdown headers: %q", got)
		}
		if !strings.Contains(got, "Refactor sqlite database index.") {
			t.Errorf("summary should contain task goal: %q", got)
		}
		if !strings.Contains(got, "COMPLETED") {
			t.Errorf("summary should contain status: %q", got)
		}
	})

	t.Run("truncates at word boundary", func(t *testing.T) {
		longText := strings.Repeat("word ", 100)
		got := extractHandoffSummary(longText, 50)
		if len(got) > 55 {
			t.Errorf("summary exceeded maximum length: len=%d, text=%q", len(got), got)
		}
		if !strings.HasSuffix(got, "...") {
			t.Errorf("truncated summary should have ellipsis: %q", got)
		}
	})
}
