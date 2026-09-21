package tools

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	_ = os.WriteFile(testFile, []byte(content), 0644)

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

func TestViewFile_SourceCodeNotBinary(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	files := map[string]string{
		"schema.proto": `syntax = "proto3";
package example;
message Test {
    string id = 1;
}`,
		"config.yaml": `version: '3'
services:
  app:
    image: golang:1.25`,
		"main.rs": `fn main() {
    println!("Hello, 🌍!");
}`,
		"app.ts": `interface User {
    id: string;
    name: string;
}
export const u: User = { id: "1", name: "Alice" };`,
		"Cargo.toml": `[package]
name = "demo"
version = "0.1.0"
edition = "2021"`,
		"script_no_ext": `#!/bin/bash
echo "running custom runner"
exit 0`,
	}

	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(wsDir, name)
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write file %s: %v", name, err)
			}

			step := &pb.StepUpdate{
				Action: &pb.StepUpdate_ViewFile{
					ViewFile: &pb.ActionViewFile{Path: path},
				},
			}

			if err := reg.Execute(ctx, "view_file", step); err != nil {
				t.Fatalf("view_file failed for %s: %v", name, err)
			}

			vf := step.GetViewFile()
			if vf.IsBinary {
				t.Errorf("file %s was falsely classified as binary", name)
			}
			if !strings.Contains(vf.Content, strings.Split(content, "\n")[0]) {
				t.Errorf("expected content of %s to be returned, got %q", name, vf.Content)
			}
		})
	}
}

func TestViewFile_BinaryFileDetection(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// File with NUL byte
	binPath := filepath.Join(wsDir, "data.bin")
	binData := []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01, 0x02, 0x03}
	if err := os.WriteFile(binPath, binData, 0644); err != nil {
		t.Fatalf("failed to write binary file: %v", err)
	}

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{Path: binPath},
		},
	}
	if err := reg.Execute(ctx, "view_file", step); err != nil {
		t.Fatalf("view_file failed: %v", err)
	}
	vf := step.GetViewFile()
	if !vf.IsBinary {
		t.Errorf("expected data.bin to be classified as binary")
	}
	if !strings.Contains(vf.Content, "[Binary file:") {
		t.Errorf("expected binary file metadata string, got %q", vf.Content)
	}
}

