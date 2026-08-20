// Package engine implements the agentic loop: LLM → tool calls → results → LLM.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/llm"
	mcpbridge "github.com/divmora/localharness/internal/mcp"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/util"
)

// StepCallback is called whenever a step update should be sent to the client.
type StepCallback func(step *pb.StepUpdate)

// TrajectoryCallback is called for trajectory state changes.
type TrajectoryCallback func(state *pb.TrajectoryState)

// HostToolHandler is called when the model invokes a host-side (SDK-registered) tool.
// It receives the tool call and the step, and must return the result JSON.
// The handler blocks until the SDK client sends back a ToolResult.
type HostToolHandler func(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (resultJSON string, isError bool, err error)

// PermissionHandler is called before executing any tool when the SDK needs
// to evaluate its policies. It emits an ActionPermissionRequest (STATE_WAITING)
// and blocks until the SDK sends a PermissionResponse.
type PermissionHandler func(ctx context.Context, req *pb.ActionPermissionRequest) (approved bool, denialReason string, err error)

// PermissionGrant represents a permission that has been granted for this session
// via the ask_permission tool. Grants persist for the lifetime of the engine.
type PermissionGrant struct {
	Action string // "read_file", "write_file", or "command"
	Target string // Absolute path (for file ops) or command prefix
	Reason string // Why it was granted
}

// QuestionHandler is called when the model invokes the ask_question tool.
// It emits an ActionUserQuestion (STATE_WAITING) and blocks until the SDK
// sends a QuestionResponse. Returns the user's answers.
type QuestionHandler func(ctx context.Context, req *pb.ActionUserQuestion) (*pb.QuestionResponse, error)

// Engine orchestrates the agentic loop.
type Engine struct {
	provider            llm.Provider
	toolRegistry        *tools.Registry
	logger              *slog.Logger
	stepCB              StepCallback
	trajCB              TrajectoryCallback
	stepIndex           atomic.Int32
	trajectoryID        string
	convID              string
	sysPrompt           string
	history             []llm.Message
	maxTurns            int // Safety limit on agentic loop iterations
	compactionThreshold int // Token threshold for context compaction (0 = disabled)
	keepRecentMessages  int // Messages to preserve during compaction
	lastRealTokenCount  int // Most recent real token count from LLM provider
	tracer              *Tracer
	brainDir            string                       // For child engine tracing
	appDataDir          string                       // Root data dir (for subagent inheritance)
	enablePlanningMode  bool                         // Planning guard: block workspace writes until plan exists
	researchToolCount   int                          // Tracks research tool calls (view_file, list_dir, search_dir)
	hostToolHandler     HostToolHandler              // Called for SDK-registered tools
	hostToolNames       map[string]bool              // Fast lookup of host tool names
	hostToolDecls       []llm.FunctionDeclaration    // Host tool schemas for LLM
	permissionHandler   PermissionHandler            // Called before tool execution for policy checks
	permissionGrants    []PermissionGrant            // Grants from ask_permission (session-scoped)
	questionHandler     QuestionHandler              // Called for ask_question tool
	mcpMgr              *mcpbridge.Manager           // MCP server bridge (nil if no MCP servers)
	msgCtx              MessageContextConfig         // Per-message context enrichment config
	notifyCh            <-chan tools.SystemMessage    // System notifications (timers, task completions)
	running             atomic.Int32                 // 1 = engine is running a turn, 0 = idle
	preCompletionHook   func()                       // Called before TRAJ_IDLE so session can save state

	// Interruption API channels
	pauseCh  chan struct{}
	resumeCh chan string

	// Subagent support
	depth              int           // Nesting depth (0 = root)
	maxDepth           int           // Max nesting depth (default: 3)
	maxSubagents       int           // Max concurrent children (default: 5)
	activeSubagents    int32         // Atomic counter of running children
	parentTrajectoryID string        // Empty for root trajectory
	subagentsEnabled   bool          // Whether subagent tools are available
	subagentRegistry   *SubagentRegistry // Type registry (built-in + SDK + agent-defined)
	subagentTracker    *SubagentTracker  // Active instance tracker

	// Tool group filtering — used by subagents to restrict tool access.
	// Keys are ToolGroup values ("read", "write"). If a group is in this set,
	// tools in that group are excluded from declarations.
	excludeToolGroups map[tools.ToolGroup]bool
	excludeHostTools  bool // If true, host (SDK-registered) tools are not declared
	excludeMCPTools   bool // If true, MCP tools are not declared

	// Knowledge Items
	globalKnowledgeStore     *KnowledgeStore
	workspaceKnowledgeStores map[string]*KnowledgeStore // keyed by absolute workspace path

	// Multi-agent coordination
	agentBus *AgentBus              // Shared pub/sub bus across agent family (nil for standalone)
	convMgr  *conversation.Manager // For creating child conversations (nil if not passed)
	conv     *conversation.Conversation // Subagent's own conversation (nil for root — Session manages it)

	mu             sync.RWMutex
	workspaces     []string
	workspaceInfos []WorkspaceInfo
	userRules      []config.UserRule
	yoloMode       bool
}

// Config holds engine configuration.
type Config struct {
	Provider            llm.Provider
	ToolRegistry        *tools.Registry
	SystemPrompt        string
	ConversationID      string
	TrajectoryID        string
	OnStep              StepCallback
	OnTrajectory        TrajectoryCallback
	MaxTurns            int
	CompactionThreshold int    // 0 = disabled, >0 = token threshold
	KeepRecentMessages  int    // 0 = default (10)
	BrainDir            string // For tracing; empty = tracing disabled
	AppDataDir          string // Root data directory (e.g. ~/.divmora/localharness)
	Logger              *slog.Logger
	HostToolHandler     HostToolHandler            // Called for SDK-registered tools
	HostToolNames       map[string]bool            // Set of host tool names
	HostToolDecls       []llm.FunctionDeclaration  // Host tool schemas for LLM
	PermissionHandler   PermissionHandler          // Called before tool execution for policy checks
	QuestionHandler     QuestionHandler            // Called for ask_question tool
	InitialHistory      []llm.Message              // Initial message history to restore context
	MCPManager          *mcpbridge.Manager         // MCP server bridge (nil if no MCP servers)
	YoloMode            bool                       // Bypass all tool permission checks


	// Structured system instructions (takes priority over SystemPrompt if set)
	StructuredInstructions *pb.StructuredSystemInstructions

	// Per-message context enrichment
	Workspaces     []string          // Workspace directories (for internal systems)
	WorkspaceInfos []WorkspaceInfo   // Workspace definitions with corpus mappings (for per-message context)
	UserRules    []config.UserRule // Content from AGENTS.md files

	// Subagent support
	Depth              int            // Nesting depth (0 = root)
	MaxDepth           int            // 0 = default (3)
	MaxSubagents       int            // 0 = default (5)
	ParentTrajectoryID string         // Empty for root trajectory
	SubagentsEnabled   bool           // Whether subagent tools are available

	// Subagent types — SDK-registered custom types to merge with built-ins.
	SubagentTypes           []SubagentTypeDef // SDK-registered custom types
	ExcludeBuiltinSubagents []string          // Built-in type names to exclude
	DisableAllBuiltins      bool              // Disable ALL built-in subagent types

	// Prompt modules
	EnableWebDev            bool             // Enable the <web_application_development> section (off by default)
	EnablePlanningMode      bool             // Enable the <planning_mode> section (off by default)
	EnableSlashCommands     bool             // Enable the <slash_commands> section (off by default)
	SlashCommands           []SlashCommandDef // Available slash commands (only with EnableSlashCommands)
	EnableKnowledgeItems    bool             // Enable the <knowledge_items> section (off by default)
	Skills                  []SkillDef       // Available skills (data-driven: non-empty = enabled)
	Plugins                 []PluginDef      // Installed plugins (data-driven: non-empty = enabled)

	// System notification channel (from timers, background tasks)
	NotifyCh <-chan tools.SystemMessage

	// Tool group filtering for subagents.
	// ExcludeToolGroups is a set of ToolGroup values to hide from the LLM.
	ExcludeToolGroups map[tools.ToolGroup]bool
	ExcludeHostTools  bool // Hide SDK-registered host tools
	ExcludeMCPTools   bool // Hide MCP tools

	// Knowledge Items — project registry for workspace → project UUID mapping.
	ProjectRegistry *ProjectRegistry

	// Multi-agent coordination
	AgentBus             *AgentBus              // Shared pub/sub bus (root creates, children inherit)
	ConversationManager  *conversation.Manager  // For creating child conversations
	ParentConversationID string                 // Parent's conv ID (empty for root)
	AgentRole            string                 // Human-readable role: "developer", "reviewer"
	AgentTypeName        string                 // Type name: "research", "self", etc.
}

// NewEngine creates a new agentic engine.
func NewEngine(cfg Config) *Engine {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 200 // Default safety limit
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.KeepRecentMessages <= 0 {
		cfg.KeepRecentMessages = 10 // Default
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = defaultMaxDepth
	}
	if cfg.MaxSubagents <= 0 {
		cfg.MaxSubagents = defaultMaxSubagents
	}

	// Build subagent registry (merge built-in + SDK types)
	var subagentTypes []SubagentTypeDef
	subagentRegistry := NewSubagentRegistry(
		BuiltinSubagentTypes(),
		cfg.SubagentTypes,
		cfg.ExcludeBuiltinSubagents,
		cfg.DisableAllBuiltins,
	)
	if cfg.SubagentsEnabled {
		subagentTypes = subagentRegistry.List()
	}

	// Create subagent tracker with parent notification channel
	var trackerNotifyCh chan tools.SystemMessage
	if cfg.SubagentsEnabled {
		trackerNotifyCh = make(chan tools.SystemMessage, 32)
	}
	subagentTracker := NewSubagentTracker(trackerNotifyCh)

	// Project registry is still used for other project-level context
	var projectID string
	if cfg.ProjectRegistry != nil && len(cfg.Workspaces) > 0 {
		if project, _ := cfg.ProjectRegistry.FindOrCreate(cfg.Workspaces); project != nil {
			projectID = project.ID
		}
	}

	// Initialize global knowledge store
	var globalKnowledgeStore *KnowledgeStore
	if cfg.AppDataDir != "" {
		globalKnowledgeStore = NewKnowledgeStore(filepath.Join(cfg.AppDataDir, "knowledge", "global"))
		if err := globalKnowledgeStore.Load(); err != nil {
			cfg.Logger.Warn("failed to load global knowledge store", "error", err)
		}
	}

	// Initialize workspace knowledge stores (read-only workspaces will gracefully ignore mkdir on write)
	workspaceKnowledgeStores := make(map[string]*KnowledgeStore)
	for _, ws := range cfg.Workspaces {
		wsStore := NewKnowledgeStore(filepath.Join(ws, ".agents", "knowledge"))
		if err := wsStore.Load(); err != nil {
			cfg.Logger.Warn("failed to load workspace knowledge store", "workspace", ws, "error", err)
		}
		// Pass just this workspace for staleness check
		wsStore.CheckStaleness([]string{ws})
		workspaceKnowledgeStores[ws] = wsStore
	}

	// Build system prompt from structured instructions or raw string
	sysPrompt := BuildSystemPrompt(SystemPromptConfig{
		UserInstructions:       cfg.SystemPrompt,
		Structured:             cfg.StructuredInstructions,
		EnableWebDev:           cfg.EnableWebDev,
		EnablePlanningMode:     cfg.EnablePlanningMode,
		EnableSlashCommands:    cfg.EnableSlashCommands,
		SlashCommands:          cfg.SlashCommands,
		EnableKnowledgeItems:   cfg.EnableKnowledgeItems,
		Skills:                 cfg.Skills,
		Plugins:                cfg.Plugins,
		SubagentsEnabled:       cfg.SubagentsEnabled,
		SubagentTypes:          subagentTypes,
		BrainDir:               cfg.BrainDir,
	})

	// Initialize agent bus: root creates, children inherit.
	bus := cfg.AgentBus
	if bus == nil && cfg.SubagentsEnabled {
		bus = NewAgentBus()
	}

	eng := &Engine{
		provider:            cfg.Provider,
		toolRegistry:        cfg.ToolRegistry,
		logger:              cfg.Logger,
		stepCB:              cfg.OnStep,
		trajCB:              cfg.OnTrajectory,
		trajectoryID:        cfg.TrajectoryID,
		convID:              cfg.ConversationID,
		sysPrompt:           sysPrompt,
		maxTurns:            cfg.MaxTurns,
		compactionThreshold: cfg.CompactionThreshold,
		keepRecentMessages:  cfg.KeepRecentMessages,
		tracer:              NewTracer(cfg.BrainDir, cfg.Logger),
		brainDir:            cfg.BrainDir,
		appDataDir:          cfg.AppDataDir,
		enablePlanningMode:  cfg.EnablePlanningMode,
		hostToolHandler:     cfg.HostToolHandler,
		hostToolNames:       cfg.HostToolNames,
		hostToolDecls:       cfg.HostToolDecls,
		permissionHandler:   cfg.PermissionHandler,
		questionHandler:     cfg.QuestionHandler,
		history:             cfg.InitialHistory,
		mcpMgr:              cfg.MCPManager,
		pauseCh:             make(chan struct{}, 1),
		resumeCh:            make(chan string, 1),
		msgCtx: MessageContextConfig{
			ConversationID: cfg.ConversationID,
			AppDataDir:     cfg.AppDataDir,
			BrainDir:       cfg.BrainDir,
			ProjectID:      projectID,
			Workspaces:     cfg.WorkspaceInfos,
			UserRules:      cfg.UserRules,
			Skills:         cfg.Skills,
			Plugins:        cfg.Plugins,
			SlashCommands:  cfg.SlashCommands,
			SubagentTypes:  subagentTypes,
		},
		notifyCh:            cfg.NotifyCh,
		depth:              cfg.Depth,
		maxDepth:           cfg.MaxDepth,
		maxSubagents:       cfg.MaxSubagents,
		parentTrajectoryID: cfg.ParentTrajectoryID,
		subagentsEnabled:   cfg.SubagentsEnabled,
		subagentRegistry:   subagentRegistry,
		subagentTracker:    subagentTracker,
		excludeToolGroups:  cfg.ExcludeToolGroups,
		excludeHostTools:   cfg.ExcludeHostTools,
		excludeMCPTools:          cfg.ExcludeMCPTools,
		globalKnowledgeStore:     globalKnowledgeStore,
		workspaceKnowledgeStores: workspaceKnowledgeStores,
		agentBus:                 bus,
		convMgr:                  cfg.ConversationManager,
		workspaces:               cfg.Workspaces,
		workspaceInfos:           cfg.WorkspaceInfos,
		userRules:                cfg.UserRules,
		yoloMode:                 cfg.YoloMode,
	}

	if eng.toolRegistry != nil {
		eng.toolRegistry.SetStepEmitter(eng.emitStep)
	}

	return eng
}

// SetYoloMode enables or disables YOLO mode (bypassing all permission checks).
func (e *Engine) SetYoloMode(enabled bool) {
	e.mu.Lock()
	e.yoloMode = enabled
	e.mu.Unlock()
}


// Run executes the agentic loop for a user message.
// It calls the LLM, dispatches tool calls, feeds results back, and repeats
// until the LLM returns a text response (no more tool calls) or finish is called.
func (e *Engine) Run(ctx context.Context, userMessage string) error {
	return e.RunWithContext(ctx, userMessage, nil)
}

// IsIdle returns true if the engine is not currently running a turn.
// Used by the session's auto-wake to decide if a synthetic turn can be started.
func (e *Engine) IsIdle() bool {
	return e.running.Load() == 0
}

// Interrupt signals the engine to pause execution at the next available turn boundary.
// It is non-blocking and safe to call concurrently.
func (e *Engine) Interrupt() {
	select {
	case e.pauseCh <- struct{}{}:
		e.logger.Info("interrupt signal sent to engine")
	default:
		// Already paused or pause pending
	}
}

// Resume signals a paused engine to resume execution.
// If injectedMsg is not empty, it is appended to the context as a human command.
func (e *Engine) Resume(injectedMsg string) {
	select {
	case e.resumeCh <- injectedMsg:
		e.logger.Info("resume signal sent to engine", "has_injected_msg", injectedMsg != "")
	default:
		// Not paused or resume already pending
	}
}

// AddWorkspace dynamically adds a workspace directory to the engine and reloads AGENTS.md rules.
func (e *Engine) AddWorkspace(ws string, info WorkspaceInfo) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, existing := range e.workspaces {
		if existing == ws {
			return
		}
	}
	e.workspaces = append(e.workspaces, ws)
	e.workspaceInfos = append(e.workspaceInfos, info)

	// Reload user rules with new workspace
	discoveredRules := config.LoadAgentsRules(e.workspaces, e.logger)
	var sdkRules []config.UserRule
	for _, r := range e.userRules {
		if !strings.HasSuffix(r.Filename, "AGENTS.md") {
			sdkRules = append(sdkRules, r)
		}
	}
	e.userRules = append(sdkRules, discoveredRules...)
	e.msgCtx.Workspaces = e.workspaceInfos
	e.msgCtx.UserRules = e.userRules
}

