package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	// Seconds string
	d, ok := parseRetryAfter("5")
	if !ok || d != 5*time.Second {
		t.Errorf("parseRetryAfter('5') = (%v, %v), want (5s, true)", d, ok)
	}

	// Empty string
	_, ok = parseRetryAfter("")
	if ok {
		t.Error("parseRetryAfter('') should return false")
	}

	// Invalid string
	_, ok = parseRetryAfter("invalid")
	if ok {
		t.Error("parseRetryAfter('invalid') should return false")
	}
}

func TestIsRetryableStatusCode(t *testing.T) {
	retryable := []int{429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !isRetryableStatusCode(code) {
			t.Errorf("expected status %d to be retryable", code)
		}
	}

	nonRetryable := []int{200, 400, 401, 403, 404}
	for _, code := range nonRetryable {
		if isRetryableStatusCode(code) {
			t.Errorf("expected status %d NOT to be retryable", code)
		}
	}
}

func TestOpenAIRetryOn429(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := attempts.Add(1)
		if att < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error": {"message": "Rate limit exceeded"}}`)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello after retry!"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`)
	}))
	defer server.Close()

	logger := slog.Default()
	p, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL:   server.URL,
		ModelName: "test-model",
	}, logger)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}

	resp, err := p.Generate(context.Background(), &GenerateRequest{
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
	})

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.Content != "Hello after retry!" {
		t.Errorf("unexpected content: %q", resp.Content)
	}

	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts.Load())
	}
}

func TestOpenAIWithModel(t *testing.T) {
	logger := slog.Default()
	p, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL:   "http://localhost:11434/v1",
		ModelName: "gpt-4o",
	}, logger)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}

	cloner, ok := interface{}(p).(ModelCloner)
	if !ok {
		t.Fatalf("OpenAIProvider must implement ModelCloner")
	}

	cloned := cloner.WithModel("gpt-4o-mini")
	if cloned == nil {
		t.Fatal("cloned provider should not be nil")
	}

	if cloned.ModelName() != "gpt-4o-mini" {
		t.Errorf("expected cloned ModelName 'gpt-4o-mini', got %q", cloned.ModelName())
	}

	// Original provider should remain unchanged
	if p.ModelName() != "gpt-4o" {
		t.Errorf("original provider ModelName changed to %q", p.ModelName())
	}
}

func TestOpenAIStreaming_DeterministicToolCallOrder(t *testing.T) {
	ssePayload := `data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"write_to_file","arguments":"{\"path\":\"a.txt\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_1","type":"function","function":{"name":"run_command","arguments":"{\"cmd\":\"cat a.txt\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":2,"id":"call_2","type":"function","function":{"name":"view_file","arguments":"{\"path\":\"b.txt\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":3,"id":"call_3","type":"function","function":{"name":"ask_question","arguments":"{\"question\":\"done?\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, ssePayload)
	}))
	defer server.Close()

	logger := slog.Default()
	p, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL:   server.URL,
		ModelName: "test-model",
	}, logger)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}

	expectedOrder := []string{"write_to_file", "run_command", "view_file", "ask_question"}

	// Run multiple iterations to verify Go map iteration randomization never scrambles the order
	for iter := 0; iter < 50; iter++ {
		chunksCh, errCh := p.GenerateStream(context.Background(), &GenerateRequest{
			Messages: []Message{{Role: "user", Content: "do work"}},
		})

		var lastChunk StreamChunk
		for chunk := range chunksCh {
			if chunk.Done {
				lastChunk = chunk
			}
		}

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("iter %d: unexpected stream error: %v", iter, err)
			}
		default:
		}

		if len(lastChunk.ToolCalls) != len(expectedOrder) {
			t.Fatalf("iter %d: expected %d tool calls, got %d", iter, len(expectedOrder), len(lastChunk.ToolCalls))
		}

		for i, tc := range lastChunk.ToolCalls {
			if tc.Name != expectedOrder[i] {
				t.Fatalf("iter %d: tool call at index %d was %q, want %q (order was randomized!)",
					iter, i, tc.Name, expectedOrder[i])
			}
		}
	}
}

func TestOpenAICustomHeadersAndTimeout(t *testing.T) {
	var receivedCustomHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCustomHeader = r.Header.Get("X-Proxy-Header")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-test",
			"object": "chat.completion",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}]
		}`)
	}))
	defer server.Close()

	p, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL:   server.URL,
		ModelName: "test-model",
		Headers:   map[string]string{"X-Proxy-Header": "litellm-val"},
		Timeout:   10 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if p.Headers()["X-Proxy-Header"] != "litellm-val" {
		t.Errorf("expected header stored, got %v", p.Headers())
	}

	_, err = p.Generate(context.Background(), &GenerateRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if receivedCustomHeader != "litellm-val" {
		t.Errorf("server received header %q, want %q", receivedCustomHeader, "litellm-val")
	}
}
