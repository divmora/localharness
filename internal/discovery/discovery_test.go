package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/divmora/localharness/internal/engine"
)

func TestParseSkillMD_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: run-security-scanner
description: >
  Run the security scanner on source files to detect vulnerabilities. Use this
  skill to scan files for common security issues.
---

# Run Security Scanner
Full instructions here.
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	name, desc, err := parseSkillMD(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "run-security-scanner" {
		t.Errorf("name = %q, want %q", name, "run-security-scanner")
	}
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !contains(desc, "security scanner") {
		t.Errorf("description should contain 'security scanner', got %q", desc)
	}
}

func TestParseSkillMD_InlineDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: simple-skill
description: A simple inline description
---
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	name, desc, err := parseSkillMD(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "simple-skill" {
		t.Errorf("name = %q, want %q", name, "simple-skill")
	}
	if desc != "A simple inline description" {
		t.Errorf("description = %q, want %q", desc, "A simple inline description")
	}
}

func TestParseSkillMD_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
description: No name field
---
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseSkillMD(path)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestParseSkillMD_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `# Just a markdown file
No frontmatter here.
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := parseSkillMD(path)
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseSkillMD_FileNotFound(t *testing.T) {
	_, _, err := parseSkillMD("/nonexistent/SKILL.md")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParsePluginJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	content := `{"name":"securecoder","description":"Security analysis and code remediation.","disabled":false}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	name, desc, disabled, err := parsePluginJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "securecoder" {
		t.Errorf("name = %q, want %q", name, "securecoder")
	}
	if desc != "Security analysis and code remediation." {
		t.Errorf("description = %q", desc)
	}
	if disabled {
		t.Error("should not be disabled")
	}
}

func TestParsePluginJSON_Disabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	content := `{"name":"old-plugin","description":"Deprecated","disabled":true}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	name, _, disabled, err := parsePluginJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "old-plugin" {
		t.Errorf("name = %q", name)
	}
	if !disabled {
		t.Error("should be disabled")
	}
}

func TestParsePluginJSON_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	content := `{"description":"No name"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := parsePluginJSON(path)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestDiscoverSkills(t *testing.T) {
	// Set up directory:
	// skills/
	//   scanner/SKILL.md
	//   linter/SKILL.md
	//   empty-dir/ (no SKILL.md)
	//   not-a-dir.txt

	dir := t.TempDir()

	// scanner skill
	scannerDir := filepath.Join(dir, "scanner")
	os.MkdirAll(scannerDir, 0755)
	os.WriteFile(filepath.Join(scannerDir, "SKILL.md"), []byte(`---
name: scanner
description: Scan for issues
---
`), 0644)

	// linter skill
	linterDir := filepath.Join(dir, "linter")
	os.MkdirAll(linterDir, 0755)
	os.WriteFile(filepath.Join(linterDir, "SKILL.md"), []byte(`---
name: linter
description: Lint code
---
`), 0644)

	// empty directory (no SKILL.md)
	os.MkdirAll(filepath.Join(dir, "empty-dir"), 0755)

	// file (not a directory)
	os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hello"), 0644)

	skills := discoverSkills(dir, nil)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	nameSet := map[string]bool{}
	for _, s := range skills {
		nameSet[s.Name] = true
		if s.SkillPath == "" {
			t.Errorf("skill %q should have SkillPath", s.Name)
		}
	}
	if !nameSet["scanner"] {
		t.Error("should find scanner skill")
	}
	if !nameSet["linter"] {
		t.Error("should find linter skill")
	}
}

func TestDiscoverSkills_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	skills := discoverSkills(dir, nil)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestDiscoverSkills_NonexistentDir(t *testing.T) {
	skills := discoverSkills("/nonexistent/dir", nil)
	if skills != nil {
		t.Error("should return nil for nonexistent directory")
	}
}

func TestDiscoverPlugins(t *testing.T) {
	// Set up directory:
	// plugins/
	//   securecoder/
	//     plugin.json
	//     skills/
	//       scan/SKILL.md
	//   disabled-plugin/
	//     plugin.json (disabled: true)
	//   broken-plugin/
	//     plugin.json (invalid JSON)

	dir := t.TempDir()

	// securecoder plugin with one skill
	secDir := filepath.Join(dir, "securecoder")
	os.MkdirAll(filepath.Join(secDir, "skills", "scan"), 0755)
	os.WriteFile(filepath.Join(secDir, "plugin.json"), []byte(`{"name":"securecoder","description":"Security tools"}`), 0644)
	os.WriteFile(filepath.Join(secDir, "skills", "scan", "SKILL.md"), []byte(`---
name: scan
description: Run scanner
---
`), 0644)

	// disabled plugin
	disabledDir := filepath.Join(dir, "disabled-plugin")
	os.MkdirAll(disabledDir, 0755)
	os.WriteFile(filepath.Join(disabledDir, "plugin.json"), []byte(`{"name":"old","disabled":true}`), 0644)

	// broken plugin (invalid JSON)
	brokenDir := filepath.Join(dir, "broken-plugin")
	os.MkdirAll(brokenDir, 0755)
	os.WriteFile(filepath.Join(brokenDir, "plugin.json"), []byte(`{invalid`), 0644)

	plugins := discoverPlugins(dir, nil)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin (disabled and broken skipped), got %d", len(plugins))
	}

	p := plugins[0]
	if p.Name != "securecoder" {
		t.Errorf("name = %q, want securecoder", p.Name)
	}
	if p.Description != "Security tools" {
		t.Errorf("description = %q", p.Description)
	}
	if p.Path != secDir {
		t.Errorf("path = %q, want %q", p.Path, secDir)
	}
	if len(p.Skills) != 1 {
		t.Fatalf("expected 1 skill in plugin, got %d", len(p.Skills))
	}
	if p.Skills[0].Name != "scan" {
		t.Errorf("skill name = %q, want scan", p.Skills[0].Name)
	}
}

