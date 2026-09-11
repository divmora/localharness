package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

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
	_ = os.MkdirAll(subDir, 0755)
	file3 := filepath.Join(subDir, "util.go")

	_ = os.WriteFile(file1, []byte("package main"), 0644)
	_ = os.WriteFile(file2, []byte("package main"), 0644)
	_ = os.WriteFile(file3, []byte("package pkg"), 0644)

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
	if !strings.Contains(rendered, "Thought") {
		t.Errorf("expected collapsed thinking block in chat: %s", rendered)
	}
	if !strings.Contains(rendered, "Hello! How can I help?") {
		t.Errorf("expected assistant response in chat: %s", rendered)
	}
	if !strings.Contains(rendered, "Read") || !strings.Contains(rendered, "main.go") {
		t.Errorf("expected semantic tool call badge in chat: %s", rendered)
	}

	// Test expanded thinking mode
	h.SetShowThinking(true)
	renderedExpanded := h.RenderView(s, 80)
	if !strings.Contains(renderedExpanded, "Thinking:") || !strings.Contains(renderedExpanded, "Thinking about how to respond") {
		t.Errorf("expected expanded thinking block in chat: %s", renderedExpanded)
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

	if len(h.items) != 4 {
		t.Fatalf("expected 4 items in chat history (tool call and result merged), got %d", len(h.items))
	}

	if h.items[0].Type != ChatItemUser || h.items[0].Content != "Refactor the database queries" {
		t.Errorf("unexpected item 0: %+v", h.items[0])
	}
	if h.items[1].Type != ChatItemAssistant || h.items[1].Content != "I will check the db files first." {
		t.Errorf("unexpected item 1: %+v", h.items[1])
	}
	if h.items[2].Type != ChatItemToolCall || h.items[2].ToolName != "view_file" || h.items[2].Summary != "1 lines" {
		t.Errorf("unexpected item 2: %+v", h.items[2])
	}
	if h.items[3].Type != ChatItemSystem || h.items[3].Content != "System info message" {
		t.Errorf("unexpected item 3: %+v", h.items[3])
	}

	s := spinner.New()
	rendered := h.RenderView(s, 80)
	if !strings.Contains(rendered, "Refactor the database queries") {
		t.Errorf("expected rendered view to contain user message: %s", rendered)
	}
}

func TestChatHistory_CopyHelpers(t *testing.T) {
	h := NewChatHistory()

	// Initial empty check
	if resp := h.LastAssistantResponse(); resp != "" {
		t.Errorf("expected empty response initially, got %q", resp)
	}
	if code := h.LastCodeBlock(); code != "" {
		t.Errorf("expected empty code block initially, got %q", code)
	}

	h.AddUserMessage("How do I write hello world in Go?")
	h.items = append(h.items, ChatItem{
		Type:      ChatItemAssistant,
		Content:   "Here is how you write hello world in Go:\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, world!\")\n}\n```\n\nRun it with `go run main.go`.",
		Timestamp: time.Now(),
	})

	wantResp := "Here is how you write hello world in Go:\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, world!\")\n}\n```\n\nRun it with `go run main.go`."
	if got := h.LastAssistantResponse(); got != wantResp {
		t.Errorf("LastAssistantResponse = %q, want %q", got, wantResp)
	}

	wantCode := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, world!\")\n}"
	if got := h.LastCodeBlock(); got != wantCode {
		t.Errorf("LastCodeBlock = %q, want %q", got, wantCode)
	}

	transcript := h.FullTranscript()
	if !strings.Contains(transcript, "User: How do I write hello world in Go?") {
		t.Errorf("expected user message in transcript: %s", transcript)
	}
	if !strings.Contains(transcript, "Assistant: Here is how you write hello world") {
		t.Errorf("expected assistant response in transcript: %s", transcript)
	}
}

