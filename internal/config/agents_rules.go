package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// AgentsRuleFiles are the filenames to search for user rules in workspace roots.
// Searched in priority order (first found wins per directory).
var AgentsRuleFiles = []string{
	"AGENTS.md",
	".agents.md",
	filepath.Join(".agents", "AGENTS.md"),
}

// UserRule represents a single user rule to inject into the prompt.
// It can originate from auto-discovered AGENTS.md files or from SDK injection.
type UserRule struct {
	// Filename is the basename of the rule file (e.g. "AGENTS.md").
	// For ADK-injected rules, this is the label (e.g. "settings.json").
	Filename string
	// Content is the trimmed rule content.
	Content string
	// WorkspaceDir is the source workspace directory (empty for ADK-injected rules).
	// Used for multi-workspace disambiguation in RULE[] tags.
	WorkspaceDir string
}

// LoadAgentsRules looks for AGENTS.md files in workspace roots and returns
// them as structured UserRule values. Each rule preserves the source filename
// so the engine can wrap it with attribution tags.
//
// Returns nil if no rule files are found.
func LoadAgentsRules(workspaceDirs []string, logger *slog.Logger) []UserRule {
	if len(workspaceDirs) == 0 {
		return nil
	}

	var rules []UserRule

	for _, wsDir := range workspaceDirs {
		rule := loadRulesFromDir(wsDir, logger)
		if rule != nil {
			rules = append(rules, *rule)
		}
	}

	return rules
}

// loadRulesFromDir searches for an agents rule file in the given directory.
func loadRulesFromDir(dir string, logger *slog.Logger) *UserRule {
	for _, filename := range AgentsRuleFiles {
		path := filepath.Join(dir, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // File doesn't exist or can't be read
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		logger.Debug("loaded agents rules", "path", path, "bytes", len(data))
		return &UserRule{Filename: filename, Content: content, WorkspaceDir: dir}
	}

	return nil
}
