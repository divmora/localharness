package middleware

import (
	"context"
	"strings"
	"testing"
)

func TestToolSelector_AllowMode_InjectsDirective(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:   ToolSelectorAllow,
		Tools:  []string{"view_file", "list_dir"},
		Reason: "Read-only task",
	}, nil)

	req := &TurnRequest{
		Prompt:   "List the files",
		Metadata: make(map[string]any),
	}
	result, err := selector.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Prompt, "<tool_guidance>") {
		t.Fatal("expected tool_guidance tag")
	}
	if !strings.Contains(result.Prompt, "ONLY use the following tools") {
		t.Fatal("expected allow-mode text")
	}
	if !strings.Contains(result.Prompt, "- view_file") {
		t.Fatal("expected view_file in list")
	}
	if !strings.Contains(result.Prompt, "Read-only task") {
		t.Fatal("expected reason")
	}
	// Original prompt should be preserved after the directive
	if !strings.HasSuffix(result.Prompt, "List the files") {
		t.Fatal("expected original prompt at end")
	}
}

func TestToolSelector_DenyMode(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:  ToolSelectorDeny,
		Tools: []string{"run_command"},
	}, nil)

	req := &TurnRequest{
		Prompt:   "Fix the bug",
		Metadata: make(map[string]any),
	}
	result, err := selector.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Prompt, "must NOT use") {
		t.Fatal("expected deny-mode text")
	}
	if !strings.Contains(result.Prompt, "- run_command") {
		t.Fatal("expected run_command in deny list")
	}
}

func TestToolSelector_NoTools_SkipsInjection(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:  ToolSelectorAllow,
		Tools: nil, // empty
	}, nil)

	req := &TurnRequest{
		Prompt:   "Hello",
		Metadata: make(map[string]any),
	}
	result, err := selector.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if result.Prompt != "Hello" {
		t.Fatalf("expected unmodified prompt, got: %s", result.Prompt)
	}
}

func TestToolSelector_Dynamic(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Dynamic: func(prompt string) (ToolSelectorMode, []string, string) {
			if strings.Contains(prompt, "read only") {
				return ToolSelectorAllow, ReadOnlyTools, "Dynamic read-only"
			}
			return 0, nil, "" // No guidance
		},
	}, nil)

	// With "read only" in prompt → should inject
	req := &TurnRequest{
		Prompt:   "Please read only the config files",
		Metadata: make(map[string]any),
	}
	result, err := selector.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Prompt, "<tool_guidance>") {
		t.Fatal("expected tool guidance for read-only prompt")
	}

	// Without "read only" → no injection
	req2 := &TurnRequest{
		Prompt:   "Fix the bug in main.go",
		Metadata: make(map[string]any),
	}
	result2, err := selector.PreTurn(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result2.Prompt, "<tool_guidance>") {
		t.Fatal("should not inject guidance for non-matching prompt")
	}
}

func TestToolSelector_ViolationDetection_Allow(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:            ToolSelectorAllow,
		Tools:           []string{"view_file", "list_dir"},
		WarnOnViolation: true,
	}, nil)

	// Tool in allow list — no violation
	event := &StepEvent{
		ToolName:  "view_file",
		ToolState: "active",
		Metadata:  make(map[string]any),
	}
	result, err := selector.ProcessStep(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Metadata["tool_selection_violation"]; ok {
		t.Fatal("view_file should not be a violation")
	}

	// Tool NOT in allow list — violation
	event2 := &StepEvent{
		ToolName:  "run_command",
		ToolState: "active",
		Metadata:  make(map[string]any),
	}
	result2, err := selector.ProcessStep(context.Background(), event2)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result2.Metadata["tool_selection_violation"]; !ok || v != true {
		t.Fatal("run_command should be a violation in allow mode")
	}
}

