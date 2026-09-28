package conversation

import (
	"strings"
	"unicode"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// ExtractDescription extracts or synthesizes a concise, human-readable summary/description
// from conversation messages, focusing on the initial user question/prompt and assistant response.
func ExtractDescription(messages []*pb.ConversationMessage) string {
	if len(messages) == 0 {
		return "New Session"
	}

	// 1. Locate the first meaningful user message
	var firstUserText string
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		text := m.Content
		if text == "" && len(m.Parts) > 0 {
			text = strings.Join(m.Parts, " ")
		}
		text = strings.TrimSpace(text)
		// Skip synthetic / system compaction markers
		if text == "" || text == "[Compact Context]" || strings.HasPrefix(text, "[System]") {
			continue
		}
		firstUserText = text
		break
	}

	// If a user message was found, clean and format it
	if firstUserText != "" {
		return CleanPromptSummary(firstUserText)
	}

	// 2. Fallback: check first model message if any
	for _, m := range messages {
		if m.Role == "model" {
			text := strings.TrimSpace(m.Content)
			if text != "" {
				return CleanPromptSummary(text)
			}
		}
	}

	return "New Session"
}

// CleanPromptSummary cleans up raw prompt text into a clean single-line description.
func CleanPromptSummary(raw string) string {
	text := strings.TrimSpace(raw)

	// Extract content from <USER_REQUEST> if present (e.g. Antigravity/agent envelopes)
	if start := strings.Index(text, "<USER_REQUEST>"); start != -1 {
		sub := text[start+len("<USER_REQUEST>"):]
		if end := strings.Index(sub, "</USER_REQUEST>"); end != -1 {
			text = strings.TrimSpace(sub[:end])
		} else {
			text = strings.TrimSpace(sub)
		}
	} else if start := strings.Index(text, "# User Requests"); start != -1 {
		// Fallback for CONTEXT_SUMMARY: look for numbered list under "# User Requests"
		sub := text[start+len("# User Requests"):]
		lines := strings.Split(sub, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "# User") {
				break
			}
			if idx := strings.Index(line, ". "); idx != -1 && idx <= 3 {
				req := strings.TrimSpace(line[idx+2:])
				if req != "" {
					text = req
					break
				}
			}
		}
	} else if strings.HasPrefix(text, "<") {
		// Strip leading XML tag if the entire prompt starts with a tag (e.g. <goal>...</goal>)
		if end := strings.Index(text, ">"); end != -1 && end < 30 {
			text = strings.TrimSpace(text[end+1:])
			if lastOpen := strings.LastIndex(text, "</"); lastOpen != -1 {
				text = strings.TrimSpace(text[:lastOpen])
			}
		}
	}

	// Remove markdown headers
	for strings.HasPrefix(text, "#") {
		text = strings.TrimLeft(text, "#")
		text = strings.TrimSpace(text)
	}

	// Strip common slash-command boilerplate
	prefixBoilerplates := []struct {
		prefix string
		label  string
	}{
		{"Please create a comprehensive implementation plan for: ", "Plan: "},
		{"Please analyze the current task and coordinate a team of autonomous specialized subagents to work on it", "Teamwork Coordination"},
		{"Please coordinate a team of autonomous specialized subagents to accomplish: ", "Teamwork: "},
		{"/plan ", "Plan: "},
		{"/teamwork ", "Teamwork: "},
		{"/btw ", "Q: "},
	}

	for _, bp := range prefixBoilerplates {
		if strings.HasPrefix(text, bp.prefix) {
			remainder := strings.TrimSpace(strings.TrimPrefix(text, bp.prefix))
			// If remainder has additional boilerplate instructions, take the core goal
			if idx := strings.Index(remainder, "\n\nFirst, research"); idx != -1 {
				remainder = strings.TrimSpace(remainder[:idx])
			}
			if idx := strings.Index(remainder, ".\n\nFirst,"); idx != -1 {
				remainder = strings.TrimSpace(remainder[:idx])
			}
			if idx := strings.Index(remainder, "\n\nDefine any specialized"); idx != -1 {
				remainder = strings.TrimSpace(remainder[:idx])
			}
			if idx := strings.Index(remainder, ".\n\nDefine"); idx != -1 {
				remainder = strings.TrimSpace(remainder[:idx])
			}
			remainder = strings.TrimSuffix(remainder, ".")
			if remainder != "" {
				remRunes := []rune(remainder)
				if len(remRunes) > 0 && unicode.IsLower(remRunes[0]) {
					remRunes[0] = unicode.ToUpper(remRunes[0])
					remainder = string(remRunes)
				}
				text = bp.label + remainder
			} else {
				text = strings.TrimSpace(bp.label)
			}
			break
		}
	}

	// Take the first line or sentence
	if idx := strings.Index(text, "\n"); idx != -1 {
		text = strings.TrimSpace(text[:idx])
	}

	// Collapse multiple spaces into one
	text = strings.Join(strings.Fields(text), " ")

	// Capitalize first letter if needed
	if len(text) > 0 {
		runes := []rune(text)
		if unicode.IsLower(runes[0]) {
			runes[0] = unicode.ToUpper(runes[0])
			text = string(runes)
		}
	}

	// Truncate to reasonable length (max 65 chars)
	const maxLen = 65
	runes := []rune(text)
	if len(runes) > maxLen {
		truncated := string(runes[:maxLen])
		// Try to break at a space
		if lastSpace := strings.LastIndex(truncated, " "); lastSpace > 35 {
			truncated = truncated[:lastSpace]
		}
		text = strings.TrimRight(truncated, " ,;:-") + "..."
	}

	if text == "" {
		return "New Session"
	}

	return text
}

// Description returns a concise human-readable summary of this conversation.
func (c *Conversation) Description() string {
	if c == nil || c.State == nil {
		return "New Session"
	}
	return ExtractDescription(c.State.Messages)
}