func TestIsBinaryFile(t *testing.T) {
	tests := []struct {
		path     string
		header   []byte
		expected bool
	}{
		{"main.go", []byte("package main\nfunc main() {}\n"), false},
		{"service.proto", []byte("syntax = \"proto3\";\n"), false},
		{"values.yaml", []byte("replicaCount: 1\n"), false},
		{"index.ts", []byte("import { Component } from '@angular/core';\n"), false},
		{"lib.rs", []byte("pub fn run() {}\n"), false},
		{"Dockerfile", []byte("FROM alpine:3.19\n"), false},
		{"Makefile", []byte("all:\n\t@echo hi\n"), false},
		{"run.sh", []byte("#!/bin/sh\necho hi\n"), false},
		{"notes.md", []byte("# Header\nSome markdown text.\n"), false},
		{"image.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), true},
		{"archive.zip", []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"), true},
		{"program.exe", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00"), true},
		{"app.wasm", []byte("\x00asm\x01\x00\x00\x00"), true},
		{"corrupt_go.go", []byte("package\x00main"), true},
		{"unknown_text.xyz", []byte("plain text without standard extension\n"), false},
		{"unknown_bin.xyz", []byte("some data\x00with nulls\n"), true},
	}

	for _, tc := range tests {
		got := isBinaryFile(tc.path, tc.header)
		if got != tc.expected {
			t.Errorf("isBinaryFile(%q) = %v, expected %v", tc.path, got, tc.expected)
		}
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
	_ = os.WriteFile(testFile, []byte(lines), 0644)

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
	_ = os.WriteFile(existingFile, []byte("original"), 0644)

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
	_ = os.WriteFile(existingFile, []byte("original"), 0644)

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
	_ = os.WriteFile(testFile, []byte("hello world\ngoodbye world\n"), 0644)

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
	_ = os.WriteFile(testFile, []byte("alpha\nbeta\ngamma\ndelta\n"), 0644)

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

func TestEditFile_MultiChunkLineShifts(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// 1. Expanding multi-chunk edit with non-unique targets
	// Build a 100-line file with identical "shared_marker" at line 10, line 50, and line 80.
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	lines[9] = "shared_marker"  // line 10 (1-indexed)
	lines[49] = "shared_marker" // line 50 (1-indexed)
	lines[79] = "shared_marker" // line 80 (1-indexed)

	testFile := filepath.Join(wsDir, "line_shifts.txt")
	_ = os.WriteFile(testFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	// Chunk 0 at line 10 adds 25 extra lines (+24 delta).
	// Chunk 1 at line 50 replaces with 1 line.
	// Chunk 2 at line 80 replaces with 1 line.
	expandedReplacement := "REPLACED_CHUNK_0\n"
	for i := 1; i <= 24; i++ {
		expandedReplacement += fmt.Sprintf("extra_line_%d\n", i)
	}
	expandedReplacement = strings.TrimSuffix(expandedReplacement, "\n")

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     8,
						EndLine:       12,
						TargetContent: "shared_marker",
						Replacement:   expandedReplacement,
					},
					{
						StartLine:     48,
						EndLine:       52,
						TargetContent: "shared_marker",
						Replacement:   "REPLACED_CHUNK_1",
					},
					{
						StartLine:     78,
						EndLine:       82,
						TargetContent: "shared_marker",
						Replacement:   "REPLACED_CHUNK_2",
					},
				},
			},
		},
	}

	err := reg.Execute(ctx, "replace_file_content", step)
	if err != nil {
		t.Fatalf("multi-chunk edit with line expansion failed: %v", err)
	}

	resultBytes, _ := os.ReadFile(testFile)
	result := string(resultBytes)

	if !strings.Contains(result, "REPLACED_CHUNK_0") {
		t.Error("expected REPLACED_CHUNK_0 in result")
	}
	if !strings.Contains(result, "REPLACED_CHUNK_1") {
		t.Error("expected REPLACED_CHUNK_1 in result")
	}
	if !strings.Contains(result, "REPLACED_CHUNK_2") {
		t.Error("expected REPLACED_CHUNK_2 in result")
	}
	if strings.Contains(result, "shared_marker") {
		t.Error("all shared_marker instances should have been replaced")
	}

	// 2. Shrinking multi-chunk edit
	shrinkFile := filepath.Join(wsDir, "shrink.txt")
	shrinkLines := make([]string, 60)
	for i := range shrinkLines {
		shrinkLines[i] = fmt.Sprintf("content_%d", i+1)
	}
	_ = os.WriteFile(shrinkFile, []byte(strings.Join(shrinkLines, "\n")+"\n"), 0644)

	// Chunk 0 shrinks lines 10-20 to 1 line (-10 delta).
	// Chunk 1 modifies line 50.
	toDelete := ""
	for i := 10; i <= 20; i++ {
		toDelete += fmt.Sprintf("content_%d\n", i)
	}
	toDelete = strings.TrimSuffix(toDelete, "\n")

	shrinkStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: shrinkFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     10,
						EndLine:       20,
						TargetContent: toDelete,
						Replacement:   "SHRUNK_REGION",
					},
					{
						StartLine:     48,
						EndLine:       52,
						TargetContent: "content_50",
						Replacement:   "CONTENT_FIFTY_UPDATED",
					},
				},
			},
		},
	}

	err = reg.Execute(ctx, "replace_file_content", shrinkStep)
	if err != nil {
		t.Fatalf("multi-chunk edit with line shrink failed: %v", err)
	}

	shrinkResultBytes, _ := os.ReadFile(shrinkFile)
	shrinkResult := string(shrinkResultBytes)
	if !strings.Contains(shrinkResult, "SHRUNK_REGION") {
		t.Error("expected SHRUNK_REGION in shrink result")
	}
	if !strings.Contains(shrinkResult, "CONTENT_FIFTY_UPDATED") {
		t.Error("expected CONTENT_FIFTY_UPDATED in shrink result")
	}
}

