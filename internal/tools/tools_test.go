package tools

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/workspace"
)

// testRegistry creates a Registry with all builtin tools registered against a temp workspace.
func testRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	wsDir := t.TempDir()

	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(wsMgr, logger)
	RegisterBuiltinTools(reg, nil) // default config: all except run_command

	return reg, wsDir
}

// testRegistryWithConfig creates a Registry with the specified builtin tools config.
func testRegistryWithConfig(t *testing.T, cfg *pb.BuiltinToolsConfig) (*Registry, string) {
	t.Helper()
	wsDir := t.TempDir()

	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(wsMgr, logger)
	RegisterBuiltinTools(reg, cfg)

	return reg, wsDir
}

func TestNewRegistry(t *testing.T) {
	logger := slog.Default()
	reg := NewRegistry(nil, logger)

	if reg == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(reg.Schemas()) != 0 {
		t.Error("new registry should have no schemas")
	}
}

func TestRegisterAndHasTool(t *testing.T) {
	logger := slog.Default()
	reg := NewRegistry(nil, logger)

	dummyFn := func(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
		return nil
	}

	reg.Register("test_tool", dummyFn, ToolSchema{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]interface{}{"type": "object"},
	})

	if !reg.HasTool("test_tool") {
		t.Error("HasTool should return true for registered tool")
	}
	if reg.HasTool("nonexistent") {
		t.Error("HasTool should return false for unregistered tool")
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	logger := slog.Default()
	reg := NewRegistry(nil, logger)

	err := reg.Execute(context.Background(), "does_not_exist", &pb.StepUpdate{})
	if err == nil {
		t.Error("Execute should error for unknown tool")
	}
}

