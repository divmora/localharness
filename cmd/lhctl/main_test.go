package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestParseRunFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantSessionID string
		wantModel     string
		wantYolo      bool
		wantDetach    bool
		wantPrompt    string
		wantWsCount   int
	}{
		{
			name:          "default starts new conversation",
			args:          []string{},
			wantSessionID: "",
		},
		{
			name:          "short flag -c resumes latest",
			args:          []string{"-c"},
			wantSessionID: "latest",
		},
		{
			name:          "short flag -c with separate ID",
			args:          []string{"-c", "0192a5b6-7c8d-7ef0-9123"},
			wantSessionID: "0192a5b6-7c8d-7ef0-9123",
		},
		{
			name:          "short flag -c with equals ID",
			args:          []string{"-c=0192a5b6-7c8d-7ef0-9123"},
			wantSessionID: "0192a5b6-7c8d-7ef0-9123",
		},
		{
			name:          "long flag --continue resumes latest",
			args:          []string{"--continue"},
			wantSessionID: "latest",
		},
		{
			name:          "long flag --continue with equals ID",
			args:          []string{"--continue=0192a5b6"},
			wantSessionID: "0192a5b6",
		},
		{
			name:          "long flag --resume resumes latest",
			args:          []string{"--resume"},
			wantSessionID: "latest",
		},
		{
			name:          "long flag --resume with equals ID",
			args:          []string{"--resume=0192a5b6"},
			wantSessionID: "0192a5b6",
		},
		{
			name:          "long flag --conversation with equals ID",
			args:          []string{"--conversation=0192a5b6"},
			wantSessionID: "0192a5b6",
		},
		{
			name:          "combined flags with model, workspace, yolo, detach",
			args:          []string{"--model=gpt-4o", "-w", "/tmp/project", "--yolo", "--detach", "--prompt=Hello", "-c", "0192a5b6"},
			wantSessionID: "0192a5b6",
			wantModel:     "gpt-4o",
			wantYolo:      true,
			wantDetach:    true,
			wantPrompt:    "Hello",
			wantWsCount:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := parseRunFlags(tt.args)
			if f.sessionID != tt.wantSessionID {
				t.Errorf("sessionID = %q, want %q", f.sessionID, tt.wantSessionID)
			}
			if tt.wantModel != "" && f.model != tt.wantModel {
				t.Errorf("model = %q, want %q", f.model, tt.wantModel)
			}
			if tt.wantYolo && !f.yolo {
				t.Errorf("yolo = false, want true")
			}
			if tt.wantDetach && !f.detach {
				t.Errorf("detach = false, want true")
			}
			if tt.wantPrompt != "" && f.prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", f.prompt, tt.wantPrompt)
			}
			if tt.wantWsCount > 0 && len(f.explicitWorkspaces) != tt.wantWsCount {
				t.Errorf("explicitWorkspaces count = %d, want %d", len(f.explicitWorkspaces), tt.wantWsCount)
			}
		})
	}
}

func TestFormatResumeCommand(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		flags     runFlags
		wantCmd   string
	}{
		{
			name:      "basic session id only",
			sessionID: "0192a5b6-7c8d-7ef0-9123-456789abcdef",
			flags:     runFlags{},
			wantCmd:   "lhctl -c 0192a5b6-7c8d-7ef0-9123-456789abcdef",
		},
		{
			name:      "with model, workspace and yolo",
			sessionID: "0192a5b6-7c8d-7ef0-9123-456789abcdef",
			flags: runFlags{
				model:              "gpt-4o",
				explicitWorkspaces: []string{"/Users/dev/myapp"},
				yolo:               true,
			},
			wantCmd: "lhctl -c 0192a5b6-7c8d-7ef0-9123-456789abcdef --model=gpt-4o --workspace=/Users/dev/myapp --yolo",
		},
		{
			name:      "with multiple workspaces",
			sessionID: "conv-1234",
			flags: runFlags{
				explicitWorkspaces: []string{"/dir1", "/dir2"},
			},
			wantCmd: "lhctl -c conv-1234 --workspace=/dir1 --workspace=/dir2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResumeCommand(tt.sessionID, tt.flags)
			if got != tt.wantCmd {
				t.Errorf("formatResumeCommand() = %q, want %q", got, tt.wantCmd)
			}
		})
	}
}

func TestResolveConversationID_And_GetLatest(t *testing.T) {
	tmpDir := t.TempDir()
	convDir := filepath.Join(tmpDir, "conversations")
	if err := os.MkdirAll(convDir, 0755); err != nil {
		t.Fatalf("failed to create temp conversations dir: %v", err)
	}

	// 1. Test when no conversations exist
	_, err := getLatestConversationID(tmpDir)
	if err == nil {
		t.Error("expected error when no conversations exist, got nil")
	}

	// Create test conversation files
	id1 := "0192a5b6-0001-7ef0-9123-000000000001"
	id2 := "0192a5b6-0002-7ef0-9123-000000000002"
	id3 := "0192b999-0003-7ef0-9123-000000000003"

	createConvPB(t, convDir, id1, "2026-01-01T10:00:00Z")
	time.Sleep(10 * time.Millisecond)
	createConvPB(t, convDir, id2, "2026-01-02T12:00:00Z")
	time.Sleep(10 * time.Millisecond)
	createConvPB(t, convDir, id3, "2026-01-03T15:00:00Z") // Latest

	// 2. Test getLatestConversationID
	latest, err := getLatestConversationID(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != id3 {
		t.Errorf("getLatestConversationID = %q, want %q", latest, id3)
	}

	// 3. Test resolveConversationID with "latest", "recent", "last"
	for _, alias := range []string{"latest", "LATEST", "recent", "last"} {
		res, err := resolveConversationID(tmpDir, alias)
		if err != nil {
			t.Errorf("resolveConversationID(%q) error: %v", alias, err)
		}
		if res != id3 {
			t.Errorf("resolveConversationID(%q) = %q, want %q", alias, res, id3)
		}
	}

	// 4. Test exact match
	resExact, err := resolveConversationID(tmpDir, id1)
	if err != nil {
		t.Fatalf("resolveConversationID(exact) error: %v", err)
	}
	if resExact != id1 {
		t.Errorf("resolveConversationID(exact) = %q, want %q", resExact, id1)
	}

	// 5. Test unique prefix match
	resPrefix, err := resolveConversationID(tmpDir, "0192b")
	if err != nil {
		t.Fatalf("resolveConversationID(prefix) error: %v", err)
	}
	if resPrefix != id3 {
		t.Errorf("resolveConversationID(prefix) = %q, want %q", resPrefix, id3)
	}

	// 6. Test ambiguous prefix match
	_, err = resolveConversationID(tmpDir, "0192a")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got %v", err)
	}

	// 7. Test non-existent conversation
	_, err = resolveConversationID(tmpDir, "nonexistent-id")
	if err == nil || !strings.Contains(err.Error(), "no conversation found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

func createConvPB(t *testing.T, convDir, id, updatedAt string) {
	t.Helper()
	state := &pb.ConversationState{
		ConversationId: id,
		CreatedAt:      updatedAt,
		UpdatedAt:      updatedAt,
		Status:         pb.ConversationState_STATUS_ACTIVE,
	}
	data, err := proto.Marshal(state)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	filePath := filepath.Join(convDir, id+".pb")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("write error: %v", err)
	}
}
