package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerFindFile(r *Registry) {
	r.Register("find_file", executeFindFile, ToolSchema{
		Group: ToolGroupRead,
		Name:        "find_file",
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

	// Try system `find` first, fall back to Go-native walk
	matches, err := trySystemFind(ctx, ff.Pattern, searchPath)
	if err != nil {
		r.Logger().Debug("system find not available, falling back to native", "error", err)
		matches, err = nativeFindFile(ctx, ff.Pattern, searchPath)
		if err != nil {
			return fmt.Errorf("find_file: %w", err)
		}
	}

	// Cap results
	maxResults := 100
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

func nativeFindFile(ctx context.Context, pattern, searchPath string) ([]string, error) {
	var matches []string

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "vendor": true, ".idea": true, ".vscode": true,
		"dist": true, "build": true, ".next": true,
	}

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Match by glob pattern
		matched, _ := filepath.Match(pattern, info.Name())
		if matched {
			matches = append(matches, path)
		}

		// Also match if pattern is a substring of the filename (case-insensitive)
		if !matched && strings.Contains(strings.ToLower(info.Name()), strings.ToLower(pattern)) {
			matches = append(matches, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return matches, nil
}
