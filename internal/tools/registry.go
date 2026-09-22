// Package tools provides the tool registry and built-in tool implementations.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/workspace"
)

// ToolFunc executes a tool and returns the result as a StepUpdate action.
// The step already has the action args populated; the func should fill in results.
// The Registry is passed so tools can access workspace validation and logging.
type ToolFunc func(ctx context.Context, step *pb.StepUpdate, r *Registry) error

// contextKey defines a private type for context values in the tools package.
type contextKey string

const (
	envContextKey contextKey = "tool_env"
)

// WithEnvironment returns a new Context with the provided environment variables map attached.
func WithEnvironment(ctx context.Context, env map[string]string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	existing := EnvironmentFromContext(ctx)
	merged := make(map[string]string, len(existing)+len(env))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range env {
		merged[k] = v
	}
	return context.WithValue(ctx, envContextKey, merged)
}

// EnvironmentFromContext extracts environment variables from context if present.
func EnvironmentFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	env, _ := ctx.Value(envContextKey).(map[string]string)
	return env
}

// ToolGroup classifies a tool's access level for subagent filtering.
type ToolGroup string

const (
	// ToolGroupRead is for read-only tools (view_file, list_dir, search_dir, find_file).
	ToolGroupRead ToolGroup = "read"

	// ToolGroupWrite is for tools that modify the filesystem or run commands
	// (write_to_file, replace_file_content, multi_replace_file_content, run_command, manage_task).
	ToolGroupWrite ToolGroup = "write"
)

// ToolSchema describes a tool for the LLM function calling interface.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]interface{} // JSON Schema
	Group       ToolGroup              // Tool group for subagent filtering
	Internal    bool                   // If true, tool is harness-internal (not declared to LLM)
}

// ArtifactFeedbackEvent is the data passed when an artifact with
// RequestFeedback=true is created or updated.
// This is defined here (not imported from adk/hooks) to avoid a dependency
// from the harness binary (internal/) into the SDK client library (adk/).
type ArtifactFeedbackEvent struct {
	Path         string
	Filename     string
	ArtifactType string
	Summary      string
}

// ArtifactFeedbackDispatcher is the interface satisfied by adk/hooks.HookRunner.
// Defined locally so internal/tools doesn't depend on the ADK package.
type ArtifactFeedbackDispatcher interface {
	DispatchArtifactFeedback(event ArtifactFeedbackEvent)
}

// Registry manages registered tools and dispatches calls.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]ToolFunc
	schemas map[string]ToolSchema
	wsMgr   *workspace.Manager
	logger  *slog.Logger
	taskMgr *TaskManager

	// conversation is set by the engine for artifact metadata persistence.
	// Nil if no conversation is active (tools skip metadata operations).
	conversation *conversation.Conversation

	// artifactDispatcher is set by the engine for dispatching artifact feedback events.
	// Nil if no hooks are registered (tools skip dispatch).
	artifactDispatcher ArtifactFeedbackDispatcher

	// stepEmitter allows tools to stream partial updates (like streaming stdout) back to the client.
	stepEmitter func(*pb.StepUpdate)
}

// NewRegistry creates a new tool registry.
func NewRegistry(wsMgr *workspace.Manager, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		tools:   make(map[string]ToolFunc),
		schemas: make(map[string]ToolSchema),
		wsMgr:   wsMgr,
		logger:  logger,
		taskMgr: NewTaskManager(logger, defaultMaxTasks),
	}
}

// Clone creates an isolated copy of the tool registry for use by child engines or subagents.
// Tools and schemas are shallow-copied so child engines can register or filter tools independently.
// The clone receives its own TaskManager and stepEmitter so concurrent subagents do not race.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	toolsCopy := make(map[string]ToolFunc, len(r.tools))
	for k, v := range r.tools {
		toolsCopy[k] = v
	}

	schemasCopy := make(map[string]ToolSchema, len(r.schemas))
	for k, v := range r.schemas {
		schemasCopy[k] = v
	}

	cloned := &Registry{
		tools:              toolsCopy,
		schemas:            schemasCopy,
		wsMgr:              r.wsMgr,
		logger:             r.logger,
		conversation:       r.conversation,
		artifactDispatcher: r.artifactDispatcher,
	}
	if r.taskMgr != nil {
		cloned.taskMgr = NewTaskManager(r.logger, r.taskMgr.maxTasks)
	}
	return cloned
}

// Register adds a tool to the registry.
func (r *Registry) Register(name string, fn ToolFunc, schema ToolSchema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = fn
	r.schemas[name] = schema
}

