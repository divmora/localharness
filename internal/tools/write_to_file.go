package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerCreateFile(r *Registry) {
	r.Register("write_to_file", executeCreateFile, ToolSchema{
		Group: ToolGroupWrite,
		Name:        "write_to_file",
		Description: "Use this tool to create new files. The file and any parent directories will be created automatically. " +
			"By default this tool will error if the file already exists. To overwrite an existing file, set overwrite to true. " +
			"WARNING: overwrite replaces the entire file contents. Only use overwrite when you explicitly intend to replace the file. " +
			"For modifying existing files, use replace_file_content or multi_replace_file_content instead.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":      map[string]interface{}{"type": "string", "description": "Absolute path for the new file"},
				"content":   map[string]interface{}{"type": "string", "description": "File content to write"},
				"overwrite": map[string]interface{}{"type": "boolean", "description": "If true, overwrite existing file"},
				"is_artifact": map[string]interface{}{"type": "boolean", "description": "Set to true when creating an artifact file"},
				"artifact_metadata": map[string]interface{}{
					"type":        "object",
					"description": "Metadata for the artifact, required when is_artifact is true.",
					"properties": map[string]interface{}{
						"artifact_type":    map[string]interface{}{"type": "string", "description": "Type of artifact: 'implementation_plan', 'walkthrough', 'task', or 'other'."},
						"summary":          map[string]interface{}{"type": "string", "description": "Description of the artifact contents."},
						"request_feedback": map[string]interface{}{"type": "boolean", "description": "Set to true to request user feedback on this artifact."},
					},
				},
			},
			"required": []string{"path", "content"},
		},
	})
}

func executeCreateFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	cf := step.GetWriteToFile()
	if cf == nil {
		return fmt.Errorf("write_to_file: missing action")
	}

	path := cf.Path
	if path == "" {
		return fmt.Errorf("write_to_file: path is required")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(path)
	if err != nil {
		return fmt.Errorf("write_to_file: %w", err)
	}
	path = validPath
	cf.Path = path

	// Check if file already exists
	if _, err := os.Stat(path); err == nil && !cf.Overwrite {
		return fmt.Errorf("write_to_file: file %s already exists (set overwrite=true to replace)", path)
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("write_to_file: cannot create directory %s: %w", dir, err)
	}

	// Write file
	if err := os.WriteFile(path, []byte(cf.Content), 0644); err != nil {
		return fmt.Errorf("write_to_file: %w", err)
	}

	cf.Created = true

	// Save artifact metadata sidecar if this is an artifact
	if cf.IsArtifact && cf.ArtifactMetadata != nil && r.conversation != nil {
		filename := filepath.Base(path)
		meta := r.conversationMeta(cf.ArtifactMetadata)
		if err := r.conversation.SaveArtifactMetadata(filename, meta); err != nil {
			// Non-fatal: artifact was created, metadata save failed
			return fmt.Errorf("write_to_file: artifact created but metadata save failed: %w", err)
		}

		// Dispatch artifact feedback if requested
		if cf.ArtifactMetadata.RequestFeedback {
			r.dispatchArtifactFeedback(path, filename, cf.ArtifactMetadata)
		}
	}

	return nil
}