func TestEditFileTargetNotFound(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	testFile := filepath.Join(wsDir, "nofind.txt")
	_ = os.WriteFile(testFile, []byte("hello world\n"), 0644)

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
	_ = os.WriteFile(testFile, []byte("foo\nbar\nfoo\n"), 0644)

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
	_ = os.WriteFile(testFile, []byte("foo\nbar\nfoo\n"), 0644)

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
	_ = os.WriteFile(testFile, []byte("content\n"), 0644)

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
	_ = os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4\n"), 0644)

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
	_ = os.WriteFile(testFile, []byte("content\n"), 0644)

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
	_ = os.WriteFile(filepath.Join(wsDir, "file1.txt"), []byte("a"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "file2.go"), []byte("b"), 0644)
	_ = os.MkdirAll(filepath.Join(wsDir, "subdir"), 0755)

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
	_ = os.WriteFile(file, []byte("content"), 0644)

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
	_ = os.WriteFile(filepath.Join(wsDir, "main.go"), []byte("package main"), 0644)
	_ = os.MkdirAll(filepath.Join(wsDir, "pkg"), 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "pkg", "util.go"), []byte("package pkg"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "readme.md"), []byte("# Readme"), 0644)

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
	_ = os.WriteFile(filepath.Join(wsDir, "file1.go"), []byte("func main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "file2.go"), []byte("func test() {\n\tfmt.Println(\"world\")\n}\n"), 0644)

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

func TestRunCommand_DefaultCwdToPrimaryWorkspace(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	// Synchronous command with omitted cwd
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "pwd",
				Cwd:     "", // Omitted cwd
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
	expectedDir, _ := filepath.EvalSymlinks(wsDir)
	actualDir, _ := filepath.EvalSymlinks(strings.TrimSpace(rc.Stdout))
	if actualDir != expectedDir {
		t.Errorf("expected executed cwd %q, got %q", expectedDir, actualDir)
	}
	if rc.Cwd != wsDir {
		t.Errorf("expected rc.Cwd to default to %q, got %q", wsDir, rc.Cwd)
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

func TestListDir_ShallowAndSkipLargeDirs(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create a subdirectory with nested structure
	subDir := filepath.Join(wsDir, "my_package")
	_ = os.MkdirAll(filepath.Join(subDir, "nested", "deeper"), 0755)
	_ = os.WriteFile(filepath.Join(subDir, "file1.txt"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("2"), 0644)
	_ = os.WriteFile(filepath.Join(subDir, "nested", "file3.txt"), []byte("3"), 0644)
	_ = os.WriteFile(filepath.Join(subDir, "nested", "deeper", "file4.txt"), []byte("4"), 0644)

	// Create directories that should be skipped from child counting
	_ = os.MkdirAll(filepath.Join(wsDir, "node_modules", "pkg1"), 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "node_modules", "pkg1", "index.js"), []byte("export default {}"), 0644)

	_ = os.MkdirAll(filepath.Join(wsDir, ".git", "objects"), 0755)
	_ = os.WriteFile(filepath.Join(wsDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)

	_ = os.MkdirAll(filepath.Join(wsDir, "vendor", "mod1"), 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "vendor", "mod1", "lib.go"), []byte("package mod1"), 0644)

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
	entryMap := make(map[string]*pb.DirEntry)
	for _, e := range ld.Entries {
		entryMap[e.Name] = e
	}

	// my_package has 3 immediate items: nested (dir), file1.txt, file2.txt
	// It should NOT count deeper descendants (nested/file3.txt, nested/deeper, etc.)
	if pkgEntry, ok := entryMap["my_package"]; !ok {
		t.Fatal("expected my_package directory entry")
	} else if pkgEntry.ChildCount != 3 {
		t.Errorf("expected shallow child count 3 for my_package, got %d", pkgEntry.ChildCount)
	}

	// node_modules, .git, and vendor must have ChildCount == 0 (strictly skipped)
	if nmEntry, ok := entryMap["node_modules"]; !ok {
		t.Fatal("expected node_modules entry")
	} else if nmEntry.ChildCount != 0 {
		t.Errorf("expected childCount 0 for node_modules, got %d", nmEntry.ChildCount)
	}

	if gitEntry, ok := entryMap[".git"]; !ok {
		t.Fatal("expected .git entry")
	} else if gitEntry.ChildCount != 0 {
		t.Errorf("expected childCount 0 for .git, got %d", gitEntry.ChildCount)
	}

	if vEntry, ok := entryMap["vendor"]; !ok {
		t.Fatal("expected vendor entry")
	} else if vEntry.ChildCount != 0 {
		t.Errorf("expected childCount 0 for vendor, got %d", vEntry.ChildCount)
	}
}

func TestViewFile_LineStreaming(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// 50-line file
	var sb strings.Builder
	for i := 1; i <= 50; i++ {
		sb.WriteString(fmt.Sprintf("line content %d\n", i))
	}
	testFile := filepath.Join(wsDir, "stream_test.txt")
	_ = os.WriteFile(testFile, []byte(sb.String()), 0644)

	// Case 1: Read subset lines 10 to 15
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{
				Path:      testFile,
				StartLine: 10,
				EndLine:   15,
			},
		},
	}
	if err := reg.Execute(ctx, "view_file", step); err != nil {
		t.Fatalf("view_file failed: %v", err)
	}
	vf := step.GetViewFile()
	if vf.TotalLines != 50 {
		t.Errorf("expected 50 total lines, got %d", vf.TotalLines)
	}
	if !strings.Contains(vf.Content, "10: line content 10\n") {
		t.Error("expected line 10 in content")
	}
	if !strings.Contains(vf.Content, "15: line content 15\n") {
		t.Error("expected line 15 in content")
	}
	if strings.Contains(vf.Content, "16: line content 16\n") {
		t.Error("line 16 should not be in content")
	}
	if !strings.Contains(vf.Content, "Showing lines 10-15 of 50 total.") {
		t.Errorf("expected partial content indicator, got %s", vf.Content)
	}

	// Case 2: StartLine beyond EOF
	stepBeyond := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{
				Path:      testFile,
				StartLine: 100,
				EndLine:   150,
			},
		},
	}
	if err := reg.Execute(ctx, "view_file", stepBeyond); err != nil {
		t.Fatalf("view_file beyond EOF failed: %v", err)
	}
	vfBeyond := stepBeyond.GetViewFile()
	if vfBeyond.TotalLines != 50 {
		t.Errorf("expected 50 total lines, got %d", vfBeyond.TotalLines)
	}
	if !strings.Contains(vfBeyond.Content, "File only has 50 lines (requested start_line 100 is beyond end of file).") {
		t.Errorf("unexpected content for beyond EOF read: %s", vfBeyond.Content)
	}
}

