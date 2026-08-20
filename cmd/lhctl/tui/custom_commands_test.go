package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCommandMarkdown_WithFrontmatter(t *testing.T) {
	content := `---
description: Perform comprehensive code review
argument_placeholder: "[branch-or-file]"
---
# Code Review Prompt
Please review {{args}} for potential bugs and performance issues.
Focus on $1 first.
`
	desc, placeholder, body := parseCommandMarkdown(content)

	if desc != "Perform comprehensive code review" {
		t.Errorf("expected desc 'Perform comprehensive code review', got %q", desc)
	}
	if placeholder != "[branch-or-file]" {
		t.Errorf("expected placeholder '[branch-or-file]', got %q", placeholder)
	}
	if !strings.Contains(body, "Please review {{args}}") {
		t.Errorf("expected body to contain template text, got %q", body)
	}
}

func TestParseCommandMarkdown_WithoutFrontmatter(t *testing.T) {
	content := `# Security Audit
Please audit the codebase for hardcoded secrets, SQL injection, and insecure dependencies.
`
	desc, placeholder, body := parseCommandMarkdown(content)

	if desc != "Security Audit" {
		t.Errorf("expected desc 'Security Audit', got %q", desc)
	}
	if placeholder != "" {
		t.Errorf("expected empty placeholder, got %q", placeholder)
	}
	if !strings.Contains(body, "Please audit the codebase") {
		t.Errorf("expected body to contain prompt text, got %q", body)
	}
}

func TestCustomCommand_Expand(t *testing.T) {
	cmd := CustomCommand{
		Name:     "test",
		Template: "Run tests for {{args}}. Specifically focus on $1 and $2.",
	}

	expanded := cmd.Expand([]string{"./internal/util", "./internal/config"})
	expected := "Run tests for ./internal/util ./internal/config. Specifically focus on ./internal/util and ./internal/config."
	if expanded != expected {
		t.Errorf("expected %q, got %q", expected, expanded)
	}

	// Test template without placeholders - arguments should be appended
	noPlaceholderCmd := CustomCommand{
		Name:     "review",
		Template: "Please perform a code review.",
	}
	expandedNoPlaceholders := noPlaceholderCmd.Expand([]string{"PR #123"})
	if !strings.Contains(expandedNoPlaceholders, "Please perform a code review.") || !strings.Contains(expandedNoPlaceholders, "PR #123") {
		t.Errorf("expected appended args, got %q", expandedNoPlaceholders)
	}
}

func TestCustomCommandManager_Discovery(t *testing.T) {
	tempDir := t.TempDir()
	commandsDir := filepath.Join(tempDir, ".agents", "commands")
	if err := os.MkdirAll(commandsDir, 0755); err != nil {
		t.Fatalf("failed to create test commands dir: %v", err)
	}

	// Write custom command review.md
	reviewMd := `---
description: Custom workspace review
argument_placeholder: "[target]"
---
Please review target: {{args}}
`
	if err := os.WriteFile(filepath.Join(commandsDir, "review.md"), []byte(reviewMd), 0644); err != nil {
		t.Fatalf("failed to write review.md: %v", err)
	}

	// Write custom command deploy.md
	deployMd := `# Deploy to Staging
Deploy the latest commit to staging environment.
`
	if err := os.WriteFile(filepath.Join(commandsDir, "deploy.md"), []byte(deployMd), 0644); err != nil {
		t.Fatalf("failed to write deploy.md: %v", err)
	}

	mgr := NewCustomCommandManager([]string{tempDir})

	// Test Find
	reviewCmd, ok := mgr.Find("review")
	if !ok {
		t.Fatal("expected to find /review command")
	}
	if reviewCmd.Description != "Custom workspace review" {
		t.Errorf("unexpected description: %s", reviewCmd.Description)
	}
	if !reviewCmd.IsWorkspace {
		t.Error("expected IsWorkspace=true")
	}

	// Test case-insensitivity and slash prefix
	_, okWithSlash := mgr.Find("/Review")
	if !okWithSlash {
		t.Error("expected to find /Review with slash and uppercase")
	}

	deployCmd, okDeploy := mgr.Find("deploy")
	if !okDeploy {
		t.Fatal("expected to find /deploy command")
	}
	if deployCmd.Description != "Deploy to Staging" {
		t.Errorf("unexpected deploy desc: %s", deployCmd.Description)
	}

	// Test Autocomplete integration
	matches := MatchAllSlashCommands("/rev", mgr)
	if len(matches) == 0 {
		t.Fatal("expected autocomplete matches for /rev")
	}
	found := false
	for _, m := range matches {
		if m.Value == "/review" {
			found = true
			if !strings.Contains(m.DisplayText, "[workspace]") {
				t.Errorf("expected [workspace] tag in autocomplete display: %s", m.DisplayText)
			}
		}
	}
	if !found {
		t.Error("expected /review candidate in autocomplete results")
	}

	// Test Help view with custom commands
	helpView := RenderHelpViewWithCustom(80, mgr)
	if !strings.Contains(helpView, "CUSTOM COMMANDS") || !strings.Contains(helpView, "/review") || !strings.Contains(helpView, "/deploy") {
		t.Errorf("expected custom commands in help view: %s", helpView)
	}
}
