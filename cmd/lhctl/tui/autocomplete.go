package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AutocompleteType differentiates between file and slash command completion.
type AutocompleteType int

const (
	AutocompleteFile AutocompleteType = iota
	AutocompleteSlashCommand
)

// SlashCommandDef holds command metadata.
type SlashCommandDef struct {
	Command     string
	Description string
}

// AvailableSlashCommands lists all interactive slash commands.
var AvailableSlashCommands = []SlashCommandDef{
	{"/help", "Show help menu & keyboard shortcuts"},
	{"/mode", "Switch mode (default, accept-edits, plan) [Shift+Tab]"},
	{"/plan", "Create implementation plan before modifying code"},
	{"/pause", "Pause the active agent turn"},
	{"/resume", "Resume execution with optional instructions"},
	{"/model", "View or switch target LLM model"},
	{"/subagents", "View subagent hierarchy & live transcript"},
	{"/workspace", "Manage workspaces (list, add, remove)"},
	{"/yolo", "Toggle YOLO Mode (skip all permission prompts)"},
	{"/status", "Display daemon state, tokens & subagent metrics"},
	{"/compact", "Compact conversation context history"},
	{"/clear", "Clear chat viewport history"},
	{"/detach", "Detach TUI (agents continue in background)"},
	{"/exit", "Exit the TUI session"},
	{"/quit", "Exit the TUI session"},
}




// AutocompleteCandidate represents a single option in the dropdown.
type AutocompleteCandidate struct {
	Value       string
	DisplayText string
}

// FileCompleter manages file discovery and autocompletion across workspaces.
type FileCompleter struct {
	mu         sync.RWMutex
	workspaces []string
	files      []string
	lastScan   int64
}

// NewFileCompleter creates a new file autocompleter.
func NewFileCompleter(workspaces []string) *FileCompleter {
	fc := &FileCompleter{
		workspaces: workspaces,
	}
	fc.Rescan()
	return fc
}

// SetWorkspaces updates the active workspace roots.
func (fc *FileCompleter) SetWorkspaces(workspaces []string) {
	fc.mu.Lock()
	fc.workspaces = workspaces
	fc.mu.Unlock()
	fc.Rescan()
}

// Rescan indexes files across all workspace directories.
func (fc *FileCompleter) Rescan() {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	var indexed []string
	for _, ws := range fc.workspaces {
		absWS, err := filepath.Abs(ws)
		if err != nil {
			continue
		}

		_ = filepath.WalkDir(absWS, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				// Skip common ignore directories
				if strings.HasPrefix(name, ".") && name != "." {
					return filepath.SkipDir
				}
				if name == "node_modules" || name == "vendor" || name == "bin" || name == "dist" || name == "target" {
					return filepath.SkipDir
				}
				return nil
			}

			// Compute relative path from workspace root
			rel, err := filepath.Rel(absWS, path)
			if err == nil && !strings.HasPrefix(rel, ".") {
				indexed = append(indexed, rel)
			}
			return nil
		})
	}

	fc.files = indexed
}

// Match finds matching files for a given query prefix.
func (fc *FileCompleter) Match(query string, maxResults int) []string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if maxResults <= 0 {
		maxResults = 8
	}

	q := strings.ToLower(query)
	var matches []string

	for _, f := range fc.files {
		fLower := strings.ToLower(f)
		if strings.Contains(fLower, q) || strings.Contains(strings.ToLower(filepath.Base(f)), q) {
			matches = append(matches, f)
			if len(matches) >= maxResults {
				break
			}
		}
	}

	return matches
}

// AutocompleteState tracks active dropdown state.
type AutocompleteState struct {
	Active        bool
	Type          AutocompleteType
	Query         string
	Candidates    []AutocompleteCandidate
	SelectedIndex int
	CursorPos     int
}

// RenderAutocomplete renders the completion dropdown.
func RenderAutocomplete(state *AutocompleteState, width int) string {
	if !state.Active || len(state.Candidates) == 0 {
		return ""
	}

	boxWidth := min(width-4, 90)
	if boxWidth < 20 {
		boxWidth = 20
	}

	var items []string
	for i, cand := range state.Candidates {
		style := AutocompleteItem
		prefix := "  "
		if i == state.SelectedIndex {
			style = AutocompleteActiveItem
			prefix = "▶ "
		}
		txt := cand.DisplayText
		if txt == "" {
			txt = cand.Value
		}
		items = append(items, style.Render(prefix+txt))
	}

	content := strings.Join(items, "\n")
	return AutocompleteBoxStyle.Width(boxWidth).Render(content)
}

// DetectFileQuery checks if the input text contains an `@...` query at the cursor.
func DetectFileQuery(text string, pos int) (query string, startPos int, found bool) {
	if pos < 0 || pos > len(text) {
		pos = len(text)
	}

	sub := text[:pos]
	lastAt := strings.LastIndex(sub, "@")
	if lastAt == -1 {
		return "", -1, false
	}

	// Ensure there is no space between @ and cursor
	q := sub[lastAt+1:]
	if strings.Contains(q, " ") || strings.Contains(q, "\n") {
		return "", -1, false
	}

	return q, lastAt, true
}

// DetectSlashCommandQuery checks if input text starts with `/` and is typing a command.
func DetectSlashCommandQuery(text string, pos int) (query string, found bool) {
	if pos < 0 || pos > len(text) {
		pos = len(text)
	}

	sub := text[:pos]
	if strings.HasPrefix(sub, "/") && !strings.Contains(sub, " ") {
		return sub, true
	}
	return "", false
}

// MatchSlashCommands returns matching slash commands for a query.
func MatchSlashCommands(query string) []AutocompleteCandidate {
	q := strings.ToLower(query)
	var matches []AutocompleteCandidate
	for _, sc := range AvailableSlashCommands {
		if q == "/" || strings.HasPrefix(strings.ToLower(sc.Command), q) || strings.Contains(strings.ToLower(sc.Command), q) {
			matches = append(matches, AutocompleteCandidate{
				Value:       sc.Command,
				DisplayText: fmt.Sprintf("%-13s %s", sc.Command, sc.Description),
			})
		}
	}
	return matches
}
