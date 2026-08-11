package tools

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// registerMultiEditFile registers the multi_replace_file_content tool.
// This is a convenience alias for edit_file with a schema optimized for
// multiple non-contiguous edits. Both tools share the same underlying
// ActionReplaceFileContent proto and execution logic.
func registerMultiEditFile(r *Registry) {
	r.Register("multi_replace_file_content", executeMultiEditFile, ToolSchema{
		Group: ToolGroupWrite,
		Name: "multi_replace_file_content",
		Description: "Use this tool to edit an existing file by making MULTIPLE, NON-CONTIGUOUS replacements in a single call. " +
			"ALWAYS read a file with view_file before modifying it. " +
			"Use this when you need to change several separate locations in the same file. " +
			"For a single contiguous edit, prefer replace_file_content instead. " +
			"Do NOT make multiple parallel calls to this tool for the same file. " +
			"Each chunk specifies a line range to scope the search, the exact target text to find, and the replacement text.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the file to edit",
				},
				"chunks": map[string]interface{}{
					"type":        "array",
					"description": "List of replacement chunks. Each chunk edits a separate location in the file. Process order matters — earlier chunks may shift line numbers for subsequent chunks.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"start_line": map[string]interface{}{
								"type":        "integer",
								"description": "Start line of the search range (1-indexed). Narrows where to look for target_content.",
							},
							"end_line": map[string]interface{}{
								"type":        "integer",
								"description": "End line of the search range (1-indexed, inclusive). Narrows where to look for target_content.",
							},
							"target_content": map[string]interface{}{
								"type":        "string",
								"description": "The exact text to find and replace. Must exactly match text in the file within the specified line range.",
							},
							"replacement": map[string]interface{}{
								"type":        "string",
								"description": "The replacement text. This is a complete drop-in replacement for target_content.",
							},
							"allow_multiple": map[string]interface{}{
								"type":        "boolean",
								"description": "If true, replace all occurrences of target_content within the line range. Default: false (error if multiple found).",
							},
						},
						"required": []string{"target_content", "replacement"},
					},
					"minItems": 2,
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Brief description of the changes being made (for logging/tracing).",
				},
			},
			"required": []string{"path", "chunks"},
		},
	})
}

func executeMultiEditFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	// multi_replace_file_content uses the same ActionReplaceFileContent proto as edit_file.
	// The engine maps both tool names to ActionReplaceFileContent in buildToolStep.
	ef := step.GetReplaceFileContent()
	if ef == nil {
		return fmt.Errorf("multi_replace_file_content: missing action")
	}

	if len(ef.Chunks) < 2 {
		return fmt.Errorf("multi_replace_file_content: requires at least 2 chunks (for a single edit, use replace_file_content instead)")
	}

	// Delegate to the same implementation as edit_file
	return executeEditFile(ctx, step, r)
}
