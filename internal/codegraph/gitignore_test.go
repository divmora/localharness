package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGitIgnoreMatcher(t *testing.T) {
	tmpDir := t.TempDir()

	gitignoreContent := `
# Comments
*.log
bin/
dist/
/secrets.json
build/output/
!build/output/keep.go
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	matcher := LoadGitIgnore(tmpDir)

	tests := []struct {
		path     string
		isDir    bool
		expected bool
	}{
		{"app.log", false, true},
		{"src/error.log", false, true},
		{"bin", true, true},
		{"bin/localharness", false, true},
		{"dist/bundle.js", false, true},
		{"secrets.json", false, true},
		{"nested/secrets.json", false, false}, // anchored with leading /
		{"build/output", true, true},
		{"build/output/temp.go", false, true},
		{"build/output/keep.go", false, false}, // negated rule
		{"src/main.go", false, false},
	}

	for _, tt := range tests {
		got := matcher.Matches(tt.path, tt.isDir)
		if got != tt.expected {
			t.Errorf("Matches(%q, isDir=%v) = %v, expected %v", tt.path, tt.isDir, got, tt.expected)
		}
	}
}

func TestIndexer_HonorsGitIgnore(t *testing.T) {
	tmpDir := t.TempDir()

	gitignoreContent := `
ignored_dir/
*.ignored.go
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	// Valid file
	if err := os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte("package test\nfunc Valid() {}\n"), 0644); err != nil {
		t.Fatalf("write valid.go: %v", err)
	}

	// Ignored file pattern
	if err := os.WriteFile(filepath.Join(tmpDir, "skip.ignored.go"), []byte("package test\nfunc Skipped() {}\n"), 0644); err != nil {
		t.Fatalf("write skip.ignored.go: %v", err)
	}

	// Ignored directory
	ignoredDir := filepath.Join(tmpDir, "ignored_dir")
	if err := os.MkdirAll(ignoredDir, 0755); err != nil {
		t.Fatalf("mkdir ignored_dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ignoredDir, "nested.go"), []byte("package test\nfunc Nested() {}\n"), 0644); err != nil {
		t.Fatalf("write nested.go: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "codegraph.duckdb")
	store := NewStore(dbPath)
	indexer := NewIndexer(tmpDir, store)

	stats, err := indexer.IndexWorkspace(context.Background(), "main")
	if err != nil {
		t.Fatalf("IndexWorkspace failed: %v", err)
	}

	if stats.TotalFiles != 1 {
		t.Fatalf("expected exactly 1 indexed file (valid.go), got %d", stats.TotalFiles)
	}

	nodes := store.GetBranchNodes("main")
	for _, n := range nodes {
		if n.Name == "Skipped" || n.Name == "Nested" {
			t.Errorf("found node %q that should have been ignored by .gitignore", n.Name)
		}
	}
}
