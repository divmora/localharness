package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// ActiveQuestion manages the state for a sequential interactive question prompt with custom write-in support.
type ActiveQuestion struct {
	RequestID       string
	Questions       []*pb.UserQuestion
	CurrentQuestion int                  // 0-based index of active question
	FocusedOption   int                  // focused option index in current question (0..len(Options))
	SelectedIndices map[int]map[int]bool // map[questionIndex]map[optionIndex]bool
	CustomText      map[int]string       // map[questionIndex]customText (for write-in answers)
	IsWritingText   bool                 // true when user is typing custom text in text input
	TextInput       textinput.Model      // text input component for write-in response
}

// NewActiveQuestion initializes an ActiveQuestion from a protobuf request.
func NewActiveQuestion(req *pb.ActionUserQuestion) *ActiveQuestion {
	if req == nil || len(req.Questions) == 0 {
		return nil
	}

	ti := textinput.New()
	ti.Placeholder = "Type custom response and press Enter..."
	ti.CharLimit = 256
	ti.Prompt = "  ✎ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true)

	aq := &ActiveQuestion{
		RequestID:       req.RequestId,
		Questions:       req.Questions,
		CurrentQuestion: 0,
		FocusedOption:   0,
		SelectedIndices: make(map[int]map[int]bool),
		CustomText:      make(map[int]string),
		IsWritingText:   false,
		TextInput:       ti,
	}
	for qIdx := range req.Questions {
		aq.SelectedIndices[qIdx] = make(map[int]bool)
	}
	return aq
}

// CurrentQ returns the current active question.
func (q *ActiveQuestion) CurrentQ() *pb.UserQuestion {
	if q == nil || q.CurrentQuestion < 0 || q.CurrentQuestion >= len(q.Questions) {
		return nil
	}
	return q.Questions[q.CurrentQuestion]
}

// TotalSelectableCount returns total selectable items for current question (predefined options + 1 for 'Other').
func (q *ActiveQuestion) TotalSelectableCount() int {
	curQ := q.CurrentQ()
	if curQ == nil {
		return 0
	}
	return len(curQ.Options) + 1
}

// IsOtherFocused returns true if the cursor is on the 'Other' write-in option.
func (q *ActiveQuestion) IsOtherFocused() bool {
	curQ := q.CurrentQ()
	if curQ == nil {
		return false
	}
	return q.FocusedOption == len(curQ.Options)
}

// IsOtherSelected returns true if a custom text response is currently set for this question.
func (q *ActiveQuestion) IsOtherSelected() bool {
	if q == nil {
		return false
	}
	return q.CustomText[q.CurrentQuestion] != ""
}

// StartWriteIn enters write-in text input mode.
func (q *ActiveQuestion) StartWriteIn() {
	curQ := q.CurrentQ()
	if curQ == nil {
		return
	}
	q.IsWritingText = true
	q.FocusedOption = len(curQ.Options)
	q.TextInput.SetValue(q.CustomText[q.CurrentQuestion])
	q.TextInput.Focus()
}

// ConfirmWriteIn saves the typed text response and exits write-in mode.
func (q *ActiveQuestion) ConfirmWriteIn() {
	curQ := q.CurrentQ()
	if curQ == nil {
		return
	}
	val := strings.TrimSpace(q.TextInput.Value())
	q.CustomText[q.CurrentQuestion] = val
	q.IsWritingText = false
	q.TextInput.Blur()

	// If single-select and custom text is set, deselect predefined options
	if val != "" && !curQ.IsMultiSelect {
		q.SelectedIndices[q.CurrentQuestion] = make(map[int]bool)
	}
}

// CancelWriteIn cancels write-in input mode without saving changes.
func (q *ActiveQuestion) CancelWriteIn() {
	q.IsWritingText = false
	q.TextInput.Blur()
}

// ClearWriteIn removes any custom write-in text for the current question.
func (q *ActiveQuestion) ClearWriteIn() {
	if q != nil {
		delete(q.CustomText, q.CurrentQuestion)
	}
}

// SelectOption selects a single predefined option for the current question (clears custom text and other options).
func (q *ActiveQuestion) SelectOption(optIdx int) {
	curQ := q.CurrentQ()
	if curQ == nil || optIdx < 0 || optIdx >= len(curQ.Options) {
		return
	}
	q.SelectedIndices[q.CurrentQuestion] = map[int]bool{optIdx: true}
	delete(q.CustomText, q.CurrentQuestion)
	q.FocusedOption = optIdx
}