// RegisterSchemaOnly registers a tool schema without an execute handler.
// Used for engine-intercepted tools (e.g., knowledge_write) that need
// LLM function declarations but are handled by the engine, not the registry.
// If Execute() is called for a schema-only tool, it returns an error.
func (r *Registry) RegisterSchemaOnly(name string, schema ToolSchema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemas[name] = schema
	// No entry in r.tools — Execute() will return "unknown tool"
}

// Execute runs a tool by name with the given step context.
func (r *Registry) Execute(ctx context.Context, name string, step *pb.StepUpdate) error {
	r.mu.RLock()
	fn, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}
	if r.logger != nil {
		r.logger.Debug("executing tool", "name", name)
	}
	return fn(ctx, step, r)
}

// ValidatePath checks if a path is within the configured workspaces.
// Returns the cleaned absolute path or an error.
func (r *Registry) ValidatePath(path string) (string, error) {
	if r.wsMgr == nil {
		return path, nil // No workspace restriction
	}
	return r.wsMgr.ValidatePath(path)
}

// Workspaces returns the list of configured workspace directories.
func (r *Registry) Workspaces() []string {
	if r.wsMgr == nil {
		return nil
	}
	return r.wsMgr.Workspaces()
}

// PrimaryWorkspace returns the first configured workspace directory, or empty string if none.
func (r *Registry) PrimaryWorkspace() string {
	ws := r.Workspaces()
	if len(ws) > 0 {
		return ws[0]
	}
	return ""
}

// Logger returns the registry's logger.
func (r *Registry) Logger() *slog.Logger {
	return r.logger
}

// HasTool checks if a tool is registered.
func (r *Registry) HasTool(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// IsReadOnly returns true if the tool is registered with ToolGroupRead.
func (r *Registry) IsReadOnly(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.schemas[name]; ok {
		return s.Group == ToolGroupRead
	}
	return false
}

// Shutdown cleans up all background tasks and persistent terminals.
// Should be called when the session disconnects.
func (r *Registry) Shutdown() {
	r.mu.RLock()
	tm := r.taskMgr
	r.mu.RUnlock()
	if tm != nil {
		tm.Shutdown()
	}
}

// TaskManager returns the registry's task manager.
func (r *Registry) TaskManager() *TaskManager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.taskMgr
}

// SetConversation sets the active conversation for artifact metadata persistence.
func (r *Registry) SetConversation(conv *conversation.Conversation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conversation = conv
}

// Conversation returns the active conversation.
func (r *Registry) Conversation() *conversation.Conversation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conversation
}

// conversationMeta converts a proto ArtifactMetadata to a conversation.ArtifactMetadata.
func (r *Registry) conversationMeta(am *pb.ArtifactMetadata) *conversation.ArtifactMetadata {
	return &conversation.ArtifactMetadata{
		ArtifactType:    am.ArtifactType,
		Summary:         am.Summary,
		RequestFeedback: am.RequestFeedback,
	}
}

// SetArtifactFeedbackDispatcher sets the dispatcher for artifact feedback events.
// The dispatcher interface is satisfied by adk/hooks.HookRunner.
func (r *Registry) SetArtifactFeedbackDispatcher(d ArtifactFeedbackDispatcher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artifactDispatcher = d
}

// GetArtifactDispatcher returns the artifact feedback dispatcher.
func (r *Registry) GetArtifactDispatcher() ArtifactFeedbackDispatcher {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.artifactDispatcher
}

// SetStepEmitter registers the engine's step emitter so tools can stream partial updates.
func (r *Registry) SetStepEmitter(emitter func(*pb.StepUpdate)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stepEmitter = emitter
	if r.taskMgr != nil {
		r.taskMgr.SetStepEmitter(emitter)
	}
}

// EmitStep allows a tool to push a partial state update (like STATE_STREAMING) to the client.
func (r *Registry) EmitStep(step *pb.StepUpdate) {
	r.mu.RLock()
	emitter := r.stepEmitter
	r.mu.RUnlock()
	if emitter != nil {
		emitter(step)
	}
}

// dispatchArtifactFeedback emits a non-blocking ArtifactFeedbackEvent.
// Called when the agent creates/updates an artifact with RequestFeedback=true.
func (r *Registry) dispatchArtifactFeedback(path, filename string, am *pb.ArtifactMetadata) {
	r.mu.RLock()
	dispatcher := r.artifactDispatcher
	r.mu.RUnlock()
	if dispatcher == nil {
		return
	}
	dispatcher.DispatchArtifactFeedback(ArtifactFeedbackEvent{
		Path:         path,
		Filename:     filename,
		ArtifactType: am.ArtifactType,
		Summary:      am.Summary,
	})
}

