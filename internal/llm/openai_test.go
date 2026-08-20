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
