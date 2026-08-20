package tui

import (
	"strings"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestActiveQuestion_SingleSelect(t *testing.T) {
	req := &pb.ActionUserQuestion{
		RequestId: "req-q1",
		Questions: []*pb.UserQuestion{
			{
				Question:      "Which database should we use?",
				Options:       []string{"PostgreSQL", "SQLite", "MySQL"},
				IsMultiSelect: false,
			},
		},
	}

	aq := NewActiveQuestion(req)
	if aq == nil {
		t.Fatal("expected non-nil ActiveQuestion")
	}
	if aq.FocusedOption != 0 {
		t.Errorf("expected current focused option 0, got %d", aq.FocusedOption)
	}

	// Select option 1 (SQLite)
	aq.SelectOption(1)
	if !aq.SelectedIndices[0][1] {
		t.Error("expected option 1 to be selected")
	}

	// Select option 0 (PostgreSQL) - should unselect option 1
	aq.SelectOption(0)
	if !aq.SelectedIndices[0][0] || aq.SelectedIndices[0][1] {
		t.Error("expected only option 0 to be selected in single-select")
	}

	answers := aq.BuildAnswers()
	if len(answers) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(answers))
	}
	if len(answers[0].SelectedOptions) != 1 || answers[0].SelectedOptions[0] != "PostgreSQL" {
		t.Errorf("expected PostgreSQL selected, got %v", answers[0].SelectedOptions)
	}

	rendered := RenderQuestionInline(aq, 90)
	if !strings.Contains(rendered, "Which database should we use?") {
		t.Errorf("expected question text in rendered card: %s", rendered)
	}
	if !strings.Contains(rendered, "(●)") || !strings.Contains(rendered, "PostgreSQL") {
		t.Errorf("expected selected radio (●) on PostgreSQL: %s", rendered)
	}
	if !strings.Contains(rendered, "[1-3] Select") {
		t.Errorf("expected shortcut footer: %s", rendered)
	}
}

func TestActiveQuestion_MultiSelect(t *testing.T) {
	req := &pb.ActionUserQuestion{
		RequestId: "req-q2",
		Questions: []*pb.UserQuestion{
			{
				Question:      "Which features to enable?",
				Options:       []string{"Auth", "Rate Limiting", "Metrics"},
				IsMultiSelect: true,
			},
		},
	}

	aq := NewActiveQuestion(req)

	// Toggle options 0 and 2
	aq.ToggleOption(0)
	aq.ToggleOption(2)

	if !aq.SelectedIndices[0][0] || !aq.SelectedIndices[0][2] || aq.SelectedIndices[0][1] {
		t.Errorf("expected options 0 and 2 selected, got %v", aq.SelectedIndices[0])
	}

	answers := aq.BuildAnswers()
	if len(answers[0].SelectedOptions) != 2 {
		t.Fatalf("expected 2 selected options, got %d", len(answers[0].SelectedOptions))
	}
	if answers[0].SelectedOptions[0] != "Auth" || answers[0].SelectedOptions[1] != "Metrics" {
		t.Errorf("unexpected selected options: %v", answers[0].SelectedOptions)
	}

	summary := aq.CurrentAnswerSummary()
	if !strings.Contains(summary, "Auth, Metrics") {
		t.Errorf("expected summary to contain Auth, Metrics: %s", summary)
	}

	rendered := RenderQuestionInline(aq, 90)
	if !strings.Contains(rendered, "[Multi-select]") {
		t.Errorf("expected [Multi-select] badge: %s", rendered)
	}
	if !strings.Contains(rendered, "[✓]") {
		t.Errorf("expected [✓] checkmark: %s", rendered)
	}
}

