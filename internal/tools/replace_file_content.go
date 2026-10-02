package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/util"
)

func registerEditFile(r *Registry) {
	r.Register("replace_file_content", executeEditFile, ToolSchema{
		Group: ToolGroupWrite,
		Name:  "replace_file_content",
		Description: "Use this tool to edit an existing file. Follow these rules:\n" +
			"1. Use this tool ONLY when you are making a SINGLE CONTIGUOUS block of edits to the same file (i.e. replacing a single contiguous block of text).\n" +
			"2. Do NOT make multiple parallel calls to this tool for the same file.\n" +
			"3. To edit multiple, non-adjacent lines of code in the same file, make multiple calls to this tool.\n" +
			"4. For the ReplacementChunk, specify StartLine, EndLine, TargetContent and ReplacementContent. StartLine and EndLine should specify a range of lines containing precisely the instances of TargetContent that you wish to edit. To edit a single instance of the TargetContent, the range should be such that it contains that specific instance of the TargetContent and no other instances. In TargetContent, specify the precise lines of code to edit. These lines MUST EXACTLY MATCH text in the existing file content. In ReplacementContent, specify the replacement content for the specified target content. This must be a complete drop-in replacement of the TargetContent, with necessary modifications made.\n" +
			"5. If you are making multiple edits across a single file, make multiple calls to this tool. DO NOT try to replace the entire existing content with the new content, this is very expensive.\n" +
			"6. You may not edit file extensions: [.ipynb]",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"AllowMultiple": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, multiple occurrences of 'targetContent' will be replaced by 'replacementContent' if they are found. Otherwise if multiple occurrences are found, an error will be returned.",
				},
				"Description": map[string]interface{}{
					"type":        "string",
					"description": "Brief, user-facing explanation of what this change did. Focus on non-obvious rationale, design decisions, or important context. Don't just restate what the code does.",
				},
				"EndLine": map[string]interface{}{
					"type":        "integer",
					"description": "The ending line number of the chunk (1-indexed). Should be at or after the last line containing the target content. Must satisfy StartLine <= EndLine <= number of lines in the file. The target content is searched for within the [StartLine, EndLine] range.",
				},
				"Instruction": map[string]interface{}{
					"type":        "string",
					"description": "A description of the changes that you are making to the file.",
				},
				"ReplacementContent": map[string]interface{}{
					"type":        "string",
					"description": "The content to replace the target content with.",
				},
				"StartLine": map[string]interface{}{
					"type":        "integer",
					"description": "The starting line number of the chunk (1-indexed). Should be at or before the first line containing the target content. Must satisfy 1 <= StartLine <= EndLine. The target content is searched for within the [StartLine, EndLine] range.",
				},
				"TargetContent": map[string]interface{}{
					"type":        "string",
					"description": "The exact string to be replaced. This must be the exact character-sequence to be replaced, including whitespace. Be very careful to include any leading whitespace otherwise this will not work at all. This must be a unique substring within the file, or else it will error.",
				},
				"TargetFile": map[string]interface{}{
					"type":        "string",
					"description": "The target file to modify. Must be an absolute path. Always specify the target file as the very first argument.",
				},
				"TargetLintErrorIds": map[string]interface{}{
					"type":        "array",
					"description": "If applicable, IDs of lint errors this edit aims to fix (they'll have been given in recent IDE feedback). If you believe the edit could fix lints, do specify lint IDs; if the edit is wholly unrelated, do not. A rule of thumb is, if your edit was influenced by lint feedback, include lint IDs. Exercise honest judgement here.",
					"items": map[string]interface{}{
						"type": "string",
					},
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
			"required": []string{
				"TargetFile",
				"Instruction",
				"Description",
				"AllowMultiple",
				"TargetContent",
				"ReplacementContent",
				"StartLine",
				"EndLine",
				"ToolSummary",
				"ToolAction",
			},
		},
	})
}

func executeEditFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	startTime := time.Now()
	ef := step.GetReplaceFileContent()
	if ef == nil {
		return fmt.Errorf("replace_file_content: missing action")
	}

	path := ef.Path
	if path == "" {
		return fmt.Errorf("replace_file_content: TargetFile is required")
	}

	if strings.HasSuffix(strings.ToLower(path), ".ipynb") {
		return fmt.Errorf("replace_file_content: editing .ipynb files is not supported")
	}

	// Workspace validation
	validPath, err := r.ValidatePathContext(ctx, path)
	if err != nil {
		return fmt.Errorf("replace_file_content: %w", err)
	}
	path = validPath
	ef.Path = path

	if len(ef.Chunks) == 0 && ef.TargetContent != "" {
		ef.Chunks = []*pb.EditChunk{
			{
				StartLine:     ef.StartLine,
				EndLine:       ef.EndLine,
				TargetContent: ef.TargetContent,
				Replacement:   ef.ReplacementContent,
				AllowMultiple: ef.AllowMultiple,
			},
		}
	}

	if len(ef.Chunks) == 0 {
		return fmt.Errorf("replace_file_content: TargetContent is required")
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

	var cleanDiffLines []string
	for _, l := range strings.Split(unifiedDiff, "\n") {
		if strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") {
			continue
		}
		cleanDiffLines = append(cleanDiffLines, l)
	}
	cleanDiff := strings.TrimSpace(strings.Join(cleanDiffLines, "\n"))
	if cleanDiff == "" {
		cleanDiff = ef.DiffBlock
	}

	completedTime := time.Now()
	timeFormat := "2006-01-02T15:04:05-07:00"
	ef.FormattedOutput = fmt.Sprintf("Created At: %s\nCompleted At: %s\nThe following changes were made by the replace_file_content tool to: %s. If relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.\n[diff_block_start]\n%s\n[diff_block_end]\n\nPlease note that the above snippet only shows the MODIFIED lines from the last change. It shows up to 3 lines of unchanged lines before and after the modified lines. The actual file contents may have many more lines not shown.",
		startTime.Format(timeFormat), completedTime.Format(timeFormat), path, cleanDiff)

	// Save artifact metadata sidecar if provided
	if ef.ArtifactMetadata != nil {
		if conv := r.Conversation(); conv != nil {
			filename := filepath.Base(path)
			meta := r.conversationMeta(ef.ArtifactMetadata)
			if err := conv.SaveArtifactMetadata(filename, meta); err != nil {
				// Non-fatal: edit succeeded, metadata save failed
				return fmt.Errorf("replace_file_content: edit succeeded but metadata save failed: %w", err)
			}
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
