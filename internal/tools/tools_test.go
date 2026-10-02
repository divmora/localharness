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

func TestRegistrySchemas_SortedAlphabetically(t *testing.T) {
	logger := slog.Default()
	reg := NewRegistry(nil, logger)

	dummyFn := func(ctx context.Context, step *pb.StepUpdate, r *Registry) error { return nil }

	// Register in intentionally unordered sequence
	names := []string{"zebra", "apple", "banana", "monkey", "cat"}
	for _, n := range names {
		reg.Register(n, dummyFn, ToolSchema{
			Name:        n,
			Description: "desc for " + n,
			Parameters:  map[string]interface{}{"type": "object"},
		})
	}

	schemas := reg.Schemas()
	if len(schemas) != 5 {
		t.Fatalf("expected 5 schemas, got %d", len(schemas))
	}
	expectedOrder := []string{"apple", "banana", "cat", "monkey", "zebra"}
	for i, s := range schemas {
		if s.Name != expectedOrder[i] {
			t.Errorf("schema[%d] = %q, want %q", i, s.Name, expectedOrder[i])
		}
	}

	jsonSchemas := reg.SchemasAsJSON()
	for i, js := range jsonSchemas {
		if js["name"] != expectedOrder[i] {
			t.Errorf("jsonSchema[%d] = %q, want %q", i, js["name"], expectedOrder[i])
		}
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
	expectedTools := []string{"view_file", "write_to_file", "replace_file_content", "finish", "schedule", "ask_question"}
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
		RunCommand: true,
		Finish:     true,
	}

	reg, _ := testRegistryWithConfig(t, cfg)

	allTools := []string{"view_file", "write_to_file", "replace_file_content", "run_command", "finish"}
	for _, name := range allTools {
		if !reg.HasTool(name) {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestRegisterBuiltinToolsNoneEnabled(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{} // All false

	reg, _ := testRegistryWithConfig(t, cfg)

	// ask_question + 4 knowledge tools + publish are visible (permission tools are Internal, hidden from LLM)
	if len(reg.Schemas()) != 6 {
		t.Errorf("expected 6 visible tools (ask_question + 4 knowledge + publish; permission tools are internal), got %d", len(reg.Schemas()))
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
	if !strings.Contains(vf.Content, "Showing lines 10 to 15") {
		t.Errorf("expected Showing lines 10 to 15 in header, got %s", vf.Content)
	}
	if !strings.Contains(vf.Content, "The above content does NOT show the entire file contents.") {
		t.Errorf("expected partial content boundary notice, got %s", vf.Content)
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

// ─── Issue #37 Tests: Task manager FD leak fixes ──────────────────────────

func TestTaskManager_StdinPipesClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tm := NewTaskManager(logger, 5)
	defer tm.Shutdown()

	ctx := context.Background()
	taskID, _, _, _, err := tm.StartBackground(ctx, "sleep 0.1", "", nil, 0, nil)
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

func TestViewFile_ContentOffsetAndTruncation(t *testing.T) {
	reg, wsDir := testRegistry(t)
	ctx := context.Background()

	// Create a large file (> 60 KB)
	var sb strings.Builder
	for i := 1; i <= 600; i++ {
		sb.WriteString(fmt.Sprintf("line %03d: %s\n", i, strings.Repeat("A", 100)))
	}
	largeFile := filepath.Join(wsDir, "large.txt")
	if err := os.WriteFile(largeFile, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("failed to write large file: %v", err)
	}

	// 1. Initial read (offset 0): exceeds 46,080 bytes
	step1 := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{
				Path:      largeFile,
				StartLine: 1,
				EndLine:   600,
			},
		},
	}
	if err := reg.Execute(ctx, "view_file", step1); err != nil {
		t.Fatalf("view_file step 1 failed: %v", err)
	}
	vf1 := step1.GetViewFile()
	if !strings.Contains(vf1.Content, "Content truncated: showing bytes 0-46080") {
		t.Errorf("expected 46KB truncation notice, got:\n%s", vf1.Content[:min(len(vf1.Content), 500)])
	}
	if !strings.Contains(vf1.Content, "ContentOffset=46080") {
		t.Errorf("expected ContentOffset=46080 continuation hint, got:\n%s", vf1.Content[:min(len(vf1.Content), 500)])
	}

	// 2. Paginated read (offset 46080)
	step2 := &pb.StepUpdate{
		Action: &pb.StepUpdate_ViewFile{
			ViewFile: &pb.ActionViewFile{
				Path:          largeFile,
				StartLine:     1,
				EndLine:       600,
				ContentOffset: 46080,
			},
		},
	}
	if err := reg.Execute(ctx, "view_file", step2); err != nil {
		t.Fatalf("view_file step 2 failed: %v", err)
	}
	vf2 := step2.GetViewFile()
	if strings.Contains(vf2.Content, "1: line 001:") {
		t.Errorf("paginated read should not contain line 1, got:\n%s", vf2.Content[:min(len(vf2.Content), 500)])
	}
}

func TestViewFile_Schema(t *testing.T) {
	reg, _ := testRegistry(t)
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "view_file" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected view_file schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{"AbsolutePath", "StartLine", "EndLine", "ContentOffset", "ToolAction", "ToolSummary"}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in view_file schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{"AbsolutePath", "ToolSummary", "ToolAction"}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestRunCommand_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{RunCommand: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "run_command" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected run_command schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"CommandLine", "Cwd", "IsDaemon", "RequestedTerminalID",
		"RunPersistent", "WaitMsBeforeAsync", "ToolAction", "ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in run_command schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{"Cwd", "WaitMsBeforeAsync", "CommandLine", "ToolSummary", "ToolAction"}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestManageTask_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{ManageTask: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "manage_task" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected manage_task schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{"Action", "Input", "TaskId", "ToolAction", "ToolSummary"}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in manage_task schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{"Action", "ToolSummary", "ToolAction"}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestRunCommand_FormattedOutput(t *testing.T) {
	reg, wsDir := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{RunCommand: true})
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "echo 'hello world'",
				Cwd:     wsDir,
			},
		},
	}

	if err := reg.Execute(ctx, "run_command", step); err != nil {
		t.Fatalf("run_command failed: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.FormattedOutput == "" {
		t.Fatal("expected non-empty FormattedOutput")
	}
	if !strings.Contains(rc.FormattedOutput, "The command exited with code 0.") {
		t.Errorf("expected exit code 0 notice, got:\n%s", rc.FormattedOutput)
	}
	if !strings.Contains(rc.FormattedOutput, "hello world") {
		t.Errorf("expected 'hello world' in output, got:\n%s", rc.FormattedOutput)
	}
}

func TestManageTask_FormattedOutput(t *testing.T) {
	reg, wsDir := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	// 1. List with no tasks
	listStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "list",
			},
		},
	}
	if err := reg.Execute(ctx, "manage_task", listStep); err != nil {
		t.Fatalf("manage_task list failed: %v", err)
	}
	mtList := listStep.GetManageTask()
	if !strings.Contains(mtList.FormattedOutput, "No background tasks currently running.") {
		t.Errorf("expected empty tasks notice, got:\n%s", mtList.FormattedOutput)
	}

	// 2. Start a background task
	runStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:           "sleep 10",
				Cwd:               wsDir,
				Background:        true,
				WaitMsBeforeAsync: 100,
			},
		},
	}
	if err := reg.Execute(ctx, "run_command", runStep); err != nil {
		t.Fatalf("run_command background failed: %v", err)
	}
	rc := runStep.GetRunCommand()
	if !strings.Contains(rc.FormattedOutput, "Tool is running as a background task with task id:") {
		t.Errorf("expected background task notice, got:\n%s", rc.FormattedOutput)
	}

	// 3. Status
	statusStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "status",
				TaskId: rc.TaskId,
			},
		},
	}
	if err := reg.Execute(ctx, "manage_task", statusStep); err != nil {
		t.Fatalf("manage_task status failed: %v", err)
	}
	mtStatus := statusStep.GetManageTask()
	if !strings.Contains(mtStatus.FormattedOutput, "Task: "+rc.TaskId) {
		t.Errorf("expected task id in status, got:\n%s", mtStatus.FormattedOutput)
	}

	// 4. Kill
	killStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "kill",
				TaskId: rc.TaskId,
			},
		},
	}
	if err := reg.Execute(ctx, "manage_task", killStep); err != nil {
		t.Fatalf("manage_task kill failed: %v", err)
	}
	mtKill := killStep.GetManageTask()
	if !strings.Contains(mtKill.FormattedOutput, fmt.Sprintf("Task %q cancelled.", rc.TaskId)) {
		t.Errorf("expected cancelled notice, got:\n%s", mtKill.FormattedOutput)
	}
}

