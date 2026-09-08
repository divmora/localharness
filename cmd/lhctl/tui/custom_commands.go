package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// CustomCommand represents a user-defined slash command loaded from a Markdown template.
type CustomCommand struct {
	Name                string // command identifier without slash (e.g. "review")
	Description         string // human-readable summary for autocomplete & /help
	ArgumentPlaceholder string // optional argument usage hint (e.g. "[path]")
	Template            string // raw prompt template markdown
	SourcePath          string // absolute path to source .md file
	IsWorkspace         bool   // true if discovered in workspace .agents/commands/
}

// Expand interpolates command arguments into the template.
func (c *CustomCommand) Expand(args []string) string {
	if c == nil {
		return ""
	}

	fullArgs := strings.TrimSpace(strings.Join(args, " "))
	expanded := c.Template

	hasSubstitutions := false

	// Replace {{args}}, {{ARGS}}, $*
	for _, placeholder := range []string{"{{args}}", "{{ARGS}}", "$*", "$ARG", "$ARGS"} {
		if strings.Contains(expanded, placeholder) {
			expanded = strings.ReplaceAll(expanded, placeholder, fullArgs)
			hasSubstitutions = true
		}
	}

	// Replace positional parameters: $1, $2, $3 or {{1}}, {{2}}, {{3}}
	for i := 1; i <= 9; i++ {
		posStr := ""
		if i-1 < len(args) {
			posStr = args[i-1]
		}
		p1 := fmt.Sprintf("$%d", i)
		p2 := fmt.Sprintf("{{%d}}", i)
		if strings.Contains(expanded, p1) {
			expanded = strings.ReplaceAll(expanded, p1, posStr)
			hasSubstitutions = true
		}
		if strings.Contains(expanded, p2) {
			expanded = strings.ReplaceAll(expanded, p2, posStr)
			hasSubstitutions = true
		}
	}

	// If arguments were provided but no placeholders were in the template, append them
	if !hasSubstitutions && fullArgs != "" {
		expanded = expanded + "\n\n" + fullArgs
	}

	return strings.TrimSpace(expanded)
}

// CustomCommandManager discovers and manages custom user slash commands.
type CustomCommandManager struct {
	mu         sync.RWMutex
	workspaces []string
	commands   map[string]CustomCommand
}

// NewCustomCommandManager creates a new CustomCommandManager and scans for commands.
func NewCustomCommandManager(workspaces []string) *CustomCommandManager {
	mgr := &CustomCommandManager{
		workspaces: workspaces,
		commands:   make(map[string]CustomCommand),
	}
	mgr.Rescan()
	return mgr
}

// SetWorkspaces updates the workspace list and rescans for commands.
func (m *CustomCommandManager) SetWorkspaces(workspaces []string) {
	m.mu.Lock()
	m.workspaces = workspaces
	m.mu.Unlock()
	m.Rescan()
}

// Rescan scans all workspace and global command directories.
func (m *CustomCommandManager) Rescan() {
	m.mu.Lock()
	defer m.mu.Unlock()

	discovered := make(map[string]CustomCommand)

	// 1. Scan Global Commands: ~/.divmora/commands/ and ~/.divmora/localharness/commands/
	if home, err := os.UserHomeDir(); err == nil {
		globalDirs := []string{
			filepath.Join(home, ".divmora", "commands"),
			filepath.Join(home, ".divmora", "localharness", "commands"),
			filepath.Join(home, ".gemini", "commands"),
		}
		for _, dir := range globalDirs {
			scanDir(dir, false, discovered)
		}
	}

	// 2. Scan Workspace Commands (higher priority, overrides global with same name)
	for _, ws := range m.workspaces {
		absWS, err := filepath.Abs(ws)
		if err != nil {
			absWS = ws
		}
		wsDirs := []string{
			filepath.Join(absWS, ".agents", "commands"),
			filepath.Join(absWS, ".agent", "commands"),
			filepath.Join(absWS, ".divmora", "commands"),
		}
		for _, dir := range wsDirs {
			scanDir(dir, true, discovered)
		}
	}

	m.commands = discovered
}

// Find looks up a custom command by name (case-insensitive).
func (m *CustomCommandManager) Find(name string) (CustomCommand, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/"))
	cmd, ok := m.commands[normalized]
	return cmd, ok
}

// List returns all registered custom commands.
func (m *CustomCommandManager) List() []CustomCommand {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []CustomCommand
	for _, cmd := range m.commands {
		result = append(result, cmd)
	}
	return result
}

func scanDir(dir string, isWorkspace bool, target map[string]CustomCommand) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		cmdName := strings.ToLower(strings.TrimSuffix(entry.Name(), ".md"))
		desc, placeholder, body := parseCommandMarkdown(string(content))

		target[cmdName] = CustomCommand{
			Name:                cmdName,
			Description:         desc,
			ArgumentPlaceholder: placeholder,
			Template:            body,
			SourcePath:          filePath,
			IsWorkspace:         isWorkspace,
		}
	}
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n(.*)$`)

func parseCommandMarkdown(content string) (desc string, placeholder string, body string) {
	trimmed := strings.TrimSpace(content)

	// Check for YAML Frontmatter
	if matches := frontmatterRegex.FindStringSubmatch(trimmed); len(matches) == 3 {
		fmContent := matches[1]
		body = strings.TrimSpace(matches[2])

		for _, line := range strings.Split(fmContent, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "description:") {
				desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				desc = strings.Trim(desc, `"'`)
			} else if strings.HasPrefix(strings.ToLower(line), "argument_placeholder:") || strings.HasPrefix(strings.ToLower(line), "usage:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					placeholder = strings.TrimSpace(parts[1])
					placeholder = strings.Trim(placeholder, `"'`)
				}
			}
		}
		if desc == "" {
			desc = extractFirstHeading(body)
		}
		return desc, placeholder, body
	}

	// No frontmatter: use first heading as description, full content as body
	desc = extractFirstHeading(trimmed)
	if desc == "" {
		desc = "Custom command from " + trimmed
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
	}
	return desc, "", trimmed
}

func extractFirstHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return ""
}