func TestToolSelector_ViolationDetection_Deny(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:            ToolSelectorDeny,
		Tools:           DangerousTools,
		WarnOnViolation: true,
	}, nil)

	// Tool in deny list — violation
	event := &StepEvent{
		ToolName:  "run_command",
		ToolState: "active",
		Metadata:  make(map[string]any),
	}
	result, err := selector.ProcessStep(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result.Metadata["tool_selection_violation"]; !ok || v != true {
		t.Fatal("run_command should be a violation in deny mode")
	}

	// Tool NOT in deny list — no violation
	event2 := &StepEvent{
		ToolName:  "view_file",
		ToolState: "active",
		Metadata:  make(map[string]any),
	}
	result2, err := selector.ProcessStep(context.Background(), event2)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result2.Metadata["tool_selection_violation"]; ok {
		t.Fatal("view_file should not be a violation in deny mode")
	}
}

func TestToolSelector_ViolationCounts(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:            ToolSelectorAllow,
		Tools:           []string{"view_file"},
		WarnOnViolation: true,
	}, nil)

	// Two violations from run_command
	for i := 0; i < 2; i++ {
		selector.ProcessStep(context.Background(), &StepEvent{
			ToolName:  "run_command",
			ToolState: "active",
			Metadata:  make(map[string]any),
		})
	}

	violations := selector.Violations()
	if violations["run_command"] != 2 {
		t.Fatalf("expected 2 violations for run_command, got %d", violations["run_command"])
	}
}

func TestToolSelector_NoViolationWhenDisabled(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:            ToolSelectorAllow,
		Tools:           []string{"view_file"},
		WarnOnViolation: false, // disabled
	}, nil)

	event := &StepEvent{
		ToolName:  "run_command",
		ToolState: "active",
		Metadata:  make(map[string]any),
	}
	result, err := selector.ProcessStep(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Metadata["tool_selection_violation"]; ok {
		t.Fatal("should not flag violations when WarnOnViolation is false")
	}
}

func TestToolSelector_SkipNonActiveState(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{
		Mode:            ToolSelectorAllow,
		Tools:           []string{"view_file"},
		WarnOnViolation: true,
	}, nil)

	// "done" state should be skipped (only check "active")
	event := &StepEvent{
		ToolName:  "run_command",
		ToolState: "done",
		Metadata:  make(map[string]any),
	}
	result, err := selector.ProcessStep(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Metadata["tool_selection_violation"]; ok {
		t.Fatal("should not flag violations for non-active state")
	}
}

func TestToolSelector_Name(t *testing.T) {
	selector := NewToolSelector(ToolSelectorConfig{}, nil)
	if selector.Name() != "tool_selector" {
		t.Fatalf("expected 'tool_selector', got %q", selector.Name())
	}
}

func TestToolSelector_PreDefinedGroups(t *testing.T) {
	if len(ReadOnlyTools) == 0 {
		t.Fatal("ReadOnlyTools should not be empty")
	}
	if len(WriteTools) == 0 {
		t.Fatal("WriteTools should not be empty")
	}
	if len(DangerousTools) == 0 {
		t.Fatal("DangerousTools should not be empty")
	}

	// DangerousTools should be a subset of WriteTools
	for _, tool := range DangerousTools {
		if !contains(WriteTools, tool) {
			t.Fatalf("DangerousTools tool %q not in WriteTools", tool)
		}
	}
}

func TestBuildToolDirective_Allow(t *testing.T) {
	d := buildToolDirective(ToolSelectorAllow, []string{"view_file"}, "test")
	if !strings.HasPrefix(d, "<tool_guidance>") {
		t.Fatal("expected XML tag prefix")
	}
	if !strings.HasSuffix(d, "</tool_guidance>") {
		t.Fatal("expected XML tag suffix")
	}
}

func TestBuildToolDirective_Deny(t *testing.T) {
	d := buildToolDirective(ToolSelectorDeny, []string{"run_command"}, "")
	if !strings.Contains(d, "must NOT use") {
		t.Fatal("expected deny text")
	}
	if strings.Contains(d, "Reason:") {
		t.Fatal("should not include reason when empty")
	}
}