// RemoveWorkspace dynamically removes a workspace directory from the engine.
func (e *Engine) RemoveWorkspace(ws string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var updatedWS []string
	var updatedInfos []WorkspaceInfo
	for i, existing := range e.workspaces {
		if existing != ws {
			updatedWS = append(updatedWS, existing)
			if i < len(e.workspaceInfos) {
				updatedInfos = append(updatedInfos, e.workspaceInfos[i])
			}
		}
	}
	e.workspaces = updatedWS
	e.workspaceInfos = updatedInfos

	// Reload rules
	discoveredRules := config.LoadAgentsRules(e.workspaces, e.logger)
	var sdkRules []config.UserRule
	for _, r := range e.userRules {
		if !strings.HasSuffix(r.Filename, "AGENTS.md") {
			sdkRules = append(sdkRules, r)
		}
	}
	e.userRules = append(sdkRules, discoveredRules...)
	e.msgCtx.Workspaces = e.workspaceInfos
	e.msgCtx.UserRules = e.userRules
}

// Workspaces returns the list of current workspace directories.
func (e *Engine) Workspaces() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, len(e.workspaces))
	copy(out, e.workspaces)
	return out
}

// SetEphemeralMessages sets ADK-injected ephemeral directives for the next turn.
// These are merged with any engine-internal ephemeral messages and rendered as
// <EPHEMERAL_MESSAGE> blocks in the enriched user prompt. The messages are
// consumed (cleared) at the start of each turn.
func (e *Engine) SetEphemeralMessages(msgs []string) {
	e.msgCtx.EphemeralMessages = append(e.msgCtx.EphemeralMessages, msgs...)
}

// SetSettingsChanges sets ADK-injected settings changes for the next turn.
// These are rendered as <USER_SETTINGS_CHANGE> blocks in the enriched user prompt
// so the model can adapt to settings changes (e.g., model switch, mode toggle).
// The changes are consumed (cleared) at the start of each turn.
func (e *Engine) SetSettingsChanges(changes []SettingsChange) {
	e.msgCtx.SettingsChanges = append(e.msgCtx.SettingsChanges, changes...)
}

// SetPreCompletionHook registers a callback that runs after the engine's
// final response but BEFORE TRAJ_IDLE is emitted over the WebSocket.
// The session uses this to save conversation state before the SDK can
// kill the harness process in response to TRAJ_IDLE.
func (e *Engine) SetPreCompletionHook(fn func()) {
	e.preCompletionHook = fn
}

