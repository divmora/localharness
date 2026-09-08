package tools

// registerCodeGraphTools registers schema-only entries for repository code-graph tools.
// These tools are engine-intercepted — the engine handles AST traversal and DuckDB queries directly.
func registerCodeGraphTools(r *Registry) {
	r.RegisterSchemaOnly("codegraph_search", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "codegraph_search",
		Description: "Search for code symbols (functions, types, structs, interfaces, methods, classes) in the repository AST code graph by name, pattern, or kind.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Symbol name or search pattern (e.g. 'ValidateToken', 'User')",
				},
				"kind": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"function", "method", "struct", "interface", "class", "type", "package", "message", "service"},
					"description": "Optional filter by symbol kind",
				},
				"branch": map[string]interface{}{
					"type":        "string",
					"description": "Branch name (defaults to active checked-out branch)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of symbols to return (default: 50)",
				},
			},
			"required": []string{"query"},
		},
	})

	r.RegisterSchemaOnly("codegraph_find_references", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "codegraph_find_references",
		Description: "Find all usages, callers, and references to a specific symbol in the code graph.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"symbol_id": map[string]interface{}{
					"type":        "string",
					"description": "Symbol ID (e.g. 'pkg/auth:ValidateToken') or symbol name",
				},
				"branch": map[string]interface{}{
					"type":        "string",
					"description": "Branch name (defaults to active branch)",
				},
			},
			"required": []string{"symbol_id"},
		},
	})

	r.RegisterSchemaOnly("codegraph_call_hierarchy", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "codegraph_call_hierarchy",
		Description: "Inspect the call hierarchy for a function/method (incoming callers or outgoing callees) across multiple hops.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"symbol_id": map[string]interface{}{
					"type":        "string",
					"description": "Function or method symbol ID / name",
				},
				"direction": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"incoming", "outgoing"},
					"description": "Direction of call tree: 'incoming' (callers) or 'outgoing' (callees). Default: 'incoming'",
				},
				"depth": map[string]interface{}{
					"type":        "integer",
					"description": "Traversal depth (default: 3, max: 10)",
				},
				"branch": map[string]interface{}{
					"type":        "string",
					"description": "Branch name (defaults to active branch)",
				},
			},
			"required": []string{"symbol_id"},
		},
	})

	r.RegisterSchemaOnly("codegraph_get_impact", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "codegraph_get_impact",
		Description: "Compute the blast radius and downstream impact before refactoring a function, struct, interface, or file. Returns all affected downstream callers, interface implementations, and affected files.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Symbol ID, function name, or workspace-relative file path (e.g. 'internal/auth/token.go')",
				},
				"depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum traversal depth (default: 5)",
				},
				"branch": map[string]interface{}{
					"type":        "string",
					"description": "Branch name (defaults to active branch)",
				},
			},
			"required": []string{"target"},
		},
	})

	r.RegisterSchemaOnly("codegraph_diff_branches", ToolSchema{
		Group:       ToolGroupRead,
		Name:        "codegraph_diff_branches",
		Description: "Compare the code graph between two branches to identify added, removed, and modified symbols, functions, and dependency edges.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"base_branch": map[string]interface{}{
					"type":        "string",
					"description": "Base branch name (e.g. 'main')",
				},
				"target_branch": map[string]interface{}{
					"type":        "string",
					"description": "Target/feature branch name (e.g. 'feat/auth')",
				},
			},
			"required": []string{"base_branch", "target_branch"},
		},
	})
}
