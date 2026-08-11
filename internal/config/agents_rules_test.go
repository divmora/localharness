package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentsRules_Found(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Rules\nKeep docs in sync.\n"), 0644)

	logger := slog.Default()
	result := LoadAgentsRules([]string{dir}, logger)

	if len(result) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(result))
	}
	if result[0].Filename != "AGENTS.md" {
		t.Errorf("expected filename AGENTS.md, got %q", result[0].Filename)
	}
	if result[0].Content != "# Rules\nKeep docs in sync." {
		t.Errorf("unexpected content: %q", result[0].Content)
	}
}

func TestLoadAgentsRules_NotFound(t *testing.T) {
	dir := t.TempDir()

	logger := slog.Default()
	result := LoadAgentsRules([]string{dir}, logger)

	if len(result) != 0 {
		t.Errorf("expected empty when no AGENTS.md, got %d rules", len(result))
	}
}

func TestLoadAgentsRules_AlternateFilename(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".agents.md"), []byte("alternate rules"), 0644)

	logger := slog.Default()
	result := LoadAgentsRules([]string{dir}, logger)

	if len(result) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(result))
	}
	if result[0].Filename != ".agents.md" {
		t.Errorf("expected filename .agents.md, got %q", result[0].Filename)
	}
	if result[0].Content != "alternate rules" {
		t.Errorf("unexpected content: %q", result[0].Content)
	}
}

func TestLoadAgentsRules_PriorityOrder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("primary rules"), 0644)
	os.WriteFile(filepath.Join(dir, ".agents.md"), []byte("alternate rules"), 0644)

	logger := slog.Default()
	result := LoadAgentsRules([]string{dir}, logger)

	if len(result) != 1 {
		t.Fatalf("expected 1 rule (priority winner), got %d", len(result))
	}
	// AGENTS.md should win over .agents.md
	if result[0].Filename != "AGENTS.md" {
		t.Errorf("expected AGENTS.md to take priority, got %q", result[0].Filename)
	}
	if result[0].Content != "primary rules" {
		t.Errorf("expected primary rules content, got %q", result[0].Content)
	}
}

func TestLoadAgentsRules_MultipleWorkspaces(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "AGENTS.md"), []byte("rules from ws1"), 0644)
	os.WriteFile(filepath.Join(dir2, "AGENTS.md"), []byte("rules from ws2"), 0644)

	logger := slog.Default()
	result := LoadAgentsRules([]string{dir1, dir2}, logger)

	if len(result) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(result))
	}
	if result[0].Content != "rules from ws1" {
		t.Error("should include ws1 rules")
	}
	if result[1].Content != "rules from ws2" {
		t.Error("should include ws2 rules")
	}
}

func TestLoadAgentsRules_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("  \n  \n"), 0644) // Whitespace only

	logger := slog.Default()
	result := LoadAgentsRules([]string{dir}, logger)

	if len(result) != 0 {
		t.Errorf("expected empty for whitespace-only AGENTS.md, got %d rules", len(result))
	}
}

func TestLoadAgentsRules_EmptyInput(t *testing.T) {
	logger := slog.Default()
	result := LoadAgentsRules(nil, logger)

	if len(result) != 0 {
		t.Errorf("expected empty for nil input, got %d rules", len(result))
	}
}