func TestRegisterBuiltinToolsDefault(t *testing.T) {
	reg, _ := testRegistry(t)

	// Default config enables all except run_command
	expectedTools := []string{"view_file", "write_to_file", "replace_file_content", "multi_replace_file_content", "list_dir", "grep_search", "find_file", "finish", "schedule", "ask_question"}
	for _, name := range expectedTools {
		if !reg.HasTool(name) {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	if reg.HasTool("run_command") {
		t.Error("run_command should NOT be registered by default")
	}
}

func TestRegisterBuiltinToolsAllEnabled(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{
		ViewFile:   true,
		CreateFile: true,
		EditFile:   true,
		ListDir:    true,
		SearchDir:  true,
		FindFile:   true,
		RunCommand: true,
		Finish:     true,
	}

	reg, _ := testRegistryWithConfig(t, cfg)

	allTools := []string{"view_file", "write_to_file", "replace_file_content", "multi_replace_file_content", "list_dir", "grep_search", "find_file", "run_command", "finish"}
	for _, name := range allTools {
		if !reg.HasTool(name) {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestRegisterBuiltinToolsNoneEnabled(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{} // All false

	reg, _ := testRegistryWithConfig(t, cfg)

	// ask_question + 3 knowledge tools + publish are visible (permission tools are Internal, hidden from LLM)
	if len(reg.Schemas()) != 5 {
		t.Errorf("expected 5 visible tools (ask_question + 3 knowledge + publish; permission tools are internal), got %d", len(reg.Schemas()))
	}
}

func TestSchemasAsJSON(t *testing.T) {
	reg, _ := testRegistry(t)

	schemas := reg.SchemasAsJSON()
	if len(schemas) == 0 {
		t.Error("SchemasAsJSON should return registered tool schemas")
	}

	for _, s := range schemas {
		if _, ok := s["name"]; !ok {
			t.Error("schema missing 'name' key")
		}
		if _, ok := s["description"]; !ok {
			t.Error("schema missing 'description' key")
		}
		if _, ok := s["parameters"]; !ok {
			t.Error("schema missing 'parameters' key")
		}
	}
}

func TestValidatePathWithNilManager(t *testing.T) {
	logger := slog.Default()
	reg := NewRegistry(nil, logger) // nil workspace manager

	path, err := reg.ValidatePath("/any/path")
	if err != nil {
		t.Errorf("ValidatePath with nil manager should not error: %v", err)
	}
	if path != "/any/path" {
		t.Errorf("ValidatePath with nil manager should return input path, got %q", path)
	}
}

func TestGetToolName(t *testing.T) {
	tests := []struct {
		name     string
		step     *pb.StepUpdate
		expected string
	}{
		{
			name:     "view_file action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_ViewFile{ViewFile: &pb.ActionViewFile{}}},
			expected: "view_file",
		},
		{
			name:     "create_file action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_WriteToFile{WriteToFile: &pb.ActionWriteToFile{}}},
			expected: "write_to_file",
		},
		{
			name:     "edit_file action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_ReplaceFileContent{ReplaceFileContent: &pb.ActionReplaceFileContent{}}},
			expected: "replace_file_content",
		},
		{
			name:     "list_dir action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_ListDir{ListDir: &pb.ActionListDir{}}},
			expected: "list_dir",
		},
		{
			name:     "search_dir action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_GrepSearch{GrepSearch: &pb.ActionGrepSearch{}}},
			expected: "grep_search",
		},
		{
			name:     "find_file action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_FindFile{FindFile: &pb.ActionFindFile{}}},
			expected: "find_file",
		},
		{
			name:     "run_command action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_RunCommand{RunCommand: &pb.ActionRunCommand{}}},
			expected: "run_command",
		},
		{
			name:     "finish action",
			step:     &pb.StepUpdate{Action: &pb.StepUpdate_Finish{Finish: &pb.ActionFinish{}}},
			expected: "finish",
		},
		{
			name: "host_tool_call action",
			step: &pb.StepUpdate{Action: &pb.StepUpdate_HostToolCall{
				HostToolCall: &pb.ActionHostToolCall{ToolName: "custom_tool"},
			}},
			expected: "custom_tool",
		},
		{
			name:     "nil action",
			step:     &pb.StepUpdate{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetToolName(tt.step)
			if got != tt.expected {
				t.Errorf("GetToolName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ─── View File Tests ─────────────────────────────────────────────────────

func TestViewFile(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create a test file
	content := "line one\nline two\nline three\nline four\nline five\n"
	testFile := filepath.Join(wsDir, "test.txt")
	os.WriteFile(testFile, []byte(content), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{Path: testFile},
		},
	}

	err := reg.Execute(ctx, "view_file", step)
	if err != nil {
		t.Fatalf("view_file failed: %v", err)
	}

	vf := step.GetViewFile()
	if vf.TotalLines != 5 {
		t.Errorf("expected 5 lines, got %d", vf.TotalLines)
	}
	if vf.IsBinary {
		t.Error("text file should not be marked as binary")
	}
	if vf.Content == "" {
		t.Error("content should not be empty")
	}
}

func TestViewFileWithLineRange(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create a multi-line test file
	var lines string
	for i := 1; i <= 20; i++ {
		lines += "line content here\n"
	}
	testFile := filepath.Join(wsDir, "multiline.txt")
	os.WriteFile(testFile, []byte(lines), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{
				Path:      testFile,
				StartLine: 5,
				EndLine:   10,
			},
		},
	}

	err := reg.Execute(ctx, "view_file", step)
	if err != nil {
		t.Fatalf("view_file failed: %v", err)
	}

	vf := step.GetViewFile()
	if vf.TotalLines != 20 {
		t.Errorf("expected 20 total lines, got %d", vf.TotalLines)
	}
}

func TestViewFileMissingPath(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{Path: ""},
		},
	}

	err := reg.Execute(ctx, "view_file", step)
	if err == nil {
		t.Error("view_file should error with empty path")
	}
}

func TestViewFileNonexistent(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{Path: filepath.Join(wsDir, "nonexistent.txt")},
		},
	}

	err := reg.Execute(ctx, "view_file", step)
	if err == nil {
		t.Error("view_file should error for nonexistent file")
	}
}

func TestViewFileDirectory(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{Path: wsDir},
		},
	}

	err := reg.Execute(ctx, "view_file", step)
	if err == nil {
		t.Error("view_file should error when given a directory")
	}
}

func TestViewFileOutsideWorkspace(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{Path: "/etc/passwd"},
		},
	}

	err := reg.Execute(ctx, "view_file", step)
	if err == nil {
		t.Error("view_file should error for path outside workspace")
	}
}

// ─── Create File Tests ───────────────────────────────────────────────────

func TestCreateFile(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	newFile := filepath.Join(wsDir, "created.txt")
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:    newFile,
				Content: "hello world",
			},
		},
	}

	err := reg.Execute(ctx, "write_to_file", step)
	if err != nil {
		t.Fatalf("create_file failed: %v", err)
	}

	cf := step.GetWriteToFile()
	if !cf.Created {
		t.Error("expected Created to be true")
	}

	// Verify file exists with correct content
	data, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("cannot read created file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("unexpected file content: %q", string(data))
	}
}