func TestSchedule_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{Schedule: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "schedule" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected schedule schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"CronExpression",
		"DurationSeconds",
		"IsDaemon",
		"MaxIterations",
		"Prompt",
		"TimerCondition",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in schedule schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{"Prompt", "ToolSummary", "ToolAction"}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestSchedule_FormattedOutput(t *testing.T) {
	wsDir := t.TempDir()
	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(wsMgr, logger)
	RegisterBuiltinTools(reg, &pb.BuiltinToolsConfig{
		Schedule: true,
	})
	defer reg.Shutdown()

	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_Schedule{
			Schedule: &pb.ActionSchedule{
				DurationSeconds: 10,
				Prompt:          "Check status",
			},
		},
	}
	if err := reg.Execute(ctx, "schedule", step); err != nil {
		t.Fatalf("schedule execute failed: %v", err)
	}

	sched := step.GetSchedule()
	if sched.FormattedOutput == "" {
		t.Fatal("expected non-empty FormattedOutput")
	}
	if !strings.Contains(sched.FormattedOutput, "Scheduled a timer to fire in 10 seconds.") {
		t.Errorf("expected timer schedule message, got:\n%s", sched.FormattedOutput)
	}
	if !strings.Contains(sched.FormattedOutput, sched.TaskId) {
		t.Errorf("expected task id %q in FormattedOutput, got:\n%s", sched.TaskId, sched.FormattedOutput)
	}
}

