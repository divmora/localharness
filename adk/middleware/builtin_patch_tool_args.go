package middleware

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// PatchToolArgs is a StepMiddleware that detects and logs common JSON
// formatting issues in tool call arguments. Since tool args are processed
// server-side in the harness binary, this middleware operates as an
// observability layer — it logs when it detects malformed patterns so users
// can track LLM reliability.
//
// Detected patterns:
//   - Trailing commas before closing braces/brackets
//   - Unescaped newlines in JSON string values
//   - Truncated/incomplete JSON
//
// Inspired by CloudWeGo Eino's patchtoolcalls middleware.
type PatchToolArgs struct {
	logger *slog.Logger

	// counts tracks how many issues have been detected per type.
	counts map[string]int
}

// NewPatchToolArgs creates a new PatchToolArgs middleware.
// If logger is nil, the default slog logger is used.
func NewPatchToolArgs(logger *slog.Logger) *PatchToolArgs {
	if logger == nil {
		logger = slog.Default()
	}
	return &PatchToolArgs{
		logger: logger,
		counts: make(map[string]int),
	}
}

func (p *PatchToolArgs) Name() string { return "patch_tool_args" }

// ProcessStep inspects tool call events for malformed JSON args.
// It only looks at tool-start events (those with a ToolName and "active" state).
func (p *PatchToolArgs) ProcessStep(ctx context.Context, event *StepEvent) (*StepEvent, error) {
	// Only inspect tool call start events
	if event.ToolName == "" || event.ToolState != "active" {
		return event, nil
	}

	// Get the metadata field that carries raw args JSON (set by agent.go)
	argsJSON, ok := event.Metadata["tool_args_json"].(string)
	if !ok || argsJSON == "" {
		return event, nil
	}

	issues := detectJSONIssues(argsJSON)
	if len(issues) > 0 {
		for _, issue := range issues {
			p.counts[issue]++
			p.logger.Warn("tool args JSON issue detected",
				"tool", event.ToolName,
				"issue", issue,
				"count", p.counts[issue],
			)
		}
		// Store issue info in metadata for downstream consumers
		event.Metadata["tool_args_issues"] = issues
	}

	return event, nil
}

// Counts returns the cumulative count of detected issues by type.
func (p *PatchToolArgs) Counts() map[string]int {
	result := make(map[string]int, len(p.counts))
	for k, v := range p.counts {
		result[k] = v
	}
	return result
}

// trailingCommaRe matches trailing commas before closing braces/brackets.
var trailingCommaRe = regexp.MustCompile(`,\s*[}\]]`)

// detectJSONIssues scans a JSON string for common formatting problems.
func detectJSONIssues(s string) []string {
	var issues []string

	// Check for trailing commas: {"key": "value",}
	if trailingCommaRe.MatchString(s) {
		issues = append(issues, "trailing_comma")
	}

	// Check for unescaped newlines inside strings
	// A simple heuristic: if the string has newlines that aren't preceded by \
	if strings.Contains(s, "\n") {
		// Count quotes to see if newlines appear inside string values
		inString := false
		escaped := false
		for i, ch := range s {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = !inString
			}
			if ch == '\n' && inString {
				_ = i // suppress unused warning
				issues = append(issues, "unescaped_newline_in_string")
				break
			}
		}
	}

	// Check for truncated JSON (unbalanced braces/brackets)
	braces := 0
	brackets := 0
	inStr := false
	esc := false
	for _, ch := range s {
		if esc {
			esc = false
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			braces++
		case '}':
			braces--
		case '[':
			brackets++
		case ']':
			brackets--
		}
	}
	if braces != 0 || brackets != 0 {
		issues = append(issues, "unbalanced_braces")
	}

	return issues
}