func TestCreateFileWithSubdirectories(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	newFile := filepath.Join(wsDir, "deep", "nested", "dir", "file.txt")
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:    newFile,
				Content: "nested content",
			},
		},
	}

	err := reg.Execute(ctx, "write_to_file", step)
	if err != nil {
		t.Fatalf("create_file should create parent directories: %v", err)
	}

	if _, err := os.Stat(newFile); err != nil {
		t.Error("nested file should exist")
	}
}

func TestCreateFileExistsNoOverwrite(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	existingFile := filepath.Join(wsDir, "exists.txt")
	os.WriteFile(existingFile, []byte("original"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:      existingFile,
				Content:   "new content",
				Overwrite: false,
			},
		},
	}

	err := reg.Execute(ctx, "write_to_file", step)
	if err == nil {
		t.Error("create_file should error when file exists and overwrite=false")
	}

	// Verify original content preserved
	data, _ := os.ReadFile(existingFile)
	if string(data) != "original" {
		t.Error("original file content should be preserved")
	}
}

func TestCreateFileExistsWithOverwrite(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	existingFile := filepath.Join(wsDir, "overwrite.txt")
	os.WriteFile(existingFile, []byte("original"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:      existingFile,
				Content:   "new content",
				Overwrite: true,
			},
		},
	}

	err := reg.Execute(ctx, "write_to_file", step)
	if err != nil {
		t.Fatalf("create_file with overwrite should succeed: %v", err)
	}

	data, _ := os.ReadFile(existingFile)
	if string(data) != "new content" {
		t.Errorf("file should have new content, got %q", string(data))
	}
}

func TestCreateFileOutsideWorkspace(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:    "/tmp/should_not_create.txt",
				Content: "evil",
			},
		},
	}

	err := reg.Execute(ctx, "write_to_file", step)
	if err == nil {
		t.Error("create_file should error for path outside workspace")
	}
}

// ─── Edit File Tests ─────────────────────────────────────────────────────

func TestEditFile(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "editable.txt")
	os.WriteFile(testFile, []byte("hello world\ngoodbye world\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     1,
						EndLine:       1,
						TargetContent: "hello world",
						Replacement:   "hello universe",
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}

	ef := step.GetReplaceFileContent()
	if !ef.Success {
		t.Error("edit_file should report success")
	}

	data, _ := os.ReadFile(testFile)
	if got := string(data); got != "hello universe\ngoodbye world\n" {
		t.Errorf("unexpected file content: %q", got)
	}
}

func TestEditFileMultipleChunks(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "multi_edit.txt")
	os.WriteFile(testFile, []byte("alpha\nbeta\ngamma\ndelta\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     1,
						EndLine:       1,
						TargetContent: "alpha",
						Replacement:   "ALPHA",
					},
					{
						StartLine:     3,
						EndLine:       3,
						TargetContent: "gamma",
						Replacement:   "GAMMA",
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}

	data, _ := os.ReadFile(testFile)
	content := string(data)
	if content != "ALPHA\nbeta\nGAMMA\ndelta\n" {
		t.Errorf("unexpected content after multi-chunk edit: %q", content)
	}
}