func TestWriteToFile_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{CreateFile: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "write_to_file" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected write_to_file schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"Append",
		"ArtifactMetadata",
		"CodeContent",
		"Description",
		"Overwrite",
		"TargetFile",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in write_to_file schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{
		"TargetFile",
		"Overwrite",
		"CodeContent",
		"Description",
		"ToolSummary",
		"ToolAction",
	}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestWriteToFile_FormattedOutput(t *testing.T) {
	reg, wsDir := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{CreateFile: true})
	ctx := context.Background()

	target := filepath.Join(wsDir, "test.txt")

	// 1. Create file
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:        target,
				Content:     "line 1\n",
				Description: "Create test.txt",
			},
		},
	}
	if err := reg.Execute(ctx, "write_to_file", step); err != nil {
		t.Fatalf("write_to_file create failed: %v", err)
	}
	wf := step.GetWriteToFile()
	if !strings.Contains(wf.FormattedOutput, fmt.Sprintf("Created file file://%s with requested content.", target)) {
		t.Errorf("expected created message in formatted output, got:\n%s", wf.FormattedOutput)
	}
	if !strings.Contains(wf.FormattedOutput, "If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.") {
		t.Errorf("expected AGY instruction trailer in formatted output, got:\n%s", wf.FormattedOutput)
	}

	// 2. Append to file
	stepAppend := &pb.StepUpdate{
		Action: &pb.StepUpdate_WriteToFile{
			WriteToFile: &pb.ActionWriteToFile{
				Path:        target,
				Content:     "line 2\n",
				Append:      true,
				Description: "Append line 2",
			},
		},
	}
	if err := reg.Execute(ctx, "write_to_file", stepAppend); err != nil {
		t.Fatalf("write_to_file append failed: %v", err)
	}
	wfAppend := stepAppend.GetWriteToFile()
	if !strings.Contains(wfAppend.FormattedOutput, fmt.Sprintf("Appended to file file://%s with requested content.", target)) {
		t.Errorf("expected appended message in formatted output, got:\n%s", wfAppend.FormattedOutput)
	}
}

