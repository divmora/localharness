package engine

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/divmora/localharness/internal/llm"
)

// Tracer logs LLM requests and responses to disk for debugging and replay.
// Files are written to: brain/<convID>/.system_generated/traces/
type Tracer struct {
	traceDir string
	logger   *slog.Logger
	enabled  bool
}

// NewTracer creates a new tracer that writes to the given brain directory.
// If brainDir is empty, tracing is disabled.
func NewTracer(brainDir string, logger *slog.Logger) *Tracer {
	if brainDir == "" {
		return &Tracer{enabled: false, logger: logger}
	}

	traceDir := filepath.Join(brainDir, ".system_generated", "traces")
	if err := os.MkdirAll(traceDir, 0755); err != nil {
		logger.Error("failed to create trace dir", "path", traceDir, "error", err)
		return &Tracer{enabled: false, logger: logger}
	}

	return &Tracer{
		traceDir: traceDir,
		logger:   logger,
		enabled:  true,
	}
}

// TraceRequest logs an LLM request to disk.
func (t *Tracer) TraceRequest(stepIndex int, modelName string, req *llm.GenerateRequest) {
	if !t.enabled {
		return
	}

	trace := map[string]interface{}{
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
		"model_name": modelName,
		"messages":   len(req.Messages),
		"tools":      len(req.Tools),
		"system_prompt_len": len(req.SystemPrompt),
	}

	// Include message summaries (not full content to keep traces small)
	var messageSummaries []map[string]interface{}
	for _, msg := range req.Messages {
		summary := map[string]interface{}{
			"role": msg.Role,
		}
		if len(msg.Parts) > 0 {
			summary["parts_count"] = len(msg.Parts)
			total := 0
			for _, p := range msg.Parts {
				total += len(p)
			}
			summary["total_content_len"] = total
		} else {
			summary["content_len"] = len(msg.Content)
		}
		if len(msg.ToolCalls) > 0 {
			var names []string
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Name)
			}
			summary["tool_calls"] = names
		}
		if msg.ToolResult != nil {
			summary["tool_result"] = msg.ToolResult.Name
			summary["tool_result_len"] = len(msg.ToolResult.Content)
			summary["tool_result_is_error"] = msg.ToolResult.IsError
		}
		messageSummaries = append(messageSummaries, summary)
	}
	trace["message_summaries"] = messageSummaries

	// Tool declarations
	if len(req.Tools) > 0 {
		var toolNames []string
		for _, tool := range req.Tools {
			toolNames = append(toolNames, tool.Name)
		}
		trace["tool_names"] = toolNames
	}

	t.writeTrace(stepIndex, "request", trace)
}

// TraceResponse logs an LLM response to disk.
func (t *Tracer) TraceResponse(stepIndex int, resp *llm.GenerateResponse, latency time.Duration, err error) {
	if !t.enabled {
		return
	}

	trace := map[string]interface{}{
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
		"api_call_index": stepIndex,
		"latency_ms":     latency.Milliseconds(),
	}

	if err != nil {
		trace["error"] = err.Error()
	} else {
		trace["finish_reason"] = resp.FinishReason
		trace["content_len"] = len(resp.Content)
		trace["thinking_len"] = len(resp.Thinking)
		trace["tool_calls"] = len(resp.ToolCalls)
		trace["usage"] = map[string]int{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"thinking_tokens":   resp.Usage.ThinkingTokens,
			"total_tokens":      resp.Usage.TotalTokens,
			"cached_tokens":     resp.Usage.CachedTokens,
		}

		if len(resp.ToolCalls) > 0 {
			var tcSummaries []map[string]interface{}
			for _, tc := range resp.ToolCalls {
				summary := map[string]interface{}{
					"id":   tc.ID,
					"name": tc.Name,
				}
				// Include key args for debugging (truncate large values)
				if len(tc.Args) > 0 {
					argsSummary := make(map[string]interface{})
					for k, v := range tc.Args {
						switch val := v.(type) {
						case string:
							if len(val) > 200 {
								argsSummary[k] = val[:200] + "…"
							} else {
								argsSummary[k] = val
							}
						default:
							argsSummary[k] = val
						}
					}
					summary["args"] = argsSummary
				}
				tcSummaries = append(tcSummaries, summary)
			}
			trace["tool_call_details"] = tcSummaries
		}
	}

	t.writeTrace(stepIndex, "response", trace)
}

// writeTrace writes a trace event to a JSON file.
func (t *Tracer) writeTrace(stepIndex int, kind string, data map[string]interface{}) {
	filename := fmt.Sprintf("step_%04d_%s.json", stepIndex, kind)
	path := filepath.Join(t.traceDir, filename)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.logger.Error("failed to marshal trace", "error", err)
		return
	}

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		t.logger.Error("failed to write trace", "path", path, "error", err)
		return
	}

	t.logger.Debug("trace written", "path", path, "kind", kind, "step", stepIndex)
}