func TestModel_BracketedPaste(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	// Simulate bracketed paste event
	pastedContent := "def hello():\n    print('world')\n    return True"
	pasteMsg := tea.KeyMsg(tea.Key{
		Type:  tea.KeyRunes,
		Runes: []rune(pastedContent),
		Paste: true,
	})

	updatedM, _ := m.Update(pasteMsg)
	model := updatedM.(Model)

	// Pasted content should be in the textarea intact with newlines
	if model.textarea.Value() != pastedContent {
		t.Errorf("textarea value = %q, want %q", model.textarea.Value(), pastedContent)
	}
	// Height should expand to show multiple lines
	if model.textarea.Height() < 3 {
		t.Errorf("expected textarea height >= 3, got %d", model.textarea.Height())
	}
	// Status should NOT be running because paste did NOT submit the message
	if model.status != "IDLE" {
		t.Errorf("expected status IDLE, got %s", model.status)
	}
}

func TestModel_EnterSubmitsImmediately(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	// Type a prompt
	for _, r := range "hello world" {
		updatedM, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{r}}))
		m = updatedM.(Model)
	}

	// Pressing Enter must immediately submit the message normally
	enterMsg := tea.KeyMsg(tea.Key{Type: tea.KeyEnter})
	updatedM, _ := m.Update(enterMsg)
	m = updatedM.(Model)

	if m.status != "RUNNING" {
		t.Errorf("expected status RUNNING after Enter, got %s", m.status)
	}
	if m.textarea.Value() != "" {
		t.Errorf("expected textarea to be reset, got %q", m.textarea.Value())
	}
	if m.textarea.Height() != 1 {
		t.Errorf("expected textarea height reset to 1, got %d", m.textarea.Height())
	}
}

func TestModel_AltEnter(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	m.textarea.SetValue("line 1")

	// Alt+Enter pressed
	altEnterMsg := tea.KeyMsg(tea.Key{Type: tea.KeyEnter, Alt: true})
	updatedM, _ := m.Update(altEnterMsg)
	m = updatedM.(Model)

	// Should insert newline, NOT submit
	if m.textarea.Value() != "line 1\n" {
		t.Errorf("expected textarea value 'line 1\\n', got %q", m.textarea.Value())
	}
	if m.status != "IDLE" {
		t.Errorf("expected status IDLE, got %s", m.status)
	}
}

func TestModel_CursorPos(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	m.textarea.SetValue("hello world")
	m.textarea.CursorEnd()
	pos := m.cursorPos()
	if pos != len("hello world") {
		t.Errorf("cursorPos = %d, want %d", pos, len("hello world"))
	}

	m.textarea.SetValue("abc\ndef")
	m.textarea.CursorEnd()
	pos2 := m.cursorPos()
	if pos2 != len("abc\ndef") {
		t.Errorf("cursorPos = %d, want %d", pos2, len("abc\ndef"))
	}
}

func TestModel_KeyCtrlY_Copy(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	// 1. Press Ctrl+Y when history is empty
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = newM.(Model)
	rendered := m.history.RenderView(m.spinner, 80)
	if !strings.Contains(rendered, "No assistant response found to copy") {
		t.Errorf("expected empty warning in chat: %s", rendered)
	}

	// 2. Add assistant message
	m.history.items = append(m.history.items, ChatItem{
		Type:      ChatItemAssistant,
		Content:   "Here is the assistant answer.",
		Timestamp: time.Now(),
	})

	// Press Ctrl+Y with response
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = newM.(Model)
	rendered = m.history.RenderView(m.spinner, 80)
	if !strings.Contains(rendered, "Copied last response to clipboard") {
		t.Errorf("expected copied last response message in chat: %s", rendered)
	}
}

func TestModel_RenderConsoleHistory(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	// 1. Empty history should return empty string
	if out := m.RenderConsoleHistory(); out != "" {
		t.Errorf("expected empty console history for fresh session, got %q", out)
	}

	// 2. Add user message and assistant message
	m.history.AddUserMessage("What is Go?")
	m.history.items = append(m.history.items, ChatItem{
		Type:      ChatItemAssistant,
		Content:   "Go is an open source programming language.",
		Timestamp: time.Now(),
	})

	consoleOut := m.RenderConsoleHistory()
	if !strings.Contains(consoleOut, "What is Go?") {
		t.Errorf("expected user prompt in console history output: %s", consoleOut)
	}
	if !strings.Contains(consoleOut, "Go is an open source programming language.") {
		t.Errorf("expected assistant response in console history output: %s", consoleOut)
	}
}

