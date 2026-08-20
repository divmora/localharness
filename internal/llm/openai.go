package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/divmora/localharness/internal/errors"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultOpenAIModel   = "gpt-4o"
	maxAPIRetries        = 5
)

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
// Works with: OpenAI, Ollama, vLLM, LM Studio, Azure OpenAI, Together AI, etc.
type OpenAIProvider struct {
	apiKey      string
	baseURL     string
	model       string
	temperature *float64
	maxTokens   int
	client      *http.Client
	logger      *slog.Logger
}

// OpenAIConfig holds configuration for the OpenAI-compatible provider.
type OpenAIConfig struct {
	APIKey      string
	BaseURL     string  // e.g., "http://localhost:11434/v1" for Ollama
	ModelName   string  // e.g., "gpt-4o", "llama3", "deepseek-coder"
	Temperature float64 // 0 = not set
	MaxTokens   int     // 0 = model default
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(cfg OpenAIConfig, logger *slog.Logger) (*OpenAIProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	model := cfg.ModelName
	if model == "" {
		model = defaultOpenAIModel
	}

	// API key is optional for local providers (Ollama, vLLM, etc.)
	if cfg.APIKey == "" && strings.Contains(baseURL, "openai.com") {
		return nil, errors.New(errors.ErrCodeInvalidAPIKey,
			"API key required for OpenAI API").
			WithContext("provider", "openai").
			WithContext("base_url", baseURL).
			WithComponent("llm_provider")
	}

	p := &OpenAIProvider{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
		logger:  logger,
	}

	if cfg.Temperature > 0 {
		t := cfg.Temperature
		p.temperature = &t
	}
	if cfg.MaxTokens > 0 {
		p.maxTokens = cfg.MaxTokens
	}

	return p, nil
}

func (o *OpenAIProvider) ModelName() string { return o.model }
func (o *OpenAIProvider) Close() error      { return nil }

// parseRetryAfter parses the Retry-After header which can be seconds or an HTTP-date.
func parseRetryAfter(header string) (time.Duration, bool) {
	if header == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

func computeBackoff(attempt int, retryAfterHeader string) time.Duration {
	if d, ok := parseRetryAfter(retryAfterHeader); ok && d > 0 {
		return d
	}
	base := 1 * time.Second
	maxDelay := 30 * time.Second
	backoff := float64(base) * math.Pow(2, float64(attempt))
	if backoff > float64(maxDelay) {
		backoff = float64(maxDelay)
	}
	// Full jitter
	jittered := rand.Float64() * backoff
	if jittered < float64(100*time.Millisecond) {
		jittered = float64(100 * time.Millisecond)
	}
	return time.Duration(jittered)
}

func isRetryableStatusCode(code int) bool {
	return code == http.StatusTooManyRequests || // 429
		code == http.StatusInternalServerError || // 500
		code == http.StatusBadGateway ||          // 502
		code == http.StatusServiceUnavailable ||  // 503
		code == http.StatusGatewayTimeout         // 504
}

// Generate sends a chat completion request to the OpenAI-compatible API with exponential backoff retries.
func (o *OpenAIProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	body := o.buildRequestBody(req)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeLLMProvider,
			"failed to marshal request").
			WithContext("model", o.model).
			WithContext("provider", "openai").
			WithComponent("llm_provider")
	}

	url := fmt.Sprintf("%s/chat/completions", o.baseURL)

	for attempt := 0; attempt <= maxAPIRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrCodeNetwork,
				"failed to create HTTP request").
				WithContext("url", url).
				WithContext("model", o.model).
				WithContext("provider", "openai").
				WithComponent("llm_provider")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if o.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
		}

		o.logger.Debug("calling OpenAI API", "model", o.model, "url", url, "messages", len(req.Messages), "attempt", attempt)

		httpResp, err := o.client.Do(httpReq)
		if err != nil {
			if attempt < maxAPIRetries && ctx.Err() == nil {
				backoff := computeBackoff(attempt, "")
				o.logger.Warn("OpenAI API network error, retrying", "error", err, "attempt", attempt, "backoff", backoff)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			return nil, errors.Wrap(err, errors.ErrCodeConnectionFailed,
				"API call failed").
				WithContext("url", url).
				WithContext("model", o.model).
				WithContext("provider", "openai").
				WithComponent("llm_provider")
		}

		respBody, err := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			if attempt < maxAPIRetries && ctx.Err() == nil {
				backoff := computeBackoff(attempt, "")
				o.logger.Warn("OpenAI API response read error, retrying", "error", err, "attempt", attempt, "backoff", backoff)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			return nil, errors.Wrap(err, errors.ErrCodeNetwork,
				"failed to read response").
				WithContext("url", url).
				WithContext("model", o.model).
				WithContext("provider", "openai").
				WithComponent("llm_provider")
		}

		if httpResp.StatusCode != http.StatusOK {
			if isRetryableStatusCode(httpResp.StatusCode) && attempt < maxAPIRetries && ctx.Err() == nil {
				retryAfter := httpResp.Header.Get("Retry-After")
				backoff := computeBackoff(attempt, retryAfter)
				o.logger.Warn("OpenAI API rate limit/server error, retrying",
					"status", httpResp.StatusCode,
					"attempt", attempt,
					"retry_after", retryAfter,
					"backoff", backoff,
				)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			return nil, errors.New(errors.ErrCodeLLMProvider,
				"API error").
				WithContext("status_code", httpResp.StatusCode).
				WithContext("response_body", string(respBody)).
				WithContext("url", url).
				WithContext("model", o.model).
				WithContext("provider", "openai").
				WithComponent("llm_provider")
		}

		return o.parseResponse(respBody)
	}

	return nil, errors.New(errors.ErrCodeLLMProvider, "exceeded max retries for LLM API")
}

