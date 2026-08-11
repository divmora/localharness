package tools

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerViewFile(r *Registry) {
	r.Register("view_file", executeViewFile, ToolSchema{
		Group: ToolGroupRead,
		Name:        "view_file",
		Description: "View the contents of a file from the local filesystem. " +
			"Use this instead of run_command with cat, head, tail, or less. " +
			"Lines are 1-indexed. You can view at most 800 lines per call. " +
			"IMPORTANT: To minimize context usage, prefer targeted reads by specifying start_line and end_line " +
			"instead of reading the entire file. Use list_dir or grep_search to locate relevant sections first, " +
			"then read only the lines you need. Only omit start_line/end_line when you genuinely need the full file. " +
			"Supports text files and detects binary files (returns metadata only for binaries).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string", "description": "Absolute path to the file"},
				"start_line": map[string]interface{}{"type": "integer", "description": "Start line (1-indexed, inclusive). 0 or omitted = from start."},
				"end_line":   map[string]interface{}{"type": "integer", "description": "End line (1-indexed, inclusive). 0 or omitted = to end."},
			},
			"required": []string{"path"},
		},
	})
}

func executeViewFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	vf := step.GetViewFile()
	if vf == nil {
		return fmt.Errorf("view_file: missing action")
	}

	path := vf.Path
	if path == "" {
		return fmt.Errorf("view_file: path is required")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(path)
	if err != nil {
		return fmt.Errorf("view_file: %w", err)
	}
	path = validPath
	vf.Path = path

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("view_file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("view_file: %s is a directory, use list_dir instead", path)
	}

	// Detect binary files
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("view_file: %w", err)
	}
	defer f.Close()

	// Read first 512 bytes for MIME detection
	header := make([]byte, 512)
	n, _ := f.Read(header)
	contentType := http.DetectContentType(header[:n])
	isBinary := !strings.HasPrefix(contentType, "text/") &&
		contentType != "application/json" &&
		contentType != "application/xml" &&
		contentType != "application/javascript"

	if isBinary {
		vf.Content = fmt.Sprintf("[Binary file: %s, size: %d bytes, type: %s]", path, info.Size(), contentType)
		vf.TotalBytes = info.Size()
		vf.IsBinary = true
		return nil
	}

	// Rewind and read lines
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("view_file: seek error: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	// Increase buffer for large lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("view_file: read error: %w", err)
	}

	totalLines := len(lines)
	startLine := int(vf.StartLine)
	endLine := int(vf.EndLine)

	// Default range
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > totalLines {
		endLine = totalLines
	}

	// If the LLM accidentally reversed start/end, swap them.
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}

	// Enforce 800 line max per read
	if endLine-startLine+1 > 800 {
		endLine = startLine + 799
	}

	// Clamp
	if startLine > totalLines {
		startLine = totalLines
	}
	if startLine < 1 {
		startLine = 1
	}

	// Extract the range (convert to 0-indexed)
	selected := lines[startLine-1 : endLine]

	// Add line numbers
	var sb strings.Builder
	for i, line := range selected {
		fmt.Fprintf(&sb, "%d: %s\n", startLine+i, line)
	}

	vf.Content = sb.String()
	vf.TotalLines = int32(totalLines)
	vf.TotalBytes = info.Size()
	vf.IsBinary = false

	// Add partial content indicator so the model knows it got a subset
	if endLine < totalLines {
		vf.Content += fmt.Sprintf(
			"The above content does NOT show the entire file contents. "+
				"Showing lines %d-%d of %d total. "+
				"Call view_file again with start_line/end_line to see remaining lines.\n",
			startLine, endLine, totalLines,
		)
	} else if startLine == 1 && endLine == totalLines {
		vf.Content += "The above content shows the entire, complete file contents of the requested file.\n"
	}

	return nil
}