func TestChatHistory_RenderItem(t *testing.T) {
	h := NewChatHistory()
	h.SetWorkspaces([]string{"/workspace"})

	// User message
	userItem := ChatItem{
		Type:    ChatItemUser,
		Content: "Fix bug in auth.go",
	}
	renderedUser := h.RenderItem(userItem, 80)
	if !strings.Contains(renderedUser, "You:") || !strings.Contains(renderedUser, "Fix bug in auth.go") {
		t.Errorf("unexpected rendered user item: %s", renderedUser)
	}

	// Tool call done
	toolItem := ChatItem{
		Type:           ChatItemToolCall,
		ToolName:       "view_file",
		SemanticAction: ActionRead,
		Target:         "/workspace/src/auth.go",
		Summary:        "42 lines",
		Duration:       25 * time.Millisecond,
	}
	renderedTool := h.RenderItem(toolItem, 80)
	if !strings.Contains(renderedTool, "Read") || !strings.Contains(renderedTool, "src/auth.go") || !strings.Contains(renderedTool, "42 lines") {
		t.Errorf("unexpected rendered tool item: %s", renderedTool)
	}

	// Assistant response
	assistantItem := ChatItem{
		Type:    ChatItemAssistant,
		Content: "I have fixed the issue.",
	}
	renderedAsst := h.RenderItem(assistantItem, 80)
	if !strings.Contains(renderedAsst, "Assistant:") || !strings.Contains(renderedAsst, "I have fixed the issue.") {
		t.Errorf("unexpected rendered assistant item: %s", renderedAsst)
	}
}

func TestChatHistory_FormatInitialHistory(t *testing.T) {
	h := NewChatHistory()
	h.AddUserMessage("Initial question")
	h.AddSystemMessage("Session resumed")

	formatted := h.FormatInitialHistory(80)
	if !strings.Contains(formatted, "Initial question") || !strings.Contains(formatted, "Session resumed") {
		t.Errorf("unexpected formatted initial history: %s", formatted)
	}
}

func TestModel_DynamicDockView(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	// 1. Idle dock view: contains textarea prompt and status bar
	idleView := m.View()
	if !strings.Contains(idleView, "❯") {
		t.Errorf("expected prompt in idle view: %s", idleView)
	}
	if !strings.Contains(idleView, "IDLE") {
		t.Errorf("expected IDLE status in idle view: %s", idleView)
	}

	// 2. Running tool dock view: contains live tool spinner line
	m.history.StartToolCall("view_file", `{"path": "main.go"}`)
	m.status = "RUNNING"
	runningView := m.View()
	if !strings.Contains(runningView, "Reading") || !strings.Contains(runningView, "main.go") {
		t.Errorf("expected in-flight reading badge in running view: %s", runningView)
	}

	// 3. Approval active in dock: contains confirmation options
	m.approval = &ActiveApproval{
		RequestID:   "req-1",
		ToolName:    "run_command",
		Description: "make test",
	}
	approvalView := m.View()
	if !strings.Contains(approvalView, "Tool Approval Required") || !strings.Contains(approvalView, "[y] Allow") {
		t.Errorf("expected approval card in dock view: %s", approvalView)
	}
}

func TestModel_SlashCommandsEmitCmd(t *testing.T) {
	m := InitialModel(nil, []string{"."}, false)
	m.width = 80
	m.height = 24
	m.updateDimensions()

	// Test that /help, /tasks, /subagents, /status return a tea.Cmd for scrollback printing
	for _, cmdName := range []string{"help", "tasks", "subagents", "status", "context", "version"} {
		cmd, isCmd := ParseCommand("/" + cmdName)
		if !isCmd {
			t.Fatalf("failed to parse command %s", cmdName)
		}
		teaCmd := m.handleSlashCommand(cmd)
		if teaCmd == nil {
			t.Errorf("expected non-nil tea.Cmd for /%s, got nil", cmdName)
		}
	}
}
