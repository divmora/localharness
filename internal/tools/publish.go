package tools

// registerPublishTool registers the schema-only entry for the publish tool.
// This tool is engine-intercepted — the engine handles execution directly
// via the AgentBus. The schema is registered so the LLM can discover and call it.
func registerPublishTool(r *Registry) {
	r.RegisterSchemaOnly("publish", ToolSchema{
		Group:       ToolGroupWrite,
		Name:        "publish",
		Description: "Publish an artifact notification to the agent bus. Other agents in the same conversation tree will receive this notification and can read the artifact using view_file. Use this to share work products, status updates, or coordinate with peer agents.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Brief description of what you're publishing (e.g., 'Security scan complete — 3 vulnerabilities found')",
				},
				"artifact_path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the artifact file being shared (optional). Other agents can read this path with view_file.",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tags for categorizing the message (e.g., ['security', 'fix', 'review-needed'])",
				},
			},
			"required": []string{"summary"},
		},
	})
}
