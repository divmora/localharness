package tools

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
)

func registerViewFile(r *Registry) {
	r.Register("view_file", executeViewFile, ToolSchema{
		Group: ToolGroupRead,
		Name:  "view_file",
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
		return errors.New(errors.ErrCodeToolValidation,
			"view_file tool missing action").
			WithContext("component", "view_file")
	}

	path := vf.Path
	if path == "" {
		return errors.New(errors.ErrCodeToolValidation,
			"view_file path is required").
			WithContext("component", "view_file")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(path)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
			"workspace validation failed").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
	}
	path = validPath
	vf.Path = path

	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeFileNotFound,
			"file not found").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
	}

	if info.IsDir() {
		return errors.New(errors.ErrCodeToolValidation,
			"path is a directory, use list_dir instead").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
	}

	// Detect binary files
	f, err := os.Open(path)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeFileNotFound,
			"failed to open file").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
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

	// Rewind and stream lines
	if _, err := f.Seek(0, 0); err != nil {
		return errors.Wrap(err, errors.ErrCodeToolExecution,
			"failed to seek in file").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
	}

	startLine := int(vf.StartLine)
	endLine := int(vf.EndLine)

	if startLine <= 0 {
		startLine = 1
	}
	// If the LLM accidentally reversed start/end, swap them.
	if endLine > 0 && startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	// Default endLine if omitted: up to 800 lines from startLine
	if endLine <= 0 {
		endLine = startLine + 799
	}
	// Enforce 800 line max per read
	if endLine-startLine+1 > 800 {
		endLine = startLine + 799
	}
	if startLine < 1 {
		startLine = 1
	}

	scanner := bufio.NewScanner(f)
	// Initial 64KB buffer, grow up to 4MB for long lines
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var sb strings.Builder
	currentLine := 0
	actualEndLine := 0

	for scanner.Scan() {
		currentLine++
		if currentLine >= startLine && currentLine <= endLine {
			actualEndLine = currentLine
			sb.WriteString(strconv.Itoa(currentLine))
			sb.WriteString(": ")
			sb.Write(scanner.Bytes())
			sb.WriteByte('\n')
		}
	}

	if err := scanner.Err(); err != nil {
		return errors.Wrap(err, errors.ErrCodeToolExecution,
			"failed to read file").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
	}

	totalLines := currentLine
	if actualEndLine == 0 && totalLines > 0 && startLine <= totalLines {
		actualEndLine = totalLines
	}

	vf.Content = sb.String()
	vf.TotalLines = int32(totalLines)
	vf.TotalBytes = info.Size()
	vf.IsBinary = false

	// Add partial content indicator so the model knows it got a subset
	if totalLines > 0 && startLine > totalLines {
		vf.Content = fmt.Sprintf("File only has %d lines (requested start_line %d is beyond end of file).\n", totalLines, startLine)
	} else if actualEndLine < totalLines {
		vf.Content += fmt.Sprintf(
			"The above content does NOT show the entire file contents. "+
				"Showing lines %d-%d of %d total. "+
				"Call view_file again with start_line/end_line to see remaining lines.\n",
			startLine, actualEndLine, totalLines,
		)
	} else if startLine == 1 && actualEndLine == totalLines {
		vf.Content += "The above content shows the entire, complete file contents of the requested file.\n"
	}

	return nil
}
