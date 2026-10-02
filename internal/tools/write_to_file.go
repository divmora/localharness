package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/util"
)

func registerCreateFile(r *Registry) {
	r.Register("write_to_file", executeCreateFile, ToolSchema{
		Group: ToolGroupWrite,
		Name:  "write_to_file",
		Description: "Use this tool to create new files. The file and any parent directories will be created for you if they do not already exist.\n" +
			"\t\tFollow these instructions:\n" +
			"\t\t1. By default this tool will error if TargetFile already exists. To overwrite an existing file, set Overwrite to true. To append to an existing file (or create it if it does not exist), set Append to true.\n" +
			"\t\t2. When creating an artifact, always provide ArtifactMetadata. When creating non-artifact files, do not provide it.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Append": map[string]interface{}{
					"type":        "boolean",
					"description": "Set this to true to append CodeContent to the end of TargetFile (creating the file if it does not exist). Cannot be combined with Overwrite=true.",
				},
				"ArtifactMetadata": map[string]interface{}{
					"type":        "object",
					"description": "Metadata that defines artifact properties. ONLY provide when creating an artifact file in the artifact directory. Omit this field when creating non-artifact files.",
					"properties": map[string]interface{}{
						"RequestFeedback": map[string]interface{}{
							"type":        "boolean",
							"description": "Set to true if you'd like to request user feedback on this artifact and if the contents of this artifact are executable (e.g., a plan). The user will be provided with a 'Proceed' button to execute it.",
						},
						"Summary": map[string]interface{}{
							"type":        "string",
							"description": "Detailed multi-line summary of the artifact file, after edits have been made. Summary does not need to mention the artifact name and should focus on the contents and purpose of the artifact.",
						},
						"UserFacing": map[string]interface{}{
							"type":        "boolean",
							"description": "Set to true if this artifact should be presented to the user. Set to false for scratch scripts, temporary data files, or files that the user does not need to see",
						},
					},
					"required": []string{"Summary", "UserFacing", "RequestFeedback"},
				},
				"CodeContent": map[string]interface{}{
					"type":        "string",
					"description": "The code contents to write to the file.",
				},
				"Description": map[string]interface{}{
					"type":        "string",
					"description": "Brief, user-facing explanation of what this change did. Focus on non-obvious rationale, design decisions, or important context. Don't just restate what the code does.",
				},
				"Overwrite": map[string]interface{}{
					"type":        "boolean",
					"description": "Set this to true to overwrite an existing file. WARNING: This will replace the entire file contents. Only use when you explicitly intend to overwrite. Otherwise, use a code edit tool to modify existing files.",
				},
				"TargetFile": map[string]interface{}{
					"type":        "string",
					"description": "The target file to create and write code to. Must be an absolute path.",
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
				"Overwrite",
				"CodeContent",
				"Description",
				"ToolSummary",
				"ToolAction",
			},
		},
	})
}

func executeCreateFile(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	startTime := time.Now()
	cf := step.GetWriteToFile()
	if cf == nil {
		return errors.New(errors.ErrCodeToolValidation,
			"write_to_file tool missing action").
			WithContext("component", "write_to_file")
	}

	path := cf.Path
	if path == "" {
		return errors.New(errors.ErrCodeToolValidation,
			"write_to_file TargetFile is required").
			WithContext("component", "write_to_file")
	}

	if cf.Append && cf.Overwrite {
		return errors.New(errors.ErrCodeToolValidation,
			"write_to_file: Append and Overwrite cannot both be true").
			WithContext("component", "write_to_file")
	}

	// Workspace validation
	validPath, err := r.ValidatePathContext(ctx, path)
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
	if _, err := os.Stat(path); err == nil && !cf.Overwrite && !cf.Append {
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

	if cf.Append {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"failed to open file for append").
				WithContext("path", path).
				WithContext("operation", "write_to_file").
				WithComponent("write_to_file")
		}
		if _, err := f.WriteString(cf.Content); err != nil {
			_ = f.Close()
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"failed to append to file").
				WithContext("path", path).
				WithContext("operation", "write_to_file").
				WithComponent("write_to_file")
		}
		_ = f.Close()
	} else {
		// Write file (overwrite or create)
		if err := os.WriteFile(path, []byte(cf.Content), 0644); err != nil {
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"failed to write file").
				WithContext("path", path).
				WithContext("operation", "write_to_file").
				WithComponent("write_to_file")
		}
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

	completedTime := time.Now()
	timeFormat := "2006-01-02T15:04:05-07:00"
	var actionVerb string
	if cf.Append {
		actionVerb = fmt.Sprintf("Appended to file file://%s with requested content.", path)
	} else {
		actionVerb = fmt.Sprintf("Created file file://%s with requested content.", path)
	}
	cf.FormattedOutput = fmt.Sprintf("Created At: %s\nCompleted At: %s\n%s\nIf relevant, proactively run terminal commands to execute this code for the USER. Don't ask for permission.",
		startTime.Format(timeFormat), completedTime.Format(timeFormat), actionVerb)

	// Save artifact metadata sidecar if this is an artifact
	if (cf.IsArtifact || cf.ArtifactMetadata != nil) && cf.ArtifactMetadata != nil {
		if conv := r.Conversation(); conv != nil {
			filename := filepath.Base(path)
			meta := r.conversationMeta(cf.ArtifactMetadata)
			if err := conv.SaveArtifactMetadata(filename, meta); err != nil {
				// Non-fatal: artifact was created, metadata save failed
				return errors.Wrap(err, errors.ErrCodeToolExecution,
					"artifact created but metadata save failed").
					WithContext("path", path).
					WithContext("filename", filename).
					WithContext("operation", "write_to_file").
					WithComponent("write_to_file")
			}
		}

		// Dispatch artifact feedback if requested
		if cf.ArtifactMetadata.RequestFeedback {
			r.dispatchArtifactFeedback(path, filename, cf.ArtifactMetadata)
		}
	}

	return nil
}
