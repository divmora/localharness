package conversation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/divmora/localharness/internal/config"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	// Verify directories were created
	convDir := filepath.Join(tmpDir, "conversations")
	if _, err := os.Stat(convDir); err != nil {
		t.Errorf("conversations dir should exist: %v", err)
	}

	brainDir := filepath.Join(tmpDir, "brain")
	if _, err := os.Stat(brainDir); err != nil {
		t.Errorf("brain dir should exist: %v", err)
	}
}

func TestCreateConversation(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &pb.HarnessConfig{
		SystemInstructions: "You are a test agent",
	}

	conv, err := mgr.Create(cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if conv.ID == "" {
		t.Error("conversation ID should not be empty")
	}

	// Verify state was initialized
	if conv.State == nil {
		t.Fatal("conversation state should not be nil")
	}
	if conv.State.ConversationId != conv.ID {
		t.Errorf("state ID mismatch: %s vs %s", conv.State.ConversationId, conv.ID)
	}
	if conv.State.Status != pb.ConversationState_STATUS_ACTIVE {
		t.Error("initial status should be ACTIVE")
	}
	if conv.State.HarnessVersion != config.HarnessVersion {
		t.Errorf("unexpected harness version: got %s, want %s", conv.State.HarnessVersion, config.HarnessVersion)
	}
	if conv.State.CreatedAt == "" {
		t.Error("CreatedAt should be set")
	}

	// Verify .pb file was created
	pbPath := filepath.Join(tmpDir, "conversations", conv.ID+".pb")
	if _, err := os.Stat(pbPath); err != nil {
		t.Errorf(".pb file should exist: %v", err)
	}

	// Verify brain directory structure
	if _, err := os.Stat(conv.BrainDir); err != nil {
		t.Errorf("brain dir should exist: %v", err)
	}
	if _, err := os.Stat(conv.StepsDir); err != nil {
		t.Errorf("steps dir should exist: %v", err)
	}
	if _, err := os.Stat(conv.MessagesDir); err != nil {
		t.Errorf("messages dir should exist: %v", err)
	}
	if _, err := os.Stat(conv.LogsDir); err != nil {
		t.Errorf("logs dir should exist: %v", err)
	}
	if _, err := os.Stat(conv.TasksDir); err != nil {
		t.Errorf("tasks dir should exist: %v", err)
	}
	if _, err := os.Stat(conv.ScratchDir); err != nil {
		t.Errorf("scratch dir should exist: %v", err)
	}
}

func TestResumeConversation(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create a conversation first
	cfg := &pb.HarnessConfig{}
	original, err := mgr.Create(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Add some state
	original.AddMessage(&pb.ConversationMessage{Role: "user", Content: "hello"})
	original.SaveState()

	// Resume the conversation
	resumed, err := mgr.Resume(original.ID)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if resumed.ID != original.ID {
		t.Errorf("resumed ID mismatch: %s vs %s", resumed.ID, original.ID)
	}
	if resumed.State.ConversationId != original.ID {
		t.Error("state ConversationId should match")
	}
}

func TestResumeNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = mgr.Resume("nonexistent-uuid")
	if err == nil {
		t.Error("Resume should error for nonexistent conversation")
	}
}

func TestListConversations(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Empty initially
	ids, err := mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 conversations, got %d", len(ids))
	}

	// Create some conversations
	cfg := &pb.HarnessConfig{}
	conv1, _ := mgr.Create(cfg)
	conv2, _ := mgr.Create(cfg)

	ids, err = mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(ids))
	}

	// Verify IDs match
	idSet := map[string]bool{conv1.ID: true, conv2.ID: true}
	for _, id := range ids {
		if !idSet[id] {
			t.Errorf("unexpected conversation ID: %s", id)
		}
	}
}

func TestNextStepIndex(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	idx0 := conv.NextStepIndex()
	idx1 := conv.NextStepIndex()
	idx2 := conv.NextStepIndex()

	if idx0 != 0 {
		t.Errorf("first step index should be 0, got %d", idx0)
	}
	if idx1 != 1 {
		t.Errorf("second step index should be 1, got %d", idx1)
	}
	if idx2 != 2 {
		t.Errorf("third step index should be 2, got %d", idx2)
	}
}

func TestNextTrajectoryID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	traj0 := conv.NextTrajectoryID()
	traj1 := conv.NextTrajectoryID()

	if traj0 != "traj_0" {
		t.Errorf("first trajectory should be 'traj_0', got %q", traj0)
	}
	if traj1 != "traj_1" {
		t.Errorf("second trajectory should be 'traj_1', got %q", traj1)
	}
}

func TestAddAndGetMessages(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	conv.AddMessage(&pb.ConversationMessage{Role: "user", Content: "hello"})
	conv.AddMessage(&pb.ConversationMessage{Role: "model", Content: "hi there"})

	msgs := conv.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("first message mismatch: %v", msgs[0])
	}
	if msgs[1].Role != "model" || msgs[1].Content != "hi there" {
		t.Errorf("second message mismatch: %v", msgs[1])
	}

	// Verify timestamps were set
	for i, msg := range msgs {
		if msg.Timestamp == "" {
			t.Errorf("message %d should have a timestamp", i)
		}
	}
}

func TestAddUsage(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	conv.AddUsage(&pb.UsageMetadata{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	})
	conv.AddUsage(&pb.UsageMetadata{
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
	})

	total := conv.State.TotalUsage
	if total.PromptTokens != 300 {
		t.Errorf("expected 300 prompt tokens, got %d", total.PromptTokens)
	}
	if total.CompletionTokens != 150 {
		t.Errorf("expected 150 completion tokens, got %d", total.CompletionTokens)
	}
	if total.TotalTokens != 450 {
		t.Errorf("expected 450 total tokens, got %d", total.TotalTokens)
	}
}