func TestEditFile_ByteBufferAndCRLF(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create file with CRLF line endings
	crlfContent := "first line\r\nsecond line\r\nthird line\r\n"
	testFile := filepath.Join(wsDir, "crlf.txt")
	_ = os.WriteFile(testFile, []byte(crlfContent), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     2,
						EndLine:       2,
						TargetContent: "second line",
						Replacement:   "SECOND LINE",
					},
				},
			},
		},
	}

	if err := reg.Execute(ctx, "replace_file_content", step); err != nil {
		t.Fatalf("replace_file_content with CRLF failed: %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}
	expected := "first line\r\nSECOND LINE\r\nthird line\r\n"
	if string(data) != expected {
		t.Errorf("expected CRLF content %q, got %q", expected, string(data))
	}
}

func TestEditFile_Fallbacks(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	var sb strings.Builder
	for i := 1; i <= 60; i++ {
		if i == 35 {
			sb.WriteString("unique target string to replace\n")
		} else {
			sb.WriteString(fmt.Sprintf("filler line %d\n", i))
		}
	}
	testFile := filepath.Join(wsDir, "fallback.txt")
	_ = os.WriteFile(testFile, []byte(sb.String()), 0644)

	// Target is on line 35, but we specify start_line=25, end_line=30
	// Fallback 1 (±20 lines) expands search to lines 5-50 and finds it!
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path: testFile,
				Chunks: []*pb.EditChunk{
					{
						StartLine:     25,
						EndLine:       30,
						TargetContent: "unique target string to replace",
						Replacement:   "successfully replaced via fallback",
					},
				},
			},
		},
	}

	if err := reg.Execute(ctx, "replace_file_content", step); err != nil {
		t.Fatalf("replace_file_content fallback failed: %v", err)
	}

	data, _ := os.ReadFile(testFile)
	if !strings.Contains(string(data), "successfully replaced via fallback\n") {
		t.Errorf("expected target to be replaced via fallback, got %s", string(data))
	}
}

