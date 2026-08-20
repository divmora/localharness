package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/util"
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
		return errors.New(errors.ErrCodeToolValidation,
			"write_to_file tool missing action").
			WithContext("component", "write_to_file")
	}

	path := cf.Path
	if path == "" {
		return errors.New(errors.ErrCodeToolValidation,
			"write_to_file path is required").
			WithContext("component", "write_to_file")
	}

	// Workspace validation
	validPath, err := r.ValidatePath(path)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
			"workspace validation failed").
			WithContext("path", path).
			WithContext("operation", "write_to_file").
			WithComponent("write_to_file")
	}
	path = validPath
	cf.Path = path

	// Check if file already exists
	if _, err := os.Stat(path); err == nil && !cf.Overwrite {
		return errors.New(errors.ErrCodeToolValidation,
			"file already exists").
			WithContext("path", path).
			WithContext("operation", "write_to_file").
			WithComponent("write_to_file")
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrap(err, errors.ErrCodeToolExecution,
			"failed to create directory").
			WithContext("directory", dir).
			WithContext("path", path).
			WithContext("operation", "write_to_file").
			WithComponent("write_to_file")
	}

	var oldContent string
	if data, readErr := os.ReadFile(path); readErr == nil {
		oldContent = string(data)
	}

	// Write file
	if err := os.WriteFile(path, []byte(cf.Content), 0644); err != nil {
		return errors.Wrap(err, errors.ErrCodeToolExecution,
			"failed to write file").
			WithContext("path", path).
			WithContext("operation", "write_to_file").
			WithComponent("write_to_file")
	}

	cf.Created = true
	filename := filepath.Base(path)
	diff := util.UnifiedDiff("a/"+filename, "b/"+filename, oldContent, cf.Content)
	if diff == "" && oldContent == "" && cf.Content != "" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("--- /dev/null\n+++ b/%s\n", filename))
		lines := strings.Split(cf.Content, "\n")
		sb.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
		for _, l := range lines {
			sb.WriteString("+" + l + "\n")
		}
		diff = sb.String()
	}
	cf.DiffBlock = diff

	// Save artifact metadata sidecar if this is an artifact
	if cf.IsArtifact && cf.ArtifactMetadata != nil && r.conversation != nil {
		filename := filepath.Base(path)
		meta := r.conversationMeta(cf.ArtifactMetadata)
		if err := r.conversation.SaveArtifactMetadata(filename, meta); err != nil {
			// Non-fatal: artifact was created, metadata save failed
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"artifact created but metadata save failed").
				WithContext("path", path).
				WithContext("filename", filename).
				WithContext("operation", "write_to_file").
				WithComponent("write_to_file")
		}

		// Dispatch artifact feedback if requested
		if cf.ArtifactMetadata.RequestFeedback {
			r.dispatchArtifactFeedback(path, filename, cf.ArtifactMetadata)
		}
	}

	return nil
}
