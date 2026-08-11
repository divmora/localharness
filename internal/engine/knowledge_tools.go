package engine

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/llm"
)

// executeKnowledgeWrite handles the knowledge_write tool call.
// Creates or updates a KI and writes an artifact file.
func (e *Engine) executeKnowledgeWrite(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, err := e.getKnowledgeStoreForTool(tc)
	if err != nil {
		return fmt.Errorf("knowledge_write: %w", err)
	}

	// Parse args
	kiName, _ := tc.Args["ki_name"].(string)
	summary, _ := tc.Args["summary"].(string)
	artifactPath, _ := tc.Args["artifact_path"].(string)
	content, _ := tc.Args["content"].(string)

	if kiName == "" {
		return fmt.Errorf("knowledge_write: ki_name is required")
	}
	if artifactPath == "" {
		return fmt.Errorf("knowledge_write: artifact_path is required")
	}
	if content == "" {
		return fmt.Errorf("knowledge_write: content is required")
	}

	// Parse references (optional array of strings)
	var refs []string
	if refsRaw, ok := tc.Args["references"]; ok {
		if refsArr, ok := refsRaw.([]interface{}); ok {
			for _, r := range refsArr {
				if s, ok := r.(string); ok {
					refs = append(refs, s)
				}
			}
		}
	}

	err = store.WriteArtifact(kiName, summary, artifactPath, content, refs)
	if err != nil {
		return fmt.Errorf("knowledge_write: %w", err)
	}

	// Result
	step.Text = fmt.Sprintf("Created/updated KI '%s', artifact '%s' (%d bytes written)", kiName, artifactPath, len(content))
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeKnowledgeReplace handles the knowledge_replace tool call.
// Performs search-and-replace within an existing KI artifact file.
func (e *Engine) executeKnowledgeReplace(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, err := e.getKnowledgeStoreForTool(tc)
	if err != nil {
		return fmt.Errorf("knowledge_replace: %w", err)
	}

	kiName, _ := tc.Args["ki_name"].(string)
	artifactPath, _ := tc.Args["artifact_path"].(string)
	target, _ := tc.Args["target"].(string)
	replacement, _ := tc.Args["replacement"].(string)

	if kiName == "" {
		return fmt.Errorf("knowledge_replace: ki_name is required")
	}
	if artifactPath == "" {
		return fmt.Errorf("knowledge_replace: artifact_path is required")
	}
	if target == "" {
		return fmt.Errorf("knowledge_replace: target is required")
	}

	err = store.ReplaceInArtifact(kiName, artifactPath, target, replacement)
	if err != nil {
		return fmt.Errorf("knowledge_replace: %w", err)
	}

	step.Text = fmt.Sprintf("Replaced content in KI '%s', artifact '%s'", kiName, artifactPath)
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeKnowledgeDelete handles the knowledge_delete tool call.
// Deletes an entire KI or a specific artifact within one.
func (e *Engine) executeKnowledgeDelete(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	store, err := e.getKnowledgeStoreForTool(tc)
	if err != nil {
		return fmt.Errorf("knowledge_delete: %w", err)
	}

	kiName, _ := tc.Args["ki_name"].(string)
	artifactPath, _ := tc.Args["artifact_path"].(string)

	if kiName == "" {
		return fmt.Errorf("knowledge_delete: ki_name is required")
	}

	if artifactPath != "" {
		// Delete specific artifact
		err := store.DeleteArtifact(kiName, artifactPath)
		if err != nil {
			return fmt.Errorf("knowledge_delete: %w", err)
		}
		step.Text = fmt.Sprintf("Deleted artifact '%s' from KI '%s'", artifactPath, kiName)
	} else {
		// Delete entire KI
		err := store.Delete(kiName)
		if err != nil {
			return fmt.Errorf("knowledge_delete: %w", err)
		}
		step.Text = fmt.Sprintf("Deleted KI '%s' and all its artifacts", kiName)
	}

	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// getKnowledgeStoreForTool resolves the correct KnowledgeStore based on tool arguments.
func (e *Engine) getKnowledgeStoreForTool(tc llm.ToolCall) (*KnowledgeStore, error) {
	scope, _ := tc.Args["scope"].(string)
	if scope == "" {
		scope = "workspace"
	}

	if scope == "global" {
		if e.globalKnowledgeStore == nil {
			return nil, fmt.Errorf("global knowledge store not available")
		}
		return e.globalKnowledgeStore, nil
	}

	if scope != "workspace" {
		return nil, fmt.Errorf("invalid scope %q", scope)
	}

	// Workspace scope
	if len(e.workspaceKnowledgeStores) == 0 {
		return nil, fmt.Errorf("no workspace knowledge stores available")
	}

	if len(e.workspaceKnowledgeStores) == 1 {
		for _, store := range e.workspaceKnowledgeStores {
			return store, nil
		}
	}

	workspacePath, _ := tc.Args["workspace_path"].(string)
	if workspacePath == "" {
		return nil, fmt.Errorf("multiple workspaces attached. You must specify 'workspace_path' when writing a workspace KI")
	}

	store, ok := e.workspaceKnowledgeStores[workspacePath]
	if !ok {
		return nil, fmt.Errorf("workspace %q not attached to session", workspacePath)
	}

	return store, nil
}
