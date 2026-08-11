package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// maxLineContentLen caps each matching line's content to prevent long lines
// (e.g., minified JS/CSS) from consuming excessive context.
const maxLineContentLen = 200

func truncateLineContent(s string) string {
	if len(s) > maxLineContentLen {
		return s[:maxLineContentLen] + "..."
	}
	return s
}

func registerSearchDir(r *Registry) {
	r.Register("grep_search", executeSearchDir, ToolSchema{
		Group: ToolGroupRead,
		Name:        "grep_search",
		Description: "Use ripgrep to find exact pattern matches within files or directories. " +
			"Use this instead of run_command with grep, rg, ag, or ack. " +
			"Results are returned with filename, line number, and line content for each match. " +
			"Total results are capped at 50 matches. Use the includes option to filter by file type " +
			"or specific paths to refine your search. Set match_per_line to false to return only filenames.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":            map[string]interface{}{"type": "string", "description": "Search term or regex pattern"},
				"path":             map[string]interface{}{"type": "string", "description": "Absolute path to directory or file to search"},
				"is_regex":         map[string]interface{}{"type": "boolean", "description": "Treat query as regex pattern"},
				"case_insensitive": map[string]interface{}{"type": "boolean", "description": "Case-insensitive search"},
				"match_per_line":   map[string]interface{}{"type": "boolean", "description": "If true, return line-level results. If false, return only filenames."},
				"includes":         map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Glob patterns to filter files (e.g., '*.go')"},
				"max_results":      map[string]interface{}{"type": "integer", "description": "Maximum results to return (default: 50)"},
			},
			"required": []string{"query", "path"},
		},
	})
}

func executeSearchDir(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	sd := step.GetGrepSearch()
	if sd == nil {
		return fmt.Errorf("grep_search: missing action")
	}

	if sd.Query == "" {
		return fmt.Errorf("grep_search: query is required")
	}

	searchPath := sd.Path
	if searchPath == "" {
		return fmt.Errorf("grep_search: path is required")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(searchPath)
	if err != nil {
		return fmt.Errorf("grep_search: %w", err)
	}
	searchPath = validPath
	sd.Path = searchPath

	maxResults := int(sd.MaxResults)
	if maxResults <= 0 {
		maxResults = 50
	}

	// Try ripgrep first, fall back to Go-native search
	matches, totalCount, err := tryRipgrep(ctx, sd, searchPath, maxResults)
	if err != nil {
		// Fallback to native Go search
		r.Logger().Debug("ripgrep not available, falling back to native search", "error", err)
		matches, totalCount, err = nativeSearch(ctx, sd, searchPath, maxResults)
		if err != nil {
			return fmt.Errorf("grep_search: %w", err)
		}
	}

	sd.Matches = matches
	sd.TotalMatches = int32(totalCount)
	sd.Truncated = totalCount > maxResults

	return nil
}

// tryRipgrep uses the `rg` binary for fast searching.
func tryRipgrep(ctx context.Context, sd *pb.ActionGrepSearch, searchPath string, maxResults int) ([]*pb.SearchMatch, int, error) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, 0, fmt.Errorf("ripgrep not found: %w", err)
	}

	args := []string{
		"--no-heading",
		"--line-number",
		"--color=never",
		"--max-count", fmt.Sprintf("%d", maxResults+1), // +1 to detect truncation
	}

	if !sd.IsRegex {
		args = append(args, "--fixed-strings")
	}
	if sd.CaseInsensitive {
		args = append(args, "--ignore-case")
	}
	if !sd.MatchPerLine {
		args = append(args, "--files-with-matches")
	}

	// Add glob includes
	for _, glob := range sd.Includes {
		args = append(args, "--glob", glob)
	}

	args = append(args, sd.Query, searchPath)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	output, err := cmd.Output()

	// rg returns exit code 1 for no matches (not an error)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, 0, nil // No matches
		}
		return nil, 0, fmt.Errorf("ripgrep error: %w", err)
	}

	return parseRipgrepOutput(string(output), sd.MatchPerLine, maxResults)
}

func parseRipgrepOutput(output string, matchPerLine bool, maxResults int) ([]*pb.SearchMatch, int, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, 0, nil
	}

	totalCount := len(lines)
	var matches []*pb.SearchMatch

	for i, line := range lines {
		if i >= maxResults {
			break
		}

		if !matchPerLine {
			// File-only mode: each line is just a filename
			matches = append(matches, &pb.SearchMatch{
				Filename: line,
			})
			continue
		}

		// Line-level mode: format is "filename:linenum:content"
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		lineNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)

		matches = append(matches, &pb.SearchMatch{
			Filename:    parts[0],
			LineNumber:  int32(lineNum),
			LineContent: truncateLineContent(parts[2]),
		})
	}

	return matches, totalCount, nil
}

// nativeSearch is a pure Go fallback when ripgrep is unavailable.
func nativeSearch(ctx context.Context, sd *pb.ActionGrepSearch, searchPath string, maxResults int) ([]*pb.SearchMatch, int, error) {
	var matches []*pb.SearchMatch
	totalCount := 0

	var re *regexp.Regexp
	if sd.IsRegex {
		flags := ""
		if sd.CaseInsensitive {
			flags = "(?i)"
		}
		var err error
		re, err = regexp.Compile(flags + sd.Query)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid regex %q: %w", sd.Query, err)
		}
	}

	searchQuery := sd.Query
	if sd.CaseInsensitive && !sd.IsRegex {
		searchQuery = strings.ToLower(searchQuery)
	}

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip unreadable entries
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			// Skip hidden dirs and common large dirs
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// Check glob includes
		if len(sd.Includes) > 0 {
			matched := false
			for _, pattern := range sd.Includes {
				if m, _ := filepath.Match(pattern, info.Name()); m {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		// Skip binary files (by extension heuristic)
		if isBinaryExtension(filepath.Ext(path)) {
			return nil
		}

		// Skip large files (>5MB)
		if info.Size() > 5*1024*1024 {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		fileMatched := false
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			var isMatch bool
			if re != nil {
				isMatch = re.MatchString(line)
			} else {
				if sd.CaseInsensitive {
					isMatch = strings.Contains(strings.ToLower(line), searchQuery)
				} else {
					isMatch = strings.Contains(line, sd.Query)
				}
			}

			if isMatch {
				totalCount++
				fileMatched = true

				if sd.MatchPerLine && len(matches) < maxResults {
					matches = append(matches, &pb.SearchMatch{
						Filename:    path,
						LineNumber:  int32(lineNum),
						LineContent: truncateLineContent(line),
					})
				}
			}
		}

		if !sd.MatchPerLine && fileMatched && len(matches) < maxResults {
			matches = append(matches, &pb.SearchMatch{
				Filename: path,
			})
		}

		return nil
	})

	if err != nil {
		return nil, 0, err
	}

	return matches, totalCount, nil
}

var binaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".svg": true, ".webp": true,
	".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".7z": true, ".rar": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".wasm": true, ".pyc": true, ".class": true,
	".ttf": true, ".woff": true, ".woff2": true, ".eot": true,
	".o": true, ".a": true, ".lib": true,
}

func isBinaryExtension(ext string) bool {
	return binaryExts[strings.ToLower(ext)]
}