// RunWithContext executes the agentic loop with optional per-message host context.
// The host context (active file, cursor, etc.) is injected into the enriched message
// sent to the LLM, while the raw userMessage is preserved for step updates.
func (e *Engine) RunWithContext(ctx context.Context, userMessage string, hostCtx *pb.UserContext) error {
	e.running.Store(1)
	defer e.running.Store(0)

	// Run pre-completion hook on ALL exit paths (success, max-turns, error, cancel).
	// This lets the session save conversation state before TRAJ_IDLE/TRAJ_ERROR
	// is emitted and the SDK kills the process.
	defer func() {
		if e.preCompletionHook != nil {
			e.preCompletionHook()
		}
	}()

	// Reset planning guard state for this turn
	e.researchToolCount = 0

	// Notify trajectory running
	e.emitTrajectoryState(pb.TrajectoryState_TRAJ_RUNNING)

	// Drain pending system notifications (timer fires, task completions)
	var pendingMsgs []string
	if e.notifyCh != nil {
		for {
			select {
			case msg := <-e.notifyCh:
				pendingMsgs = append(pendingMsgs, msg.FormatForPrompt())
			default:
				goto drained
			}
		}
	}
drained:

	// Enrich the user message with dynamic context for the LLM
	msgCtx := e.msgCtx // Copy base config (includes any ADK-injected ephemeral messages)
	msgCtx.HostContext = hostCtx
	msgCtx.PendingMessages = pendingMsgs
	// Merge Global and Workspace Knowledge Items (Workspace overrides Global if name matches)
	mergedKIs := make(map[string]KnowledgeItem)

	if e.globalKnowledgeStore != nil {
		for _, ki := range e.globalKnowledgeStore.List() {
			mergedKIs[ki.Name] = ki
		}
	}

	for _, store := range e.workspaceKnowledgeStores {
		for _, ki := range store.List() {
			mergedKIs[ki.Name] = ki
		}
	}

	if len(mergedKIs) > 0 {
		var kiList []KnowledgeItem
		for _, ki := range mergedKIs {
			kiList = append(kiList, ki)
		}
		// Sort to ensure deterministic output
		sort.Slice(kiList, func(i, j int) bool {
			return kiList[i].Name < kiList[j].Name
		})
		msgCtx.KnowledgeItems = kiList
	}
	enrichedParts := EnrichUserMessage(userMessage, msgCtx)

	// Clear ephemeral messages and settings changes after consumption — they are single-use per turn.
	e.msgCtx.EphemeralMessages = nil
	e.msgCtx.SettingsChanges = nil

	// Add enriched message to history (LLM sees the full context as multi-part)
	e.history = append(e.history, llm.Message{
		Role:  "user",
		Parts: enrichedParts,
	})

	// Emit user step with the original (non-enriched) message for display
	e.emitStep(&pb.StepUpdate{
		ConversationId: e.convID,
		TrajectoryId:   e.trajectoryID,
		StepIndex:      e.nextStepIndex(),
		Text:           userMessage,
		Source:         pb.StepUpdate_SOURCE_USER,
		State:          pb.StepUpdate_STATE_DONE,
		Target:         pb.StepUpdate_TARGET_INTERNAL,
	})

	// Build tool declarations
	toolDecls := e.buildToolDeclarations()

	// Agentic loop
	emptyRetries := 0
	for turn := 0; turn < e.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			e.emitTrajectoryState(pb.TrajectoryState_TRAJ_ERROR)
			return ctx.Err()
		default:
		}

		// Check for pause request
		select {
		case <-e.pauseCh:
			e.logger.Info("engine paused, waiting for resume")
			e.emitTrajectoryState(pb.TrajectoryState_TRAJ_PAUSED)
			
			select {
			case <-ctx.Done():
				e.emitTrajectoryState(pb.TrajectoryState_TRAJ_ERROR)
				return ctx.Err()
			case injectedMsg := <-e.resumeCh:
				e.logger.Info("engine resumed", "injectedMsg", injectedMsg)
				e.emitTrajectoryState(pb.TrajectoryState_TRAJ_RUNNING)
				if injectedMsg != "" {
					// Inject the human command into the history
					e.history = append(e.history, llm.Message{
						Role:    "user",
						Content: injectedMsg,
					})
					
					// Emit a step for the UI so the user sees their injected command
					e.emitStep(&pb.StepUpdate{
						ConversationId: e.convID,
						TrajectoryId:   e.trajectoryID,
						StepIndex:      e.nextStepIndex(),
						Text:           injectedMsg,
						Source:         pb.StepUpdate_SOURCE_USER,
						State:          pb.StepUpdate_STATE_DONE,
						Target:         pb.StepUpdate_TARGET_USER,
					})
				}
			}
		default:
			// Not paused
		}

		e.logger.Info("agentic turn", "turn", turn, "history_len", len(e.history))

		// Pre-compaction: reduce redundant tool results (zero-cost, no LLM call)
		// Deduplicates re-reads, collapses command reruns, trims large stale outputs.
		var reduction ReductionResult
		e.history, reduction = ReduceHistory(e.history, 8)
		if reduction.TokensSaved > 0 {
			e.logger.Debug("context reduced",
				"dedup_files", reduction.DeduplicatedFiles,
				"collapsed_cmds", reduction.CollapsedCommands,
				"trimmed", reduction.TrimmedResults,
				"tokens_saved", reduction.TokensSaved,
			)
		}

		// Context compaction — summarize old messages if over threshold
		if e.compactionThreshold > 0 {
			compacted, result, compErr := CompactIfNeeded(
				ctx, e.provider, e.history, CompactionConfig{
					Threshold:          e.compactionThreshold,
					KeepRecentMessages: e.keepRecentMessages,
					LastRealTokenCount: e.lastRealTokenCount,
					SystemPromptTokens: estimateStringTokens(e.sysPrompt),
				}, e.logger,
			)
			if compErr != nil {
				e.logger.Warn("compaction failed, continuing with full history", "error", compErr)
			} else if result != nil {
				e.history = compacted
				// Re-inject per-message metadata that was lost during compaction.
				// The original first user message had <user_information>, <user_rules>,
				// <artifacts> etc. prepended by EnrichUserMessage, but compaction
				// summarizes that away. Re-prepend essential context so the agent
				// retains brain dir, workspace info, and rules.
				e.reinjectMetadataAfterCompaction()
				// Emit compaction step to client
				e.emitStep(&pb.StepUpdate{
					ConversationId: e.convID,
					TrajectoryId:   e.trajectoryID,
					StepIndex:      e.nextStepIndex(),
					Source:         pb.StepUpdate_SOURCE_SYSTEM,
					State:          pb.StepUpdate_STATE_DONE,
					Target:         pb.StepUpdate_TARGET_INTERNAL,
					Action: &pb.StepUpdate_Compaction{
						Compaction: &pb.ActionCompaction{
							OriginalTokens:  int32(result.OriginalTokens),
							CompactedTokens: int32(result.CompactedTokens),
							MessagesRemoved: int32(result.MessagesRemoved),
							Summary:         result.Summary,
						},
					},
				})
				// Reset real token count since history changed
				e.lastRealTokenCount = 0
			}
		}

		// Call LLM (streaming if supported, otherwise blocking)
		req := &llm.GenerateRequest{
			Messages:     e.history,
			Tools:        toolDecls,
			SystemPrompt: e.sysPrompt,
		}

		// Trace the request
		stepForTrace := int(e.stepIndex.Load())
		e.tracer.TraceRequest(stepForTrace, e.provider.ModelName(), req)

		var resp *llm.GenerateResponse
		var err error
		start := time.Now()

		// Per-call timeout prevents hanging when the LLM API stalls
		// (e.g., a proxy dies mid-stream without closing the connection).
		callCtx, callCancel := context.WithTimeout(ctx, llmCallTimeout)

		if sp, ok := e.provider.(llm.StreamingProvider); ok {
			resp, err = e.streamGenerate(callCtx, sp, req)
		} else {
			resp, err = e.provider.Generate(callCtx, req)
		}
		callCancel()
		latency := time.Since(start)

		// Trace the response
		e.tracer.TraceResponse(stepForTrace, resp, latency, err)

		if err != nil {
			e.emitErrorStep(fmt.Sprintf("LLM error: %v", err))
			e.emitTrajectoryState(pb.TrajectoryState_TRAJ_ERROR)
			return errors.Wrap(err, errors.ErrCodeLLMProvider,
				"LLM call failed").
				WithContext("model", e.provider.ModelName()).
				WithContext("trajectory_id", e.trajectoryID).
				WithContext("conversation_id", e.convID).
				WithComponent("engine")
		}

		// Emit usage
		e.logger.Debug("LLM response",
			"finish_reason", resp.FinishReason,
			"tool_calls", len(resp.ToolCalls),
			"content_len", len(resp.Content),
			"tokens", resp.Usage.TotalTokens,
		)

		// Track real token count for compaction decisions
		if resp.Usage.TotalTokens > 0 {
			e.lastRealTokenCount = resp.Usage.TotalTokens
		}

		// Handle max_tokens with empty content — the model exhausted its
		// output budget (usually on thinking) without producing any content
		// or tool calls. Instead of returning nothing, inject a recovery
		// message and retry so the model can try with a shorter response.
		if resp.FinishReason == "max_tokens" && resp.Content == "" && len(resp.ToolCalls) == 0 {
			e.logger.Warn("model hit max_tokens with no content (thinking starvation)",
				"thinking_tokens", resp.Usage.ThinkingTokens,
				"prompt_tokens", resp.Usage.PromptTokens,
				"turn", turn,
			)
			// Add the empty model response + a synthetic user nudge
			e.history = append(e.history, llm.Message{
				Role: "model",
				Content: "[Response truncated — output token limit reached during reasoning]",
			})
			e.history = append(e.history, llm.Message{
				Role:    "user",
				Content: "Your previous response was truncated because the output token limit was reached during reasoning. Please provide a shorter, more concise response. Focus on the most important finding and action.",
			})
			continue // Retry the turn
		}

		// ── Text-to-tool-call recovery ──
		// Some models (e.g., Llama 3.3 via Workers AI) output tool calls as text
		// instead of structured tool_calls. Detect and recover so the agentic
		// loop continues instead of stalling.
		if len(resp.ToolCalls) == 0 && resp.Content != "" {
			recovered, remaining := tryExtractToolCallsFromText(resp.Content, e.knownToolNames(), e.logger)
			if len(recovered) > 0 {
				resp.ToolCalls = recovered
				resp.Content = remaining
				resp.FinishReason = "tool_calls"
			}
		}

		// Empty model response recovery — model returned no text AND no tool calls
		// with a non-max_tokens finish reason. This happens with smaller models
		// (Workers AI, Ollama) that occasionally produce empty completions.
		if resp.Content == "" && len(resp.ToolCalls) == 0 && resp.FinishReason != "max_tokens" {
			emptyRetries++
			if emptyRetries <= 2 {
				e.logger.Warn("model returned empty response, retrying with nudge",
					"finish_reason", resp.FinishReason,
					"retry", emptyRetries,
					"turn", turn,
				)
				e.history = append(e.history, llm.Message{
					Role:    "model",
					Content: "[Empty response]",
				})
				e.history = append(e.history, llm.Message{
					Role:    "user",
					Content: "Your previous response was empty. Please continue with the task. If you need to use a tool, call it. If you're done, provide your final answer.",
				})
				continue
			}
			// After 2 retries, fall through to handleFinalResponse with empty text
		}

		// If model returned text with no tool calls → final response
		if resp.FinishReason == "stop" || len(resp.ToolCalls) == 0 {
			return e.handleFinalResponse(resp)
		}

		// Model wants to call tools
		// First, add the model's response to history
		modelMsg := llm.Message{
			Role:      "model",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		e.history = append(e.history, modelMsg)

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			if err := e.executeTool(ctx, tc, resp); err != nil {
				e.logger.Error("tool execution failed", "tool", tc.Name, "error", err)
				// Add error result to history so LLM can recover
				e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf("Error: %v", err), true))
			}
		}

		// Check if finish was called
		for _, tc := range resp.ToolCalls {
			if tc.Name == "finish" {
				e.emitTrajectoryState(pb.TrajectoryState_TRAJ_COMPLETED)
				return nil
			}
		}
	}

	// Exceeded max turns
	e.emitErrorStep("exceeded maximum agentic loop iterations")
	e.emitTrajectoryState(pb.TrajectoryState_TRAJ_ERROR)
	return errors.New(errors.ErrCodeMaxTurnsExceeded,
		"exceeded maximum agentic loop iterations").
		WithContext("max_turns", e.maxTurns).
		WithContext("trajectory_id", e.trajectoryID).
		WithContext("conversation_id", e.convID).
		WithComponent("engine")
}