// buildRequestBody constructs the OpenAI chat completion request.
func (o *OpenAIProvider) buildRequestBody(req *GenerateRequest) map[string]interface{} {
	body := map[string]interface{}{
		"model": o.model,
	}

	// Build messages array
	var messages []map[string]interface{}

	// System prompt
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}

	// Conversation messages
	for _, msg := range req.Messages {
		converted := o.convertMessage(msg)
		if converted != nil {
			messages = append(messages, converted)
		}
	}

	body["messages"] = messages

	// Tools (function calling)
	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, t := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	// Optional parameters
	if o.temperature != nil {
		body["temperature"] = *o.temperature
	}
	if o.maxTokens > 0 {
		body["max_tokens"] = o.maxTokens
	}

	return body
}

// convertMessage converts an internal Message to OpenAI format.
func (o *OpenAIProvider) convertMessage(msg Message) map[string]interface{} {
	switch msg.Role {
	case "user":
		content := msg.TextContent()
		return map[string]interface{}{
			"role":    "user",
			"content": content,
		}

	case "model":
		m := map[string]interface{}{
			"role": "assistant",
		}
		if msg.Content != "" {
			m["content"] = msg.Content
		}
		// Tool calls
		if len(msg.ToolCalls) > 0 {
			var toolCalls []map[string]interface{}
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Args)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				})
			}
			m["tool_calls"] = toolCalls
		}
		return m

	case "tool":
		if msg.ToolResult != nil {
			return map[string]interface{}{
				"role":         "tool",
				"tool_call_id": msg.ToolResult.CallID,
				"content":      msg.ToolResult.Content,
			}
		}
		return nil

	case "system":
		return nil // Handled above

	default:
		return nil
	}
}

// parseResponse parses the OpenAI chat completion response.
func (o *OpenAIProvider) parseResponse(body []byte) (*GenerateResponse, error) {
	var apiResp openAIChatResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("openai: parse response: %w\nBody: %s", err, string(body))
	}

	if len(apiResp.Choices) == 0 {
		// Check for error
		if apiResp.Error != nil {
			return nil, fmt.Errorf("openai: API error: %s (%s)", apiResp.Error.Message, apiResp.Error.Type)
		}
		return nil, fmt.Errorf("openai: no choices in response")
	}

	choice := apiResp.Choices[0]
	resp := &GenerateResponse{
		Content:      choice.Message.Content,
		FinishReason: o.mapFinishReason(choice.FinishReason),
	}

	// Extract tool calls
	for _, tc := range choice.Message.ToolCalls {
		// Parse the arguments JSON string into a map
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			// If the arguments aren't valid JSON, wrap them
			args = map[string]interface{}{"raw": tc.Function.Arguments}
		}

		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: args,
		})
	}

	if len(resp.ToolCalls) > 0 {
		resp.FinishReason = "tool_calls"
	}

	// Parse usage
	if apiResp.Usage != nil {
		resp.Usage = Usage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		}
		if apiResp.Usage.PromptTokensDetails != nil {
			resp.Usage.CachedTokens = apiResp.Usage.PromptTokensDetails.CachedTokens
		}
	}

	o.logger.Debug("LLM response",
		"finish_reason", resp.FinishReason,
		"tool_calls", len(resp.ToolCalls),
		"content_len", len(resp.Content),
		"tokens", resp.Usage.TotalTokens,
	)

	return resp, nil
}