func TestLogStep(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	entry := &TranscriptJSONEntry{
		StepIndex: 0,
		Source:    "USER_EXPLICIT",
		Type:      "USER_INPUT",
		Status:    "DONE",
		Content:   "test message",
	}

	err := conv.LogStep(entry)
	if err != nil {
		t.Fatalf("LogStep failed: %v", err)
	}

	// Verify transcript file was created and contains valid JSONL
	data, err := os.ReadFile(conv.TranscriptPath)
	if err != nil {
		t.Fatalf("cannot read transcript: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line in transcript, got %d", len(lines))
	}

	var parsed TranscriptJSONEntry
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("invalid JSON in transcript: %v", err)
	}
	if parsed.Source != "USER_EXPLICIT" {
		t.Errorf("parsed source mismatch: %s", parsed.Source)
	}
	if parsed.CreatedAt == "" {
		t.Error("created_at should be set by LogStep")
	}
}

func TestLogStepMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	for i := 0; i < 5; i++ {
		err := conv.LogStep(&TranscriptJSONEntry{
			StepIndex: int32(i),
			Source:    "MODEL",
			Type:      "MODEL_RESPONSE",
		})
		if err != nil {
			t.Fatalf("LogStep %d failed: %v", i, err)
		}
	}

	data, _ := os.ReadFile(conv.TranscriptPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines in transcript, got %d", len(lines))
	}
}

func TestSaveStepContent(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	err := conv.SaveStepContent(0, "# Step 0 Content\n\nThis is the step content.")
	if err != nil {
		t.Fatalf("SaveStepContent failed: %v", err)
	}

	contentPath := filepath.Join(conv.StepsDir, "0", "content.md")
	data, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("cannot read step content: %v", err)
	}
	if !strings.Contains(string(data), "Step 0 Content") {
		t.Error("step content file should contain the saved content")
	}
}

func TestArtifactPath(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	path := conv.ArtifactPath("implementation_plan.md")
	expected := filepath.Join(conv.BrainDir, "implementation_plan.md")
	if path != expected {
		t.Errorf("ArtifactPath mismatch: %s vs %s", path, expected)
	}
}

func TestSaveArtifactMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	meta := &ArtifactMetadata{
		ArtifactType: "implementation_plan",
		Summary:      "Test plan summary",
	}

	err := conv.SaveArtifactMetadata("plan.md", meta)
	if err != nil {
		t.Fatalf("SaveArtifactMetadata failed: %v", err)
	}

	metaPath := filepath.Join(conv.BrainDir, "plan.md.metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("cannot read metadata: %v", err)
	}

	var parsed ArtifactMetadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid metadata JSON: %v", err)
	}
	if parsed.ArtifactType != "implementation_plan" {
		t.Errorf("artifact type mismatch: %s", parsed.ArtifactType)
	}
	if parsed.UpdatedAt == "" {
		t.Error("UpdatedAt should be set")
	}

	// RequestFeedback=false should be omitted from JSON (omitempty)
	if strings.Contains(string(data), "requestFeedback") {
		t.Error("requestFeedback should be omitted when false")
	}
}

func TestSaveArtifactMetadata_RequestFeedback(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	meta := &ArtifactMetadata{
		ArtifactType:    "implementation_plan",
		Summary:         "Refactor auth module",
		RequestFeedback: true,
	}

	err := conv.SaveArtifactMetadata("implementation_plan.md", meta)
	if err != nil {
		t.Fatalf("SaveArtifactMetadata failed: %v", err)
	}

	metaPath := filepath.Join(conv.BrainDir, "implementation_plan.md.metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("cannot read metadata: %v", err)
	}

	var parsed ArtifactMetadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid metadata JSON: %v", err)
	}
	if !parsed.RequestFeedback {
		t.Error("RequestFeedback should be true")
	}
	if parsed.ArtifactType != "implementation_plan" {
		t.Errorf("artifact type mismatch: %s", parsed.ArtifactType)
	}
	if parsed.Summary != "Refactor auth module" {
		t.Errorf("summary mismatch: %s", parsed.Summary)
	}

	// Verify the JSON contains the requestFeedback field
	if !strings.Contains(string(data), `"requestFeedback": true`) {
		t.Error("JSON should contain requestFeedback: true")
	}
}

func TestSaveAndReloadState(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{
		SystemInstructions: "test instructions",
	})

	// Modify state
	conv.AddMessage(&pb.ConversationMessage{Role: "user", Content: "test msg"})
	conv.NextStepIndex()
	conv.NextTrajectoryID()

	// Save
	err := conv.SaveState()
	if err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Reload via Resume
	resumed, err := mgr.Resume(conv.ID)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if resumed.State.StepCount != 1 {
		t.Errorf("expected step count 1, got %d", resumed.State.StepCount)
	}
	if resumed.State.TrajectoryCount != 1 {
		t.Errorf("expected trajectory count 1, got %d", resumed.State.TrajectoryCount)
	}
	if len(resumed.State.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(resumed.State.Messages))
	}
}

func TestSaveAll(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	conv, _ := mgr.Create(&pb.HarnessConfig{})

	// SaveAll should not error
	err := conv.SaveAll()
	if err != nil {
		t.Fatalf("SaveAll failed: %v", err)
	}
}