// llmCallTimeout is the maximum time allowed for a single LLM API call
// (including streaming). 300s accommodates slower providers like Cloudflare Workers AI.
const llmCallTimeout = 300 * time.Second

// maxToolResultSize is the maximum size (in bytes) of a single tool result
// added to the conversation history. Results exceeding this are truncated to
// prevent overwhelming smaller LLMs (e.g., Workers AI free-tier models).
// 32KB ≈ 8K tokens — enough for meaningful content while staying within
// context budget limits for most models.
const maxToolResultSize = 32_000

// toolResultMsg builds a tool result message, propagating the ThoughtSignature
// from the ToolCall. Gemini 3.5+ requires thought_signature on functionResponse
// parts to maintain chain integrity.
func toolResultMsg(tc llm.ToolCall, content string, isError bool) llm.Message {
	// Truncate oversized tool results to prevent context blowup
	if len(content) > maxToolResultSize {
		content = content[:maxToolResultSize] + fmt.Sprintf(
			"\n\n... [output truncated, showing %d/%d bytes. Use more specific queries or line ranges to get targeted results.]",
			maxToolResultSize, len(content),
		)
	}
	return llm.Message{
		Role: "tool",
		ToolResult: &llm.ToolCallResult{
			CallID:           tc.ID,
			Name:             tc.Name,
			Content:          content,
			IsError:          isError,
			ThoughtSignature: tc.ThoughtSignature,
		},
	}
}

// reinjectMetadataAfterCompaction prepends essential per-message metadata
// to the first user message in the compacted history. After compaction,
// the original enriched user message (with <user_information>, <user_rules>,
// <artifacts>, etc.) is summarized away. This re-injects a minimal metadata
// block so the agent retains workspace paths, brain dir, conversation ID,
// user rules, and artifact listings.
func (e *Engine) reinjectMetadataAfterCompaction() {
	if len(e.history) == 0 {
		return
	}

	// Find the first user message (should be the compaction summary)
	for i := range e.history {
		if e.history[i].Role != "user" {
			continue
		}

		// Build a minimal re-injection using EnrichUserMessage on an empty prompt.
		// This generates all the metadata parts without a USER_REQUEST block.
		metaParts := EnrichUserMessage("", e.msgCtx)

		// EnrichUserMessage always adds <USER_REQUEST>\n\n</USER_REQUEST> as the
		// last part. We want everything EXCEPT that trailing empty request tag.
		if len(metaParts) > 1 {
			metaParts = metaParts[:len(metaParts)-1] // Drop the empty <USER_REQUEST> part
		}

		// Prepend the metadata parts to the existing message content.
		// The compacted summary is in Content (single-part), so convert
		// it to multi-part with metadata prepended.
		existing := e.history[i].TextContent()
		allParts := make([]string, 0, len(metaParts)+1)
		allParts = append(allParts, metaParts...)
		allParts = append(allParts, existing)

		e.history[i].Parts = allParts
		e.history[i].Content = "" // Clear single-part since we're using Parts now

		e.logger.Info("re-injected metadata after compaction",
			"metadata_parts", len(metaParts),
			"target_message_index", i,
		)
		return
	}
}

// handleFinalResponse processes the LLM's final text response.
func (e *Engine) handleFinalResponse(resp *llm.GenerateResponse) error {
	stepIdx := e.nextStepIndex()

	// Emit thinking if present
	step := &pb.StepUpdate{
		ConversationId: e.convID,
		TrajectoryId:   e.trajectoryID,
		StepIndex:      stepIdx,
		Text:           resp.Content,
		Thinking:       resp.Thinking,
		Source:         pb.StepUpdate_SOURCE_MODEL,
		State:          pb.StepUpdate_STATE_DONE,
		Target:         pb.StepUpdate_TARGET_USER,
		Usage: &pb.UsageMetadata{
			PromptTokens:     int32(resp.Usage.PromptTokens),
			CompletionTokens: int32(resp.Usage.CompletionTokens),
			ThinkingTokens:   int32(resp.Usage.ThinkingTokens),
			TotalTokens:      int32(resp.Usage.TotalTokens),
			CachedTokens:     int32(resp.Usage.CachedTokens),
		},
	}

	if e.conv != nil {
		e.conv.AddUsage(step.Usage)
	}

	e.emitStep(step)

	// Add to history
	e.history = append(e.history, llm.Message{
		Role:    "model",
		Content: resp.Content,
	})

	// Save state BEFORE emitting TRAJ_IDLE — the SDK kills the process
	// immediately on receiving TRAJ_IDLE, so this is our last chance.
	// (The defer in RunWithContext covers error/max-turns exits.)
	if e.preCompletionHook != nil {
		e.preCompletionHook()
	}

	e.emitTrajectoryState(pb.TrajectoryState_TRAJ_IDLE)
	return nil
}

// executeTool dispatches a single tool call and streams step updates.
func (e *Engine) executeTool(ctx context.Context, tc llm.ToolCall, resp *llm.GenerateResponse) error {
	stepIdx := e.nextStepIndex()

	// Create step with tool action (STATE_ACTIVE)
	step := e.buildToolStep(tc, stepIdx)
	step.State = pb.StepUpdate_STATE_ACTIVE
	step.Source = pb.StepUpdate_SOURCE_MODEL
	step.Target = pb.StepUpdate_TARGET_INTERNAL
	// Usage is deferred to STATE_DONE to avoid the SDK counting it multiple
	// times across sub-step updates (Active → Permission → Done).
	toolUsage := &pb.UsageMetadata{
		PromptTokens:     int32(resp.Usage.PromptTokens),
		CompletionTokens: int32(resp.Usage.CompletionTokens),
		ThinkingTokens:   int32(resp.Usage.ThinkingTokens),
		TotalTokens:      int32(resp.Usage.TotalTokens),
		CachedTokens:     int32(resp.Usage.CachedTokens),
	}

	if e.conv != nil {
		e.conv.AddUsage(toolUsage)
	}

	e.emitStep(step)

	// ── Permission check (if handler registered) ──
	// Policy:
	// 1. YOLO mode: bypass all prompts.
	// 2. Non-file / interactive / web tools: always allowed without prompts.
	// 3. Agent internal AppData paths (brain/knowledge artifacts): always allowed without prompts.
	// 4. Read-only file tools: auto-approved ONLY if target is inside workspace or AppDataDir (~/.divmora).
	//    If viewing/reading outside workspaces & outside ~/.divmora, prompt user for permission!
	// 5. Mutating tools (write, edit, command): always prompt unless YOLO mode.
	requiresPermission := true
	if e.yoloMode {
		requiresPermission = false
	} else if isAlwaysAllowedTool(tc.Name) {
		requiresPermission = false
	} else if e.isAppDataDirPath(tc) {
		requiresPermission = false
	} else if isReadOnlyFileTool(tc.Name) {
		targetPath := extractToolPath(tc)
		if e.isPathInsideWorkspaceOrAppData(targetPath) {
			requiresPermission = false
		} else {
			requiresPermission = true
		}
	}

	if e.permissionHandler != nil && requiresPermission {
		approved, reason, err := e.requestPermission(ctx, tc, step)


		if err != nil {
			step.State = pb.StepUpdate_STATE_ERROR
			step.ErrorInfo = &pb.ErrorInfo{
				Message: fmt.Sprintf("permission check error: %v", err),
				Code:    "PERMISSION_ERROR",
			}
			e.emitStep(step)
			return err
		}
		if !approved {
			// Feed denial back to LLM as a tool error so it can adapt
			e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf("Permission denied: %s", reason), true))
			step.State = pb.StepUpdate_STATE_ERROR
			step.ErrorInfo = &pb.ErrorInfo{
				Message: fmt.Sprintf("Permission denied for tool '%s': %s", tc.Name, reason),
				Code:    "PERMISSION_DENIED",
			}
			e.emitStep(step)
			return nil // Not fatal — LLM can adapt
		}
	}

	// ── Planning guard ──
	// When planning mode is enabled, block workspace write tools until
	// implementation_plan.md exists in the brain directory.
	if denied, reason := e.checkPlanningGuard(tc); denied {
		e.history = append(e.history, toolResultMsg(tc, reason, true))
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: reason,
			Code:    "PLANNING_REQUIRED",
		}
		e.emitStep(step)
		return nil // Not fatal — LLM should create the plan first
	}

	// Check if this is a subagent-related tool (handled by engine directly)
	switch tc.Name {
	case "invoke_subagent":
		return e.executeSubagent(ctx, tc, step)
	case "define_subagent":
		return e.executeDefineSubagent(ctx, tc, step)
	case "manage_subagents":
		return e.executeManageSubagents(ctx, tc, step)
	case "send_message":
		return e.executeSendMessage(ctx, tc, step)
	case "browser_subagent":
		return e.executeBrowserSubagent(ctx, tc, step)
	}

	// Check if this is a knowledge tool (handled by engine directly)
	switch tc.Name {
	case "knowledge_write":
		return e.executeKnowledgeWrite(ctx, tc, step)
	case "knowledge_replace":
		return e.executeKnowledgeReplace(ctx, tc, step)
	case "knowledge_delete":
		return e.executeKnowledgeDelete(ctx, tc, step)
	}

	// Check if this is the publish tool (agent bus coordination)
	if tc.Name == "publish" {
		return e.executePublish(ctx, tc, step)
	}

	// Check if this is an MCP tool
	if e.mcpMgr != nil && e.mcpMgr.IsMCPTool(tc.Name) {
		return e.executeMCPTool(ctx, tc, step)
	}

	// Check if this is a host-side tool
	if e.hostToolNames[tc.Name] {
		return e.executeHostTool(ctx, tc, step)
	}

	// Check if this is an ask_question call (handled like permission requests)
	if tc.Name == "ask_question" {
		return e.executeAskQuestion(ctx, tc, step)
	}

	// Check if this is a permission management tool (engine-intercepted)
	switch tc.Name {
	case "ask_permission":
		return e.executeAskPermission(ctx, tc, step)
	case "list_permissions":
		return e.executeListPermissions(ctx, tc, step)
	}

	// Execute built-in tool — dispatch by the original tool name from the LLM,
	// not the proto action type, since multiple tools can share the same proto
	// (e.g., replace_file_content and multi_replace_file_content both use ActionReplaceFileContent).
	//
	// Panic recovery: catch runtime panics (e.g. invalid slice bounds) so a
	// single misbehaving tool doesn't crash the entire engine. The panic is
	// logged and fed back to the LLM as a tool error.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New(errors.ErrCodeToolExecution,
					"tool panicked").
					WithContext("tool", tc.Name).
					WithContext("panic", r).
					WithContext("trajectory_id", e.trajectoryID).
					WithContext("conversation_id", e.convID).
					WithComponent("engine")
				e.logger.Error("tool panic recovered", "tool", tc.Name, "panic", r)
			}
		}()
		err = e.toolRegistry.Execute(ctx, tc.Name, step)
	}()

	if err != nil {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: err.Error(),
			Code:    "TOOL_ERROR",
		}
		e.emitStep(step)
		return err
	}

	// Emit completed step (STATE_DONE) with usage attached
	step.State = pb.StepUpdate_STATE_DONE
	step.Usage = toolUsage
	e.emitStep(step)

	// Build tool result for history
	resultJSON := e.extractToolResult(step)
	e.history = append(e.history, toolResultMsg(tc, resultJSON, false))

	return nil
}

