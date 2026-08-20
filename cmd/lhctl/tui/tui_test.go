package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)


func TestParseCommand(t *testing.T) {
	tests := []struct {
		input    string
		isCmd    bool
		wantName string
		wantArgs []string
	}{
		{"/help", true, "help", []string{}},
		{"/model gpt-4o", true, "model", []string{"gpt-4o"}},
		{"/workspace add /path/to/dir", true, "workspace", []string{"add", "/path/to/dir"}},
		{"/yolo", true, "yolo", []string{}},
		{"/detach", true, "detach", []string{}},
		{"/exit", true, "exit", []string{}},
		{"/quit", true, "quit", []string{}},
		{"hello world", false, "", nil},
		{"@file.txt", false, "", nil},
	}


	for _, tt := range tests {
		cmd, isCmd := ParseCommand(tt.input)
		if isCmd != tt.isCmd {
			t.Errorf("ParseCommand(%q) isCmd = %v, want %v", tt.input, isCmd, tt.isCmd)
		}
		if isCmd {
			if cmd.Name != tt.wantName {
				t.Errorf("ParseCommand(%q) Name = %q, want %q", tt.input, cmd.Name, tt.wantName)
			}
			if len(cmd.Args) != len(tt.wantArgs) {
				t.Errorf("ParseCommand(%q) Args len = %d, want %d", tt.input, len(cmd.Args), len(tt.wantArgs))
			}
		}
	}
}

func TestFileCompleter(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "main.go")
	file2 := filepath.Join(tmpDir, "handler.go")
	subDir := filepath.Join(tmpDir, "pkg")
	os.MkdirAll(subDir, 0755)
	file3 := filepath.Join(subDir, "util.go")

	os.WriteFile(file1, []byte("package main"), 0644)
	os.WriteFile(file2, []byte("package main"), 0644)
	os.WriteFile(file3, []byte("package pkg"), 0644)

	fc := NewFileCompleter([]string{tmpDir})

	matches := fc.Match("main", 5)
	if len(matches) == 0 || matches[0] != "main.go" {
		t.Errorf("expected match main.go, got %v", matches)
	}

	matchesPkg := fc.Match("util", 5)
	if len(matchesPkg) == 0 || !strings.Contains(matchesPkg[0], "util.go") {
		t.Errorf("expected match util.go, got %v", matchesPkg)
	}

	q, pos, found := DetectFileQuery("Please edit @mai", len("Please edit @mai"))
	if !found || q != "mai" || pos != 12 {
		t.Errorf("DetectFileQuery failed: found=%v, q=%q, pos=%d", found, q, pos)
	}
}

func TestSlashCommandCompleter(t *testing.T) {
	q, found := DetectSlashCommandQuery("/", 1)
	if !found || q != "/" {
		t.Errorf("expected found=true, q='/' for input '/', got found=%v, q=%q", found, q)
	}

	matches := MatchSlashCommands("/")
	if len(matches) < 5 {
		t.Errorf("expected all slash commands to match '/', got %d", len(matches))
	}

	qMod, foundMod := DetectSlashCommandQuery("/mo", 3)
	if !foundMod || qMod != "/mo" {
		t.Errorf("expected found=true, q='/mo', got found=%v, q=%q", foundMod, qMod)
	}

	matchesMod := MatchSlashCommands("/mo")
	if len(matchesMod) < 2 {
		t.Errorf("expected at least 2 matches for '/mo', got %v", matchesMod)
	}

	matchesMode := MatchSlashCommands("/mode")
	if len(matchesMode) == 0 || matchesMode[0].Value != "/mode" {
		t.Errorf("expected /mode match for '/mode', got %v", matchesMode)
	}
}