// ─── Issue #37 Tests: Grep search, Find file, and Task manager FD leak fixes ─

func TestGrepSearch_HonorsGitIgnore(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Setup files
	_ = os.WriteFile(filepath.Join(wsDir, "included.go"), []byte("package test\nconst Token = \"MY_SECRET_QUERY\"\n"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "ignored.txt"), []byte("const Token = \"MY_SECRET_QUERY\"\n"), 0644)
	_ = os.MkdirAll(filepath.Join(wsDir, "ignored_dir"), 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "ignored_dir", "sub.go"), []byte("const Token = \"MY_SECRET_QUERY\"\n"), 0644)

	// .gitignore
	_ = os.WriteFile(filepath.Join(wsDir, ".gitignore"), []byte("*.txt\nignored_dir/\n"), 0644)

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_GrepSearch{
			GrepSearch: &pb.ActionGrepSearch{
				Query:        "MY_SECRET_QUERY",
				Path:         wsDir,
				MatchPerLine: true,
			},
		},
	}

	err := reg.Execute(ctx, "grep_search", step)
	if err != nil {
		t.Fatalf("grep_search failed: %v", err)
	}

	sd := step.GetGrepSearch()
	if len(sd.Matches) != 1 {
		t.Fatalf("expected exactly 1 match (included.go), got %d: %v", len(sd.Matches), sd.Matches)
	}
	if !strings.HasSuffix(sd.Matches[0].Filename, "included.go") {
		t.Errorf("expected match to be included.go, got %s", sd.Matches[0].Filename)
	}
}

func TestGrepSearch_NativeSearchNoFDLeak(t *testing.T) {
	wsDir := t.TempDir()
	ctx := context.Background()

	// Create 40 files across subdirectories
	for i := 0; i < 40; i++ {
		dir := filepath.Join(wsDir, fmt.Sprintf("sub%d", i%5))
		_ = os.MkdirAll(dir, 0755)
		content := fmt.Sprintf("file %d content\nmarker_target_string\n", i)
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), []byte(content), 0644)
	}

	sd := &pb.ActionGrepSearch{
		Query:        "marker_target_string",
		Path:         wsDir,
		MatchPerLine: true,
	}

	matches, total, err := nativeSearch(ctx, sd, wsDir, 50)
	if err != nil {
		t.Fatalf("nativeSearch failed: %v", err)
	}
	if total != 40 {
		t.Errorf("expected 40 total matches, got %d", total)
	}
	if len(matches) != 40 {
		t.Errorf("expected 40 matches, got %d", len(matches))
	}
}

func TestNativeSearch_EarlyTermination(t *testing.T) {
	wsDir := t.TempDir()
	ctx := context.Background()

	// Create 30 files with matching lines
	for i := 0; i < 30; i++ {
		content := fmt.Sprintf("file %d content\nearly_termination_marker\n", i)
		_ = os.WriteFile(filepath.Join(wsDir, fmt.Sprintf("file_%02d.txt", i)), []byte(content), 0644)
	}

	sd := &pb.ActionGrepSearch{
		Query:        "early_termination_marker",
		Path:         wsDir,
		MatchPerLine: true,
	}

	maxResults := 5
	matches, total, err := nativeSearch(ctx, sd, wsDir, maxResults)
	if err != nil {
		t.Fatalf("nativeSearch failed: %v", err)
	}
	if len(matches) != maxResults {
		t.Fatalf("expected exactly %d matches capped by maxResults, got %d", maxResults, len(matches))
	}
	if total <= maxResults {
		t.Fatalf("expected total count to exceed maxResults (%d), got %d", maxResults, total)
	}
}

