package tools

import (
	"bufio"
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

	// Try fast fd first, then system find with streaming and early exit, fall back to Go-native walk
	matches, err := tryFd(ctx, ff.Pattern, searchPath, maxResults)
	if err != nil {
		matches, err = trySystemFind(ctx, ff.Pattern, searchPath, maxResults, gitignore)
		if err != nil {
			r.Logger().Debug("system find not available, falling back to native", "error", err)
			matches, err = nativeFindFile(ctx, ff.Pattern, searchPath, maxResults)
			if err != nil {
				return fmt.Errorf("find_file: %w", err)
			}
		}
	}

	// Cap results
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	ff.Matches = matches
	return nil
}

// tryFd uses the `fd` (or `fdfind`) binary for ultra-fast directory walking with native gitignore support.
func tryFd(ctx context.Context, pattern, searchPath string, maxResults int) ([]string, error) {
	fdPath, err := exec.LookPath("fd")
	if err != nil {
		fdPath, err = exec.LookPath("fdfind")
		if err != nil {
			return nil, fmt.Errorf("fd not found: %w", err)
		}
	}

	args := []string{
		"--color=never",
		"--max-results", fmt.Sprintf("%d", maxResults),
		"--glob",
		"-E", ".git",
		"-E", "node_modules",
		"-E", "vendor",
		"-E", "__pycache__",
		"-E", ".venv",
		"-E", "dist",
		"-E", "build",
		"-E", "target",
		"-E", "bin",
		"-E", ".gemini",
		"-E", ".divmora",
		pattern,
		searchPath,
	}

	cmd := exec.CommandContext(ctx, fdPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("fd stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("fd start: %w", err)
	}

	var matches []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		matches = append(matches, line)
		if len(matches) >= maxResults {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			break
		}
	}

	_ = cmd.Wait()
	return matches, nil
}

// trySystemFind executes the system find binary with streaming stdout and early termination.
func trySystemFind(ctx context.Context, pattern, searchPath string, maxResults int, gitignore *util.GitIgnoreMatcher) ([]string, error) {
	findPath, err := exec.LookPath("find")
	if err != nil {
		return nil, fmt.Errorf("find not found: %w", err)
	}

	// Build find command with common exclusions
	args := []string{
		searchPath,
		"(",
		"-name", ".git", "-o",
		"-name", "node_modules", "-o",
		"-name", "__pycache__", "-o",
		"-name", ".venv", "-o",
		"-name", "vendor", "-o",
		"-name", "dist", "-o",
		"-name", "build", "-o",
		"-name", "target", "-o",
		"-name", "bin", "-o",
		"-name", ".gemini", "-o",
		"-name", ".divmora", "-o",
		"-name", ".next", "-o",
		"-name", ".turbo", "-o",
		"-name", ".cache",
		")",
		"-prune", "-o",
		"-name", pattern,
		"-print",
	}

	cmd := exec.CommandContext(ctx, findPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("find stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("find start: %w", err)
	}

	var results []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Filter system find results through gitignore as they arrive
		if gitignore != nil {
			rel, relErr := filepath.Rel(searchPath, line)
			if relErr == nil && gitignore.Matches(rel, false) {
				continue
			}
		}

		results = append(results, line)
		if len(results) >= maxResults {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			break
		}
	}

	_ = cmd.Wait()
	return results, nil
}

func nativeFindFile(ctx context.Context, pattern, searchPath string, maxResults int) ([]string, error) {
	if maxResults <= 0 {
		maxResults = 100
	}

	var matches []string
	gitignore := util.LoadGitIgnore(searchPath)
	lowerPattern := strings.ToLower(pattern)

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "vendor": true, ".idea": true, ".vscode": true,
		"dist": true, "build": true, ".next": true, "target": true,
		"bin": true, ".gemini": true, ".divmora": true, ".cache": true,
		".turbo": true, ".agents": true,
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
		if !matched && (strings.Contains(d.Name(), pattern) || strings.Contains(strings.ToLower(d.Name()), lowerPattern)) {
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
