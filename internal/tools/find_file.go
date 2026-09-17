package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/util"
)

func registerFindFile(r *Registry) {
	r.Register("find_file", executeFindFile, ToolSchema{
		Group: ToolGroupRead,
		Name:  "find_file",
		Description: "Find files by name or glob pattern within a directory tree. " +
			"Use this instead of run_command with find or fd for locating files. " +
			"Skips hidden directories and common build directories.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{"type": "string", "description": "File name or glob pattern (e.g., '*.go', 'main.py')"},
				"path":    map[string]interface{}{"type": "string", "description": "Absolute path to directory to search in"},
			},
			"required": []string{"pattern", "path"},
		},
	})
}

func executeFindFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	ff := step.GetFindFile()
	if ff == nil {
		return fmt.Errorf("find_file: missing action")
	}

	if ff.Pattern == "" {
		return fmt.Errorf("find_file: pattern is required")
	}

	searchPath := ff.Path
	if searchPath == "" {
		return fmt.Errorf("find_file: path is required")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(searchPath)
	if err != nil {
		return fmt.Errorf("find_file: %w", err)
	}
	searchPath = validPath
	ff.Path = searchPath

	maxResults := 100
	gitignore := util.LoadGitIgnore(searchPath)

	// Try system `find` first, fall back to Go-native walk
	matches, err := trySystemFind(ctx, ff.Pattern, searchPath)
	if err != nil {
		r.Logger().Debug("system find not available, falling back to native", "error", err)
		matches, err = nativeFindFile(ctx, ff.Pattern, searchPath, maxResults)
		if err != nil {
			return fmt.Errorf("find_file: %w", err)
		}
	} else {
		// Filter system find results through gitignore
		var filtered []string
		for _, m := range matches {
			rel, relErr := filepath.Rel(searchPath, m)
			if relErr == nil && gitignore.Matches(rel, false) {
				continue
			}
			filtered = append(filtered, m)
			if len(filtered) >= maxResults {
				break
			}
		}
		matches = filtered
	}

	// Cap results
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	ff.Matches = matches
	return nil
}

func trySystemFind(ctx context.Context, pattern, searchPath string) ([]string, error) {
	findPath, err := exec.LookPath("find")
	if err != nil {
		return nil, fmt.Errorf("find not found: %w", err)
	}

	// Build find command with common exclusions
	args := []string{
		searchPath,
		"(", "-name", ".git", "-o", "-name", "node_modules", "-o", "-name", "__pycache__",
		"-o", "-name", ".venv", "-o", "-name", "vendor", ")",
		"-prune", "-o",
		"-name", pattern,
		"-print",
	}

	cmd := exec.CommandContext(ctx, findPath, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("find error: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var results []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			results = append(results, line)
		}
	}

	return results, nil
}

func nativeFindFile(ctx context.Context, pattern, searchPath string, maxResults int) ([]string, error) {
	if maxResults <= 0 {
		maxResults = 100
	}

	var matches []string
	gitignore := util.LoadGitIgnore(searchPath)

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "vendor": true, ".idea": true, ".vscode": true,
		"dist": true, "build": true, ".next": true,
	}

	err := filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, _ := filepath.Rel(searchPath, path)

		if d.IsDir() {
			if path == searchPath {
				return nil
			}
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if gitignore.Matches(relPath, true) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check gitignore for files
		if gitignore.Matches(relPath, false) {
			return nil
		}

		// Match by glob pattern
		matched, _ := filepath.Match(pattern, d.Name())
		// Also match if pattern is a substring of the filename (case-insensitive)
		if !matched && strings.Contains(strings.ToLower(d.Name()), strings.ToLower(pattern)) {
			matched = true
		}

		if matched {
			matches = append(matches, path)
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return matches, nil
}