func TestReplaceFileContent_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{EditFile: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "replace_file_content" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected replace_file_content schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"AllowMultiple",
		"Description",
		"EndLine",
		"Instruction",
		"ReplacementContent",
		"StartLine",
		"TargetContent",
		"TargetFile",
		"TargetLintErrorIds",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in replace_file_content schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{
		"TargetFile",
		"Instruction",
		"Description",
		"AllowMultiple",
		"TargetContent",
		"ReplacementContent",
		"StartLine",
		"EndLine",
		"ToolSummary",
		"ToolAction",
	}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestReplaceFileContent_FormattedOutput(t *testing.T) {
	reg, wsDir := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{EditFile: true})
	ctx := context.Background()

	target := filepath.Join(wsDir, "edit_test.txt")
	if err := os.WriteFile(target, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Rejection of .ipynb files
	stepIpynb := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path:               filepath.Join(wsDir, "notebook.ipynb"),
				TargetContent:      "print(1)",
				ReplacementContent: "print(2)",
			},
		},
	}
	if err := reg.Execute(ctx, "replace_file_content", stepIpynb); err == nil {
		t.Fatal("expected error when attempting to edit .ipynb file")
	}

	// 2. Flat AGY parameters execution and formatted output
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReplaceFileContent{
			ReplaceFileContent: &pb.ActionReplaceFileContent{
				Path:               target,
				Instruction:        "Replace beta with delta",
				Description:        "Update Greek letter",
				TargetContent:      "beta",
				ReplacementContent: "delta",
				StartLine:          1,
				EndLine:            3,
			},
		},
	}
	if err := reg.Execute(ctx, "replace_file_content", step); err != nil {
		t.Fatalf("replace_file_content execute failed: %v", err)
	}

	rfc := step.GetReplaceFileContent()
	if rfc.FormattedOutput == "" {
		t.Fatal("expected non-empty FormattedOutput")
	}
	if !strings.Contains(rfc.FormattedOutput, fmt.Sprintf("The following changes were made by the replace_file_content tool to: %s.", target)) {
		t.Errorf("expected target path in header, got:\n%s", rfc.FormattedOutput)
	}
	if !strings.Contains(rfc.FormattedOutput, "[diff_block_start]") || !strings.Contains(rfc.FormattedOutput, "[diff_block_end]") {
		t.Errorf("expected diff block delimiters, got:\n%s", rfc.FormattedOutput)
	}
	if !strings.Contains(rfc.FormattedOutput, "-beta") || !strings.Contains(rfc.FormattedOutput, "+delta") {
		t.Errorf("expected diff content, got:\n%s", rfc.FormattedOutput)
	}
	if !strings.Contains(rfc.FormattedOutput, "Please note that the above snippet only shows the MODIFIED lines from the last change.") {
		t.Errorf("expected trailer note in formatted output, got:\n%s", rfc.FormattedOutput)
	}
}

func TestGenerateImage_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{GenerateImage: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "generate_image" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected generate_image schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"AspectRatio",
		"ImageName",
		"ImagePaths",
		"Prompt",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in generate_image schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{
		"Prompt",
		"ImageName",
		"ToolSummary",
		"ToolAction",
	}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestGenerateImage_ExecutionAndFormattedOutput(t *testing.T) {
	reg, wsDir := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{GenerateImage: true})
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_GenerateImage{
			GenerateImage: &pb.ActionGenerateImage{
				Prompt:    "A test image of a sunset",
				ImageName: "sunset_test.png",
			},
		},
	}

	if err := reg.Execute(ctx, "generate_image", step); err != nil {
		t.Fatalf("generate_image execute failed: %v", err)
	}

	gi := step.GetGenerateImage()
	if gi.ArtifactPath == "" {
		t.Fatal("expected non-empty ArtifactPath")
	}
	if gi.FormattedOutput == "" {
		t.Fatal("expected non-empty FormattedOutput")
	}
	if !strings.Contains(gi.FormattedOutput, "Created At:") || !strings.Contains(gi.FormattedOutput, "Completed At:") {
		t.Errorf("expected timestamps in formatted output, got:\n%s", gi.FormattedOutput)
	}
	if !strings.Contains(gi.FormattedOutput, "Image generated and saved to: file://") {
		t.Errorf("expected image URI in formatted output, got:\n%s", gi.FormattedOutput)
	}
	// Verify file was written
	if _, err := os.Stat(filepath.Join(wsDir, "sunset_test.png")); err != nil {
		t.Errorf("expected generated image file to exist: %v", err)
	}
}