func TestActiveQuestion_SequentialNavigation(t *testing.T) {
	req := &pb.ActionUserQuestion{
		RequestId: "req-q3",
		Questions: []*pb.UserQuestion{
			{
				Question: "Database type?",
				Options:  []string{"PostgreSQL", "SQLite"},
			},
			{
				Question:      "Components?",
				Options:       []string{"Auth", "Metrics"},
				IsMultiSelect: true,
			},
		},
	}

	aq := NewActiveQuestion(req)
	if !aq.HasNext() || aq.HasPrev() {
		t.Errorf("expected HasNext=true, HasPrev=false at start")
	}

	// Answer Q1
	aq.SelectOption(1) // SQLite
	if aq.CurrentAnswerSummary() != "Database type? ➔ SQLite" {
		t.Errorf("unexpected Q1 summary: %s", aq.CurrentAnswerSummary())
	}

	// Advance to Q2
	if !aq.NextQuestion() {
		t.Fatal("expected NextQuestion to succeed")
	}
	if aq.CurrentQuestion != 1 || aq.HasNext() || !aq.HasPrev() {
		t.Errorf("expected Q1 active, HasNext=false, HasPrev=true")
	}

	// Answer Q2
	aq.ToggleOption(0) // Auth
	aq.ToggleOption(1) // Metrics
	if aq.CurrentAnswerSummary() != "Components? ➔ Auth, Metrics" {
		t.Errorf("unexpected Q2 summary: %s", aq.CurrentAnswerSummary())
	}

	allSummary := aq.AllAnswersSummary()
	if !strings.Contains(allSummary, "SQLite") || !strings.Contains(allSummary, "Auth, Metrics") {
		t.Errorf("unexpected all summary: %s", allSummary)
	}

	// Back to Q1
	if !aq.PrevQuestion() {
		t.Fatal("expected PrevQuestion to succeed")
	}
	if aq.CurrentQuestion != 0 {
		t.Errorf("expected back at Q0, got %d", aq.CurrentQuestion)
	}
}

func TestActiveQuestion_CursorMovement(t *testing.T) {
	req := &pb.ActionUserQuestion{
		RequestId: "req-q4",
		Questions: []*pb.UserQuestion{
			{
				Question: "Choose one",
				Options:  []string{"Opt1", "Opt2", "Opt3"},
			},
		},
	}

	aq := NewActiveQuestion(req)
	if aq.FocusedOption != 0 {
		t.Errorf("expected initial focus 0, got %d", aq.FocusedOption)
	}

	aq.MoveCursorDown()
	if aq.FocusedOption != 1 {
		t.Errorf("expected focus 1, got %d", aq.FocusedOption)
	}

	aq.MoveCursorDown()
	if aq.FocusedOption != 2 {
		t.Errorf("expected focus 2, got %d", aq.FocusedOption)
	}

	// Move down to 'Other' (index 3)
	aq.MoveCursorDown()
	if aq.FocusedOption != 3 || !aq.IsOtherFocused() {
		t.Errorf("expected focus 3 (Other), got %d", aq.FocusedOption)
	}

	// Wrap around down
	aq.MoveCursorDown()
	if aq.FocusedOption != 0 {
		t.Errorf("expected wrap to 0, got %d", aq.FocusedOption)
	}

	// Wrap around up
	aq.MoveCursorUp()
	if aq.FocusedOption != 3 {
		t.Errorf("expected wrap to 3, got %d", aq.FocusedOption)
	}

	// Move up to Opt3 (index 2) and toggle
	aq.MoveCursorUp()
	aq.ToggleFocused()
	if !aq.SelectedIndices[0][2] {
		t.Error("expected Opt3 to be selected via ToggleFocused")
	}
}

func TestActiveQuestion_CustomWriteIn(t *testing.T) {
	req := &pb.ActionUserQuestion{
		RequestId: "req-q5",
		Questions: []*pb.UserQuestion{
			{
				Question:      "Which database should we use?",
				Options:       []string{"PostgreSQL", "SQLite"},
				IsMultiSelect: false,
			},
		},
	}

	aq := NewActiveQuestion(req)
	if aq.TotalSelectableCount() != 3 { // 2 options + 1 for 'Other'
		t.Errorf("expected 3 selectable items, got %d", aq.TotalSelectableCount())
	}

	// Focus 'Other' (option index 2)
	aq.MoveCursorDown()
	aq.MoveCursorDown()
	if !aq.IsOtherFocused() {
		t.Error("expected 'Other' to be focused")
	}

	// Start write-in
	aq.StartWriteIn()
	if !aq.IsWritingText {
		t.Error("expected IsWritingText=true")
	}

	// Type custom text
	aq.TextInput.SetValue("DuckDB in-memory")
	aq.ConfirmWriteIn()

	if aq.IsWritingText {
		t.Error("expected IsWritingText=false after confirm")
	}
	if !aq.IsOtherSelected() {
		t.Error("expected IsOtherSelected=true")
	}

	answers := aq.BuildAnswers()
	if len(answers) != 1 || answers[0].Text != "DuckDB in-memory" {
		t.Errorf("expected answers[0].Text = 'DuckDB in-memory', got %v", answers)
	}

	summary := aq.CurrentAnswerSummary()
	if !strings.Contains(summary, `Other: "DuckDB in-memory"`) {
		t.Errorf("expected summary to contain DuckDB in-memory: %s", summary)
	}

	rendered := RenderQuestionInline(aq, 90)
	if !strings.Contains(rendered, "Other: DuckDB in-memory") {
		t.Errorf("expected custom text in rendered card: %s", rendered)
	}
}
