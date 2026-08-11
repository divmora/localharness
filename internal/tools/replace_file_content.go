package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerEditFile(r *Registry) {
	r.Register("replace_file_content", executeEditFile, ToolSchema{
		Group: ToolGroupWrite,
		Name:        "replace_file_content",
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

	// Read the file into lines
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("replace_file_content: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	f.Close()
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("replace_file_content: read error: %w", err)
	}

	totalLines := len(lines)
	var diffParts []string

	// Apply each chunk — process in order, working on the lines slice.
	// We use line-range scoping: only search within [start_line, end_line].
	for i, chunk := range ef.Chunks {
		target := chunk.TargetContent
		replacement := chunk.Replacement

		if target == "" {
			return fmt.Errorf("replace_file_content: chunk %d: target_content is required", i)
		}

		startLine := int(chunk.StartLine)
		endLine := int(chunk.EndLine)

		// Default range: entire file
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 || endLine > len(lines) {
			endLine = len(lines)
		}

		// Validate range
		if startLine > len(lines) {
			return fmt.Errorf("replace_file_content: chunk %d: start_line %d exceeds file length %d", i, startLine, totalLines)
		}
		if startLine > endLine {
			return fmt.Errorf("replace_file_content: chunk %d: start_line %d > end_line %d", i, startLine, endLine)
		}

		// Extract the scoped region (0-indexed)
		scopeStart := startLine - 1
		scopeEnd := endLine
		scopedText := strings.Join(lines[scopeStart:scopeEnd], "\n")

		// Search within scoped region only
		count := strings.Count(scopedText, target)
		if count == 0 {
			return fmt.Errorf("replace_file_content: chunk %d: target_content not found within lines %d-%d", i, startLine, endLine)
		}
		if count > 1 && !chunk.AllowMultiple {
			return fmt.Errorf("replace_file_content: chunk %d: target_content found %d times in lines %d-%d (set allow_multiple=true to replace all)", i, count, startLine, endLine)
		}

		// Perform replacement within the scoped region
		var newScopedText string
		if chunk.AllowMultiple {
			newScopedText = strings.ReplaceAll(scopedText, target, replacement)
		} else {
			newScopedText = strings.Replace(scopedText, target, replacement, 1)
		}

		// Replace the lines in the scoped region
		newLines := strings.Split(newScopedText, "\n")
		// Rebuild the lines slice: before + new scoped + after
		result := make([]string, 0, scopeStart+len(newLines)+(len(lines)-scopeEnd))
		result = append(result, lines[:scopeStart]...)
		result = append(result, newLines...)
		result = append(result, lines[scopeEnd:]...)
		lines = result

		// Build diff
		diffParts = append(diffParts, fmt.Sprintf("--- chunk %d (lines %d-%d) ---\n- %s\n+ %s",
			i+1, startLine, endLine,
			truncateForDiff(target, 200),
			truncateForDiff(replacement, 200)))
	}

	// Reconstruct content and write back
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("replace_file_content: write error: %w", err)
	}

	ef.DiffBlock = strings.Join(diffParts, "\n")
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

func truncateForDiff(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
