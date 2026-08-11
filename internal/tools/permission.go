package tools

import (
	"context"
	"fmt"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// registerPermissionTools registers ask_permission and list_permissions.
// Both are engine-intercepted and Internal by default — the harness/SDK uses
// them programmatically, but they are NOT declared to the LLM. SDKs that want
// the AI to call these directly can set Internal=false on the schema.
func registerPermissionTools(r *Registry) {
	r.Register("ask_permission", executeAskPermission, ToolSchema{
		Name:     "ask_permission",
		Internal: true, // Harness-only by default; SDK can override
		Description: "Request additional permissions after a tool call fails due to insufficient access. " +
			"Use this when a file read, write, or command is denied because it targets a path outside " +
			"the workspace or a command that requires explicit approval. " +
			"Specify the narrowest scope that covers your planned operations. " +
			"The request blocks until the user approves or denies.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{"read_file", "write_file", "command"},
					"description": "The type of access needed: " +
						"'read_file' for reading files/directories, " +
						"'write_file' for writing files/directories (also covers reads), " +
						"'command' for running specific shell commands.",
				},
				"target": map[string]interface{}{
					"type": "string",
					"description": "The target of the permission. " +
						"For read_file/write_file: absolute path to a file or directory. " +
						"For command: command prefix (e.g., 'git' matches 'git add', 'git commit').",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Why this permission is needed. Be specific about what you plan to do.",
				},
			},
			"required": []string{"action", "target", "reason"},
		},
	})

	r.Register("list_permissions", executeListPermissions, ToolSchema{
		Name:     "list_permissions",
		Internal: true, // Harness-only by default; SDK can override
		Description: "List all permissions currently granted in this session. " +
			"Use this to understand what files, directories, and commands you can " +
			"access without triggering a permission prompt.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
}

// executeAskPermission is engine-intercepted — the engine handles it via
// the PermissionHandler callback. This fallback returns an error if the
// engine fails to intercept.
func executeAskPermission(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	return fmt.Errorf("ask_permission should be handled by the engine, not the tool registry")
}

// executeListPermissions is engine-intercepted — the engine handles it
// by returning the current permission grants. This fallback returns an
// error if the engine fails to intercept.
func executeListPermissions(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	return fmt.Errorf("list_permissions should be handled by the engine, not the tool registry")
}