func TestApprovalModalRender(t *testing.T) {
	app := &ActiveApproval{
		RequestID:   "req-1",
		ToolName:    "replace_file_content",
		Description: "Edit internal/config.go",
		DiffPreview: "--- a/config.go\n+++ b/config.go\n@@ -1,3 +1,3 @@\n-old line\n+new line\n",
	}

	rendered := RenderApprovalModal(app, 80)
	if !strings.Contains(rendered, "replace_file_content") {
		t.Errorf("expected tool name in rendered modal: %s", rendered)
	}
	if !strings.Contains(rendered, "old line") || !strings.Contains(rendered, "new line") {
		t.Errorf("expected diff content in rendered modal: %s", rendered)
	}
	if !strings.Contains(rendered, "[y] Approve") {
		t.Errorf("expected approval buttons in rendered modal: %s", rendered)
	}
}

func TestStatusBarRender(t *testing.T) {
	state := StatusBarState{
		Status:           "STREAMING",
		ModelName:        "gpt-4o",
		PromptTokens:     1500,
		CompletionTokens: 500,
		TotalTokens:      2000,
		RunningSubagents: 2,
		YoloMode:         true,
		WorkspaceCount:   1,
	}

	rendered := RenderStatusBar(state, 120)
	if !strings.Contains(rendered, "STREAMING") {
		t.Errorf("expected STREAMING status badge: %s", rendered)
	}
	if !strings.Contains(rendered, "gpt-4o") {
		t.Errorf("expected model name in status bar: %s", rendered)
	}
	if !strings.Contains(rendered, "YOLO MODE") {
		t.Errorf("expected YOLO MODE in status bar: %s", rendered)
	}
	if !strings.Contains(rendered, "Subagents: 2 running") {
		t.Errorf("expected subagents badge in status bar: %s", rendered)
	}
}

func TestSubagentViewManager(t *testing.T) {
	mgr := NewSubagentViewManager()
	mgr.AddOrUpdate(&SubagentState{
		ConversationID: "sub-1",
		Role:           "Researcher",
		TypeName:       "research",
		State:          "RUNNING",
		Depth:          1,
		StepsExecuted:  3,
	})

	if mgr.RunningCount() != 1 {
		t.Errorf("expected 1 running subagent, got %d", mgr.RunningCount())
	}

	mgr.AppendTranscript("sub-1", "Step 1: grep_search")
	mgr.SelectDrillDown()

	if !mgr.IsDrillDown() {
		t.Error("expected drill down active")
	}

	renderedTranscript := mgr.Render(80, 24)
	if !strings.Contains(renderedTranscript, "Step 1: grep_search") {
		t.Errorf("expected transcript line in render: %s", renderedTranscript)
	}

	mgr.ExitDrillDown()
	if mgr.IsDrillDown() {
		t.Error("expected drill down inactive after exit")
	}
}

func TestChatHistory(t *testing.T) {
	h := NewChatHistory()
	h.AddUserMessage("Hello agent")
	h.AppendThinkingText("Thinking about how to respond")
	h.AppendStreamingText("Hello! How can I help?")
	h.FlushStreaming()

	h.StartToolCall("view_file", "path: main.go")
	h.FinishToolCall("view_file", "file contents", false, "")

	s := spinner.New()
	rendered := h.RenderView(s, 80)

	if !strings.Contains(rendered, "Hello agent") {
		t.Errorf("expected user message in chat: %s", rendered)
	}
	if !strings.Contains(rendered, "Thinking:") {
		t.Errorf("expected thinking block in chat: %s", rendered)
	}
	if !strings.Contains(rendered, "Hello! How can I help?") {
		t.Errorf("expected assistant response in chat: %s", rendered)
	}
	if !strings.Contains(rendered, "view_file") {
		t.Errorf("expected tool call in chat: %s", rendered)
	}
}

func TestAgentModeCycle(t *testing.T) {
	mode := ModeDefault
	if mode.Next() != ModeAcceptEdits {
		t.Errorf("expected ModeDefault.Next() == ModeAcceptEdits, got %v", mode.Next())
	}
	mode = ModeAcceptEdits
	if mode.Next() != ModePlan {
		t.Errorf("expected ModeAcceptEdits.Next() == ModePlan, got %v", mode.Next())
	}
	mode = ModePlan
	if mode.Next() != ModeDefault {
		t.Errorf("expected ModePlan.Next() == ModeDefault, got %v", mode.Next())
	}
}

