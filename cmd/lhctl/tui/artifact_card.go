package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// ActiveArtifactReview manages state for reviewing an artifact requiring feedback.
type ActiveArtifactReview struct {
	Path              string
	Filename          string
	ArtifactType      string
	Summary           string
	IsWritingFeedback bool
	TextInput         textinput.Model
	ViewingFull       bool
	FullContent       string
}

// NewActiveArtifactReview creates a new review state.
func NewActiveArtifactReview(path, artifactType, summary string) *ActiveArtifactReview {
	ti := textinput.New()
	ti.Placeholder = "Type feedback or revisions for the agent..."
	ti.CharLimit = 512
	ti.Prompt = "  ✎ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true)

	filename := filepath.Base(path)
	if filename == "" || filename == "." {
		filename = "artifact.md"
	}
	if artifactType == "" {
		artifactType = "artifact"
	}

	return &ActiveArtifactReview{
		Path:         path,
		Filename:     filename,
		ArtifactType: artifactType,
		Summary:      summary,
		TextInput:    ti,
	}
}

// StartFeedback enters inline typing mode.
func (r *ActiveArtifactReview) StartFeedback() {
	r.IsWritingFeedback = true
	r.TextInput.Focus()
}

// CancelFeedback exits typing mode.
func (r *ActiveArtifactReview) CancelFeedback() {
	r.IsWritingFeedback = false
	r.TextInput.Blur()
}

// ToggleView toggles full artifact viewing.
func (r *ActiveArtifactReview) ToggleView() {
	r.ViewingFull = !r.ViewingFull
	if r.ViewingFull && r.FullContent == "" && r.Path != "" {
		if data, err := os.ReadFile(r.Path); err == nil {
			r.FullContent = string(data)
		}
	}
}

// RenderArtifactReviewInline renders the artifact card above the status bar.
func RenderArtifactReviewInline(rev *ActiveArtifactReview, width int) string {
	if rev == nil {
		return ""
	}

	cardWidth := width - 4
	if cardWidth < 40 {
		cardWidth = 40
	}

	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight)
	badgeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#374151")).
		Foreground(lipgloss.Color("#F3F4F6")).
		Padding(0, 1)
	pathStyle := lipgloss.NewStyle().Faint(true)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
	dimKeyStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	sb.WriteString(titleStyle.Render("📋 Artifact Review: "+rev.Filename) + "\n")
	sb.WriteString(badgeStyle.Render(strings.ToUpper(rev.ArtifactType)) + "  " + pathStyle.Render(rev.Path) + "\n\n")

	if rev.Summary != "" {
		sb.WriteString(wrapString(rev.Summary, cardWidth-4) + "\n\n")
	}

	if rev.ViewingFull && rev.FullContent != "" {
		previewLines := strings.Split(rev.FullContent, "\n")
		maxLines := 15
		if len(previewLines) > maxLines {
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render(strings.Join(previewLines[:maxLines], "\n")) + "\n")
			sb.WriteString(lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf("... (%d more lines in %s) ...\n\n", len(previewLines)-maxLines, rev.Path)))
		} else {
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render(rev.FullContent) + "\n\n")
		}
	}

	if rev.IsWritingFeedback {
		sb.WriteString(rev.TextInput.View() + "\n\n")
		actions := fmt.Sprintf("%s Submit feedback   %s Cancel",
			keyStyle.Render("[Enter]"),
			dimKeyStyle.Render("[Esc]"),
		)
		sb.WriteString(actions)
	} else {
		actions := fmt.Sprintf("%s Proceed   %s Provide Feedback   %s Toggle Full View   %s Dismiss",
			keyStyle.Render("[Enter/p]"),
			dimKeyStyle.Render("[f]"),
			dimKeyStyle.Render("[v]"),
			dimKeyStyle.Render("[Esc]"),
		)
		sb.WriteString(actions)
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorHighlight).
		Padding(1, 2).
		Width(cardWidth)

	return boxStyle.Render(sb.String())
}