func TestEditFileTargetNotFound(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "nofind.txt")
	os.WriteFile(testFile, []byte("hello world\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						TargetContent: "does not exist",
						Replacement:   "whatever",
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err == nil {
		t.Error("edit_file should error when target content not found")
	}
}

func TestEditFileMultipleOccurrencesWithoutFlag(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "dups.txt")
	os.WriteFile(testFile, []byte("foo\nbar\nfoo\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						TargetContent: "foo",
						Replacement:   "baz",
						AllowMultiple: false,
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err == nil {
		t.Error("edit_file should error when multiple occurrences found and AllowMultiple=false")
	}
}

func TestEditFileMultipleOccurrencesWithFlag(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "dups2.txt")
	os.WriteFile(testFile, []byte("foo\nbar\nfoo\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						TargetContent: "foo",
						Replacement:   "baz",
						AllowMultiple: true,
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err != nil {
		t.Fatalf("edit_file with AllowMultiple should succeed: %v", err)
	}

	data, _ := os.ReadFile(testFile)
	content := string(data)
	if content != "baz\nbar\nbaz\n" {
		t.Errorf("expected all 'foo' replaced with 'baz', got: %q", content)
	}
}

func TestEditFileNoChunks(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "empty_chunks.txt")
	os.WriteFile(testFile, []byte("content\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path:   testFile,
				Chunks: []*pb.EditChunk{},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err == nil {
		t.Error("edit_file should error with no chunks")
	}
}

// ─── Multi Edit File Tests ───────────────────────────────────────────────

func TestMultiEditFile(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "multi_edit_tool.txt")
	os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     1,
						EndLine:       1,
						TargetContent: "line1",
						Replacement:   "LINE_ONE",
					},
					{
						StartLine:     3,
						EndLine:       3,
						TargetContent: "line3",
						Replacement:   "LINE_THREE",
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "multi_replace_file_content", step)
	if err != nil {
		t.Fatalf("multi_replace_file_content failed: %v", err)
	}

	data, _ := os.ReadFile(testFile)
	if string(data) != "LINE_ONE\nline2\nLINE_THREE\nline4\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestMultiEditFileRequiresMinTwoChunks(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "single_chunk.txt")
	os.WriteFile(testFile, []byte("content\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						TargetContent: "content",
						Replacement:   "new content",
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "multi_replace_file_content", step)
	if err == nil {
		t.Error("multi_replace_file_content should error with fewer than 2 chunks")
	}
}

// ─── List Dir Tests ──────────────────────────────────────────────────────


func TestListDir(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create some files and directories
	os.WriteFile(filepath.Join(wsDir, "file1.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(wsDir, "file2.go"), []byte("b"), 0644)
	os.MkdirAll(filepath.Join(wsDir, "subdir"), 0755)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ListDir{
			ListDir: &pb.ActionListDir{Path: wsDir},
		},
	}

	err := reg.Execute(ctx, "list_dir", step)
	if err != nil {
		t.Fatalf("list_dir failed: %v", err)
	}

	ld := step.GetListDir()
	if len(ld.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(ld.Entries))
	}

	// Directories should come first (due to sorting)
	if len(ld.Entries) > 0 && !ld.Entries[0].IsDir {
		t.Error("directories should be listed first")
	}
}

func TestListDirNotDirectory(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	file := filepath.Join(wsDir, "notadir.txt")
	os.WriteFile(file, []byte("content"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ListDir{
			ListDir: &pb.ActionListDir{Path: file},
		},
	}

	err := reg.Execute(ctx, "list_dir", step)
	if err == nil {
		t.Error("list_dir should error when given a file instead of directory")
	}
}

func TestListDirEmpty(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	emptyDir := t.TempDir()
	// Need a new registry that includes this dir
	wsMgr, _ := workspace.NewManager([]string{emptyDir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emptyReg := NewRegistry(wsMgr, logger)
	RegisterBuiltinTools(emptyReg, nil)
	_ = reg

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ListDir{
			ListDir: &pb.ActionListDir{Path: emptyDir},
		},
	}

	err := emptyReg.Execute(ctx, "list_dir", step)
	if err != nil {
		t.Fatalf("list_dir on empty dir should succeed: %v", err)
	}

	ld := step.GetListDir()
	if len(ld.Entries) != 0 {
		t.Errorf("expected 0 entries for empty dir, got %d", len(ld.Entries))
	}
}

// ─── Find File Tests ─────────────────────────────────────────────────────

func TestFindFile(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create test file structure
	os.WriteFile(filepath.Join(wsDir, "main.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(wsDir, "pkg"), 0755)
	os.WriteFile(filepath.Join(wsDir, "pkg", "util.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(wsDir, "readme.md"), []byte("# Readme"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_FindFile{
			FindFile: &pb.ActionFindFile{
				Pattern: "*.go",
				Path:    wsDir,
			},
		},
	}

	err := reg.Execute(ctx, "find_file", step)
	if err != nil {
		t.Fatalf("find_file failed: %v", err)
	}

	ff := step.GetFindFile()
	if len(ff.Matches) < 2 {
		t.Errorf("expected at least 2 .go files, got %d", len(ff.Matches))
	}
}

func TestFindFileMissingPattern(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_FindFile{
			FindFile: &pb.ActionFindFile{
				Pattern: "",
				Path:    wsDir,
			},
		},
	}

	err := reg.Execute(ctx, "find_file", step)
	if err == nil {
		t.Error("find_file should error with empty pattern")
	}
}

func TestFindFileMissingPath(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_FindFile{
			FindFile: &pb.ActionFindFile{
				Pattern: "*.go",
				Path:    "",
			},
		},
	}

	err := reg.Execute(ctx, "find_file", step)
	if err == nil {
		t.Error("find_file should error with empty path")
	}
}

// ─── Search Dir Tests ────────────────────────────────────────────────────

func TestSearchDir(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create searchable files
	os.WriteFile(filepath.Join(wsDir, "file1.go"), []byte("func main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)
	os.WriteFile(filepath.Join(wsDir, "file2.go"), []byte("func test() {\n\tfmt.Println(\"world\")\n}\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_GrepSearch{
			GrepSearch: &pb.ActionGrepSearch{
				Query:        "Println",
				Path:         wsDir,
				MatchPerLine: true,
			},
		},
	}

	err := reg.Execute(ctx, "grep_search", step)
	if err != nil {
		t.Fatalf("search_dir failed: %v", err)
	}

	sd := step.GetGrepSearch()
	if len(sd.Matches) < 2 {
		t.Errorf("expected at least 2 matches, got %d", len(sd.Matches))
	}
}

func TestSearchDirMissingQuery(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_GrepSearch{
			GrepSearch: &pb.ActionGrepSearch{
				Query: "",
				Path:  wsDir,
			},
		},
	}

	err := reg.Execute(ctx, "grep_search", step)
	if err == nil {
		t.Error("search_dir should error with empty query")
	}
}

// ─── Run Command Tests ──────────────────────────────────────────────────

func TestRunCommand(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "echo hello",
				Cwd:     wsDir,
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("run_command failed: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", rc.ExitCode)
	}
	if rc.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", rc.Stdout)
	}
}

func TestRunCommandMissingCommand(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, _ := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "",
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err == nil {
		t.Error("run_command should error with empty command")
	}
}

func TestRunCommandNonZeroExit(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "exit 42",
				Cwd:     wsDir,
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("run_command should not return error for non-zero exit: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", rc.ExitCode)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:   "sleep 60",
				Cwd:       wsDir,
				TimeoutMs: 100, // 100ms timeout
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("run_command timeout should not return error: %v", err)
	}

	rc := step.GetRunCommand()
	if !rc.TimedOut {
		t.Error("expected TimedOut to be true")
	}
}

// ─── Finish Tests ────────────────────────────────────────────────────────

func TestFinish(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_Finish{
			Finish: &pb.ActionFinish{
				OutputJson: `{"result": "done"}`,
			},
		},
	}

	err := reg.Execute(ctx, "finish", step)
	if err != nil {
		t.Fatalf("finish failed: %v", err)
	}
}

func TestFinishMissingAction(t *testing.T) {
	reg, _ := testRegistry(t)
	ctx := context.Background()

	// Step without Finish action set
	step := &pb.StepUpdate{}

	err := reg.Execute(ctx, "finish", step)
	if err == nil {
		t.Error("finish should error with missing action")
	}
}

// ─── Helper Function Tests ──────────────────────────────────────────────

func TestTruncateForDiff(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForDiff(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForDiff(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		short  bool // whether output should be same as input
	}{
		{"short output", "hello", 100, true},
		{"exact length", "hello", 5, true},
		{"needs truncation", "hello world long text", 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateOutput(tt.input, tt.maxLen)
			if tt.short && got != tt.input {
				t.Errorf("expected %q, got %q", tt.input, got)
			}
			if !tt.short && len(got) <= len(tt.input) {
				// Truncated output includes a suffix, but string length varies
				// Just check it starts with the truncated prefix
				if got[:tt.maxLen] != tt.input[:tt.maxLen] {
					t.Errorf("truncated output should start with first %d bytes", tt.maxLen)
				}
			}
		})
	}
}

func TestIsBinaryExtension(t *testing.T) {
	binaryExts := []string{".png", ".jpg", ".exe", ".zip", ".pdf", ".wasm"}
	textExts := []string{".go", ".py", ".js", ".html", ".css", ".md", ".txt"}

	for _, ext := range binaryExts {
		if !isBinaryExtension(ext) {
			t.Errorf("expected %q to be recognized as binary", ext)
		}
	}

	for _, ext := range textExts {
		if isBinaryExtension(ext) {
			t.Errorf("expected %q to NOT be recognized as binary", ext)
		}
	}
}

func TestMustMarshalSchema(t *testing.T) {
	input := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]string{"type": "string"},
		},
	}

	result := mustMarshalSchema(input)
	if result == nil {
		t.Error("mustMarshalSchema should return non-nil map")
	}
	if result["type"] != "object" {
		t.Error("schema should preserve 'type' field")
	}
}