// ToggleOption toggles an option selection. If optIdx == len(Options), starts write-in.
func (q *ActiveQuestion) ToggleOption(optIdx int) {
	curQ := q.CurrentQ()
	if curQ == nil {
		return
	}
	if optIdx == len(curQ.Options) {
		// Toggle/start write-in
		if q.IsOtherSelected() && curQ.IsMultiSelect {
			delete(q.CustomText, q.CurrentQuestion)
		} else {
			q.StartWriteIn()
		}
		return
	}
	if optIdx < 0 || optIdx >= len(curQ.Options) {
		return
	}
	if !curQ.IsMultiSelect {
		q.SelectOption(optIdx)
		return
	}
	current := q.SelectedIndices[q.CurrentQuestion][optIdx]
	q.SelectedIndices[q.CurrentQuestion][optIdx] = !current
	q.FocusedOption = optIdx
}

// ToggleFocused toggles selection of the currently focused option or starts write-in if on 'Other'.
func (q *ActiveQuestion) ToggleFocused() {
	if q == nil {
		return
	}
	q.ToggleOption(q.FocusedOption)
}

// MoveCursorUp moves focus up one option in the current question.
func (q *ActiveQuestion) MoveCursorUp() {
	total := q.TotalSelectableCount()
	if total == 0 {
		return
	}
	q.FocusedOption = (q.FocusedOption - 1 + total) % total
}

// MoveCursorDown moves focus down one option in the current question.
func (q *ActiveQuestion) MoveCursorDown() {
	total := q.TotalSelectableCount()
	if total == 0 {
		return
	}
	q.FocusedOption = (q.FocusedOption + 1) % total
}

// HasNext returns true if there are more questions after the current one.
func (q *ActiveQuestion) HasNext() bool {
	if q == nil {
		return false
	}
	return q.CurrentQuestion < len(q.Questions)-1
}

// HasPrev returns true if there is a previous question before the current one.
func (q *ActiveQuestion) HasPrev() bool {
	if q == nil {
		return false
	}
	return q.CurrentQuestion > 0
}

// NextQuestion advances to the next question. Returns true if advanced.
func (q *ActiveQuestion) NextQuestion() bool {
	if q.HasNext() {
		q.CurrentQuestion++
		q.FocusedOption = 0
		q.IsWritingText = false
		q.TextInput.Blur()
		return true
	}
	return false
}

// PrevQuestion moves back to the previous question. Returns true if moved.
func (q *ActiveQuestion) PrevQuestion() bool {
	if q.HasPrev() {
		q.CurrentQuestion--
		q.FocusedOption = 0
		q.IsWritingText = false
		q.TextInput.Blur()
		return true
	}
	return false
}

// CurrentAnswerSummary returns a summary string for the current question's answer.
func (q *ActiveQuestion) CurrentAnswerSummary() string {
	curQ := q.CurrentQ()
	if curQ == nil {
		return ""
	}
	var sel []string
	for optIdx, optText := range curQ.Options {
		if q.SelectedIndices[q.CurrentQuestion][optIdx] {
			sel = append(sel, optText)
		}
	}
	if text, ok := q.CustomText[q.CurrentQuestion]; ok && text != "" {
		sel = append(sel, fmt.Sprintf("Other: %q", text))
	}
	if len(sel) == 0 {
		return fmt.Sprintf("%s ➔ (none)", curQ.Question)
	}
	return fmt.Sprintf("%s ➔ %s", curQ.Question, strings.Join(sel, ", "))
}

// AllAnswersSummary returns a summary string for all answered questions.
func (q *ActiveQuestion) AllAnswersSummary() string {
	if q == nil {
		return ""
	}
	var parts []string
	for qIdx, uq := range q.Questions {
		var sel []string
		for optIdx, optText := range uq.Options {
			if q.SelectedIndices[qIdx][optIdx] {
				sel = append(sel, optText)
			}
		}
		if text, ok := q.CustomText[qIdx]; ok && text != "" {
			sel = append(sel, fmt.Sprintf("Other: %q", text))
		}
		if len(sel) == 0 {
			parts = append(parts, fmt.Sprintf("%s ➔ (none)", uq.Question))
		} else {
			parts = append(parts, fmt.Sprintf("%s ➔ %s", uq.Question, strings.Join(sel, ", ")))
		}
	}
	return strings.Join(parts, " | ")
}

// BuildAnswers constructs the QuestionAnswer protobuf messages to send to the server.
func (q *ActiveQuestion) BuildAnswers() []*pb.QuestionAnswer {
	if q == nil {
		return nil
	}
	var answers []*pb.QuestionAnswer
	for qIdx, uq := range q.Questions {
		var selectedIndices []int32
		var selectedOptions []string
		for optIdx, optText := range uq.Options {
			if q.SelectedIndices[qIdx][optIdx] {
				selectedIndices = append(selectedIndices, int32(optIdx))
				selectedOptions = append(selectedOptions, optText)
			}
		}
		var textAnswer string
		if custom, ok := q.CustomText[qIdx]; ok {
			textAnswer = custom
		}
		answers = append(answers, &pb.QuestionAnswer{
			SelectedIndices: selectedIndices,
			SelectedOptions: selectedOptions,
			Text:            textAnswer,
		})
	}
	return answers
}