// executeHostTool handles a host-side (SDK-registered) tool call.
// It emits STATE_WAITING, blocks until the SDK client responds with a ToolResult,
// then emits STATE_DONE and adds the result to conversation history.
func (e *Engine) executeHostTool(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	if e.hostToolHandler == nil {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: fmt.Sprintf("host tool %q called but no handler registered", tc.Name),
			Code:    "HOST_TOOL_NO_HANDLER",
		}
		e.emitStep(step)
		return errors.New(errors.ErrCodeToolExecution,
			"no handler registered for host tool").
			WithContext("tool", tc.Name).
			WithContext("trajectory_id", e.trajectoryID).
			WithContext("conversation_id", e.convID).
			WithComponent("engine")
	}

	// Transition to WAITING — tells the SDK client to execute the tool
	step.State = pb.StepUpdate_STATE_WAITING
	step.Target = pb.StepUpdate_TARGET_USER
	e.emitStep(step)

	e.logger.Info("waiting for host tool result", "tool", tc.Name, "step", step.StepIndex)

	// Block until the SDK responds (or context is cancelled)
	resultJSON, isError, err := e.hostToolHandler(ctx, tc, step)
	if err != nil {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: fmt.Sprintf("host tool %q: %v", tc.Name, err),
			Code:    "HOST_TOOL_ERROR",
		}
		e.emitStep(step)
		return errors.Wrap(err, errors.ErrCodeToolExecution,
			"host tool execution failed").
			WithContext("tool", tc.Name).
			WithContext("trajectory_id", e.trajectoryID).
			WithContext("conversation_id", e.convID).
			WithComponent("engine")
	}

	e.logger.Info("host tool result received", "tool", tc.Name, "is_error", isError, "result_len", len(resultJSON))

	// Emit completed step
	step.State = pb.StepUpdate_STATE_DONE
	step.Target = pb.StepUpdate_TARGET_INTERNAL
	e.emitStep(step)

	// Add result to conversation history
	e.history = append(e.history, toolResultMsg(tc, resultJSON, isError))

	return nil
}

// executeAskQuestion handles the ask_question tool by presenting questions
// to the user via the SDK and waiting for their response.
// It follows the same STATE_WAITING → block → STATE_DONE pattern as permission requests.
func (e *Engine) executeAskQuestion(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	if e.questionHandler == nil {
		// No handler — fall back to returning a "skipped" result
		e.logger.Warn("ask_question called but no question handler registered")
		e.history = append(e.history, toolResultMsg(tc, `{"skipped": true, "reason": "no question handler registered — user interaction not available"}`, false))
		step.State = pb.StepUpdate_STATE_DONE
		e.emitStep(step)
		return nil
	}

	// Build the ActionUserQuestion from tool call args
	req := &pb.ActionUserQuestion{
		RequestId: fmt.Sprintf("question-%d", step.StepIndex),
	}

	// Parse questions from tool args
	if questionsRaw, ok := tc.Args["questions"]; ok {
		if questionsList, ok := questionsRaw.([]interface{}); ok {
			for _, qRaw := range questionsList {
				if qMap, ok := qRaw.(map[string]interface{}); ok {
					q := &pb.UserQuestion{}
					if text, ok := qMap["question"].(string); ok {
						q.Question = text
					}
					if options, ok := qMap["options"].([]interface{}); ok {
						for _, o := range options {
							if s, ok := o.(string); ok {
								q.Options = append(q.Options, s)
							}
						}
					}
					if multi, ok := qMap["is_multi_select"].(bool); ok {
						q.IsMultiSelect = multi
					}
					req.Questions = append(req.Questions, q)
				}
			}
		}
	}

	if len(req.Questions) == 0 {
		e.history = append(e.history, toolResultMsg(tc, `{"error": "no questions provided"}`, true))
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: "ask_question: no questions provided",
			Code:    "TOOL_ERROR",
		}
		e.emitStep(step)
		return nil
	}

	// Attach the question to the step and emit STATE_WAITING
	step.Action = &pb.StepUpdate_UserQuestion{UserQuestion: req}
	step.State = pb.StepUpdate_STATE_WAITING
	step.Target = pb.StepUpdate_TARGET_USER
	e.emitStep(step)

	e.logger.Info("waiting for user answers", "questions", len(req.Questions), "step", step.StepIndex)

	// Block until the SDK responds (or context is cancelled)
	resp, err := e.questionHandler(ctx, req)
	if err != nil {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: fmt.Sprintf("ask_question: handler error: %v", err),
			Code:    "QUESTION_ERROR",
		}
		e.emitStep(step)
		return err
	}

	// Build result for LLM
	var resultJSON string
	if resp.Skipped {
		resultJSON = `{"skipped": true, "reason": "user skipped the question"}`
	} else {
		// Format answers nicely
		type answerResult struct {
			QuestionIndex   int      `json:"question_index"`
			Question        string   `json:"question"`
			SelectedOptions []string `json:"selected_options,omitempty"`
			Text            string   `json:"text,omitempty"`
		}

		var results []answerResult
		for i, answer := range resp.Answers {
			ar := answerResult{
				QuestionIndex:   i,
				SelectedOptions: answer.SelectedOptions,
				Text:            answer.Text,
			}
			if i < len(req.Questions) {
				ar.Question = req.Questions[i].Question
			}
			results = append(results, ar)
		}
		b, _ := json.Marshal(map[string]interface{}{
			"skipped": false,
			"answers": results,
		})
		resultJSON = string(b)
	}

	// Update step with answers
	uq := step.GetUserQuestion()
	if uq != nil {
		uq.Answers = resp.Answers
		uq.Skipped = resp.Skipped
	}
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	// Add to conversation history
	e.history = append(e.history, toolResultMsg(tc, resultJSON, false))

	return nil
}

// executeAskPermission handles the ask_permission tool — LLM-initiated
// permission requests. Uses the same permissionHandler as auto-permission
// checks but lets the LLM proactively request scoped access (e.g., read
// access to a directory outside the workspace). If approved, the grant
// is stored for the session.
func (e *Engine) executeAskPermission(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	if e.permissionHandler == nil {
		// No permission handler — auto-approve (standalone mode)
		e.logger.Warn("ask_permission called but no permission handler registered, auto-approving")
		grant := PermissionGrant{
			Action: stringArg(tc.Args, "action"),
			Target: stringArg(tc.Args, "target"),
			Reason: stringArg(tc.Args, "reason"),
		}
		e.permissionGrants = append(e.permissionGrants, grant)
		resultJSON := fmt.Sprintf(`{"approved": true, "action": %q, "target": %q}`, grant.Action, grant.Target)
		e.history = append(e.history, toolResultMsg(tc, resultJSON, false))
		step.State = pb.StepUpdate_STATE_DONE
		e.emitStep(step)
		return nil
	}

	action := stringArg(tc.Args, "action")
	target := stringArg(tc.Args, "target")
	reason := stringArg(tc.Args, "reason")

	if action == "" || target == "" {
		e.history = append(e.history, toolResultMsg(tc, `{"error": "action and target are required"}`, true))
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: "ask_permission: action and target are required",
			Code:    "TOOL_ERROR",
		}
		e.emitStep(step)
		return nil
	}

	// Build a permission request using the existing mechanism
	summary := fmt.Sprintf("Permission request: %s access to %s — %s", action, target, reason)
	req := &pb.ActionPermissionRequest{
		RequestId:   fmt.Sprintf("ask-perm-%d", step.StepIndex),
		ToolName:    "ask_permission",
		ArgsJson:    fmt.Sprintf(`{"action": %q, "target": %q, "reason": %q}`, action, target, reason),
		ArgsSummary: summary,
	}

	// Emit STATE_WAITING — the SDK will present this to the user
	permStep := &pb.StepUpdate{
		ConversationId: step.ConversationId,
		TrajectoryId:   step.TrajectoryId,
		StepIndex:      step.StepIndex,
		Source:         step.Source,
		State:          pb.StepUpdate_STATE_WAITING,
		Action:         &pb.StepUpdate_PermissionRequest{PermissionRequest: req},
		Target:         pb.StepUpdate_TARGET_USER,
	}
	e.emitStep(permStep)

	e.logger.Info("waiting for permission grant", "action", action, "target", target, "reason", reason)

	// Block until SDK responds
	approved, denialReason, err := e.permissionHandler(ctx, req)
	if err != nil {
		e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf(`{"error": "permission handler error: %v"}`, err), true))
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: fmt.Sprintf("ask_permission: %v", err),
			Code:    "PERMISSION_ERROR",
		}
		e.emitStep(step)
		return nil
	}

	if approved {
		// Store the grant for future checks
		grant := PermissionGrant{
			Action: action,
			Target: target,
			Reason: reason,
		}
		e.permissionGrants = append(e.permissionGrants, grant)
		e.logger.Info("permission granted", "action", action, "target", target)

		resultJSON := fmt.Sprintf(`{"approved": true, "action": %q, "target": %q}`, action, target)
		e.history = append(e.history, toolResultMsg(tc, resultJSON, false))
	} else {
		e.logger.Info("permission denied", "action", action, "target", target, "reason", denialReason)
		resultJSON := fmt.Sprintf(`{"approved": false, "reason": %q}`, denialReason)
		e.history = append(e.history, toolResultMsg(tc, resultJSON, false))
	}

	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// executeListPermissions handles the list_permissions tool — returns all
