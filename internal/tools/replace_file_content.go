package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/util"
)

func registerEditFile(r *Registry) {
	r.Register("replace_file_content", executeEditFile, ToolSchema{
		Group: ToolGroupWrite,
		Name:  "replace_file_content",
		Description: "Use this tool to edit an existing file by replacing target content with new content. " +
			"ALWAYS read a file with view_file before modifying it. " +
			"Use this tool ONLY when making a SINGLE CONTIGUOUS block of edits. " +
			"For multiple non-contiguous edits, use multi_replace_file_content instead. " +
			"Do NOT make multiple parallel calls to this tool for the same file. " +
			"Each chunk specifies a line range to narrow the search, the exact target text to find, and replacement text.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Absolute path to the file to edit"},
				"chunks": map[string]interface{}{
					"type":        "array",
					"description": "List of edit chunks",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"start_line":     map[string]interface{}{"type": "integer", "description": "Start line of search range (1-indexed). Narrows where to look for target_content."},
							"end_line":       map[string]interface{}{"type": "integer", "description": "End line of search range (1-indexed). Narrows where to look for target_content."},
							"target_content": map[string]interface{}{"type": "string", "description": "Exact text to find and replace"},
							"replacement":    map[string]interface{}{"type": "string", "description": "Replacement text"},
							"allow_multiple": map[string]interface{}{"type": "boolean", "description": "Replace all occurrences in range"},
						},
						"required": []string{"target_content", "replacement"},
					},
				},
				"artifact_metadata": map[string]interface{}{
					"type":        "object",
					"description": "Metadata updates if updating an artifact file, leave blank if not updating an artifact. Should be updated if the content is changing meaningfully.",
					"properties": map[string]interface{}{
						"artifact_type":    map[string]interface{}{"type": "string", "description": "Type of artifact: 'implementation_plan', 'walkthrough', 'task', or 'other'."},
						"summary":          map[string]interface{}{"type": "string", "description": "Description of the artifact contents after edits."},
						"request_feedback": map[string]interface{}{"type": "boolean", "description": "Set to true to request user feedback on this artifact."},
					},
				},
			},
			"required": []string{"path", "chunks"},
		},
	})
}

func executeEditFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	ef := step.GetReplaceFileContent()
	if ef == nil {
		return fmt.Errorf("replace_file_content: missing action")
	}

	path := ef.Path
	if path == "" {
		return fmt.Errorf("replace_file_content: path is required")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(path)
	if err != nil {
		return fmt.Errorf("replace_file_content: %w", err)
	}
	path = validPath
	ef.Path = path

	if len(ef.Chunks) == 0 {
		return fmt.Errorf("replace_file_content: at least one chunk is required")
	}

	// Read the entire file into a single byte buffer
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("replace_file_content: %w", err)
	}

	oldFileContent := string(rawBytes)
	content := rawBytes
	type indexedChunk struct {
		origIndex int
		chunk     *pb.EditChunk
	}

	// Prepare indexed chunks for execution.
	// When multiple chunks are provided, sorting them by start_line in descending order (bottom-to-top)
	// ensures that line modifications (additions/deletions) lower in the file never shift or invalidate
	// the line numbers of earlier/higher chunks.
	orderedChunks := make([]indexedChunk, len(ef.Chunks))
	for i, chunk := range ef.Chunks {
		cloned := &pb.EditChunk{
			StartLine:     chunk.StartLine,
			EndLine:       chunk.EndLine,
			TargetContent: chunk.TargetContent,
			Replacement:   chunk.Replacement,
			AllowMultiple: chunk.AllowMultiple,
		}
		orderedChunks[i] = indexedChunk{origIndex: i, chunk: cloned}
	}

	sort.SliceStable(orderedChunks, func(i, j int) bool {
		c1 := orderedChunks[i].chunk
		c2 := orderedChunks[j].chunk
		if c1.StartLine > 0 && c2.StartLine > 0 {
			if c1.StartLine != c2.StartLine {
				return c1.StartLine > c2.StartLine // Descending: higher line numbers first (bottom-to-top)
			}
			return orderedChunks[i].origIndex < orderedChunks[j].origIndex
		}
		if c1.StartLine > 0 && c2.StartLine <= 0 {
			return true // Chunks with specific line ranges run before whole-file chunks
		}
		if c1.StartLine <= 0 && c2.StartLine > 0 {
			return false
		}
		return orderedChunks[i].origIndex < orderedChunks[j].origIndex
	})

	diffParts := make([]string, len(ef.Chunks))

	// Apply each chunk in bottom-to-top order
	for idx, item := range orderedChunks {
		chunk := item.chunk
		target := chunk.TargetContent
		replacement := chunk.Replacement

		if target == "" {
			return fmt.Errorf("replace_file_content: chunk %d: target_content is required", item.origIndex)
		}

		targetBytes := []byte(target)
		replacementBytes := []byte(replacement)

		totalLines := countFileLines(content)
		startLine := int(chunk.StartLine)
		endLine := int(chunk.EndLine)

		// Default range: entire file
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 || endLine > totalLines {
			endLine = totalLines
		}

		// Validate range
		if startLine > totalLines {
			return fmt.Errorf("replace_file_content: chunk %d: start_line %d exceeds file length %d", item.origIndex, startLine, totalLines)
		}
		if startLine > endLine {
			return fmt.Errorf("replace_file_content: chunk %d: start_line %d > end_line %d", item.origIndex, startLine, endLine)
		}

		// Extract the scoped region byte range
		scopeStart, scopeEnd := getLineByteRange(content, startLine, endLine, totalLines)
		scopedBytes := content[scopeStart:scopeEnd]

		// Support CRLF line endings transparently
		if !bytes.Contains(targetBytes, []byte("\r\n")) && bytes.Contains(scopedBytes, []byte("\r\n")) {
			crlfTarget := bytes.ReplaceAll(targetBytes, []byte("\n"), []byte("\r\n"))
			if bytes.Count(scopedBytes, crlfTarget) > 0 {
				targetBytes = crlfTarget
				replacementBytes = bytes.ReplaceAll(replacementBytes, []byte("\n"), []byte("\r\n"))
			}
		}

		// Search within scoped region only
		count := bytes.Count(scopedBytes, targetBytes)
		if count == 0 {
			// Resilient fallback 1: Expand search window by ±20 lines
			expStartLine := startLine - 20
			if expStartLine < 1 {
				expStartLine = 1
			}
			expEndLine := endLine + 20
			if expEndLine > totalLines {
				expEndLine = totalLines
			}
			expScopeStart, expScopeEnd := getLineByteRange(content, expStartLine, expEndLine, totalLines)
			expScopedBytes := content[expScopeStart:expScopeEnd]

			if bytes.Count(expScopedBytes, targetBytes) == 1 {
				scopeStart = expScopeStart
				scopeEnd = expScopeEnd
				scopedBytes = expScopedBytes
				count = 1
			} else {
				// Resilient fallback 2: Check if target is unique across entire file
				if bytes.Count(content, targetBytes) == 1 {
					scopeStart = 0
					scopeEnd = len(content)
					scopedBytes = content
					count = 1
				} else {
					return fmt.Errorf("replace_file_content: chunk %d: target_content not found within lines %d-%d", item.origIndex, startLine, endLine)
				}
			}
		}
		if count > 1 && !chunk.AllowMultiple {
			return fmt.Errorf("replace_file_content: chunk %d: target_content found %d times in lines %d-%d (set allow_multiple=true to replace all)", item.origIndex, count, startLine, endLine)
		}

		linesBefore := totalLines

		// Perform replacement within the scoped region
		var newScopedBytes []byte
		if chunk.AllowMultiple {
			newScopedBytes = bytes.ReplaceAll(scopedBytes, targetBytes, replacementBytes)
		} else {
			newScopedBytes = bytes.Replace(scopedBytes, targetBytes, replacementBytes, 1)
		}

		// Rebuild content buffer without line/string splitting
		newContent := make([]byte, 0, scopeStart+len(newScopedBytes)+(len(content)-scopeEnd))
		newContent = append(newContent, content[:scopeStart]...)
		newContent = append(newContent, newScopedBytes...)
		newContent = append(newContent, content[scopeEnd:]...)
		content = newContent

		linesAfter := countFileLines(content)
		delta := linesAfter - linesBefore

		// If this replacement changed the line count, adjust any remaining chunks that overlap this range
		if delta != 0 {
			for i := idx + 1; i < len(orderedChunks); i++ {
				rem := &orderedChunks[i]
				if rem.chunk.EndLine >= int32(startLine) {
					rem.chunk.EndLine = int32(int(rem.chunk.EndLine) + delta)
				}
			}
		}

		// Build diff
		diffParts[item.origIndex] = fmt.Sprintf("--- chunk %d (lines %d-%d) ---\n- %s\n+ %s",
			item.origIndex+1, startLine, endLine,
			truncateForDiff(target, 200),
			truncateForDiff(replacement, 200))
	}

	// Write back modified content
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("replace_file_content: write error: %w", err)
	}

	newFileContent := string(content)
	filename := filepath.Base(path)
	unifiedDiff := util.UnifiedDiff("a/"+filename, "b/"+filename, oldFileContent, newFileContent)
	if unifiedDiff != "" {
		ef.DiffBlock = unifiedDiff
	} else {
		ef.DiffBlock = strings.Join(diffParts, "\n")
	}
	ef.Success = true

	// Save artifact metadata sidecar if provided
	if ef.ArtifactMetadata != nil && r.conversation != nil {
		filename := filepath.Base(path)
		meta := r.conversationMeta(ef.ArtifactMetadata)
		if err := r.conversation.SaveArtifactMetadata(filename, meta); err != nil {
			// Non-fatal: edit succeeded, metadata save failed
			return fmt.Errorf("replace_file_content: edit succeeded but metadata save failed: %w", err)
		}

		// Dispatch artifact feedback if requested
		if ef.ArtifactMetadata.RequestFeedback {
			r.dispatchArtifactFeedback(path, filename, ef.ArtifactMetadata)
		}
	}

	return nil
}

// countFileLines counts the number of lines in a byte buffer matching bufio.Scanner line semantics.
func countFileLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	n := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		n++
	}
	return n
}

// getLineByteRange returns the byte offsets [start, end) for lines [startLine, endLine] (1-indexed).
// The returned range includes the trailing newline of endLine if one is present in content.
func getLineByteRange(content []byte, startLine, endLine, totalLines int) (int, int) {
	if len(content) == 0 {
		return 0, 0
	}

	scopeStart := 0
	if startLine > 1 {
		nlCount := 0
		for i, b := range content {
			if b == '\n' {
				nlCount++
				if nlCount == startLine-1 {
					scopeStart = i + 1
					break
				}
			}
		}
	}

	scopeEnd := len(content)
	if endLine < totalLines {
		nlCount := 0
		for i, b := range content {
			if b == '\n' {
				nlCount++
				if nlCount == endLine {
					scopeEnd = i + 1
					break
				}
			}
		}
	}

	if scopeStart > len(content) {
		scopeStart = len(content)
	}
	if scopeEnd < scopeStart {
		scopeEnd = scopeStart
	}

	return scopeStart, scopeEnd
}

func truncateForDiff(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