// RenderQuestionInline renders the current question sequentially in an interactive inline card.
func RenderQuestionInline(q *ActiveQuestion, width int) string {
	if q == nil || len(q.Questions) == 0 {
		return ""
	}

	curQ := q.CurrentQ()
	if curQ == nil {
		return ""
	}

	var sb strings.Builder

	// Header: e.g. "❓ QUESTION (1 of 3): Which database would you like to configure?"
	qNumStr := ""
	if len(q.Questions) > 1 {
		qNumStr = fmt.Sprintf(" (%d of %d)", q.CurrentQuestion+1, len(q.Questions))
	}
	multiTag := ""
	if curQ.IsMultiSelect {
		multiTag = " " + lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("[Multi-select]")
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("❓ QUESTION"+qNumStr+": ") +
		lipgloss.NewStyle().Bold(true).Foreground(ColorText).Render(curQ.Question) + multiTag
	sb.WriteString(header + "\n\n")

	// Predefined options
	for optIdx, optText := range curQ.Options {
		isSelected := q.SelectedIndices[q.CurrentQuestion][optIdx]
		isFocused := optIdx == q.FocusedOption

		var checkSymbol string
		if curQ.IsMultiSelect {
			if isSelected {
				checkSymbol = lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("[✓]")
			} else {
				checkSymbol = lipgloss.NewStyle().Foreground(ColorSubtle).Render("[ ]")
			}
		} else {
			if isSelected {
				checkSymbol = lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("(●)")
			} else {
				checkSymbol = lipgloss.NewStyle().Foreground(ColorSubtle).Render("( )")
			}
		}

		numStr := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(fmt.Sprintf(" %d. ", optIdx+1))

		textStyle := lipgloss.NewStyle()
		if isSelected {
			textStyle = textStyle.Bold(true).Foreground(ColorSecondary)
		} else {
			textStyle = textStyle.Foreground(ColorText)
		}

		cursorPrefix := "  "
		if isFocused && !q.IsWritingText {
			cursorPrefix = lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("❯ ")
		}

		sb.WriteString(cursorPrefix + checkSymbol + numStr + textStyle.Render(optText) + "\n")
	}

	// [o] Other (write-in response) option
	otherFocused := q.IsOtherFocused()
	otherSelected := q.IsOtherSelected()

	var otherCheckSymbol string
	if curQ.IsMultiSelect {
		if otherSelected {
			otherCheckSymbol = lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("[✓]")
		} else {
			otherCheckSymbol = lipgloss.NewStyle().Foreground(ColorSubtle).Render("[ ]")
		}
	} else {
		if otherSelected {
			otherCheckSymbol = lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("(●)")
		} else {
			otherCheckSymbol = lipgloss.NewStyle().Foreground(ColorSubtle).Render("( )")
		}
	}

	otherCursor := "  "
	if otherFocused || q.IsWritingText {
		otherCursor = lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("❯ ")
	}

	otherLabel := " [o] Other (type custom response)"
	if customVal, ok := q.CustomText[q.CurrentQuestion]; ok && customVal != "" {
		otherLabel = fmt.Sprintf(" [o] Other: %s", lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render(customVal))
	}

	sb.WriteString(otherCursor + otherCheckSymbol + otherLabel + "\n")

	// If currently typing in write-in mode, render the text input box
	if q.IsWritingText {
		inputBox := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorHighlight).
			Padding(0, 1).
			MarginLeft(4).
			Width(min(width-10, 80)).
			Render(q.TextInput.View())
		sb.WriteString(inputBox + "\n")
	}

	sb.WriteString("\n")

	// Navigation / Action footer
	var navActions []string
	if q.IsWritingText {
		navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("[Enter] Confirm text"))
		navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorSubtle).Render("[Esc] Cancel write-in"))
	} else {
		if len(curQ.Options) > 0 {
			navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render(fmt.Sprintf("[1-%d] Select", len(curQ.Options))))
		}
		navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true).Render("[o] Write-in"))
		navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorText).Render("[Space] Toggle"))
		navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorText).Render("[↑/↓] Navigate"))

		if q.HasNext() {
			navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("[Enter] Next"))
		} else {
			navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("[Enter] Submit"))
		}

		if q.HasPrev() {
			navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorSubtle).Render("[b] Back"))
		}

		navActions = append(navActions, lipgloss.NewStyle().Foreground(ColorError).Render("[s] Skip"))
	}

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Keys: ") + strings.Join(navActions, "  "))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorHighlight).
		Padding(0, 1).
		Width(min(width-2, 96)).
		Render(sb.String())
}
