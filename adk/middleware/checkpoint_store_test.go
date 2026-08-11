package middleware

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointStore_SaveAndLatest(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)

	cp := TurnCheckpoint{
		TurnIndex:       1,
		Prompt:          "Fix the bug",
		PartialResponse: "I found the issue",
		TotalTokens:     500,
		StepCount:       3,
		Reason:          InterruptUser,
		ReasonDetail:    "user cancelled",
		Timestamp:       time.Now(),
		Metadata:        map[string]any{"key": "value"},
	}

	store.Save(cp)

	loaded, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest() error: %v", err)
	}

	if loaded.Prompt != "Fix the bug" {
		t.Fatalf("expected prompt, got: %s", loaded.Prompt)
	}
	if loaded.PartialResponse != "I found the issue" {
		t.Fatalf("expected partial response, got: %s", loaded.PartialResponse)
	}
	if loaded.TotalTokens != 500 {
		t.Fatalf("expected 500 tokens, got: %d", loaded.TotalTokens)
	}
	if loaded.StepCount != 3 {
		t.Fatalf("expected 3 steps, got: %d", loaded.StepCount)
	}
	if loaded.Reason != InterruptUser {
		t.Fatalf("expected user reason, got: %s", loaded.Reason)
	}
	if loaded.ReasonDetail != "user cancelled" {
		t.Fatalf("expected detail, got: %s", loaded.ReasonDetail)
	}
}

func TestCheckpointStore_LoadByTurn(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)

	// Save two checkpoints for different turns
	store.Save(TurnCheckpoint{
		TurnIndex: 1,
		Prompt:    "task 1",
		Reason:    InterruptUser,
		Timestamp: time.Now(),
	})
	time.Sleep(10 * time.Millisecond) // ensure different timestamp
	store.Save(TurnCheckpoint{
		TurnIndex: 2,
		Prompt:    "task 2",
		Reason:    InterruptBudget,
		Timestamp: time.Now(),
	})

	cp1, err := store.Load(1)
	if err != nil {
		t.Fatalf("Load(1) error: %v", err)
	}
	if cp1.Prompt != "task 1" {
		t.Fatalf("expected task 1, got: %s", cp1.Prompt)
	}

	cp2, err := store.Load(2)
	if err != nil {
		t.Fatalf("Load(2) error: %v", err)
	}
	if cp2.Prompt != "task 2" {
		t.Fatalf("expected task 2, got: %s", cp2.Prompt)
	}

	// Non-existent turn
	_, err = store.Load(99)
	if err == nil {
		t.Fatal("expected error for non-existent turn")
	}
}

func TestCheckpointStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)

	for i := 1; i <= 3; i++ {
		store.Save(TurnCheckpoint{
			TurnIndex: i,
			Prompt:    "task",
			Reason:    InterruptUser,
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(all))
	}
	if all[0].TurnIndex != 1 || all[2].TurnIndex != 3 {
		t.Fatal("expected ascending order")
	}
}

func TestCheckpointStore_Clear(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)

	store.Save(TurnCheckpoint{
		TurnIndex: 1,
		Prompt:    "test",
		Reason:    InterruptUser,
		Timestamp: time.Now(),
	})

	if store.Count() != 1 {
		t.Fatal("expected 1 checkpoint")
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear() error: %v", err)
	}

	if store.Count() != 0 {
		t.Fatal("expected 0 checkpoints after clear")
	}

	_, err := store.Latest()
	if err == nil {
		t.Fatal("expected error after clear")
	}
}

func TestCheckpointStore_EmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	store := NewCheckpointStore(dir, nil)

	_, err := store.Latest()
	if err == nil {
		t.Fatal("expected error for empty store")
	}

	if store.Count() != 0 {
		t.Fatal("expected 0 count for non-existent dir")
	}
}

func TestCheckpointStore_Dir(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)
	if store.Dir() != dir {
		t.Fatalf("expected %s, got %s", dir, store.Dir())
	}
}

func TestCheckpointStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep", "checkpoints")
	store := NewCheckpointStore(dir, nil)

	store.Save(TurnCheckpoint{
		TurnIndex: 1,
		Prompt:    "test",
		Reason:    InterruptUser,
		Timestamp: time.Now(),
	})

	// Directory should have been created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected directory to be created")
	}

	if store.Count() != 1 {
		t.Fatal("expected 1 checkpoint after save")
	}
}

func TestCheckpointStore_IntegrationWithInterruptResume(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)

	// Wire store.Save as the OnInterrupt callback
	ir := NewInterruptResume(InterruptResumeConfig{
		OnInterrupt:          store.Save,
		ResumePromptTemplate: "Resume: {original_prompt} | Steps: {step_count}",
	}, nil)

	// Simulate a turn
	ir.PreTurn(nil, &TurnRequest{
		Prompt:   "Analyze the codebase",
		Metadata: make(map[string]any),
	})
	ir.ProcessStep(nil, &StepEvent{TextDelta: "Looking at files...", Metadata: make(map[string]any)})

	// Interrupt
	ir.Interrupt(InterruptUser, "user paused")

	// Store should have the checkpoint
	if store.Count() != 1 {
		t.Fatalf("expected 1 checkpoint in store, got %d", store.Count())
	}

	// Load and resume
	cp, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest() error: %v", err)
	}
	if cp.Prompt != "Analyze the codebase" {
		t.Fatalf("expected original prompt, got: %s", cp.Prompt)
	}

	// Build resume prompt
	resume := ir.BuildResumePrompt(*cp)
	if resume != "Resume: Analyze the codebase | Steps: 1" {
		t.Fatalf("unexpected resume prompt: %s", resume)
	}
}

func TestCheckpointStore_CorruptFileSkipped(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir, nil)

	// Write a valid checkpoint
	store.Save(TurnCheckpoint{
		TurnIndex: 1,
		Prompt:    "valid",
		Reason:    InterruptUser,
		Timestamp: time.Now(),
	})

	// Write a corrupt file
	corrupt := filepath.Join(dir, "checkpoint_0002_20260101T000000.json")
	os.WriteFile(corrupt, []byte("{invalid json"), 0o644)

	// List should skip the corrupt file
	all, err := store.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 valid checkpoint, got %d", len(all))
	}
}