func TestReadUrlContent_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{WebFetch: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "read_url_content" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected read_url_content schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"Url",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in read_url_content schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{
		"Url",
		"ToolSummary",
		"ToolAction",
	}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestReadUrlContent_FormattedOutput(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{WebFetch: true})
	ctx := context.Background()

	MockFetchFunc = func(url string) (string, string, error) {
		return "Hello from documentation", "text/plain", nil
	}
	defer func() { MockFetchFunc = nil }()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReadUrlContent{
			ReadUrlContent: &pb.ActionReadUrlContent{
				Url: "https://example.com/docs",
			},
		},
	}

	if err := reg.Execute(ctx, "read_url_content", step); err != nil {
		t.Fatalf("read_url_content execute failed: %v", err)
	}

	wf := step.GetReadUrlContent()
	if wf.FormattedOutput == "" {
		t.Fatal("expected non-empty FormattedOutput")
	}
	if !strings.Contains(wf.FormattedOutput, "Created At:") || !strings.Contains(wf.FormattedOutput, "Completed At:") {
		t.Errorf("expected timestamps in formatted output, got:\n%s", wf.FormattedOutput)
	}
	if !strings.Contains(wf.FormattedOutput, "Hello from documentation") {
		t.Errorf("expected content in formatted output, got:\n%s", wf.FormattedOutput)
	}
}

func TestSearchWeb_Schema(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{WebSearch: true})
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "search_web" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected search_web schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"Domain",
		"Query",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in search_web schema", prop)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{
		"Query",
		"ToolSummary",
		"ToolAction",
	}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}

func TestSearchWeb_FormattedOutput(t *testing.T) {
	reg, _ := testRegistryWithConfig(t, &pb.BuiltinToolsConfig{WebSearch: true})
	ctx := context.Background()

	MockSearchFunc = func(query string) ([]*pb.WebSearchResult, error) {
		return []*pb.WebSearchResult{
			{Title: "Go Docs", Url: "https://golang.org", Snippet: "Go is an open source programming language."},
		}, nil
	}
	defer func() { MockSearchFunc = nil }()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_SearchWeb{
			SearchWeb: &pb.ActionSearchWeb{
				Query: "golang",
			},
		},
	}

	if err := reg.Execute(ctx, "search_web", step); err != nil {
		t.Fatalf("search_web execute failed: %v", err)
	}

	ws := step.GetSearchWeb()
	if ws.FormattedOutput == "" {
		t.Fatal("expected non-empty FormattedOutput")
	}
	if !strings.Contains(ws.FormattedOutput, "Created At:") || !strings.Contains(ws.FormattedOutput, "Completed At:") {
		t.Errorf("expected timestamps in formatted output, got:\n%s", ws.FormattedOutput)
	}
	if !strings.Contains(ws.FormattedOutput, "The search for \"golang\" returned 1 results:") {
		t.Errorf("expected result count in formatted output, got:\n%s", ws.FormattedOutput)
	}
	if !strings.Contains(ws.FormattedOutput, "Go Docs") || !strings.Contains(ws.FormattedOutput, "https://golang.org") {
		t.Errorf("expected title and url in formatted output, got:\n%s", ws.FormattedOutput)
	}
}

func TestAskQuestion_Schema(t *testing.T) {
	reg, _ := testRegistry(t)
	var schema *ToolSchema
	for _, s := range reg.Schemas() {
		if s.Name == "ask_question" {
			sCopy := s
			schema = &sCopy
			break
		}
	}
	if schema == nil {
		t.Fatal("expected ask_question schema to be registered")
	}

	props, ok := schema.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	expectedProps := []string{
		"Questions",
		"ToolAction",
		"ToolSummary",
	}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q in ask_question schema", prop)
		}
	}

	// Verify nested Questions item schema
	questionsProp, ok := props["Questions"].(map[string]interface{})
	if !ok {
		t.Fatal("expected Questions property map in schema")
	}
	itemsMap, ok := questionsProp["items"].(map[string]interface{})
	if !ok {
		t.Fatal("expected items map in Questions property")
	}
	itemProps, ok := itemsMap["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected item properties in Questions")
	}
	for _, qp := range []string{"Question", "Options", "IsMultiSelect"} {
		if _, exists := itemProps[qp]; !exists {
			t.Errorf("expected nested property %q in Questions item schema", qp)
		}
	}

	req, ok := schema.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required slice in schema")
	}
	expectedReq := []string{
		"ToolSummary",
		"ToolAction",
	}
	if len(req) != len(expectedReq) {
		t.Fatalf("expected required len %d, got %d (%v)", len(expectedReq), len(req), req)
	}
	for i, r := range expectedReq {
		if req[i] != r {
			t.Errorf("expected required[%d]=%q, got %q", i, r, req[i])
		}
	}
}
