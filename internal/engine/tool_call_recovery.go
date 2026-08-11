package engine

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/divmora/localharness/internal/llm"
)

// tryExtractToolCallsFromText scans model response text for JSON objects that
// look like tool calls. Some models without native function calling support
// output tool calls as plain text instead of using the structured tool_calls
// API field. This function detects those patterns and extracts them as proper
// ToolCall objects so the agentic loop can continue.
//
// Two common patterns are supported:
//  1. OpenAI-style: {"type": "function", "name": "...", "parameters": {...}}
//  2. Simplified:   {"name": "...", "args": {...}}  or  {"name": "...", "parameters": {...}}
//
// Only tool calls with names matching knownTools are extracted. Unknown names
// are left as text to avoid executing hallucinated tools.
//
// Returns the extracted tool calls and any remaining non-tool text content.
func tryExtractToolCallsFromText(content string, knownTools map[string]bool, logger *slog.Logger) ([]llm.ToolCall, string) {
	if content == "" || len(knownTools) == 0 {
		return nil, content
	}

	// Quick check: does the content even look like it could contain a tool call?
	// Check for "name" key or any known tool name mentioned in the text.
	hasToolHint := strings.Contains(content, `"name"`)
	if !hasToolHint {
		for toolName := range knownTools {
			if strings.Contains(content, toolName) {
				hasToolHint = true
				break
			}
		}
	}
	if !hasToolHint {
		return nil, content
	}

	var extracted []llm.ToolCall
	remaining := content

	// Try to find JSON objects in the content. We look for top-level { ... }
	// blocks and attempt to parse each one.
	jsonObjects := findJSONObjects(content)
	for _, obj := range jsonObjects {
		tc, ok := parseToolCallJSON(obj.text, knownTools)
		if !ok {
			continue
		}

		tc.ID = fmt.Sprintf("recovered_call_%d", len(extracted))
		extracted = append(extracted, tc)

		// Remove the extracted JSON from the remaining text
		remaining = strings.Replace(remaining, obj.text, "", 1)
	}

	// Clean up remaining text (trim whitespace, collapse blank lines)
	remaining = strings.TrimSpace(remaining)

	if len(extracted) > 0 {
		logger.Warn("recovered tool calls from model text content",
			"count", len(extracted),
			"tools", toolCallNames(extracted),
			"hint", "model may not support native function calling — consider using a model with tool calling support",
		)
	}

	return extracted, remaining
}

// jsonObject represents a JSON object found in text.
type jsonObject struct {
	text string // The raw JSON string
}

// findJSONObjects finds balanced top-level JSON objects in text.
// It handles nested braces correctly.
func findJSONObjects(text string) []jsonObject {
	var objects []jsonObject
	i := 0
	for i < len(text) {
		// Find the next opening brace
		start := strings.IndexByte(text[i:], '{')
		if start == -1 {
			break
		}
		start += i

		// Find the matching closing brace
		depth := 0
		inString := false
		escaped := false
		end := -1

		for j := start; j < len(text); j++ {
			if escaped {
				escaped = false
				continue
			}
			ch := text[j]
			switch {
			case ch == '\\' && inString:
				escaped = true
			case ch == '"' && !escaped:
				inString = !inString
			case ch == '{' && !inString:
				depth++
			case ch == '}' && !inString:
				depth--
				if depth == 0 {
					end = j + 1
					break
				}
			}
			if end != -1 {
				break
			}
		}

		if end == -1 {
			// No matching closing brace found
			break
		}

		candidate := text[start:end]
		// Quick validation: must be valid JSON
		if json.Valid([]byte(candidate)) {
			objects = append(objects, jsonObject{text: candidate})
		}

		i = end
	}

	return objects
}

// parseToolCallJSON attempts to parse a JSON string as a tool call.
// Returns the ToolCall and true if successful, or zero value and false otherwise.
func parseToolCallJSON(jsonStr string, knownTools map[string]bool) (llm.ToolCall, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return llm.ToolCall{}, false
	}

	// Extract tool name — try standard "name" key first
	name, _ := raw["name"].(string)

	// Fallback: some models produce {"name: tool_name": ..., "parameters": {...}}
	// where the name and value are merged into a single key.
	if name == "" {
		for key := range raw {
			for toolName := range knownTools {
				// Match patterns: "name: tool_name", "name:tool_name", "name tool_name"
				if strings.Contains(key, toolName) {
					name = toolName
					break
				}
			}
			if name != "" {
				break
			}
		}
	}

	if name == "" {
		return llm.ToolCall{}, false
	}

	// Validate against known tools
	if !knownTools[name] {
		return llm.ToolCall{}, false
	}

	// Extract arguments — try "parameters" first (OpenAI-style), then "args"
	var args map[string]interface{}
	if params, ok := raw["parameters"]; ok {
		switch v := params.(type) {
		case map[string]interface{}:
			args = v
		case string:
			// Some models output parameters as a JSON string
			if err := json.Unmarshal([]byte(v), &args); err != nil {
				args = map[string]interface{}{"raw": v}
			}
		}
	} else if argsVal, ok := raw["args"]; ok {
		if v, ok := argsVal.(map[string]interface{}); ok {
			args = v
		}
	}

	if args == nil {
		args = make(map[string]interface{})
	}

	return llm.ToolCall{
		Name: name,
		Args: args,
	}, true
}

// toolCallNames returns a slice of tool names from tool calls.
func toolCallNames(tcs []llm.ToolCall) []string {
	names := make([]string, len(tcs))
	for i, tc := range tcs {
		names[i] = tc.Name
	}
	return names
}