// permission grants that have been approved in this session.
func (e *Engine) executeListPermissions(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	type grantInfo struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Reason string `json:"reason"`
	}

	grants := make([]grantInfo, 0, len(e.permissionGrants))
	for _, g := range e.permissionGrants {
		grants = append(grants, grantInfo{
			Action: g.Action,
			Target: g.Target,
			Reason: g.Reason,
		})
	}

	b, _ := json.Marshal(map[string]interface{}{
		"grants": grants,
		"count":  len(grants),
	})
	resultJSON := string(b)

	e.history = append(e.history, toolResultMsg(tc, resultJSON, false))
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	return nil
}

// stringArg extracts a string argument from a tool call's args map.
func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// executeMCPTool dispatches a tool call to the MCP server that owns it.
// Unlike host tools that require SDK round-trip, MCP tools execute inside the binary.
func (e *Engine) executeMCPTool(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	serverName := e.mcpMgr.ServerName(tc.Name)
	argsJSON, _ := json.Marshal(tc.Args)

	// Build MCP-specific step action
	step.Action = &pb.StepUpdate_McpTool{
		McpTool: &pb.ActionMcpTool{
			ServerName: serverName,
			ToolName:   tc.Name,
			ArgsJson:   string(argsJSON),
			CallId:     tc.ID,
		},
	}

	// Emit ACTIVE step
	step.State = pb.StepUpdate_STATE_ACTIVE
	step.Source = pb.StepUpdate_SOURCE_MODEL
	step.Target = pb.StepUpdate_TARGET_INTERNAL
	e.emitStep(step)

	e.logger.Info("executing MCP tool", "server", serverName, "tool", tc.Name)

	// Call the MCP server
	resultJSON, isError, err := e.mcpMgr.CallTool(ctx, tc.Name, tc.Args)
	if err != nil {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: fmt.Sprintf("MCP tool %q (%s): %v", tc.Name, serverName, err),
			Code:    "MCP_TOOL_ERROR",
		}
		e.emitStep(step)

		// Still add error to history so LLM can self-correct
		e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf("Error calling MCP tool: %v", err), true))
		return nil // Don't break the loop; let LLM see the error
	}

	e.logger.Info("MCP tool result", "server", serverName, "tool", tc.Name, "is_error", isError, "result_len", len(resultJSON))

	// Set result on the step action
	if mcpAction, ok := step.Action.(*pb.StepUpdate_McpTool); ok {
		mcpAction.McpTool.ResultJson = resultJSON
		mcpAction.McpTool.IsError = isError
	}

	// Emit completed step
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	// Add result to conversation history
	e.history = append(e.history, toolResultMsg(tc, resultJSON, isError))

	return nil
}

// isReadOnlyFileTool returns true if the tool reads or lists files/directories.
func isReadOnlyFileTool(toolName string) bool {
	switch toolName {
	case "view_file", "list_dir", "grep_search", "find_file", "find_by_name":
		return true
	default:
		return false
	}
}

// isAlwaysAllowedTool returns true for non-file, internal, or web tools that
// do not touch the local workspace filesystem directly.
func isAlwaysAllowedTool(toolName string) bool {
	switch toolName {
	case "read_url_content", "search_web", "finish", "ask_question",
		"ask_permission", "list_permissions", "invoke_subagent", "send_message",
		"schedule", "define_subagent", "manage_subagents":
		return true
	default:
		return false
	}
}

// extractToolPath extracts the target file or directory path from tool call arguments.
func extractToolPath(tc llm.ToolCall) string {
	keys := []string{
		"AbsolutePath", "DirectoryPath", "SearchPath", "SearchDirectory",
		"TargetFile", "path", "file_path", "directory_path", "search_path", "target_file",
	}
	for _, key := range keys {
		if p, ok := tc.Args[key].(string); ok && p != "" {
			return p
		}
	}
	return ""
}

// isPathInsideWorkspaceOrAppData checks if a path is located inside any attached workspace
// or inside the appDataDir (~/.divmora/localharness/ including conversations, brain, knowledge).
func (e *Engine) isPathInsideWorkspaceOrAppData(p string) bool {
	if p == "" {
		return true
	}
	absPath := filepath.Clean(p)
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		realPath = absPath
	}

	// 1. Check AppDataDir (~/.divmora/localharness/ or ~/.divmora)
	if e.appDataDir != "" {
		appDataClean := filepath.Clean(e.appDataDir)
		realAppData, _ := filepath.EvalSymlinks(appDataClean)
		if strings.HasPrefix(absPath, appDataClean) || (realAppData != "" && strings.HasPrefix(realPath, realAppData)) {
			return true
		}
	}

	// Also check ~/.divmora generally (e.g. conversations, brain, knowledge, settings)
	home, err := os.UserHomeDir()
	if err == nil {
		divmoraDir := filepath.Join(home, ".divmora")
		realDivmora, _ := filepath.EvalSymlinks(divmoraDir)
		if strings.HasPrefix(absPath, divmoraDir) || (realDivmora != "" && strings.HasPrefix(realPath, realDivmora)) {
			return true
		}
	}

	// 2. Check all attached workspaces
	e.mu.RLock()
	workspaces := append([]string{}, e.workspaces...)
	e.mu.RUnlock()

	for _, ws := range workspaces {
		wsClean := filepath.Clean(ws)
		realWS, _ := filepath.EvalSymlinks(wsClean)
		if absPath == wsClean || strings.HasPrefix(absPath, wsClean+string(filepath.Separator)) {
			return true
		}
		if realWS != "" && (realPath == realWS || strings.HasPrefix(realPath, realWS+string(filepath.Separator))) {
			return true
		}
	}

	return false
}

// isAppDataDirPath returns true if the tool call targets a path inside the
// agent-writable subdirectories of appDataDir (brain/ and knowledge/).
// Only these directories should bypass SDK policy checks — the agent must not
// be able to write to conversations/, projects.json, plugins/, skills/, etc.
func (e *Engine) isAppDataDirPath(tc llm.ToolCall) bool {
	if e.appDataDir == "" {
		return false
	}
	// Only brain/ and knowledge/ are agent-writable
	allowedPrefixes := []string{
		filepath.Join(e.appDataDir, "brain") + string(filepath.Separator),
		filepath.Join(e.appDataDir, "knowledge") + string(filepath.Separator),
	}
	// Check common path arguments used by file tools
	for _, key := range []string{"path", "file_path", "directory_path", "search_path", "AbsolutePath", "DirectoryPath", "TargetFile"} {
		if p, ok := tc.Args[key].(string); ok && p != "" {
			absPath := filepath.Clean(p)
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(absPath, prefix) {
					return true
				}
			}
		}
	}
	return false
}