func TestNativeSearch_CaseInsensitiveBytes(t *testing.T) {
	wsDir := t.TempDir()
	ctx := context.Background()

	content := "Line 1: Hello World\nLine 2: hello world\nLine 3: HELLO WORLD\nLine 4: Unrelated\n"
	_ = os.WriteFile(filepath.Join(wsDir, "test.txt"), []byte(content), 0644)

	sd := &pb.ActionGrepSearch{
		Query:           "HeLLo WoRLd",
		Path:            wsDir,
		CaseInsensitive: true,
		MatchPerLine:    true,
	}

	matches, total, err := nativeSearch(ctx, sd, wsDir, 10)
	if err != nil {
		t.Fatalf("nativeSearch failed: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 case-insensitive matches, got %d", total)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches in result, got %d", len(matches))
	}
}

func TestParseRipgrepLine(t *testing.T) {
	// Line mode
	line := "foo/bar.go:42:func TestMethod() {"
	match := parseRipgrepLine(line, true)
	if match == nil {
		t.Fatal("expected non-nil match")
	}
	if match.Filename != "foo/bar.go" {
		t.Errorf("expected filename foo/bar.go, got %s", match.Filename)
	}
	if match.LineNumber != 42 {
		t.Errorf("expected line number 42, got %d", match.LineNumber)
	}
	if match.LineContent != "func TestMethod() {" {
		t.Errorf("expected line content func TestMethod() {, got %s", match.LineContent)
	}

	// Line mode with truncation
	longContent := strings.Repeat("a", 250)
	longLine := "file.go:1:" + longContent
	matchLong := parseRipgrepLine(longLine, true)
	if len(matchLong.LineContent) != maxLineContentLen+3 { // 200 + "..."
		t.Errorf("expected truncated line content len %d, got %d", maxLineContentLen+3, len(matchLong.LineContent))
	}

	// File mode
	fileMatch := parseRipgrepLine("pkg/main.go", false)
	if fileMatch == nil || fileMatch.Filename != "pkg/main.go" {
		t.Errorf("expected pkg/main.go, got %v", fileMatch)
	}

	// Windows path with drive letter (backslash)
	winLine := `C:\project\main.go:42:fmt.Println("hello")`
	winMatch := parseRipgrepLine(winLine, true)
	if winMatch == nil {
		t.Fatal("expected non-nil winMatch")
	}
	if winMatch.Filename != `C:\project\main.go` {
		t.Errorf("expected filename C:\\project\\main.go, got %s", winMatch.Filename)
	}
	if winMatch.LineNumber != 42 {
		t.Errorf("expected line number 42, got %d", winMatch.LineNumber)
	}
	if winMatch.LineContent != `fmt.Println("hello")` {
		t.Errorf("expected line content fmt.Println(\"hello\"), got %s", winMatch.LineContent)
	}

	// Windows path with drive letter (forward slash) and colons in content
	winSlashLine := `D:/project/src/lib.rs:105:pub fn run() -> Result<String, Error> { // key:value`
	winSlashMatch := parseRipgrepLine(winSlashLine, true)
	if winSlashMatch == nil {
		t.Fatal("expected non-nil winSlashMatch")
	}
	if winSlashMatch.Filename != `D:/project/src/lib.rs` {
		t.Errorf("expected filename D:/project/src/lib.rs, got %s", winSlashMatch.Filename)
	}
	if winSlashMatch.LineNumber != 105 {
		t.Errorf("expected line number 105, got %d", winSlashMatch.LineNumber)
	}
	if winSlashMatch.LineContent != `pub fn run() -> Result<String, Error> { // key:value` {
		t.Errorf("expected correct line content, got %s", winSlashMatch.LineContent)
	}

	// Windows path in file mode
	winFileMatch := parseRipgrepLine(`C:\project\main.go`, false)
	if winFileMatch == nil || winFileMatch.Filename != `C:\project\main.go` {
		t.Errorf("expected C:\\project\\main.go, got %v", winFileMatch)
	}
}

