package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ContextInfo holds snapshot data for the /context command.
type ContextInfo struct {
	ModelName        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	MaxTokens        int
	Workspaces       []string
	SessionID        string
}

// DefaultMaxTokens returns the context window size for common models.
func DefaultMaxTokens(modelName string) int {
	lower := strings.ToLower(modelName)
	switch {
	case strings.Contains(lower, "gemini"):
		return 1048576 // 1.0M
	case strings.Contains(lower, "claude"):
		return 200000 // 200k
	case strings.Contains(lower, "gpt-4"), strings.Contains(lower, "o1"), strings.Contains(lower, "o3"):
		return 128000 // 128k
	default:
		return 1000000 // 1.0M default
	}
}

// FormatTokenCount formats token counts as 1.0M, 29.9k, or 500.
func FormatTokenCount(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000.0)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

// RenderContextView formats the full Antigravity-style 2D context usage card.
func RenderContextView(info ContextInfo, width int) string {
	maxTokens := info.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens(info.ModelName)
	}

	usedTokens := info.TotalTokens
	if usedTokens <= 0 {
		usedTokens = info.PromptTokens + info.CompletionTokens
	}
	if usedTokens <= 0 {
		usedTokens = 100 // Minimal placeholder if unstarted
	}

	usedPct := (float64(usedTokens) / float64(maxTokens)) * 100.0
	if usedPct > 100.0 {
		usedPct = 100.0
	}

	// 5 rows of 20 dots = 100 dots (each dot = 1%)
	totalDots := 100
	filledDots := int(math.Round(usedPct))
	if filledDots == 0 && usedTokens > 0 {
		filledDots = 1
	}
	if filledDots > totalDots {
		filledDots = totalDots
	}

	filledDotStyle := lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true)
	emptyDotStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var gridRows []string
	dotCount := 0
	for row := 0; row < 5; row++ {
		var rowDots []string
		for col := 0; col < 20; col++ {
			if dotCount < filledDots {
				rowDots = append(rowDots, filledDotStyle.Render("◉"))
			} else {
				rowDots = append(rowDots, emptyDotStyle.Render("□"))
			}
			dotCount++
		}
		gridRows = append(gridRows, strings.Join(rowDots, " "))
	}
	gridBlock := strings.Join(gridRows, "\n")

	// Model name & token stats
	modelDisplay := info.ModelName
	if modelDisplay == "" {
		modelDisplay = "Gemini 2.5 Pro"
	}

	remainingTokens := maxTokens - usedTokens
	if remainingTokens < 0 {
		remainingTokens = 0
	}
	remainingPct := (float64(remainingTokens) / float64(maxTokens)) * 100.0

	var statsSb strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight)
	labelStyle := lipgloss.NewStyle().Foreground(ColorText)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	statsSb.WriteString(titleStyle.Render("└ Context Usage") + "\n\n")
	statsSb.WriteString(fmt.Sprintf("%s · %s/%s tokens (%.1f%%)\n\n",
		lipgloss.NewStyle().Bold(true).Render(modelDisplay),
		FormatTokenCount(usedTokens),
		FormatTokenCount(maxTokens),
		usedPct,
	))

	statsSb.WriteString(lipgloss.NewStyle().Underline(true).Render("Token usage breakdown") + "\n")
	statsSb.WriteString(fmt.Sprintf("  %s Prompt / Context:    %s tokens (%.1f%%)\n",
		filledDotStyle.Render("◉"),
		FormatTokenCount(info.PromptTokens),
		(float64(info.PromptTokens)/float64(maxTokens))*100.0,
	))
	statsSb.WriteString(fmt.Sprintf("  %s Model Completion:    %s tokens (%.1f%%)\n",
		filledDotStyle.Render("◉"),
		FormatTokenCount(info.CompletionTokens),
		(float64(info.CompletionTokens)/float64(maxTokens))*100.0,
	))
	statsSb.WriteString(fmt.Sprintf("  %s Remaining context:   %s tokens (%.1f%%)\n\n",
		emptyDotStyle.Render("□"),
		FormatTokenCount(remainingTokens),
		remainingPct,
	))

	// Discover active artifacts in brain dir
	var artifacts []string
	if info.SessionID != "" {
		home, _ := os.UserHomeDir()
		brainDir := filepath.Join(home, ".divmora", "localharness", "brain", info.SessionID)
		if entries, err := os.ReadDir(brainDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					artifacts = append(artifacts, e.Name())
				}
			}
		}
	}

	// Active context items
	statsSb.WriteString(lipgloss.NewStyle().Bold(true).Render("Active Context Items:") + "\n")
	if len(info.Workspaces) > 0 {
		statsSb.WriteString(fmt.Sprintf("  • Workspaces (%d): %s\n", len(info.Workspaces), strings.Join(info.Workspaces, ", ")))
	} else {
		statsSb.WriteString("  • Workspaces: None active\n")
	}

	if len(artifacts) > 0 {
		statsSb.WriteString(fmt.Sprintf("  • Brain Artifacts (%d): %s\n", len(artifacts), strings.Join(artifacts, ", ")))
	} else {
		statsSb.WriteString("  • Brain Artifacts: None created yet\n")
	}

	// Check AGENTS.md
	hasAgentsMD := false
	for _, ws := range info.Workspaces {
		if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); err == nil {
			hasAgentsMD = true
			break
		}
	}
	if hasAgentsMD {
		statsSb.WriteString("  • Rules & Configuration: " + labelStyle.Render("AGENTS.md loaded") + "\n")
	} else {
		statsSb.WriteString("  • Rules & Configuration: " + dimStyle.Render("Default system prompt") + "\n")
	}

	contentWidth := width - 6
	if contentWidth < 50 {
		contentWidth = 50
	}

	// Join grid and stats side-by-side or stacked depending on terminal width
	var body string
	if contentWidth >= 80 {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(44).Render(gridBlock),
			"   ",
			statsSb.String(),
		)
	} else {
		body = gridBlock + "\n\n" + statsSb.String()
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorHighlight).
		Padding(1, 2).
		Width(contentWidth)

	return cardStyle.Render(body)
}
