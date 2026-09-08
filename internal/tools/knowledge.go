package tools

// registerKnowledgeTools registers schema-only entries for knowledge tools.
// These tools are engine-intercepted — the engine handles execution directly.
// The schemas are registered so the LLM can discover and call them.
func registerKnowledgeTools(r *Registry) {
	r.RegisterSchemaOnly("knowledge_write", ToolSchema{
		Group:       ToolGroupWrite,
		Name:        "knowledge_write",
		Description: "Create or update a Knowledge Item (KI) artifact. Creates the KI directory and metadata if it doesn't exist. Use this to persist curated knowledge about the codebase (patterns, conventions, known issues) for future conversations.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ki_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the knowledge item (kebab-case, e.g. 'error-handling-patterns')",
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Short summary of the KI (1-2 sentences). Shown in per-conversation KI listings.",
				},
				"artifact_path": map[string]interface{}{
					"type":        "string",
					"description": "Relative filename within the KI (e.g. 'overview.md', 'patterns.md')",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the artifact file",
				},
				"references": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Source files this KI was derived from (relative workspace paths)",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"workspace", "global"},
					"description": "Whether this knowledge applies globally or only to the current workspace (default: 'workspace')",
				},
				"workspace_path": map[string]interface{}{
					"type":        "string",
					"description": "Required only if multiple workspaces are attached and scope is 'workspace'. The absolute path of the workspace to save this KI into.",
				},
			},
			"required": []string{"ki_name", "artifact_path", "content"},
		},
	})

	r.RegisterSchemaOnly("knowledge_replace", ToolSchema{
		Group:       ToolGroupWrite,
		Name:        "knowledge_replace",
		Description: "Replace content within an existing Knowledge Item artifact file. Performs a single search-and-replace operation.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ki_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the knowledge item",
				},
				"artifact_path": map[string]interface{}{
					"type":        "string",
					"description": "Relative filename within the KI",
				},
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Exact text to find and replace",
				},
				"replacement": map[string]interface{}{
					"type":        "string",
					"description": "Replacement text",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"workspace", "global"},
					"description": "Whether this knowledge applies globally or only to the current workspace (default: 'workspace')",
				},
				"workspace_path": map[string]interface{}{
					"type":        "string",
					"description": "Required only if multiple workspaces are attached and scope is 'workspace'. The absolute path of the workspace KI resides in.",
				},
			},
			"required": []string{"ki_name", "artifact_path", "target", "replacement"},
		},
	})

	r.RegisterSchemaOnly("knowledge_delete", ToolSchema{
		Group:       ToolGroupWrite,
		Name:        "knowledge_delete",
		Description: "Delete a Knowledge Item or a specific artifact within one. If artifact_path is omitted, deletes the entire KI.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ki_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the knowledge item to delete",
				},
				"artifact_path": map[string]interface{}{
					"type":        "string",
					"description": "If set, delete only this artifact. If omitted, delete the entire KI.",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"workspace", "global"},
					"description": "Whether this knowledge applies globally or only to the current workspace (default: 'workspace')",
				},
				"workspace_path": map[string]interface{}{
					"type":        "string",
					"description": "Required only if multiple workspaces are attached and scope is 'workspace'. The absolute path of the workspace KI resides in.",
				},
			},
			"required": []string{"ki_name"},
		},
	})
}