func TestMergeSkills_Deduplication(t *testing.T) {
	sdk := []engine.SkillDef{
		{Name: "scanner", Description: "SDK version", SkillPath: "/sdk/scanner/SKILL.md"},
	}
	workspace := []engine.SkillDef{
		{Name: "scanner", Description: "WS version", SkillPath: "/ws/scanner/SKILL.md"},
		{Name: "linter", Description: "WS linter", SkillPath: "/ws/linter/SKILL.md"},
	}
	global := []engine.SkillDef{
		{Name: "scanner", Description: "Global version", SkillPath: "/global/scanner/SKILL.md"},
		{Name: "linter", Description: "Global linter", SkillPath: "/global/linter/SKILL.md"},
		{Name: "formatter", Description: "Global formatter", SkillPath: "/global/formatter/SKILL.md"},
	}

	result := MergeSkills(sdk, workspace, global)
	if len(result) != 3 {
		t.Fatalf("expected 3 unique skills, got %d", len(result))
	}

	// SDK version of scanner should win
	for _, s := range result {
		if s.Name == "scanner" && s.Description != "SDK version" {
			t.Errorf("SDK scanner should win, got %q", s.Description)
		}
		if s.Name == "linter" && s.Description != "WS linter" {
			t.Errorf("workspace linter should win, got %q", s.Description)
		}
	}
}

func TestMergePlugins_Deduplication(t *testing.T) {
	sdk := []engine.PluginDef{
		{Name: "my-plugin", Description: "SDK"},
	}
	global := []engine.PluginDef{
		{Name: "my-plugin", Description: "Global"},
		{Name: "other", Description: "Other"},
	}

	result := MergePlugins(sdk, global)
	if len(result) != 2 {
		t.Fatalf("expected 2 unique plugins, got %d", len(result))
	}

	for _, p := range result {
		if p.Name == "my-plugin" && p.Description != "SDK" {
			t.Errorf("SDK plugin should win, got %q", p.Description)
		}
	}
}

func TestDiscoverAll(t *testing.T) {
	// Create global + workspace structure
	appDataDir := t.TempDir()
	workspaceDir := t.TempDir()

	// Global skill
	globalSkillDir := filepath.Join(appDataDir, "skills", "global-skill")
	os.MkdirAll(globalSkillDir, 0755)
	os.WriteFile(filepath.Join(globalSkillDir, "SKILL.md"), []byte(`---
name: global-skill
description: Global standalone skill
---
`), 0644)

	// Global plugin
	globalPluginDir := filepath.Join(appDataDir, "plugins", "global-plugin")
	os.MkdirAll(globalPluginDir, 0755)
	os.WriteFile(filepath.Join(globalPluginDir, "plugin.json"), []byte(`{"name":"global-plugin","description":"Global plugin"}`), 0644)

	// Workspace skill (same name as global — should override)
	wsSkillDir := filepath.Join(workspaceDir, ".agents", "skills", "global-skill")
	os.MkdirAll(wsSkillDir, 0755)
	os.WriteFile(filepath.Join(wsSkillDir, "SKILL.md"), []byte(`---
name: global-skill
description: Workspace override
---
`), 0644)

	// Workspace-only skill
	wsOnlyDir := filepath.Join(workspaceDir, ".agents", "skills", "ws-only")
	os.MkdirAll(wsOnlyDir, 0755)
	os.WriteFile(filepath.Join(wsOnlyDir, "SKILL.md"), []byte(`---
name: ws-only
description: Workspace only
---
`), 0644)

	// SDK skills
	adkSkills := []engine.SkillDef{
		{Name: "sdk-skill", Description: "From SDK"},
	}

	skills, plugins := DiscoverAll(appDataDir, []string{workspaceDir}, adkSkills, nil, nil)

	// Should have 3 unique skills: sdk-skill, global-skill (ws override), ws-only
	if len(skills) != 3 {
		t.Fatalf("expected 3 skills, got %d: %+v", len(skills), skills)
	}

	// global-skill should be the workspace version (not global)
	for _, s := range skills {
		if s.Name == "global-skill" && s.Description != "Workspace override" {
			t.Errorf("workspace should override global, got %q", s.Description)
		}
	}

	// Should have 1 plugin
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "global-plugin" {
		t.Errorf("plugin name = %q", plugins[0].Name)
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