// mapFinishReason converts OpenAI finish reasons to our internal format.
func (o *OpenAIProvider) mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "stop"
	case "tool_calls":
		return "tool_calls"
	case "length":
		return "length"
	case "content_filter":
		return "error"
	default:
		return reason
	}
}

// GenerateStream sends a streaming chat completion request.
// It returns a channel of StreamChunks and an error channel.
func (o *OpenAIProvider) GenerateStream(ctx context.Context, req *GenerateRequest) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errCh)

		body := o.buildRequestBody(req)
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			errCh <- fmt.Errorf("openai: marshal error: %w", err)
			return
		}

		url := fmt.Sprintf("%s/chat/completions", o.baseURL)

		var httpResp *http.Response
		for attempt := 0; attempt <= maxAPIRetries; attempt++ {
			httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
			if err != nil {
				errCh <- fmt.Errorf("openai: request error: %w", err)
				return
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			if o.apiKey != "" {
				httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
			}

			o.logger.Debug("calling OpenAI streaming API", "model", o.model, "url", url, "messages", len(req.Messages), "attempt", attempt)

			httpResp, err = o.client.Do(httpReq)
			if err != nil {
				if attempt < maxAPIRetries && ctx.Err() == nil {
					backoff := computeBackoff(attempt, "")
					o.logger.Warn("OpenAI streaming network error, retrying", "error", err, "attempt", attempt, "backoff", backoff)
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case <-time.After(backoff):
						continue
					}
				}
				errCh <- fmt.Errorf("openai: streaming API call failed: %w", err)
				return
			}

			if httpResp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(httpResp.Body)
				httpResp.Body.Close()

				if isRetryableStatusCode(httpResp.StatusCode) && attempt < maxAPIRetries && ctx.Err() == nil {
					retryAfter := httpResp.Header.Get("Retry-After")
					backoff := computeBackoff(attempt, retryAfter)
					o.logger.Warn("OpenAI streaming rate limit/server error, retrying",
						"status", httpResp.StatusCode,
						"attempt", attempt,
						"retry_after", retryAfter,
						"backoff", backoff,
					)
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case <-time.After(backoff):
						continue
					}
				}
				errCh <- fmt.Errorf("openai: streaming API error (status %d): %s", httpResp.StatusCode, string(respBody))
				return
			}

			break
		}

		if httpResp == nil {
			errCh <- fmt.Errorf("openai: streaming failed after retries")
			return
		}
		defer httpResp.Body.Close()

		o.parseOpenAISSEStream(ctx, httpResp.Body, chunks, errCh)
	}()

	return chunks, errCh
}

