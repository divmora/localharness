// Package llm defines the LLM provider interface and implementations.
package llm

import (
	"context"
	"strings"
)

// Message represents a single message in the conversation.
type Message struct {
	Role       string         `json:"role"` // "user", "model", "system", "tool"
	Content    string         `json:"content,omitempty"`
	Parts      []string       `json:"parts,omitempty"`       // Multi-part content (user messages with per-section parts)
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolResult *ToolCallResult `json:"tool_result,omitempty"`
}

// TextContent returns the full text of the message.
// For multi-part messages, parts are joined with newlines.
// For single-part messages, returns Content directly.
func (m Message) TextContent() string {
	if len(m.Parts) > 0 {
		return strings.Join(m.Parts, "\n")
	}
	return m.Content
}


// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Args             map[string]interface{} `json:"args"`
	ThoughtSignature string                 `json:"thought_signature,omitempty"` // Gemini 3.5+ thought chain integrity
}

// ToolCallResult is the result of executing a tool.
type ToolCallResult struct {
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Content          string `json:"content"`
	IsError          bool   `json:"is_error"`
	ThoughtSignature string `json:"thought_signature,omitempty"` // Echo back from ToolCall
}

// FunctionDeclaration describes a tool for the LLM.
type FunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// GenerateRequest contains all inputs for a generation call.
type GenerateRequest struct {
	Messages     []Message             `json:"messages"`
	Tools        []FunctionDeclaration `json:"tools,omitempty"`
	SystemPrompt string                `json:"system_prompt,omitempty"`
}

// GenerateResponse contains the LLM's response.
type GenerateResponse struct {
	// Text content of the response
	Content string `json:"content,omitempty"`

	// Thinking/reasoning (for thinking models)
	Thinking string `json:"thinking,omitempty"`

	// Tool calls requested by the model
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Token usage
	Usage Usage `json:"usage"`

	// FinishReason: "stop", "tool_calls", "length", "error"
	FinishReason string `json:"finish_reason"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ThinkingTokens   int `json:"thinking_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

// Provider is the interface that LLM backends must implement.
type Provider interface {
	// Generate sends a request to the LLM and returns the response.
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)

	// ModelName returns the configured model name.
	ModelName() string

	// Close releases any resources held by the provider.
	Close() error
}

// StreamChunk is a partial response from a streaming LLM call.
type StreamChunk struct {
	// Incremental text content
	TextDelta string `json:"text_delta,omitempty"`

	// Incremental thinking/reasoning content
	ThinkingDelta string `json:"thinking_delta,omitempty"`

	// Tool calls (typically only in the final chunk)
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Token usage (typically only in the final chunk)
	Usage Usage `json:"usage"`

	// FinishReason is only set in the final chunk ("stop", "tool_calls", etc.)
	FinishReason string `json:"finish_reason,omitempty"`

	// Done is true when the stream is complete
	Done bool `json:"done"`
}

// StreamingProvider extends Provider with streaming support.
// Providers that support streaming implement this interface;
// the engine checks at runtime and falls back to Generate if unavailable.
type StreamingProvider interface {
	Provider

	// GenerateStream sends a request and returns a channel of incremental chunks.
	// The channel is closed when the stream is complete (after a chunk with Done=true).
	// The error channel receives at most one error and is then closed.
	GenerateStream(ctx context.Context, req *GenerateRequest) (<-chan StreamChunk, <-chan error)
}

