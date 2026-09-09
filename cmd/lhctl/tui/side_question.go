package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/llm"
)

// SideQuestionResultMsg delivers the response from an asynchronous /btw inquiry.
type SideQuestionResultMsg struct {
	Question string
	Answer   string
}

// AskSideQuestionCmd returns a tea.Cmd that processes a side question asynchronously.
func AskSideQuestionCmd(question string, history *ChatHistory, modelName string) tea.Cmd {
	return func() tea.Msg {
		// Collect recent conversation context (last 6 items)
		var recentLines []string
		if history != nil && len(history.items) > 0 {
			start := len(history.items) - 6
			if start < 0 {
				start = 0
			}
			for _, it := range history.items[start:] {
				switch it.Type {
				case ChatItemUser:
					recentLines = append(recentLines, "User: "+truncateForSideQ(it.Content, 200))
				case ChatItemAssistant:
					recentLines = append(recentLines, "Assistant: "+truncateForSideQ(it.Content, 200))
				case ChatItemToolCall:
					recentLines = append(recentLines, fmt.Sprintf("Action: %s (%s)", it.ToolName, truncateForSideQ(it.ToolArgs, 100)))
				}
			}
		}

		// Resolve the LiteLLM endpoint from the global config, mirroring
		// Session.createProvider so side questions use the same provider as
		// the main engine rather than reaching for direct Gemini/OpenAI keys.
		liteCfg := config.LoadGlobalLiteLLMConfig(nil)

		var baseURL, apiKey, model string
		if liteCfg != nil && liteCfg.DefaultEndpoint != "" {
			if ep, ok := liteCfg.Endpoints[liteCfg.DefaultEndpoint]; ok {
				baseURL = ep.BaseURL
				apiKey = ep.APIKey
				model = ep.DefaultModel
			}
		}
		if modelName == "" {
			modelName = model
		}

		if baseURL != "" && apiKey != "" {
			prov, err := llm.NewOpenAIProvider(llm.OpenAIConfig{
				BaseURL:   baseURL,
				APIKey:    apiKey,
				ModelName: modelName,
			}, nil)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				systemPrompt := "You are a coding assistant answering a quick side question (/btw) about the ongoing session. Answer directly, clearly, and concisely in 1-3 sentences. Do not use markdown headers or suggest tool commands."
				var userPrompt string
				if len(recentLines) > 0 {
					userPrompt = fmt.Sprintf("Session context:\n%s\n\nQuestion: %s", strings.Join(recentLines, "\n"), question)
				} else {
					userPrompt = question
				}

				resp, genErr := prov.Generate(ctx, &llm.GenerateRequest{
					SystemPrompt: systemPrompt,
					Messages: []llm.Message{
						{Role: "user", Content: userPrompt},
					},
				})
				if genErr == nil && resp != nil && resp.Content != "" {
					return SideQuestionResultMsg{
						Question: question,
						Answer:   strings.TrimSpace(resp.Content),
					}
				}
			}
		}

		// Local search fallback across recent conversation items
		lowerQ := strings.ToLower(question)
		var matches []string
		if history != nil {
			for _, it := range history.items {
				content := it.Content
				if content != "" && strings.Contains(strings.ToLower(content), lowerQ) {
					matches = append(matches, truncateForSideQ(content, 160))
					if len(matches) >= 2 {
						break
					}
				}
			}
		}

		if len(matches) > 0 {
			return SideQuestionResultMsg{
				Question: question,
				Answer:   fmt.Sprintf("Matched session history:\n• %s", strings.Join(matches, "\n• ")),
			}
		}

		return SideQuestionResultMsg{
			Question: question,
			Answer:   fmt.Sprintf("Side inquiry noted: %q. (No background provider configured for standalone /btw side queries).", question),
		}
	}
}

func truncateForSideQ(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