// requestPermission emits a STATE_WAITING step with an ActionPermissionRequest
// and blocks until the SDK responds with a PermissionResponse.
func (e *Engine) requestPermission(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (bool, string, error) {
	argsJSON, _ := json.Marshal(tc.Args)
	diffPreview := generateDiffPreview(tc)
	req := &pb.ActionPermissionRequest{
		RequestId:   fmt.Sprintf("perm-%d", step.StepIndex),
		ToolName:    tc.Name,
		ArgsJson:    string(argsJSON),
		ArgsSummary: summarizeToolCall(tc),
		CallId:      tc.ID,
		DiffPreview: diffPreview,
	}

	// Create a separate StepUpdate to emit the permission request
	permStep := &pb.StepUpdate{
		ConversationId: step.ConversationId,
		TrajectoryId:   step.TrajectoryId,
		StepIndex:      step.StepIndex,
		Source:         step.Source,
		State:          pb.StepUpdate_STATE_WAITING,
		Action:         &pb.StepUpdate_PermissionRequest{PermissionRequest: req},
		Target:         pb.StepUpdate_TARGET_USER,
	}
	e.emitStep(permStep)

	e.logger.Info("waiting for permission", "tool", tc.Name, "step", step.StepIndex)

	// Block until SDK responds (or context is cancelled)
	return e.permissionHandler(ctx, req)
}

// summarizeToolCall creates a human-readable description of a tool call
// for permission request UIs.
func summarizeToolCall(tc llm.ToolCall) string {
	switch tc.Name {
	case "run_command":
		if cmd, ok := tc.Args["command"].(string); ok {
			cwd, _ := tc.Args["cwd"].(string)
			if cwd != "" {
				return fmt.Sprintf("Run command: %s (in %s)", cmd, cwd)
			}
			return fmt.Sprintf("Run command: %s", cmd)
		}
	case "write_to_file":
		if path, ok := tc.Args["path"].(string); ok {
			return fmt.Sprintf("Create file: %s", path)
		}
	case "replace_file_content":
		if path, ok := tc.Args["path"].(string); ok {
			return fmt.Sprintf("Edit file: %s", path)
		}
	case "view_file":
		if path, ok := tc.Args["path"].(string); ok {
			return fmt.Sprintf("View file: %s", path)
		}
	case "list_dir":
		if path, ok := tc.Args["path"].(string); ok {
			return fmt.Sprintf("List directory: %s", path)
		}
	case "grep_search":
		if query, ok := tc.Args["query"].(string); ok {
			return fmt.Sprintf("Search: %s", query)
		}
	case "search_web":
		if q, ok := tc.Args["query"].(string); ok {
			return fmt.Sprintf("Web search: %s", q)
		}
	case "read_url_content":
		if u, ok := tc.Args["url"].(string); ok {
			return fmt.Sprintf("Fetch URL: %s", u)
		}
	}
	argsJSON, _ := json.Marshal(tc.Args)
	return fmt.Sprintf("Tool: %s, Args: %s", tc.Name, string(argsJSON))
}

func generateDiffPreview(tc llm.ToolCall) string {
	switch tc.Name {
	case "write_to_file":
		path, _ := tc.Args["path"].(string)
		content, _ := tc.Args["content"].(string)
		if path == "" {
			return ""
		}
		var oldContent string
		if data, err := os.ReadFile(path); err == nil {
			oldContent = string(data)
		}
		filename := filepath.Base(path)
		diff := util.UnifiedDiff("a/"+filename, "b/"+filename, oldContent, content)
		if diff == "" && oldContent == "" && content != "" {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("--- /dev/null\n+++ b/%s\n", filename))
			lines := strings.Split(content, "\n")
			sb.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
			for _, l := range lines {
				sb.WriteString("+" + l + "\n")
			}
			return sb.String()
		}
		return diff

	case "replace_file_content":
		path, _ := tc.Args["path"].(string)
		if path == "" {
			return ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		oldContent := string(data)
		lines := strings.Split(strings.ReplaceAll(oldContent, "\r\n", "\n"), "\n")
		chunksRaw, ok := tc.Args["chunks"].([]interface{})
		if !ok || len(chunksRaw) == 0 {
			return ""
		}
		for _, c := range chunksRaw {
			chunkMap, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			target, _ := chunkMap["target_content"].(string)
			replacement, _ := chunkMap["replacement"].(string)
			startLine := 1
			if sl, ok := chunkMap["start_line"].(float64); ok && int(sl) > 0 {
				startLine = int(sl)
			}
			endLine := len(lines)
			if el, ok := chunkMap["end_line"].(float64); ok && int(el) > 0 && int(el) <= len(lines) {
				endLine = int(el)
			}
			if startLine <= len(lines) && startLine <= endLine {
				scopeStart := startLine - 1
				scopeEnd := endLine
				scopedText := strings.Join(lines[scopeStart:scopeEnd], "\n")
				newScopedText := strings.Replace(scopedText, target, replacement, 1)
				newLines := strings.Split(newScopedText, "\n")
				result := make([]string, 0, scopeStart+len(newLines)+(len(lines)-scopeEnd))
				result = append(result, lines[:scopeStart]...)
				result = append(result, newLines...)
				result = append(result, lines[scopeEnd:]...)
				lines = result
			}
		}
		newContent := strings.Join(lines, "\n")
		filename := filepath.Base(path)
		return util.UnifiedDiff("a/"+filename, "b/"+filename, oldContent, newContent)
	}
	return ""
}

// checkPlanningGuard enforces the planning workflow when EnablePlanningMode is set.
// Returns (true, reason) if the tool call should be blocked.
//
// Uses a research heuristic to avoid blocking simple fixes:
//   - Tracks research tool calls (view_file, list_dir, search_dir)
//   - Only blocks workspace writes after 2+ research calls without a plan
//   - If the agent goes straight to replace_file_content without researching, it's a
//     simple fix and the guard stays out of the way
//   - Always allows writes to the brain directory (artifact files)
//   - Once implementation_plan.md exists, all writes are allowed
func (e *Engine) checkPlanningGuard(tc llm.ToolCall) (bool, string) {
	if !e.enablePlanningMode {
		return false, ""
	}

	// Track research tool calls
	switch tc.Name {
	case "view_file", "list_dir", "grep_search", "find_file":
		e.researchToolCount++
		return false, ""
	}

	// Only guard write tools
	switch tc.Name {
	case "write_to_file", "replace_file_content":
		// continue to check
	default:
		return false, ""
	}

	// Extract target path from tool args
	targetPath, _ := tc.Args["path"].(string)
	if targetPath == "" {
		return false, ""
	}

	// Allow writes to brain directory (artifacts, plans, tasks, walkthroughs)
	if e.brainDir != "" && strings.HasPrefix(targetPath, e.brainDir) {
		return false, ""
	}

	// Check if implementation_plan.md exists — if so, allow all writes
	if e.brainDir != "" {
		planPath := filepath.Join(e.brainDir, "implementation_plan.md")
		if _, err := os.Stat(planPath); err == nil {
			return false, ""
		}
	}

	// Research heuristic: only block if the agent has done 2+ research calls.
	// If the agent goes straight to writing without research, it's a simple
	// fix that doesn't need a plan.
	if e.researchToolCount < 2 {
		return false, ""
	}

	return true, "Planning mode is active. You have done research but have not created a plan. " +
		"You must create an implementation_plan.md artifact in the brain directory before " +
		"making workspace code changes. Use write_to_file to write your plan first, then " +
		"wait for user approval."
}

func (e *Engine) buildToolStep(tc llm.ToolCall, stepIdx int32) *pb.StepUpdate {
	step := &pb.StepUpdate{
		ConversationId: e.convID,
		TrajectoryId:   e.trajectoryID,
		StepIndex:      stepIdx,
	}

	argsJSON, _ := json.Marshal(tc.Args)

	switch tc.Name {
	case "view_file":
		action := &pb.ActionViewFile{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_ViewFile{ViewFile: action}

	case "write_to_file":
		action := &pb.ActionWriteToFile{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_WriteToFile{WriteToFile: action}

	case "replace_file_content", "multi_replace_file_content":
		action := &pb.ActionReplaceFileContent{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_ReplaceFileContent{ReplaceFileContent: action}

	case "list_dir":
		action := &pb.ActionListDir{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_ListDir{ListDir: action}

	case "grep_search":
		action := &pb.ActionGrepSearch{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_GrepSearch{GrepSearch: action}

	case "find_file":
		action := &pb.ActionFindFile{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_FindFile{FindFile: action}

	case "run_command":
		action := &pb.ActionRunCommand{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_RunCommand{RunCommand: action}

	case "manage_task":
		action := &pb.ActionManageTask{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_ManageTask{ManageTask: action}

	case "finish":
		action := &pb.ActionFinish{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_Finish{Finish: action}

	case "invoke_subagent":
		action := &pb.ActionInvokeSubagent{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_InvokeSubagent{InvokeSubagent: action}

	case "define_subagent":
		action := &pb.ActionDefineSubagent{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_DefineSubagent{DefineSubagent: action}

	case "manage_subagents":
		action := &pb.ActionManageSubagents{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_ManageSubagents{ManageSubagents: action}

	case "send_message":
		action := &pb.ActionSendMessage{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_SendMessageAction{SendMessageAction: action}

	case "browser_subagent":
		action := &pb.ActionBrowserSubagent{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_BrowserSubagent{BrowserSubagent: action}

	case "search_web":
		action := &pb.ActionSearchWeb{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_SearchWeb{SearchWeb: action}

	case "read_url_content":
		action := &pb.ActionReadUrlContent{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_ReadUrlContent{ReadUrlContent: action}

	case "schedule":
		action := &pb.ActionSchedule{}
		json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_Schedule{Schedule: action}

	default:
		// Check if this is an MCP tool
		if e.mcpMgr != nil && e.mcpMgr.IsMCPTool(tc.Name) {
			serverName := e.mcpMgr.ServerName(tc.Name)
			step.Action = &pb.StepUpdate_McpTool{
				McpTool: &pb.ActionMcpTool{
					ServerName: serverName,
					ToolName:   tc.Name,
					ArgsJson:   string(argsJSON),
					CallId:     tc.ID,
				},
			}
		} else {
			// Unknown tool — treat as host tool call
			step.Action = &pb.StepUpdate_HostToolCall{
				HostToolCall: &pb.ActionHostToolCall{
					ToolName:  tc.Name,
					ArgsJson:  string(argsJSON),
					CallId:    tc.ID,
					StepIndex: stepIdx,
				},
			}
		}
	}

	return step
}

// extractToolResult serializes the tool result from the step for the conversation history.
func (e *Engine) extractToolResult(step *pb.StepUpdate) string {
	var result interface{}

	switch a := step.Action.(type) {
	case *pb.StepUpdate_ViewFile:
		vf := a.ViewFile
		result = map[string]interface{}{
			"content": vf.Content, "total_lines": vf.TotalLines,
			"total_bytes": vf.TotalBytes, "is_binary": vf.IsBinary,
		}
	case *pb.StepUpdate_WriteToFile:
		result = map[string]interface{}{"created": a.WriteToFile.Created}
	case *pb.StepUpdate_ReplaceFileContent:
		result = map[string]interface{}{
			"success": a.ReplaceFileContent.Success, "diff": a.ReplaceFileContent.DiffBlock,
		}
	case *pb.StepUpdate_ListDir:
		var entries []map[string]interface{}
		for _, e := range a.ListDir.Entries {
			entries = append(entries, map[string]interface{}{
				"name": e.Name, "is_dir": e.IsDir,
				"size_bytes": e.SizeBytes, "child_count": e.ChildCount,
			})
		}
		result = map[string]interface{}{
			"entries":       entries,
			"total_entries": len(a.ListDir.Entries),
		}
	case *pb.StepUpdate_GrepSearch:
		var matches []map[string]interface{}
		for _, m := range a.GrepSearch.Matches {
			matches = append(matches, map[string]interface{}{
				"filename": m.Filename, "line_number": m.LineNumber,
				"line_content": m.LineContent,
			})
		}
		result = map[string]interface{}{
			"matches": matches, "total": a.GrepSearch.TotalMatches,
			"truncated": a.GrepSearch.Truncated,
		}
	case *pb.StepUpdate_FindFile:
		result = map[string]interface{}{"matches": a.FindFile.Matches}
	case *pb.StepUpdate_RunCommand:
		rc := a.RunCommand
		result = map[string]interface{}{
			"stdout": rc.Stdout, "stderr": rc.Stderr,
			"exit_code": rc.ExitCode, "timed_out": rc.TimedOut,
			"task_id": rc.TaskId, "assigned_terminal_id": rc.AssignedTerminalId,
		}
	case *pb.StepUpdate_ManageTask:
		mt := a.ManageTask
		var tasks []map[string]interface{}
		for _, t := range mt.Tasks {
			tasks = append(tasks, map[string]interface{}{
				"task_id": t.TaskId, "command": t.Command,
				"cwd": t.Cwd, "status": t.Status,
				"exit_code": t.ExitCode, "started_at": t.StartedAt,
				"completed_at": t.CompletedAt, "recent_output": t.RecentOutput,
				"terminal_id": t.TerminalId,
			})
		}
		result = map[string]interface{}{"tasks": tasks, "success": mt.Success}
	case *pb.StepUpdate_Finish:
		result = map[string]interface{}{"output": a.Finish.OutputJson}
	case *pb.StepUpdate_HostToolCall:
		// Host tool results are handled separately via the HostToolHandler;
		// this path is only reached for extractToolResult fallback.
		result = map[string]interface{}{"tool": a.HostToolCall.ToolName, "status": "delegated"}
	case *pb.StepUpdate_InvokeSubagent:
		sub := a.InvokeSubagent
		result = map[string]interface{}{
			"result":               sub.ResultText,
			"steps_executed":       sub.StepsExecuted,
			"child_trajectory_id":  sub.ChildTrajectoryId,
			"error":               sub.ErrorMessage,
		}
	case *pb.StepUpdate_SearchWeb:
		ws := a.SearchWeb
		var results []map[string]interface{}
		for _, res := range ws.Results {
			results = append(results, map[string]interface{}{
				"title": res.Title, "url": res.Url, "snippet": res.Snippet,
			})
		}
		result = map[string]interface{}{"results": results}
	case *pb.StepUpdate_ReadUrlContent:
		wf := a.ReadUrlContent
		result = map[string]interface{}{
			"content": wf.Content, "content_type": wf.ContentType,
		}
	case *pb.StepUpdate_McpTool:
		mt := a.McpTool
		result = map[string]interface{}{
			"server": mt.ServerName, "tool": mt.ToolName,
			"result": mt.ResultJson, "is_error": mt.IsError,
		}
	default:
		result = map[string]interface{}{"status": "done"}
	}

	b, _ := json.Marshal(result)
	return string(b)
}

// emitStep sends a step update to the client.
func (e *Engine) emitStep(step *pb.StepUpdate) {
	if e.stepCB != nil {
		e.stepCB(step)
	}
}

// emitTrajectoryState sends a trajectory state change.
func (e *Engine) emitTrajectoryState(state pb.TrajectoryState_TrajState) {
	if e.trajCB != nil {
		e.trajCB(&pb.TrajectoryState{
			TrajectoryId:       e.trajectoryID,
			State:              state,
			ParentTrajectoryId: e.parentTrajectoryID,
			Depth:              int32(e.depth),
		})
	}
}

// emitErrorStep emits a system error step.
func (e *Engine) emitErrorStep(msg string) {
	e.emitStep(&pb.StepUpdate{
		ConversationId: e.convID,
		TrajectoryId:   e.trajectoryID,
		StepIndex:      e.nextStepIndex(),
		Source:         pb.StepUpdate_SOURCE_SYSTEM,
		State:          pb.StepUpdate_STATE_ERROR,
		Target:         pb.StepUpdate_TARGET_USER,
		ErrorInfo:      &pb.ErrorInfo{Message: msg, Code: "ENGINE_ERROR"},
	})
}

// emitStructuredErrorStep emits a system error step with structured error metadata.
func (e *Engine) emitStructuredErrorStep(err error) {
	var hErr *errors.HarnessError
	if errors.As(err, &hErr) {
		// Convert HarnessError to ErrorInfo with structured context
		errorInfo := &pb.ErrorInfo{
			Message: hErr.Message,
			Code:    string(hErr.Code),
		}

		// Add structured context as additional metadata
		if hErr.Context != nil && len(hErr.Context) > 0 {
			if errorInfo.Metadata == nil {
				errorInfo.Metadata = make(map[string]string)
			}
			for key, value := range hErr.Context {
				errorInfo.Metadata[key] = errors.SerializeValue(value)
			}
		}

		// Add component if set
		if hErr.Component != "" {
			if errorInfo.Metadata == nil {
				errorInfo.Metadata = make(map[string]string)
			}
			errorInfo.Metadata["component"] = hErr.Component
		}

		e.emitStep(&pb.StepUpdate{
			ConversationId: e.convID,
			TrajectoryId:   e.trajectoryID,
			StepIndex:      e.nextStepIndex(),
			Source:         pb.StepUpdate_SOURCE_SYSTEM,
			State:          pb.StepUpdate_STATE_ERROR,
			Target:         pb.StepUpdate_TARGET_USER,
			ErrorInfo:      errorInfo,
		})
	} else {
		// Fallback to legacy error format
		e.emitErrorStep(err.Error())
	}
}

func (e *Engine) nextStepIndex() int32 {
	return e.stepIndex.Add(1) - 1
}

// streamGenerate reads from a streaming LLM provider, emitting STATE_STREAMING
// StepUpdates for each text/thinking delta, and returns the assembled GenerateResponse.
func (e *Engine) streamGenerate(ctx context.Context, sp llm.StreamingProvider, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	chunksCh, errCh := sp.GenerateStream(ctx, req)

	// Pre-allocate a step index for streaming deltas
	streamStepIdx := e.nextStepIndex()

	var contentBuf, thinkingBuf strings.Builder
	var allToolCalls []llm.ToolCall
	var finalUsage llm.Usage
	var finishReason string

	for chunk := range chunksCh {
		// Emit text delta
		if chunk.TextDelta != "" {
			contentBuf.WriteString(chunk.TextDelta)
			e.emitStep(&pb.StepUpdate{
				ConversationId: e.convID,
				TrajectoryId:   e.trajectoryID,
				StepIndex:      streamStepIdx,
				TextDelta:      chunk.TextDelta,
				Source:         pb.StepUpdate_SOURCE_MODEL,
				State:          pb.StepUpdate_STATE_STREAMING,
				Target:         pb.StepUpdate_TARGET_USER,
			})
		}

		// Emit thinking delta
		if chunk.ThinkingDelta != "" {
			thinkingBuf.WriteString(chunk.ThinkingDelta)
			e.emitStep(&pb.StepUpdate{
				ConversationId: e.convID,
				TrajectoryId:   e.trajectoryID,
				StepIndex:      streamStepIdx,
				ThinkingDelta:  chunk.ThinkingDelta,
				Source:         pb.StepUpdate_SOURCE_MODEL,
				State:          pb.StepUpdate_STATE_STREAMING,
				Target:         pb.StepUpdate_TARGET_USER,
			})
		}

		// Collect tool calls (typically only in the final chunk)
		if len(chunk.ToolCalls) > 0 {
			allToolCalls = append(allToolCalls, chunk.ToolCalls...)
		}

		// Capture final metadata
		if chunk.Done {
			finalUsage = chunk.Usage
			finishReason = chunk.FinishReason
		}
	}

	// Check for stream error
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	default:
	}

	// Determine finish reason
	if finishReason == "" {
		finishReason = "stop"
	}
	if len(allToolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return &llm.GenerateResponse{
		Content:      contentBuf.String(),
		Thinking:     thinkingBuf.String(),
		ToolCalls:    allToolCalls,
		Usage:        finalUsage,
		FinishReason: finishReason,
	}, nil
}

// buildToolDeclarations converts registered tools to LLM function declarations.
// Includes built-in tools (from registry), host-side tools (from config),
// MCP tools, and subagent tools. Applies tool group filtering for subagents.
func (e *Engine) buildToolDeclarations() []llm.FunctionDeclaration {
	schemas := e.toolRegistry.Schemas()
	decls := make([]llm.FunctionDeclaration, 0, len(schemas)+len(e.hostToolDecls))
	for _, s := range schemas {
		// Apply tool group filter — skip tools in excluded groups
		if len(e.excludeToolGroups) > 0 && s.Group != "" && e.excludeToolGroups[s.Group] {
			continue
		}
		decls = append(decls, llm.FunctionDeclaration{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  s.Parameters,
		})
	}

	// Append host-side tool declarations (unless excluded for this subagent)
	if !e.excludeHostTools {
		decls = append(decls, e.hostToolDecls...)
	}

	// Append MCP tool declarations (unless excluded for this subagent)
	if e.mcpMgr != nil && !e.excludeMCPTools {
		decls = append(decls, e.mcpMgr.ToolDeclarations()...)
	}

	// Append subagent tool declarations if enabled and depth allows
	if e.subagentsEnabled && e.depth < e.maxDepth {
		decls = append(decls, subagentToolDeclarations()...)
	}

	// Append browser subagent if enabled and depth allows
	if e.subagentsEnabled && e.depth < e.maxDepth {
		decls = append(decls, browserSubagentDeclaration())
	}

	return decls
}

// History returns the current conversation history.
func (e *Engine) History() []llm.Message {
	return e.history
}

// knownToolNames returns a set of all tool names the engine knows about.
// This includes built-in tools, engine-intercepted tools, host tools, and MCP tools.
// Used by text-to-tool-call recovery to validate extracted tool names.
func (e *Engine) knownToolNames() map[string]bool {
	names := make(map[string]bool)

	// Built-in + engine-intercepted tools from the registry
	for _, s := range e.toolRegistry.Schemas() {
		names[s.Name] = true
	}

	// Host-side tools
	for name := range e.hostToolNames {
		names[name] = true
	}

	// Host tool declarations (some may not be in hostToolNames)
	for _, d := range e.hostToolDecls {
		names[d.Name] = true
	}

	// MCP tools
	if e.mcpMgr != nil {
		for _, d := range e.mcpMgr.ToolDeclarations() {
			names[d.Name] = true
		}
	}

	// Engine-intercepted tools (not in registry)
	for _, name := range []string{
		"invoke_subagent", "define_subagent", "manage_subagents",
		"send_message", "ask_question", "ask_permission", "list_permissions",
		"knowledge_write", "knowledge_replace", "knowledge_delete",
		"publish", "finish", "browser_subagent",
	} {
		names[name] = true
	}

	return names
}