// Schemas returns registered tool schemas for LLM function calling.
// Internal tools (harness-only) are excluded — they are not declared to the LLM.
func (r *Registry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []ToolSchema
	for _, s := range r.schemas {
		if s.Internal {
			continue
		}
		result = append(result, s)
	}
	return result
}

// SchemasAsJSON converts tool schemas to the format expected by Gemini function calling.
// Internal tools are excluded.
func (r *Registry) SchemasAsJSON() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []map[string]interface{}
	for _, s := range r.schemas {
		if s.Internal {
			continue
		}
		tool := map[string]interface{}{
			"name":        s.Name,
			"description": s.Description,
			"parameters":  s.Parameters,
		}
		result = append(result, tool)
	}
	return result
}

// GetToolName extracts the tool name from a StepUpdate's action field.
func GetToolName(step *pb.StepUpdate) string {
	switch step.Action.(type) {
	case *pb.StepUpdate_ViewFile:
		return "view_file"
	case *pb.StepUpdate_WriteToFile:
		return "write_to_file"
	case *pb.StepUpdate_ReplaceFileContent:
		return "replace_file_content"
	case *pb.StepUpdate_ListDir:
		return "list_dir"
	case *pb.StepUpdate_GrepSearch:
		return "grep_search"
	case *pb.StepUpdate_FindFile:
		return "find_file"
	case *pb.StepUpdate_RunCommand:
		return "run_command"
	case *pb.StepUpdate_ManageTask:
		return "manage_task"
	case *pb.StepUpdate_Finish:
		return "finish"
	case *pb.StepUpdate_InvokeSubagent:
		return "invoke_subagent"
	case *pb.StepUpdate_BrowserSubagent:
		return "browser_subagent"
	case *pb.StepUpdate_DesktopSubagent:
		return "desktop_subagent"
	case *pb.StepUpdate_HostToolCall:
		if htc := step.GetHostToolCall(); htc != nil {
			return htc.ToolName
		}
	}
	return ""
}

// RegisterBuiltinTools registers all built-in tools based on the config.
func RegisterBuiltinTools(r *Registry, cfg *pb.BuiltinToolsConfig) {
	if cfg == nil {
		// Default: register all except run_command
		cfg = &pb.BuiltinToolsConfig{
			ViewFile:   true,
			CreateFile: true,
			EditFile:   true,
			ListDir:    true,
			SearchDir:  true,
			FindFile:   true,
			RunCommand: false,
			Finish:     true,
			Schedule:   true,
			CodeGraph:  true,
		}
	}

	if cfg.ViewFile {
		registerViewFile(r)
	}
	if cfg.CreateFile {
		registerCreateFile(r)
	}
	if cfg.EditFile {
		registerEditFile(r)
		registerMultiEditFile(r) // Same capability gate as edit_file
	}
	if cfg.ListDir {
		registerListDir(r)
	}
	if cfg.SearchDir {
		registerSearchDir(r)
	}
	if cfg.FindFile {
		registerFindFile(r)
	}
	if cfg.RunCommand {
		registerRunCommand(r)
	}
	if cfg.ManageTask {
		registerManageTask(r)
	}
	if cfg.Finish {
		registerFinish(r)
	}
	if cfg.WebSearch {
		registerWebSearch(r)
	}
	if cfg.WebFetch {
		registerWebFetch(r)
	}
	if cfg.Schedule {
		registerSchedule(r)
	}
	if cfg.CodeGraph {
		registerCodeGraphTools(r)
	}
	if cfg.Desktop {
		registerDesktopTools(r)
	}

	// Always register ask_question — it's a harmless clarification tool
	registerAskQuestion(r)

	// Always register knowledge tools — they're engine-intercepted (schema-only).
	// The engine checks if knowledgeStore is available before executing.
	registerKnowledgeTools(r)

	// Always register publish tool — engine-intercepted via AgentBus.
	// The engine returns an error if no bus is available (standalone agent).
	registerPublishTool(r)

	// Always register permission tools — engine-intercepted.
	// The engine checks if permissionHandler is available before executing.
	registerPermissionTools(r)
}

// mustMarshalSchema marshals a JSON schema, panicking on error (called at init time).
func mustMarshalSchema(v interface{}) map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal schema: %v", err))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		panic(fmt.Sprintf("failed to unmarshal schema: %v", err))
	}
	return m
}