// parseOpenAISSEStream reads SSE events from the OpenAI streaming response.
func (o *OpenAIProvider) parseOpenAISSEStream(ctx context.Context, body io.Reader, chunks chan<- StreamChunk, errCh chan<- error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	// Accumulate tool calls across chunks (OpenAI streams them in pieces)
	type toolCallAccum struct {
		ID       string
		Name     string
		ArgsJSON strings.Builder
	}
	toolCallAccums := make(map[int]*toolCallAccum)

	var lastUsage Usage
	var lastFinishReason string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if data == "" {
			continue
		}

		var streamResp openAIStreamResponse
		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			o.logger.Warn("openai: failed to parse streaming chunk", "error", err)
			continue
		}

		// Check for errors embedded in the SSE stream (e.g., some proxies
		// return HTTP 200 but include an error object in the stream data)
		if streamResp.Error != nil {
			errMsg := streamResp.Error.Message
			if errMsg == "" {
				errMsg = "unknown streaming error"
			}
			errCh <- fmt.Errorf("openai: streaming error: %s", errMsg)
			return
		}

		// Track usage (appears in the final chunks)
		if streamResp.Usage != nil {
			lastUsage = Usage{
				PromptTokens:     streamResp.Usage.PromptTokens,
				CompletionTokens: streamResp.Usage.CompletionTokens,
				TotalTokens:      streamResp.Usage.TotalTokens,
			}
			if streamResp.Usage.PromptTokensDetails != nil {
				lastUsage.CachedTokens = streamResp.Usage.PromptTokensDetails.CachedTokens
			}
		}

		if len(streamResp.Choices) == 0 {
			continue
		}

		choice := streamResp.Choices[0]
		chunk := StreamChunk{}

		// Text delta
		if choice.Delta.Content != "" {
			chunk.TextDelta = choice.Delta.Content
		}

		// Tool call deltas (accumulated across chunks)
		for _, tcDelta := range choice.Delta.ToolCalls {
			acc, ok := toolCallAccums[tcDelta.Index]
			if !ok {
				acc = &toolCallAccum{}
				toolCallAccums[tcDelta.Index] = acc
			}
			if tcDelta.ID != "" {
				acc.ID = tcDelta.ID
			}
			if tcDelta.Function.Name != "" {
				acc.Name = tcDelta.Function.Name
			}
			if tcDelta.Function.Arguments != "" {
				acc.ArgsJSON.WriteString(tcDelta.Function.Arguments)
			}
		}

		// Track finish reason
		if choice.FinishReason != "" {
			lastFinishReason = o.mapFinishReason(choice.FinishReason)
		}

		// Emit text delta chunks immediately
		if chunk.TextDelta != "" {
			chunks <- chunk
		}
	}

	if err := scanner.Err(); err != nil {
		errCh <- fmt.Errorf("openai: SSE stream read error: %w", err)
		return
	}

	// Build final chunk with accumulated tool calls and usage
	finalChunk := StreamChunk{
		Usage:        lastUsage,
		FinishReason: lastFinishReason,
		Done:         true,
	}
	if finalChunk.FinishReason == "" {
		finalChunk.FinishReason = "stop"
	}

	// Convert accumulated tool calls to the final list
	for _, acc := range toolCallAccums {
		var args map[string]interface{}
		argsStr := acc.ArgsJSON.String()
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			args = map[string]interface{}{"raw": argsStr}
		}
		finalChunk.ToolCalls = append(finalChunk.ToolCalls, ToolCall{
			ID:   acc.ID,
			Name: acc.Name,
			Args: args,
		})
	}

	if len(finalChunk.ToolCalls) > 0 {
		finalChunk.FinishReason = "tool_calls"
	}

	chunks <- finalChunk
}

// ─── OpenAI API response types ──────────────────────────────────────────

type openAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Choices []openAIChoice     `json:"choices"`
	Usage   *openAIUsage       `json:"usage,omitempty"`
	Error   *openAIError       `json:"error,omitempty"`
}

type openAIChoice struct {
	Index        int            `json:"index"`
	Message      openAIMessage  `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type openAIMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function openAIFunction   `json:"function"`
}

type openAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type openAIUsage struct {
	PromptTokens        int                    `json:"prompt_tokens"`
	CompletionTokens    int                    `json:"completion_tokens"`
	TotalTokens         int                    `json:"total_tokens"`
	PromptTokensDetails *openAIPromptDetails   `json:"prompt_tokens_details,omitempty"`
}

type openAIPromptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ─── OpenAI Streaming response types ────────────────────────────────────

type openAIStreamResponse struct {
	ID      string                `json:"id"`
	Object  string                `json:"object"`
	Choices []openAIStreamChoice  `json:"choices"`
	Usage   *openAIUsage          `json:"usage,omitempty"`
	Error   *openAIError          `json:"error,omitempty"`
}

type openAIStreamChoice struct {
	Index        int                  `json:"index"`
	Delta        openAIStreamDelta    `json:"delta"`
	FinishReason string               `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Role      string                    `json:"role,omitempty"`
	Content   string                    `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCall    `json:"tool_calls,omitempty"`
}

type openAIStreamToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function openAIFunction   `json:"function"`
}

