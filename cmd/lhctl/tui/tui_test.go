package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
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
		{"/teamwork Build auth service", true, "teamwork", []string{"Build", "auth", "service"}},
		{"/team", true, "team", []string{}},
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
	if !strings.Contains(rendered, "[y] Allow selected") || !strings.Contains(rendered, "[c] Allow in conversation") || !strings.Contains(rendered, "[g] Always allow") || !strings.Contains(rendered, "[n] Deny") {
		t.Errorf("expected scoped approval buttons in rendered inline card: %s", rendered)
	}
}

func TestApprovalModalRender_ChainedCommand(t *testing.T) {
	app := &ActiveApproval{
		RequestID:   "req-2",
		ToolName:    "run_command",
		Description: "Run build pipeline",
		ArgsJSON:    `{"command": "go test ./... && go run main.go"}`,
	}
	app.InitSubcommands()

	if len(app.SubCommands) != 2 {
		t.Fatalf("expected 2 sub-commands, got %d", len(app.SubCommands))
	}
	if len(app.ApprovedSubcommands()) != 2 {
		t.Fatalf("expected all 2 approved initially, got %d", len(app.ApprovedSubcommands()))
	}

	rendered := RenderApprovalInline(app, 90)
	if !strings.Contains(rendered, "Chained Sub-commands") {
		t.Errorf("expected chained sub-commands section: %s", rendered)
	}
	if !strings.Contains(rendered, "1. ") || !strings.Contains(rendered, "go test ./...") || !strings.Contains(rendered, "2. ") || !strings.Contains(rendered, "go run main.go") {
		t.Errorf("expected numbered sub-commands: %s", rendered)
	}

	// Toggle second command (go run) to denied
	app.ToggleSubcommand(1)
	if len(app.ApprovedSubcommands()) != 1 || app.ApprovedSubcommands()[0] != "go test ./..." {
		t.Errorf("expected only 'go test ./...' approved, got %v", app.ApprovedSubcommands())
	}
	if len(app.DeniedSubcommands()) != 1 || app.DeniedSubcommands()[0] != "go run main.go" {
		t.Errorf("expected 'go run main.go' denied, got %v", app.DeniedSubcommands())
	}

	renderedToggled := RenderApprovalInline(app, 90)
	if !strings.Contains(renderedToggled, "[✓]") || !strings.Contains(renderedToggled, "[✗]") {
		t.Errorf("expected toggle checkmarks [✓] and [✗]: %s", renderedToggled)
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
		RunningTasks:     3,
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
	if !strings.Contains(rendered, "Tasks: 3") {
		t.Errorf("expected tasks badge in status bar: %s", rendered)
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

func TestChatHistory_LoadFromState(t *testing.T) {
	state := &pb.ConversationState{
		ConversationId: "conv-test-123",
		Messages: []*pb.ConversationMessage{
			{
				Role:    "user",
				Content: "Refactor the database queries",
			},
			{
				Role:    "model",
				Content: "I will check the db files first.",
				ToolCalls: []*pb.ToolCallRecord{
					{
						CallId:   "call_1",
						Name:     "view_file",
						ArgsJson: `{"path": "db/query.go"}`,
					},
				},
			},
			{
				Role: "tool",
				ToolResult: &pb.ToolResultRecord{
					CallId:  "call_1",
					Name:    "view_file",
					Content: "package db\nfunc Query() {}",
					IsError: false,
				},
			},
			{
				Role:    "system",
				Content: "System info message",
			},
		},
	}

	h := NewChatHistory()
	h.LoadFromState(state)

	if len(h.items) != 5 {
		t.Fatalf("expected 5 items in chat history, got %d", len(h.items))
	}

	if h.items[0].Type != ChatItemUser || h.items[0].Content != "Refactor the database queries" {
		t.Errorf("unexpected item 0: %+v", h.items[0])
	}
	if h.items[1].Type != ChatItemAssistant || h.items[1].Content != "I will check the db files first." {
		t.Errorf("unexpected item 1: %+v", h.items[1])
	}
	if h.items[2].Type != ChatItemToolCall || h.items[2].ToolName != "view_file" {
		t.Errorf("unexpected item 2: %+v", h.items[2])
	}
	if h.items[3].Type != ChatItemToolResult || h.items[3].ToolName != "view_file" {
		t.Errorf("unexpected item 3: %+v", h.items[3])
	}
	if h.items[4].Type != ChatItemSystem || h.items[4].Content != "System info message" {
		t.Errorf("unexpected item 4: %+v", h.items[4])
	}

	s := spinner.New()
	rendered := h.RenderView(s, 80)
	if !strings.Contains(rendered, "Refactor the database queries") {
		t.Errorf("expected rendered view to contain user message: %s", rendered)
	}
}


