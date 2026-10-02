package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
)

const (
	maxViewLines     = 800
	maxViewByteChunk = 46080
	maxBinaryBytes   = 100 * 1024 * 1024 // 100 MB
)

func registerViewFile(r *Registry) {
	r.Register("view_file", executeViewFile, ToolSchema{
		Group: ToolGroupRead,
		Name:  "view_file",
		Description: "View the contents of a file from the local filesystem. This tool supports text files and following binary files: image, pdf, video, audio.\n" +
			"Text file usage:\n" +
			"- The lines of the file are 1-indexed\n" +
			"- You can view at most 800 lines at a time\n" +
			"- Specify StartLine and EndLine to view the lines of the file using slice notation:\n" +
			"  - Omit both to view the entire file, or the first 800 lines of the file, whichever is smaller.\n" +
			"  - Specify StartLine only to view the remaining lines of the file, or the next 800 lines, whichever is smaller\n" +
			"  - Specify EndLine only to view the remaining preceding lines of the file, or the previous 800 lines, whichever is smaller\n" +
			"  - Specify both to view a precise line range. This range must be smaller than 800 lines or only the first 800 lines of the range will be shown.\n" +
			"- Content is limited to 46080 bytes per view. If content is truncated, use the ContentOffset parameter to view the remaining content\n" +
			"Binary file usage:\n" +
			"- Do not provide StartLine or EndLine arguments, this tool always returns the entire file\n" +
			"- Files larger than 100 MB cannot be viewed.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"AbsolutePath": map[string]interface{}{
					"type":        "string",
					"description": "Path to file to view. Must be an absolute path.",
				},
				"ContentOffset": map[string]interface{}{
					"type":        "integer",
					"description": "Optional. Byte offset into the content. Use this to view content beyond the initial byte limit when the tool output indicates content was truncated.",
				},
				"EndLine": map[string]interface{}{
					"type":        "integer",
					"description": "Optional. Endline to view, 1-indexed, inclusive. When specified, this value must be greater than or equal to StartLine.",
				},
				"StartLine": map[string]interface{}{
					"type":        "integer",
					"description": "Optional. Startline to view, 1-indexed, inclusive. When specified, this value must be less than or equal to EndLine.",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"AbsolutePath", "ToolSummary", "ToolAction"},
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
	validPath, err := r.ValidatePathContext(ctx, path)
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
			"path is a directory, not a file").
			WithContext("path", path).
			WithContext("operation", "view_file").
			WithComponent("view_file")
	}

	if info.Size() > maxBinaryBytes {
		return errors.New(errors.ErrCodeToolValidation,
			"files larger than 100 MB cannot be viewed").
			WithContext("path", path).
			WithContext("size_bytes", info.Size()).
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

	// Read first 1024 bytes for binary detection
	header := make([]byte, 1024)
	n, _ := f.Read(header)
	sample := header[:n]

	nowStr := time.Now().Format("2006-01-02T15:04:05-07:00")

	if isBinaryFile(path, sample) {
		contentType := http.DetectContentType(sample)
		vf.IsBinary = true
		vf.MimeType = contentType
		vf.TotalBytes = info.Size()
		vf.Content = fmt.Sprintf("Created At: %s\nCompleted At: %s\nFile Path: `file://%s`\nMIME Type: %s\nTotal Bytes: %d\n[Binary file: %s, size: %d bytes]\n",
			nowStr, nowStr, path, contentType, info.Size(), contentType, info.Size())
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

	if startLine <= 0 && endLine <= 0 {
		startLine = 1
		endLine = maxViewLines
	} else if startLine > 0 && endLine <= 0 {
		endLine = startLine + maxViewLines - 1
	} else if endLine > 0 && startLine <= 0 {
		startLine = endLine - maxViewLines + 1
		if startLine < 1 {
			startLine = 1
		}
	} else {
		if startLine > endLine {
			startLine, endLine = endLine, startLine
		}
		if endLine-startLine+1 > maxViewLines {
			endLine = startLine + maxViewLines - 1
		}
		if startLine < 1 {
			startLine = 1
		}
	}

	scanner := bufio.NewScanner(f)
	// Initial 64KB buffer, grow up to 4MB for long lines
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var lineBuf bytes.Buffer
	currentLine := 0
	actualEndLine := 0

	for scanner.Scan() {
		currentLine++
		if currentLine >= startLine && currentLine <= endLine {
			actualEndLine = currentLine
			lineBuf.WriteString(strconv.Itoa(currentLine))
			lineBuf.WriteString(": ")
			lineBuf.Write(scanner.Bytes())
			lineBuf.WriteByte('\n')
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

	slicedBytes := lineBuf.Bytes()
	totalSlicedBytes := len(slicedBytes)

	offset := int(vf.ContentOffset)
	if offset < 0 {
		offset = 0
	}

	var displayBytes []byte
	truncated := false
	endOffset := 0

	if offset < totalSlicedBytes {
		remaining := totalSlicedBytes - offset
		if remaining > maxViewByteChunk {
			endOffset = offset + maxViewByteChunk
			displayBytes = slicedBytes[offset:endOffset]
			truncated = true
		} else {
			endOffset = totalSlicedBytes
			displayBytes = slicedBytes[offset:]
			truncated = false
		}
	} else if totalSlicedBytes > 0 {
		offset = totalSlicedBytes
		endOffset = totalSlicedBytes
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Created At: %s\nCompleted At: %s\nFile Path: `file://%s`\nTotal Lines: %d\nTotal Bytes: %d\n",
		nowStr, nowStr, path, totalLines, info.Size()))

	if totalLines == 0 {
		sb.WriteString("Showing lines 0 to 0\nThe above content shows the entire, complete file contents of the requested file.\n")
	} else if startLine > totalLines {
		sb.WriteString(fmt.Sprintf("File only has %d lines (requested start_line %d is beyond end of file).\n", totalLines, startLine))
	} else {
		sb.WriteString(fmt.Sprintf("Showing lines %d to %d\n", startLine, actualEndLine))
		if truncated {
			sb.WriteString(fmt.Sprintf("Content truncated: showing bytes %d-%d of %d. To see more, call this tool again with the same line range and ContentOffset=%d.\n",
				offset, endOffset, totalSlicedBytes, endOffset))
		}
		sb.WriteString("The following code has been modified to include a line number before every line, in the format: <line_number>: <original_line>. Please note that any changes targeting the original code should remove the line number, colon, and leading space.\n")
		sb.Write(displayBytes)

		if !truncated {
			if startLine == 1 && actualEndLine == totalLines {
				sb.WriteString("The above content shows the entire, complete file contents of the requested file.\n")
			} else {
				sb.WriteString("The above content does NOT show the entire file contents. If you need to view any lines of the file which were not shown to complete your task, call this tool again to view those lines.\n")
			}
		}
	}

	vf.Content = sb.String()
	vf.TotalLines = int32(totalLines)
	vf.TotalBytes = info.Size()
	vf.IsBinary = false

	return nil
}

// knownTextExts contains file extensions (with leading dot, lowercase) that are always text/source files.
var knownTextExts = map[string]bool{
	// Go
	".go": true,
	// Python
	".py": true, ".pyi": true,
	// JavaScript / TypeScript / Web
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".html": true, ".htm": true, ".xhtml": true, ".xml": true, ".svg": true,
	".css": true, ".scss": true, ".sass": true, ".less": true,
	// Systems languages
	".rs": true, ".c": true, ".cpp": true, ".cc": true, ".cxx": true,
	".h": true, ".hpp": true, ".hxx": true, ".zig": true, ".nim": true,
	// JVM languages
	".java": true, ".kt": true, ".kts": true, ".scala": true, ".groovy": true, ".clj": true, ".cljs": true,
	// Other languages
	".swift": true, ".rb": true, ".php": true, ".cs": true, ".fs": true, ".vb": true,
	".r": true, ".m": true, ".mm": true, ".lua": true, ".pl": true, ".pm": true,
	".dart": true, ".elm": true, ".erl": true, ".ex": true, ".exs": true, ".hs": true,
	// Shell scripting
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".bat": true, ".cmd": true, ".ps1": true, ".psm1": true,
	// Config, data & schemas
	".json": true, ".jsonc": true, ".json5": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".conf": true, ".properties": true,
	".env": true, ".proto": true, ".sql": true, ".graphql": true, ".gql": true,
	".csv": true, ".tsv": true,
	// Documentation
	".md": true, ".markdown": true, ".rst": true, ".txt": true, ".text": true,
	".log": true, ".tex": true, ".diff": true, ".patch": true,
	// Containers & build
	".dockerfile": true,
}

// knownTextFiles contains exact filenames (lowercase) that are always text files.
var knownTextFiles = map[string]bool{
	"dockerfile": true, "makefile": true, "gnumakefile": true,
	"containerfile": true, "vagrantfile": true, "rakefile": true,
	"gemfile": true, "pipfile": true, "brewfile": true, "procfile": true,
	"license": true, "licence": true, "notice": true, "readme": true,
	"changelog": true, "authors": true, "contributing": true,
	".gitignore": true, ".gitattributes": true, ".dockerignore": true,
	".editorconfig": true, ".env": true, ".env.example": true,
}

// isBinaryFile determines whether a file should be treated as binary or text.
// It uses a combination of known text extensions/filenames, NUL-byte scanning
// (standard Git/Unix heuristic), known binary extensions, and specific binary MIME types.
func isBinaryFile(path string, header []byte) bool {
	// NUL byte detection: valid UTF-8/ASCII text never contains \x00.
	if bytes.IndexByte(header, 0) != -1 {
		return true
	}

	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))

	// Known text extensions and filenames take precedence
	if knownTextExts[ext] || knownTextFiles[base] {
		return false
	}

	// Known binary extensions
	if isBinaryExtension(ext) {
		return true
	}

	// For unrecognized extensions, sniff content type but DO NOT treat application/octet-stream as binary
	// unless it matched specific binary prefixes (image/, audio/, video/, font/, pdf, zip, etc.)
	if len(header) > 0 {
		contentType := http.DetectContentType(header)
		if strings.HasPrefix(contentType, "image/") ||
			strings.HasPrefix(contentType, "audio/") ||
			strings.HasPrefix(contentType, "video/") ||
			strings.HasPrefix(contentType, "font/") ||
			contentType == "application/pdf" ||
			contentType == "application/zip" ||
			contentType == "application/x-gzip" {
			return true
		}
	}

	return false
}

var binaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".webp": true,
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
