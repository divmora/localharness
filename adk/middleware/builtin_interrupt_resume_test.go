package middleware

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInterruptResume_PreTurn_RecordsState(t *testing.T) {
	ir := NewInterruptResume(InterruptResumeConfig{}, nil)

	req := &TurnRequest{
		Prompt:   "Fix the bug",
		Metadata: make(map[string]any),
	}
	_, err := ir.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	ir.mu.Lock()
	if ir.currentPrompt != "Fix the bug" {
		t.Fatalf("expected prompt recorded, got: %s", ir.currentPrompt)
	}
	if ir.turnIndex != 1 {
		t.Fatalf("expected turn index 1, got %d", ir.turnIndex)
	}
	ir.mu.Unlock()
}

func TestInterruptResume_ManualInterrupt(t *testing.T) {
	var captured *TurnCheckpoint
	ir := NewInterruptResume(InterruptResumeConfig{
		OnInterrupt: func(cp TurnCheckpoint) {
			captured = &cp
		},
	}, nil)

	// Simulate a turn
	ir.PreTurn(context.Background(), &TurnRequest{
		Prompt:   "Deploy to production",
		Metadata: make(map[string]any),
	})

	// Simulate some steps
	ir.ProcessStep(context.Background(), &StepEvent{TextDelta: "Checking ", Metadata: make(map[string]any)})
	ir.ProcessStep(context.Background(), &StepEvent{TextDelta: "status...", Metadata: make(map[string]any)})

	// Manual interrupt
	cp := ir.Interrupt(InterruptUser, "user cancelled")

	if cp.Reason != InterruptUser {
		t.Fatalf("expected user interrupt reason, got: %s", cp.Reason)
	}
	if cp.Prompt != "Deploy to production" {
		t.Fatalf("expected original prompt, got: %s", cp.Prompt)
	}
	if cp.PartialResponse != "Checking status..." {
		t.Fatalf("expected accumulated text, got: %s", cp.PartialResponse)
	}
	if cp.StepCount != 2 {
		t.Fatalf("expected 2 steps, got %d", cp.StepCount)
	}

	// Callback should have been called
	if captured == nil {
		t.Fatal("OnInterrupt callback should have been called")
	}
	if captured.ReasonDetail != "user cancelled" {
		t.Fatalf("expected detail in callback, got: %s", captured.ReasonDetail)
	}

	// LastCheckpoint should return it
	last := ir.LastCheckpoint()
	if last == nil || last.Prompt != "Deploy to production" {
		t.Fatal("LastCheckpoint should return the saved checkpoint")
	}
}

func TestInterruptResume_BuildResumePrompt(t *testing.T) {
	ir := NewInterruptResume(InterruptResumeConfig{
		ResumePromptTemplate: "Resume: {original_prompt}\nPartial: {partial_response}\nReason: {reason}",
	}, nil)

	cp := TurnCheckpoint{
		Prompt:          "Fix the bug in auth.go",
		PartialResponse: "I found the issue in line 42",
		Reason:          InterruptBudget,
	}

	prompt := ir.BuildResumePrompt(cp)
	if !strings.Contains(prompt, "Fix the bug in auth.go") {
		t.Fatal("expected original prompt in resume")
	}
	if !strings.Contains(prompt, "I found the issue in line 42") {
		t.Fatal("expected partial response in resume")
	}
	if !strings.Contains(prompt, "budget") {
		t.Fatal("expected reason in resume")
	}
}

func TestInterruptResume_DefaultResumeTemplate(t *testing.T) {
	ir := NewInterruptResume(InterruptResumeConfig{}, nil)

	cp := TurnCheckpoint{
		Prompt: "List the files",
	}

	prompt := ir.BuildResumePrompt(cp)
	if !strings.Contains(prompt, "List the files") {
		t.Fatal("expected prompt in default template")
	}
	if !strings.Contains(prompt, "Continue from where") {
		t.Fatal("expected default template text")
	}
}

func TestInterruptResume_TimeoutDetection(t *testing.T) {
	var captured *TurnCheckpoint
	ir := NewInterruptResume(InterruptResumeConfig{
		TurnTimeout: 10 * time.Millisecond, // very short
		OnInterrupt: func(cp TurnCheckpoint) {
			captured = &cp
		},
	}, nil)

	ir.PreTurn(context.Background(), &TurnRequest{
		Prompt:   "Slow task",
		Metadata: make(map[string]any),
	})

	// Wait for timeout
	time.Sleep(20 * time.Millisecond)

	// PostTurn should detect the timeout
	ir.PostTurn(context.Background(), &TurnResponse{
		Text:        "partial result",
		TotalTokens: 500,
		StepCount:   3,
		Metadata:    make(map[string]any),
	})

	if captured == nil {
		t.Fatal("timeout should trigger interrupt")
	}
	if captured.Reason != InterruptTimeout {
		t.Fatalf("expected timeout reason, got: %s", captured.Reason)
	}
	if captured.TotalTokens != 500 {
		t.Fatalf("expected 500 tokens, got %d", captured.TotalTokens)
	}
}

func TestInterruptResume_NoTimeoutWhenFast(t *testing.T) {
	var captured *TurnCheckpoint
	ir := NewInterruptResume(InterruptResumeConfig{
		TurnTimeout: 1 * time.Second, // generous
		OnInterrupt: func(cp TurnCheckpoint) {
			captured = &cp
		},
	}, nil)

	ir.PreTurn(context.Background(), &TurnRequest{
		Prompt:   "Fast task",
		Metadata: make(map[string]any),
	})

	ir.PostTurn(context.Background(), &TurnResponse{
		Text:     "done",
		Metadata: make(map[string]any),
	})

	if captured != nil {
		t.Fatal("should not trigger interrupt for fast turns")
	}
}

func TestInterruptResume_TurnDeadlineMetadata(t *testing.T) {
	ir := NewInterruptResume(InterruptResumeConfig{
		TurnTimeout: 5 * time.Minute,
	}, nil)

	req := &TurnRequest{
		Prompt:   "test",
		Metadata: make(map[string]any),
	}
	result, err := ir.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	deadline, ok := result.Metadata["turn_deadline"].(time.Time)
	if !ok {
		t.Fatal("expected turn_deadline in metadata")
	}
	if time.Until(deadline) < 4*time.Minute {
		t.Fatalf("deadline too soon: %v", deadline)
	}
}

func TestInterruptResume_Name(t *testing.T) {
	ir := NewInterruptResume(InterruptResumeConfig{}, nil)
	if ir.Name() != "interrupt_resume" {
		t.Fatalf("expected 'interrupt_resume', got %q", ir.Name())
	}
}

func TestInterruptResume_MultipleInterrupts(t *testing.T) {
	ir := NewInterruptResume(InterruptResumeConfig{}, nil)

	// Turn 1
	ir.PreTurn(context.Background(), &TurnRequest{Prompt: "task 1", Metadata: make(map[string]any)})
	cp1 := ir.Interrupt(InterruptUser, "first")

	// Turn 2
	ir.PreTurn(context.Background(), &TurnRequest{Prompt: "task 2", Metadata: make(map[string]any)})
	cp2 := ir.Interrupt(InterruptBudget, "second")

	if cp1.Prompt != "task 1" || cp2.Prompt != "task 2" {
		t.Fatal("each interrupt should capture its turn's prompt")
	}
	if cp1.TurnIndex == cp2.TurnIndex {
		t.Fatal("turn indices should differ")
	}

	// Last checkpoint should be the most recent
	last := ir.LastCheckpoint()
	if last.Prompt != "task 2" {
		t.Fatal("last checkpoint should be from turn 2")
	}
}