func TestNativeFindFile_HonorsGitIgnoreAndMaxResults(t *testing.T) {
	wsDir := t.TempDir()
	ctx := context.Background()

	_ = os.WriteFile(filepath.Join(wsDir, "file1.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "file2.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "file3.go"), []byte("package main"), 0644)
	_ = os.WriteFile(filepath.Join(wsDir, "secret.go"), []byte("package main"), 0644)
	_ = os.MkdirAll(filepath.Join(wsDir, "ignored_dir"), 0755)
	_ = os.WriteFile(filepath.Join(wsDir, "ignored_dir", "file4.go"), []byte("package main"), 0644)

	_ = os.WriteFile(filepath.Join(wsDir, ".gitignore"), []byte("secret.go\nignored_dir/\n"), 0644)

	// Test maxResults early termination
	matches, err := nativeFindFile(ctx, "*.go", wsDir, 2)
	if err != nil {
		t.Fatalf("nativeFindFile failed: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected early exit with exactly 2 matches, got %d", len(matches))
	}

	// Test gitignore honoring without hitting maxResults
	matchesAll, err := nativeFindFile(ctx, "*.go", wsDir, 100)
	if err != nil {
		t.Fatalf("nativeFindFile failed: %v", err)
	}
	if len(matchesAll) != 3 {
		t.Fatalf("expected 3 matches (file1, file2, file3), got %d: %v", len(matchesAll), matchesAll)
	}
	for _, m := range matchesAll {
		if strings.Contains(m, "secret.go") || strings.Contains(m, "ignored_dir") {
			t.Errorf("unexpected ignored file found: %s", m)
		}
	}
}

func TestTaskManager_StdinPipesClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tm := NewTaskManager(logger, 5)
	defer tm.Shutdown()

	ctx := context.Background()
	taskID, _, err := tm.StartBackground(ctx, "sleep 0.1", "", nil, 0, nil)
	if err != nil {
		t.Fatalf("StartBackground failed: %v", err)
	}

	// Wait for task to finish
	tm.mu.RLock()
	task := tm.tasks[taskID]
	tm.mu.RUnlock()

	<-task.done

	// Verify task stdin is closed
	if task.stdin != nil {
		_, writeErr := task.stdin.Write([]byte("test"))
		if writeErr == nil {
			t.Error("expected write to task.stdin to fail after completion")
		}
	}

	// Test persistent terminal stdin closed on Shutdown
	term, err := tm.createTerminal("", nil)
	if err != nil {
		t.Fatalf("createTerminal failed: %v", err)
	}

	tm.Shutdown()
	<-term.done

	if term.stdin != nil {
		_, writeErr := term.stdin.Write([]byte("echo hi\n"))
		if writeErr == nil {
			t.Error("expected write to term.stdin to fail after terminal closed")
		}
	}
}

func TestWithEnvironment_RunCommandAndTaskManager(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)

	ctx := WithEnvironment(context.Background(), map[string]string{
		"ISOLATED_TEST_VAR": "isolated_test_val_123",
	})

	envMap := EnvironmentFromContext(ctx)
	if envMap["ISOLATED_TEST_VAR"] != "isolated_test_val_123" {
		t.Fatalf("expected ISOLATED_TEST_VAR in context env, got %v", envMap)
	}

	// 1. run_command execution inherits context env
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "echo -n $ISOLATED_TEST_VAR",
				Cwd:     wsDir,
			},
		},
	}
	if err := reg.Execute(ctx, "run_command", step); err != nil {
		t.Fatalf("run_command failed: %v", err)
	}
	rc := step.GetRunCommand()
	if rc == nil || !strings.Contains(rc.Stdout, "isolated_test_val_123") {
		t.Errorf("expected stdout to contain isolated_test_val_123, got %q", rc.Stdout)
	}

	// Process-wide env must NOT be mutated
	if val := os.Getenv("ISOLATED_TEST_VAR"); val != "" {
		t.Errorf("expected ISOLATED_TEST_VAR to NOT be set in process-wide env, got %q", val)
	}
}
