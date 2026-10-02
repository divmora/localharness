// Package engine implements the agentic loop: LLM → tool calls → results → LLM.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/codegraph"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/llm"
	mcpbridge "github.com/divmora/localharness/internal/mcp"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/util"
	"github.com/divmora/localharness/internal/workspace"
	"golang.org/x/sync/errgroup"
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
// via the ask_permission tool or user approval. Grants persist for the lifetime of the engine.
type PermissionGrant struct {
	Action string // e.g. "run_command", "write_file", etc.
	Target string // Absolute path (for file ops) or command prefix/string
	Reason string // Why it was granted
	Scope  string // "once", "conversation", "global"
}

// QuestionHandler is called when the model invokes the ask_question tool.
// It emits an ActionUserQuestion (STATE_WAITING) and blocks until the SDK
// sends a QuestionResponse. Returns the user's answers.
type QuestionHandler func(ctx context.Context, req *pb.ActionUserQuestion) (*pb.QuestionResponse, error)

// DefaultStreamFlushInterval is the default batching/debounce window for streaming deltas.
// Chunks arriving within this window are coalesced into a single StepUpdate, reducing
// WebSocket frame serialization thrashing and UI render churn while preserving ~30-60fps display rate.
const DefaultStreamFlushInterval = 30 * time.Millisecond

// maxPendingStreamBytes is the buffer size limit triggering an immediate flush before the interval timer.
const maxPendingStreamBytes = 512

// defaultMaxConcurrentToolWorkers is the maximum number of concurrent workers
// used to execute read-only tools within a single turn.
const defaultMaxConcurrentToolWorkers = 8

// maxConsecutivePermissionDenials is the threshold of consecutive permission denials
// or timeouts before the circuit breaker trips to prevent infinite loops and token bleed.
const maxConsecutivePermissionDenials = 3

// Engine orchestrates the agentic loop.
type Engine struct {
	provider                     llm.Provider
	summarizerProvider           llm.Provider
	modelTierResolver            ModelTierResolver
	toolRegistry                 *tools.Registry
	logger                       *slog.Logger
	stepCB                       StepCallback
	stepMu                       sync.Mutex // Serializes stepCB invocations across concurrent tool executions
	trajCB                       TrajectoryCallback
	stepIndex                    atomic.Int32
	trajectoryID                 string
	convID                       string
	sysPrompt                    string
	history                      []llm.Message
	streamFlushInterval          time.Duration
	maxConcurrentToolWorkers     int // Max concurrent workers for parallel read-only tools
	maxTurns                     int // Safety limit on agentic loop iterations
	compactionThreshold          int // Token threshold for context compaction (0 = disabled)
	keepRecentMessages           int // Messages to preserve during compaction
	lastRealTokenCount           int // Most recent real token count from LLM provider
	tracer                       *Tracer
	contextWindow                int                        // Active model context window in tokens (e.g. 128000, 200000, 1048576)
	customContextWindow          int                        // Explicit user/config override if > 0
	brainDir                     string                     // For child engine tracing
	appDataDir                   string                     // Root data dir (for subagent inheritance)
	enablePlanningMode           bool                       // Planning guard: block workspace writes until plan exists
	researchToolCount            atomic.Int32               // Tracks research tool calls (view_file)
	hostToolHandler              HostToolHandler            // Called for SDK-registered tools
	hostToolNames                map[string]bool            // Fast lookup of host tool names
	hostToolDecls                []llm.FunctionDeclaration  // Host tool schemas for LLM
	permissionHandler            PermissionHandler          // Called before tool execution for policy checks
	permissionGrants             []PermissionGrant          // Grants from ask_permission (session-scoped)
	questionHandler              QuestionHandler            // Called for ask_question tool
	mcpMgr                       *mcpbridge.Manager         // MCP server bridge (nil if no MCP servers)
	msgCtx                       MessageContextConfig       // Per-message context enrichment config
	notifyCh                     <-chan tools.SystemMessage // System notifications (timers, task completions)
	notifySendCh                 chan<- tools.SystemMessage // Channel to forward notifications from subagents
	hasBrowserConfig             bool                       // Explicit browser configuration flag
	hasDesktopConfig             bool                       // Explicit desktop configuration flag
	pendingSyntheticMsgs         []string                   // Buffered synthetic notifications to include on next turn
	rulesFingerprint             string                     // Fingerprint of user rules & workspaces to detect updates
	running                      atomic.Int32               // 1 = engine is running a turn, 0 = idle
	preCompletionHook            func()                     // Called before TRAJ_IDLE so session can save state
	turnCheckpointHook           func()                     // Called after each agentic turn to persist state incrementally
	consecutivePermissionDenials int                        // Consecutive permission denials/timeouts counter

	// Interruption API channels
	pauseCh  chan struct{}
	resumeCh chan string

	// Subagent support
	depth                       int               // Nesting depth (0 = root)
	maxDepth                    int               // Max nesting depth (default: 3)
	maxSubagents                int               // Max concurrent children (default: 5)
	activeSubagents             int32             // Atomic counter of running children
	parentTrajectoryID          string            // Empty for root trajectory
	subagentsEnabled            bool              // Whether subagent tools are available
	inheritSubagentCapabilities bool              // Whether subagents inherit capabilities by default
	subagentRegistry            *SubagentRegistry // Type registry (built-in + SDK + agent-defined)
	subagentTracker             *SubagentTracker  // Active instance tracker

	// Tool group filtering — used by subagents to restrict tool access.
	// Keys are ToolGroup values ("read", "write"). If a group is in this set,
	// tools in that group are excluded from declarations.
	excludeToolGroups map[tools.ToolGroup]bool
	excludeHostTools  bool // If true, host (SDK-registered) tools are not declared
	excludeMCPTools   bool // If true, MCP tools are not declared

	// Knowledge Items
	globalKnowledgeStore     *KnowledgeStore
	workspaceKnowledgeStores map[string]*KnowledgeStore // keyed by absolute workspace path

	// Code Graph
	codeGraphManager *codegraph.Manager
	projectRegistry  *ProjectRegistry

	// Multi-agent coordination
	agentBus *AgentBus                  // Shared pub/sub bus across agent family (nil for standalone)
	convMgr  *conversation.Manager      // For creating child conversations (nil if not passed)
	conv     *conversation.Conversation // Subagent's own conversation (nil for root — Session manages it)

	mu             sync.RWMutex
	accessMode     pb.AccessMode     // Access mode (workspace, system, unrestricted)
	env            map[string]string // Isolated environment variables for child processes/tools
	workspaces     []string
	workspaceInfos []WorkspaceInfo
	userRules      []config.UserRule
	yoloMode       bool
	globalSettings *config.GlobalSettings
}

// Config holds engine configuration.
type Config struct {
	AccessMode          pb.AccessMode // Access mode (workspace, system, unrestricted)
	Provider            llm.Provider
	SummarizerProvider  llm.Provider      // Optional: dedicated fast provider for context compaction (defaults to flash tier)
	ModelTierResolver   ModelTierResolver // Optional: custom resolver for model tiers (defaults to DefaultModelTierResolver)
	ToolRegistry        *tools.Registry
	SystemPrompt        string
	ConversationID      string
	TrajectoryID        string
	OnStep              StepCallback
	OnTrajectory        TrajectoryCallback
	MaxTurns            int
	CompactionThreshold int    // 0 = auto-calculate for model, -1 = disabled, >0 = custom token threshold
	ContextWindow       int    // 0 = auto-detect from model name, >0 = explicit context window
	KeepRecentMessages  int    // 0 = default (10)
	BrainDir            string // For tracing; empty = tracing disabled
	AppDataDir          string // Root data directory (e.g. ~/.divmora/localharness)
	Logger              *slog.Logger
	HostToolHandler     HostToolHandler           // Called for SDK-registered tools
	HostToolNames       map[string]bool           // Set of host tool names
	HostToolDecls       []llm.FunctionDeclaration // Host tool schemas for LLM
	PermissionHandler   PermissionHandler         // Called before tool execution for policy checks
	QuestionHandler     QuestionHandler           // Called for ask_question tool
	InitialHistory      []llm.Message             // Initial message history to restore context
	MCPManager          *mcpbridge.Manager        // MCP server bridge (nil if no MCP servers)
	YoloMode            bool                      // Bypass all tool permission checks

	CodeGraphManager *codegraph.Manager

	// Structured system instructions (takes priority over SystemPrompt if set)
	StructuredInstructions *pb.StructuredSystemInstructions

	// Per-message context enrichment
	Workspaces     []string          // Workspace directories (for internal systems)
	WorkspaceInfos []WorkspaceInfo   // Workspace definitions with corpus mappings (for per-message context)
	UserRules      []config.UserRule // Content from AGENTS.md files

	// Subagent support
	Depth              int    // Nesting depth (0 = root)
	MaxDepth           int    // 0 = default (3)
	MaxSubagents       int    // 0 = default (5)
	ParentTrajectoryID string // Empty for root trajectory
	SubagentsEnabled   bool   // Whether subagent tools are available

	// Subagent types — SDK-registered custom types to merge with built-ins.
	SubagentTypes               []SubagentTypeDef // SDK-registered custom types
	ExcludeBuiltinSubagents     []string          // Built-in type names to exclude
	DisableAllBuiltins          bool              // Disable ALL built-in subagent types
	InheritSubagentCapabilities bool              // Default to inheriting capabilities for all subagents

	// Prompt modules
	EnableWebDev         bool              // Enable the <web_application_development> section (off by default)
	EnablePlanningMode   bool              // Enable the <planning_mode> section (off by default)
	EnableSlashCommands  bool              // Enable the <slash_commands> section (off by default)
	SlashCommands        []SlashCommandDef // Available slash commands (only with EnableSlashCommands)
	EnableKnowledgeItems bool              // Enable the <knowledge_items> section (off by default)
	EnableCodeGraph      bool              // Enable the <code_graph> section (off by default)
	Skills               []SkillDef        // Available skills (data-driven: non-empty = enabled)
	Plugins              []PluginDef       // Installed plugins (data-driven: non-empty = enabled)

	// System notification channel (from timers, background tasks)
	NotifyCh     <-chan tools.SystemMessage
	NotifySendCh chan<- tools.SystemMessage // Channel for subagents to send completion notifications

	HasBrowserConfig bool // Whether browser capability is explicitly configured
	HasDesktopConfig bool // Whether desktop capability is explicitly configured

	// Tool group filtering for subagents.
	// ExcludeToolGroups is a set of ToolGroup values to hide from the LLM.
	ExcludeToolGroups map[tools.ToolGroup]bool
	ExcludeHostTools  bool // Hide SDK-registered host tools
	ExcludeMCPTools   bool // Hide MCP tools

	// StreamFlushInterval configures the batching/debounce window for streaming deltas.
	// Defaults to DefaultStreamFlushInterval (30ms) if 0. Set to <0 to disable batching.
	StreamFlushInterval time.Duration

	// MaxConcurrentToolWorkers is the maximum number of workers for parallel read-only tool execution.
	// Defaults to defaultMaxConcurrentToolWorkers (8) if 0. Set to 1 to force strictly sequential execution.
	MaxConcurrentToolWorkers int

	// Env specifies isolated environment variables for child processes and tools launched by this engine.
	Env map[string]string

	// Knowledge Items — project registry for workspace → project UUID mapping.
	ProjectRegistry *ProjectRegistry

	// Multi-agent coordination
	AgentBus             *AgentBus                  // Shared pub/sub bus (root creates, children inherit)
	ConversationManager  *conversation.Manager      // For creating child conversations
	Conversation         *conversation.Conversation // Active conversation for state & token usage tracking
	ParentConversationID string                     // Parent's conv ID (empty for root)
	AgentRole            string                     // Human-readable role: "developer", "reviewer"
	AgentTypeName        string                     // Type name: "research", "self", etc.
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

	flushInterval := cfg.StreamFlushInterval
	if flushInterval == 0 {
		flushInterval = DefaultStreamFlushInterval
	} else if flushInterval < 0 {
		flushInterval = 0 // disabled
	}

	maxWorkers := cfg.MaxConcurrentToolWorkers
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxConcurrentToolWorkers
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
	var trackerNotifyCh chan<- tools.SystemMessage
	if cfg.NotifySendCh != nil {
		trackerNotifyCh = cfg.NotifySendCh
	} else if cfg.SubagentsEnabled {
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
		UserInstructions:     cfg.SystemPrompt,
		Structured:           cfg.StructuredInstructions,
		EnableWebDev:         cfg.EnableWebDev,
		EnablePlanningMode:   cfg.EnablePlanningMode,
		EnableSlashCommands:  cfg.EnableSlashCommands,
		SlashCommands:        cfg.SlashCommands,
		EnableKnowledgeItems: cfg.EnableKnowledgeItems,
		EnableCodeGraph:      cfg.EnableCodeGraph,
		Skills:               cfg.Skills,
		Plugins:              cfg.Plugins,
		SubagentsEnabled:     cfg.SubagentsEnabled,
		SubagentTypes:        subagentTypes,
		BrainDir:             cfg.BrainDir,
	})

	// Initialize code graph manager
	codeGraphMgr := cfg.CodeGraphManager
	if codeGraphMgr == nil && cfg.AppDataDir != "" {
		codeGraphMgr = codegraph.NewManager(filepath.Join(cfg.AppDataDir, "knowledge"))
	}
	if codeGraphMgr != nil && cfg.ProjectRegistry != nil {
		for _, ws := range cfg.Workspaces {
			if proj, _ := cfg.ProjectRegistry.FindOrCreate([]string{ws}); proj != nil {
				codeGraphMgr.RegisterWorkspace(ws, proj.ID)
				go func(pID, w string) {
					_, _ = codeGraphMgr.IndexWorkspace(context.Background(), pID, w, "")
				}(proj.ID, ws)
			}
		}
	}

	// Initialize agent bus: root creates, children inherit.
	bus := cfg.AgentBus
	if bus == nil && cfg.SubagentsEnabled {
		bus = NewAgentBus()
	}

	resolver := cfg.ModelTierResolver
	if resolver == nil {
		resolver = DefaultModelTierResolver
	}

	summarizer := cfg.SummarizerProvider
	if summarizer == nil && cfg.Provider != nil {
		tierModel := resolver(cfg.Provider.ModelName(), string(ModelTierFlash))
		if tierModel != "" && tierModel != cfg.Provider.ModelName() {
			if cloner, ok := cfg.Provider.(llm.ModelCloner); ok {
				summarizer = cloner.WithModel(tierModel)
			}
		}
	}
	if summarizer == nil {
		summarizer = cfg.Provider
	}

	compactionThreshold := cfg.CompactionThreshold
	var contextWindow int
	if cfg.Provider != nil {
		win, compThresh := CalculateModelCompactionThreshold(cfg.Provider.ModelName())
		contextWindow = win
		if compactionThreshold == 0 {
			compactionThreshold = compThresh
		}
	} else {
		contextWindow = 128000
	}
	if cfg.ContextWindow > 0 {
		contextWindow = cfg.ContextWindow
	}
	if compactionThreshold < 0 {
		compactionThreshold = 0 // Explicitly disabled
	}

	eng := &Engine{
		provider:                 cfg.Provider,
		summarizerProvider:       summarizer,
		modelTierResolver:        resolver,
		toolRegistry:             cfg.ToolRegistry,
		logger:                   cfg.Logger,
		stepCB:                   cfg.OnStep,
		trajCB:                   cfg.OnTrajectory,
		trajectoryID:             cfg.TrajectoryID,
		convID:                   cfg.ConversationID,
		sysPrompt:                sysPrompt,
		streamFlushInterval:      flushInterval,
		maxConcurrentToolWorkers: maxWorkers,
		maxTurns:                 cfg.MaxTurns,
		compactionThreshold:      compactionThreshold,
		contextWindow:            contextWindow,
		customContextWindow:      cfg.ContextWindow,
		keepRecentMessages:       cfg.KeepRecentMessages,
		tracer:                   NewTracer(cfg.BrainDir, cfg.Logger),
		brainDir:                 cfg.BrainDir,
		appDataDir:               cfg.AppDataDir,
		enablePlanningMode:       cfg.EnablePlanningMode,
		hostToolHandler:          cfg.HostToolHandler,
		hostToolNames:            cfg.HostToolNames,
		hostToolDecls:            cfg.HostToolDecls,
		permissionHandler:        cfg.PermissionHandler,
		questionHandler:          cfg.QuestionHandler,
		history:                  cfg.InitialHistory,
		mcpMgr:                   cfg.MCPManager,
		pauseCh:                  make(chan struct{}, 1),
		resumeCh:                 make(chan string, 1),
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
		notifyCh:                    cfg.NotifyCh,
		notifySendCh:                cfg.NotifySendCh,
		hasBrowserConfig:            cfg.HasBrowserConfig,
		hasDesktopConfig:            cfg.HasDesktopConfig,
		depth:                       cfg.Depth,
		maxDepth:                    cfg.MaxDepth,
		maxSubagents:                cfg.MaxSubagents,
		parentTrajectoryID:          cfg.ParentTrajectoryID,
		subagentsEnabled:            cfg.SubagentsEnabled,
		inheritSubagentCapabilities: cfg.InheritSubagentCapabilities,
		subagentRegistry:            subagentRegistry,
		subagentTracker:             subagentTracker,
		excludeToolGroups:           cfg.ExcludeToolGroups,
		excludeHostTools:            cfg.ExcludeHostTools,
		excludeMCPTools:             cfg.ExcludeMCPTools,
		globalKnowledgeStore:        globalKnowledgeStore,
		workspaceKnowledgeStores:    workspaceKnowledgeStores,
		codeGraphManager:            codeGraphMgr,
		projectRegistry:             cfg.ProjectRegistry,
		agentBus:                    bus,
		convMgr:                     cfg.ConversationManager,
		conv:                        cfg.Conversation,
		workspaces:                  cfg.Workspaces,
		workspaceInfos:              cfg.WorkspaceInfos,
		userRules:                   cfg.UserRules,
		yoloMode:                    cfg.YoloMode,
		accessMode:                  cfg.AccessMode,
		globalSettings:              config.LoadGlobalSettings(cfg.Logger),
	}

	if len(cfg.Env) > 0 {
		eng.env = make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			eng.env[k] = v
		}
	}

	if eng.toolRegistry != nil {
		eng.toolRegistry = eng.toolRegistry.Clone()
	} else {
		eng.toolRegistry = tools.NewRegistry(nil, eng.logger)
	}
	eng.toolRegistry.SetStepEmitter(eng.emitStep)
	if eng.conv != nil {
		eng.toolRegistry.SetConversation(eng.conv)
	}

	if len(eng.history) > 0 {
		eng.rulesFingerprint = eng.computeRulesFingerprint()
	}

	return eng
}

// Env returns a copy of the engine's isolated environment variables.
func (e *Engine) Env() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.env) == 0 {
		return nil
	}
	res := make(map[string]string, len(e.env))
	for k, v := range e.env {
		res[k] = v
	}
	return res
}

// SetYoloMode enables or disables YOLO mode (bypassing all permission checks).
func (e *Engine) SetYoloMode(enabled bool) {
	e.mu.Lock()
	e.yoloMode = enabled
	e.mu.Unlock()
}

// AccessMode returns the current access mode for the engine.
func (e *Engine) AccessMode() pb.AccessMode {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.accessMode
}

// SetAccessMode updates the access mode for the engine.
func (e *Engine) SetAccessMode(mode pb.AccessMode) {
	e.mu.Lock()
	e.accessMode = mode
	e.mu.Unlock()
}

// Provider returns the active LLM provider.
func (e *Engine) Provider() llm.Provider {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.provider
}

// CompactionThreshold returns the current token threshold for context compaction.
func (e *Engine) CompactionThreshold() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.compactionThreshold
}

// ContextWindow returns the active model context window in tokens.
func (e *Engine) ContextWindow() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.contextWindow <= 0 {
		return 128000
	}
	return e.contextWindow
}

// SetContextWindow sets an explicit context window override for the engine.
func (e *Engine) SetContextWindow(win int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if win > 0 {
		e.customContextWindow = win
		e.contextWindow = win
	}
}

// SetProvider dynamically switches the active LLM provider mid-session.
// It recalculates the summarizer provider and context window compaction threshold
// to match the new model's capabilities unless customCompactionThreshold > 0.
func (e *Engine) SetProvider(p llm.Provider, customCompactionThreshold int) (contextWindow int, compactionThreshold int) {
	if p == nil {
		return 0, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	e.provider = p

	// Re-derive summarizer provider (defaults to flash tier or same provider)
	resolver := e.modelTierResolver
	if resolver == nil {
		resolver = DefaultModelTierResolver
	}
	tierModel := resolver(p.ModelName(), string(ModelTierFlash))
	if tierModel != "" && tierModel != p.ModelName() {
		if cloner, ok := p.(llm.ModelCloner); ok {
			e.summarizerProvider = cloner.WithModel(tierModel)
		} else {
			e.summarizerProvider = p
		}
	} else {
		e.summarizerProvider = p
	}

	win, compThresh := CalculateModelCompactionThreshold(p.ModelName())
	if e.customContextWindow > 0 {
		e.contextWindow = e.customContextWindow
	} else {
		e.contextWindow = win
	}
	if customCompactionThreshold > 0 {
		e.compactionThreshold = customCompactionThreshold
	} else {
		e.compactionThreshold = compThresh
	}

	if e.logger != nil {
		e.logger.Info("switched active LLM provider",
			"model", p.ModelName(),
			"context_window", win,
			"compaction_threshold", e.compactionThreshold,
		)
	}

	return win, e.compactionThreshold
}

// getSummarizerProvider returns the provider to use for context compaction.
func (e *Engine) getSummarizerProvider() llm.Provider {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.summarizerProvider != nil {
		return e.summarizerProvider
	}
	return e.provider
}

// AddPermissionGrant adds a conversation-scoped permission grant.
func (e *Engine) AddPermissionGrant(grant PermissionGrant) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.permissionGrants = append(e.permissionGrants, grant)
}

// ReloadGlobalSettings reloads settings from ~/.divmora/config/settings.json.
func (e *Engine) ReloadGlobalSettings() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalSettings = config.LoadGlobalSettings(e.logger)
}

// isPermissionGranted checks if a tool call is permitted by global settings or conversation grants.
func (e *Engine) isPermissionGranted(tc llm.ToolCall) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check Global Settings (~/.divmora/config/settings.json) & Conversation Grants
	if tc.Name == "run_command" {
		cmd := extractToolCommand(tc)
		if cmd != "" {
			var allowedCmds []string
			if e.globalSettings != nil {
				if e.globalSettings.IsToolAllowed(tc.Name) {
					return true
				}
				allowedCmds = append(allowedCmds, e.globalSettings.AllowedCommands...)
			}
			for _, g := range e.permissionGrants {
				if g.Action == "*" || g.Action == "run_command" {
					if g.Target != "" {
						allowedCmds = append(allowedCmds, g.Target)
					} else {
						allowedCmds = append(allowedCmds, "*")
					}
				}
			}
			if len(allowedCmds) > 0 && util.IsCommandAllowedAgainstRules(cmd, allowedCmds) {
				return true
			}
		}
	} else {
		if e.globalSettings != nil && e.globalSettings.IsToolAllowed(tc.Name) {
			return true
		}
		for _, g := range e.permissionGrants {
			if g.Action == "*" || g.Action == tc.Name {
				if g.Target == "" || g.Target == "*" {
					return true
				}
				targetPath := extractToolPath(tc)
				if targetPath != "" {
					absTarget := filepath.Clean(targetPath)
					absGrant := filepath.Clean(g.Target)
					if absTarget == absGrant || strings.HasPrefix(absTarget, absGrant+string(filepath.Separator)) {
						return true
					}
				}
			}
		}
	}

	return false
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

	// Persist to conversation state config if active
	if e.conv != nil && e.conv.State != nil && e.conv.State.Config != nil {
		e.conv.State.Config.Workspaces = append(e.conv.State.Config.Workspaces, &pb.Workspace{
			Directory:  ws,
			Name:       filepath.Base(ws),
			CorpusName: info.CorpusName,
		})
		_ = e.conv.SaveAll()
	}
}

// UserRules returns the active user rules loaded in the engine.
func (e *Engine) UserRules() []config.UserRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]config.UserRule, len(e.userRules))
	copy(out, e.userRules)
	return out
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

// SetTurnCheckpointHook registers a callback that runs after each agentic turn
// executes tools and updates history, allowing callers to persist state incrementally.
func (e *Engine) SetTurnCheckpointHook(fn func()) {
	e.turnCheckpointHook = fn
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
	e.researchToolCount.Store(0)

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
	e.mu.Lock()
	if len(e.pendingSyntheticMsgs) > 0 {
		pendingMsgs = append(pendingMsgs, e.pendingSyntheticMsgs...)
		e.pendingSyntheticMsgs = nil
	}
	e.mu.Unlock()

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
	var enrichedParts []string
	if e.shouldEnrichFull() {
		enrichedParts = EnrichUserMessage(userMessage, msgCtx)
		e.mu.Lock()
		e.rulesFingerprint = e.computeRulesFingerprintLocked()
		e.mu.Unlock()
	} else {
		enrichedParts = EnrichFollowUpUserMessage(userMessage, msgCtx)
	}

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
	e.consecutivePermissionDenials = 0
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
				"pruned_user_ctx", reduction.PrunedUserContext,
				"tokens_saved", reduction.TokensSaved,
			)
		}

		// Context compaction — summarize old messages if over threshold
		threshold := e.CompactionThreshold()
		if threshold > 0 {
			compacted, result, compErr := CompactIfNeeded(
				ctx, e.getSummarizerProvider(), e.history, CompactionConfig{
					Threshold:          threshold,
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

		// Pre-flight context budgeting & token overflow protection:
		// Proactively check if estimated prompt tokens will exceed the model's context ceiling
		// minus safety margin. If so, execute emergency compaction and trimming to prevent
		// unrecoverable 400 context_length_exceeded API errors.
		contextWindow := e.ContextWindow()
		safetyMargin := 4096
		if margin := int(float64(contextWindow) * 0.10); margin > safetyMargin {
			safetyMargin = margin
		}
		maxAllowed := contextWindow - safetyMargin
		if maxAllowed < 2000 {
			maxAllowed = 2000
		}

		estTokens := EstimateTokens(e.history) + estimateStringTokens(e.sysPrompt)
		for _, td := range toolDecls {
			estTokens += 20 + estimateStringTokens(td.Name) + estimateStringTokens(td.Description)
		}

		if estTokens > maxAllowed {
			e.logger.Warn("pre-flight context overflow detected, executing emergency compaction",
				"estimated_tokens", estTokens,
				"max_allowed", maxAllowed,
				"context_window", contextWindow,
			)

			// Step 1: Force immediate compaction with minimal preserved window (4 messages)
			emergencyCompacted, compResult, compErr := CompactIfNeeded(
				ctx, e.getSummarizerProvider(), e.history, CompactionConfig{
					Threshold:          1, // Force compaction
					KeepRecentMessages: 4,
					SystemPromptTokens: estimateStringTokens(e.sysPrompt),
				}, e.logger,
			)
			if compErr == nil && compResult != nil {
				e.history = emergencyCompacted
				e.reinjectMetadataAfterCompaction()
				e.lastRealTokenCount = 0
				e.emitStep(&pb.StepUpdate{
					ConversationId: e.convID,
					TrajectoryId:   e.trajectoryID,
					StepIndex:      e.nextStepIndex(),
					Source:         pb.StepUpdate_SOURCE_SYSTEM,
					State:          pb.StepUpdate_STATE_DONE,
					Target:         pb.StepUpdate_TARGET_INTERNAL,
					Action: &pb.StepUpdate_Compaction{
						Compaction: &pb.ActionCompaction{
							OriginalTokens:  int32(compResult.OriginalTokens),
							CompactedTokens: int32(compResult.CompactedTokens),
							MessagesRemoved: int32(compResult.MessagesRemoved),
							Summary:         compResult.Summary,
						},
					},
				})
			}

			// Step 2: If still over budget, aggressively trim historical tool outputs
			newEst := EstimateTokens(e.history) + estimateStringTokens(e.sysPrompt)
			if newEst > maxAllowed {
				var saved int
				e.history, saved = EmergencyTrimHistory(e.history, 2)
				e.logger.Warn("emergency tool output trimming applied",
					"tokens_saved", saved,
					"new_estimate", EstimateTokens(e.history),
				)
			}
		}

		// Call LLM (streaming if supported, otherwise blocking)
		req := &llm.GenerateRequest{
			Messages:     e.history,
			Tools:        toolDecls,
			SystemPrompt: e.sysPrompt,
		}

		activeProvider := e.Provider()

		// Trace the request
		stepForTrace := int(e.stepIndex.Load())
		e.tracer.TraceRequest(stepForTrace, activeProvider.ModelName(), req)

		var resp *llm.GenerateResponse
		var streamStepIdx int32 = -1
		var err error
		start := time.Now()

		// Per-call timeout prevents hanging when the LLM API stalls
		// (e.g., a proxy dies mid-stream without closing the connection).
		callCtx, callCancel := context.WithTimeout(ctx, llmCallTimeout)

		if sp, ok := activeProvider.(llm.StreamingProvider); ok {
			resp, streamStepIdx, err = e.streamGenerate(callCtx, sp, req)
		} else {
			resp, err = activeProvider.Generate(callCtx, req)
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
				WithContext("model", activeProvider.ModelName()).
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

		// Accumulate generation usage into conversation total exactly once per LLM call
		if e.conv != nil && (resp.Usage.TotalTokens > 0 || resp.Usage.PromptTokens > 0) {
			e.conv.AddUsage(&pb.UsageMetadata{
				PromptTokens:     int32(resp.Usage.PromptTokens),
				CompletionTokens: int32(resp.Usage.CompletionTokens),
				ThinkingTokens:   int32(resp.Usage.ThinkingTokens),
				TotalTokens:      int32(resp.Usage.TotalTokens),
				CachedTokens:     int32(resp.Usage.CachedTokens),
			})
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
				Role:    "model",
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
			return e.handleFinalResponse(resp, streamStepIdx)
		}

		// Model wants to call tools
		// First, add the model's response to history
		modelMsg := llm.Message{
			Role:      "model",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		e.history = append(e.history, modelMsg)

		// Execute tool calls (concurrently for read-only batches, sequentially with barriers for mutating tools)
		if err := e.executeTools(ctx, resp.ToolCalls, resp); err != nil {
			e.emitErrorStep(fmt.Sprintf("Tool execution error: %v", err))
			e.emitTrajectoryState(pb.TrajectoryState_TRAJ_ERROR)
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"tool execution failed").
				WithContext("trajectory_id", e.trajectoryID).
				WithContext("conversation_id", e.convID).
				WithComponent("engine")
		}

		// Check if finish was called
		for _, tc := range resp.ToolCalls {
			if tc.Name == "finish" {
				if e.turnCheckpointHook != nil {
					e.turnCheckpointHook()
				}
				e.emitTrajectoryState(pb.TrajectoryState_TRAJ_COMPLETED)
				return nil
			}
		}

		// Incremental turn checkpoint hook
		if e.turnCheckpointHook != nil {
			e.turnCheckpointHook()
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
		e.mu.Lock()
		e.rulesFingerprint = e.computeRulesFingerprintLocked()
		e.mu.Unlock()
		return
	}
}

// computeRulesFingerprintLocked generates a stable fingerprint of current workspaces and user rules.
// Must be called with e.mu held (or during initialization before concurrent access).
func (e *Engine) computeRulesFingerprintLocked() string {
	var b strings.Builder
	for _, ws := range e.workspaces {
		b.WriteString(ws)
		b.WriteString(";")
	}
	for _, r := range e.userRules {
		b.WriteString(r.Filename)
		b.WriteString(":")
		b.WriteString(r.Content)
		b.WriteString(";")
	}
	return b.String()
}

// computeRulesFingerprint generates a stable fingerprint of current workspaces and user rules.
func (e *Engine) computeRulesFingerprint() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.computeRulesFingerprintLocked()
}

// shouldEnrichFull determines whether the next user message needs full context enrichment
// (<user_rules>, <user_information>, <skills>, etc.) or whether follow-up enrichment is sufficient.
func (e *Engine) shouldEnrichFull() bool {
	if len(e.history) == 0 {
		return true
	}
	currentFP := e.computeRulesFingerprint()
	e.mu.RLock()
	lastFP := e.rulesFingerprint
	e.mu.RUnlock()
	if lastFP == "" || lastFP != currentFP {
		return true
	}
	// Check if any existing user message in history already has the rules or user information.
	for _, msg := range e.history {
		if msg.Role == "user" {
			for _, p := range msg.Parts {
				if strings.Contains(p, "<user_rules>") || strings.Contains(p, "<user_information>") {
					return false
				}
			}
			if strings.Contains(msg.Content, "<user_rules>") || strings.Contains(msg.Content, "<user_information>") {
				return false
			}
		}
	}
	return true
}

// handleFinalResponse processes the LLM's final text response.
func (e *Engine) handleFinalResponse(resp *llm.GenerateResponse, stepIdx int32) error {
	if stepIdx < 0 {
		stepIdx = e.nextStepIndex()
	}

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

// toolRequiresPermission returns true if the tool call requires user or policy approval.
func (e *Engine) toolRequiresPermission(tc llm.ToolCall) bool {
	if e.permissionHandler == nil {
		return false
	}
	if e.yoloMode {
		return false
	}
	if e.AccessMode() == pb.AccessMode_ACCESS_MODE_UNRESTRICTED {
		return false
	}
	if isAlwaysAllowedTool(tc.Name) {
		return false
	}
	if e.isAppDataDirPath(tc) {
		return false
	}

	targetPath := extractToolPath(tc)

	if e.AccessMode() == pb.AccessMode_ACCESS_MODE_SYSTEM {
		// In system mode, supervised host-wide access:
		// 1. Destructive commands or dangerous shell calls require permission
		if tc.Name == "run_command" {
			cmd := extractToolCommand(tc)
			if util.IsDestructiveCommand(cmd) {
				return !e.isPermissionGranted(tc)
			}
			// Non-destructive commands in system mode do not prompt unless blocked by policy
			return false
		}
		// 2. Sensitive host paths require permission (even for read-only tools)
		if targetPath != "" && workspace.IsSensitivePath(targetPath) {
			return !e.isPermissionGranted(tc)
		}
		// 3. Modifying files outside the workspace in system mode requires permission
		if !isReadOnlyFileTool(tc.Name) && targetPath != "" {
			if !e.isPathInsideWorkspaceOrAppData(targetPath) {
				return !e.isPermissionGranted(tc)
			}
			return false
		}
		// Read-only access to non-sensitive host paths is permitted without prompt
		if isReadOnlyFileTool(tc.Name) {
			return false
		}
		return !e.isPermissionGranted(tc)
	}

	// ACCESS_MODE_WORKSPACE (default)
	if isReadOnlyFileTool(tc.Name) {
		if e.isPathInsideWorkspaceOrAppData(targetPath) {
			return false
		}
		return !e.isPermissionGranted(tc)
	}
	return !e.isPermissionGranted(tc)
}

// extractToolCommand extracts the command string from tool call arguments.
func extractToolCommand(tc llm.ToolCall) string {
	if c, ok := tc.Args["command"].(string); ok && c != "" {
		return c
	}
	if c, ok := tc.Args["CommandLine"].(string); ok && c != "" {
		return c
	}
	return ""
}

// isConcurrentReadOnlyTool returns true if tc is a read-only tool that can safely
// execute concurrently with other read-only tools in the same turn.
func (e *Engine) isConcurrentReadOnlyTool(tc llm.ToolCall) bool {
	if e.toolRegistry == nil || !e.toolRegistry.IsReadOnly(tc.Name) {
		return false
	}
	// Exclude desktop automation and interactive tools
	if strings.HasPrefix(tc.Name, "desktop_") {
		return false
	}
	switch tc.Name {
	case "ask_question", "ask_permission", "finish", "invoke_subagent",
		"define_subagent", "manage_subagents", "send_message", "browser_subagent",
		"desktop_subagent", "manage_task", "publish":
		return false
	}
	// If the tool requires interactive permission evaluation, run sequentially
	if e.toolRequiresPermission(tc) {
		return false
	}
	return true
}

// executeReadOnlyTool executes a single read-only tool call without modifying e.history.
// Returns the resulting llm.Message (success or error) to be appended deterministically by executeTools.
func (e *Engine) executeReadOnlyTool(ctx context.Context, tc llm.ToolCall, usage *pb.UsageMetadata) (llm.Message, error) {
	stepIdx := e.nextStepIndex()

	// Create step with tool action (STATE_ACTIVE)
	step := e.buildToolStep(tc, stepIdx)
	step.State = pb.StepUpdate_STATE_ACTIVE
	step.Source = pb.StepUpdate_SOURCE_MODEL
	step.Target = pb.StepUpdate_TARGET_INTERNAL

	e.emitStep(step)

	// Planning guard check (tracks research tools)
	if denied, reason := e.checkPlanningGuard(tc); denied {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: reason,
			Code:    "PLANNING_REQUIRED",
		}
		step.Usage = usage
		e.emitStep(step)
		return toolResultMsg(tc, reason, true), nil
	}

	if len(e.env) > 0 {
		ctx = tools.WithEnvironment(ctx, e.env)
	}

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

		switch tc.Name {
		case "codegraph_search":
			err = e.executeCodeGraphSearch(ctx, tc, step)
		case "codegraph_find_references":
			err = e.executeCodeGraphFindReferences(ctx, tc, step)
		case "codegraph_call_hierarchy":
			err = e.executeCodeGraphCallHierarchy(ctx, tc, step)
		case "codegraph_get_impact":
			err = e.executeCodeGraphGetImpact(ctx, tc, step)
		case "codegraph_diff_branches":
			err = e.executeCodeGraphDiffBranches(ctx, tc, step)
		default:
			err = e.toolRegistry.Execute(ctx, tc.Name, step)
		}
	}()

	if err != nil {
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: err.Error(),
			Code:    "TOOL_ERROR",
		}
		step.Usage = usage
		e.emitStep(step)
		return toolResultMsg(tc, fmt.Sprintf("Error: %v", err), true), nil
	}

	// Emit completed step (STATE_DONE) with usage attached if not already done
	if step.State != pb.StepUpdate_STATE_DONE {
		step.State = pb.StepUpdate_STATE_DONE
		step.Usage = usage
		e.emitStep(step)
	}

	// Build tool result for history
	resultJSON := e.extractToolResult(step)
	e.consecutivePermissionDenials = 0
	return toolResultMsg(tc, resultJSON, false), nil
}

// executeTools dispatches tool calls emitted in a turn.
// Contiguous sequences of read-only tool calls are executed concurrently
// up to e.maxConcurrentToolWorkers, while mutating or interactive tool calls
// are executed sequentially with synchronization barriers.
func (e *Engine) executeTools(ctx context.Context, toolCalls []llm.ToolCall, resp *llm.GenerateResponse) error {
	var turnUsage *pb.UsageMetadata
	if resp != nil && (resp.Usage.TotalTokens > 0 || resp.Usage.PromptTokens > 0) {
		turnUsage = &pb.UsageMetadata{
			PromptTokens:     int32(resp.Usage.PromptTokens),
			CompletionTokens: int32(resp.Usage.CompletionTokens),
			ThinkingTokens:   int32(resp.Usage.ThinkingTokens),
			TotalTokens:      int32(resp.Usage.TotalTokens),
			CachedTokens:     int32(resp.Usage.CachedTokens),
		}
	}

	i := 0
	for i < len(toolCalls) {
		tc := toolCalls[i]

		// If this tool starts a concurrent read-only batch
		if e.maxConcurrentToolWorkers > 1 && e.isConcurrentReadOnlyTool(tc) {
			start := i
			for i < len(toolCalls) && e.isConcurrentReadOnlyTool(toolCalls[i]) {
				i++
			}
			batch := toolCalls[start:i]

			if len(batch) == 1 {
				// Single read-only tool — run directly
				var u *pb.UsageMetadata
				if start == 0 {
					u = turnUsage
				}
				msg, err := e.executeReadOnlyTool(ctx, batch[0], u)
				if err != nil {
					e.logger.Error("tool execution failed", "tool", batch[0].Name, "error", err)
					e.history = append(e.history, toolResultMsg(batch[0], fmt.Sprintf("Error: %v", err), true))
				} else {
					e.history = append(e.history, msg)
				}
			} else {
				// Multiple read-only tools — run concurrently in worker pool
				results := make([]llm.Message, len(batch))
				g, gCtx := errgroup.WithContext(ctx)
				g.SetLimit(e.maxConcurrentToolWorkers)

				for bIdx, bTC := range batch {
					idx := bIdx
					call := bTC
					var u *pb.UsageMetadata
					if start == 0 && bIdx == 0 {
						u = turnUsage
					}
					g.Go(func() error {
						if gCtx.Err() != nil {
							return gCtx.Err()
						}
						msg, err := e.executeReadOnlyTool(gCtx, call, u)
						if err != nil {
							e.logger.Error("concurrent tool execution failed", "tool", call.Name, "error", err)
							results[idx] = toolResultMsg(call, fmt.Sprintf("Error: %v", err), true)
						} else {
							results[idx] = msg
						}
						return nil
					})
				}

				if err := g.Wait(); err != nil {
					return err
				}

				// Append all results to conversation history in deterministic order
				e.history = append(e.history, results...)
			}
		} else {
			// Mutating or sequential tool — execute with sequential barrier
			var u *pb.UsageMetadata
			if i == 0 {
				u = turnUsage
			}
			if err := e.executeTool(ctx, tc, u); err != nil {
				e.logger.Error("tool execution failed", "tool", tc.Name, "error", err)
				e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf("Error: %v", err), true))
			}
			i++
		}
	}
	return nil
}

// executeTool dispatches a single tool call and streams step updates.
func (e *Engine) executeTool(ctx context.Context, tc llm.ToolCall, usage *pb.UsageMetadata) error {
	stepIdx := e.nextStepIndex()

	// Create step with tool action (STATE_ACTIVE)
	step := e.buildToolStep(tc, stepIdx)
	step.State = pb.StepUpdate_STATE_ACTIVE
	step.Source = pb.StepUpdate_SOURCE_MODEL
	step.Target = pb.StepUpdate_TARGET_INTERNAL

	e.emitStep(step)

	// ── Pre-execution Tool Validation ──
	// 1. Unknown tool check: reject hallucinated/corrupted tool calls immediately
	// without prompting the user for permissions or timing out while detached.
	if !e.isKnownTool(tc.Name) {
		errMsg := fmt.Sprintf("Error: unknown tool: %s", tc.Name)
		e.logger.Warn("rejecting unknown tool call before permission check", "tool", tc.Name)
		e.history = append(e.history, toolResultMsg(tc, errMsg, true))
		step.State = pb.StepUpdate_STATE_ERROR
		step.ErrorInfo = &pb.ErrorInfo{
			Message: errMsg,
			Code:    "UNKNOWN_TOOL",
		}
		e.emitStep(step)
		return nil
	}

	// 2. Early argument validation: check mandatory fields before prompting for permissions.
	// Prompting the user to approve an empty command or empty path that is guaranteed to fail
	// causes confusion and leads to 10-minute timeouts when detached.
	switch tc.Name {
	case "run_command":
		if strings.TrimSpace(extractToolCommand(tc)) == "" {
			errMsg := "Error: [TOOL_VALIDATION] run_command CommandLine is required"
			e.history = append(e.history, toolResultMsg(tc, errMsg, true))
			step.State = pb.StepUpdate_STATE_ERROR
			step.ErrorInfo = &pb.ErrorInfo{
				Message: errMsg,
				Code:    "TOOL_VALIDATION",
			}
			e.emitStep(step)
			return nil
		}
	case "write_to_file", "replace_file_content", "view_file":
		if strings.TrimSpace(extractToolPath(tc)) == "" {
			errMsg := fmt.Sprintf("Error: [TOOL_VALIDATION] %s path is required", tc.Name)
			e.history = append(e.history, toolResultMsg(tc, errMsg, true))
			step.State = pb.StepUpdate_STATE_ERROR
			step.ErrorInfo = &pb.ErrorInfo{
				Message: errMsg,
				Code:    "TOOL_VALIDATION",
			}
			e.emitStep(step)
			return nil
		}
	}

	// ── Permission check (if handler registered) ──
	if e.toolRequiresPermission(tc) {
		// Circuit breaker: prevent infinite loops and runaway token bleed
		if e.consecutivePermissionDenials >= maxConsecutivePermissionDenials {
			e.logger.Warn("permission circuit breaker tripped: blocking tool call",
				"tool", tc.Name,
				"consecutive_denials", e.consecutivePermissionDenials,
			)
			step.State = pb.StepUpdate_STATE_ERROR
			step.ErrorInfo = &pb.ErrorInfo{
				Message: "tool execution blocked by permission circuit breaker (consecutive denials exceeded)",
				Code:    "PERMISSION_CIRCUIT_BREAKER",
			}
			e.emitStep(step)
			e.history = append(e.history, toolResultMsg(tc, "Error: Tool execution blocked by permission circuit breaker after repeated denials or timeouts. You MUST NOT attempt this tool call again. Please summarize your findings and conclude your response immediately.", true))
			return nil
		}

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
			e.consecutivePermissionDenials++
			denialMsg := fmt.Sprintf("Permission denied: %s", reason)
			if e.consecutivePermissionDenials >= 2 {
				denialMsg = fmt.Sprintf("Permission denied: %s. Multiple consecutive permission denials/timeouts have occurred (%d). Do not retry this command or similar actions. Conclude your response or proceed using available non-restricted tools.", reason, e.consecutivePermissionDenials)
			}
			// Feed denial back to LLM as a tool error so it can adapt
			e.history = append(e.history, toolResultMsg(tc, denialMsg, true))
			step.State = pb.StepUpdate_STATE_ERROR
			step.ErrorInfo = &pb.ErrorInfo{
				Message: fmt.Sprintf("Permission denied for tool '%s': %s", tc.Name, reason),
				Code:    "PERMISSION_DENIED",
			}
			e.emitStep(step)
			return nil // Not fatal — LLM can adapt
		}

		// Reset counter on successful approval
		e.consecutivePermissionDenials = 0

		// Dynamic workspace promotion & allowed path authorization
		targetPath := extractToolPath(tc)
		if targetPath == "" && tc.Name == "run_command" {
			if cwd, ok := tc.Args["cwd"].(string); ok && cwd != "" {
				targetPath = cwd
			}
		}

		if targetPath != "" && !e.isPathInsideWorkspaceOrAppData(targetPath) {
			var wsMgr *workspace.Manager
			if e.toolRegistry != nil {
				wsMgr = e.toolRegistry.WorkspaceManager()
			}
			if repoRoot, isRepo := workspace.FindProjectRoot(targetPath); isRepo {
				e.logger.Info("auto-promoting external repository to workspace",
					"repo", repoRoot,
					"target", targetPath,
					"tool", tc.Name,
				)
				e.AddWorkspace(repoRoot, WorkspaceInfo{
					Directory: repoRoot,
				})
				if wsMgr != nil {
					_ = wsMgr.AddWorkspace(repoRoot)
				}
				_ = config.AddTrustedWorkspace(repoRoot, e.logger)
				e.mu.Lock()
				e.permissionGrants = append(e.permissionGrants, PermissionGrant{
					Action: "*",
					Target: repoRoot,
				})
				e.mu.Unlock()
			} else {
				if wsMgr != nil {
					_ = wsMgr.AllowDynamicPath(targetPath)
				}
				e.mu.Lock()
				e.permissionGrants = append(e.permissionGrants, PermissionGrant{
					Action: tc.Name,
					Target: targetPath,
				})
				e.mu.Unlock()
			}
			ctx = workspace.WithApprovedPath(ctx, targetPath)
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
	case "desktop_subagent":
		return e.executeDesktopSubagent(ctx, tc, step)
	}

	// Check if this is a desktop atomic tool
	switch tc.Name {
	case "desktop_screenshot":
		return e.executeDesktopScreenshot(ctx, tc, step)
	case "desktop_list_windows":
		return e.executeDesktopListWindows(ctx, tc, step)
	case "desktop_focus_window":
		return e.executeDesktopFocusWindow(ctx, tc, step)
	case "desktop_click":
		return e.executeDesktopClick(ctx, tc, step)
	case "desktop_type":
		return e.executeDesktopType(ctx, tc, step)
	case "desktop_shortcut":
		return e.executeDesktopShortcut(ctx, tc, step)
	}

	// Check if this is a knowledge tool (handled by engine directly)
	switch tc.Name {
	case "knowledge_read":
		return e.executeKnowledgeRead(ctx, tc, step)
	case "knowledge_write":
		return e.executeKnowledgeWrite(ctx, tc, step)
	case "knowledge_replace":
		return e.executeKnowledgeReplace(ctx, tc, step)
	case "knowledge_delete":
		return e.executeKnowledgeDelete(ctx, tc, step)
	}

	// Check if this is a code-graph tool (handled by engine directly)
	switch tc.Name {
	case "codegraph_search":
		return e.executeCodeGraphSearch(ctx, tc, step)
	case "codegraph_find_references":
		return e.executeCodeGraphFindReferences(ctx, tc, step)
	case "codegraph_call_hierarchy":
		return e.executeCodeGraphCallHierarchy(ctx, tc, step)
	case "codegraph_get_impact":
		return e.executeCodeGraphGetImpact(ctx, tc, step)
	case "codegraph_diff_branches":
		return e.executeCodeGraphDiffBranches(ctx, tc, step)
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
	// not the proto action type.
	//
	// Panic recovery: catch runtime panics (e.g. invalid slice bounds) so a
	// single misbehaving tool doesn't crash the entire engine. The panic is
	// logged and fed back to the LLM as a tool error.
	if len(e.env) > 0 {
		ctx = tools.WithEnvironment(ctx, e.env)
	}

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
		step.Usage = usage
		e.emitStep(step)
		return err
	}

	// Trigger live incremental code graph indexing on file mutations
	if e.codeGraphManager != nil {
		switch tc.Name {
		case "write_to_file", "replace_file_content":
			if p := extractToolPath(tc); p != "" {
				ws := "."
				if len(e.workspaces) > 0 {
					ws = e.workspaces[0]
				}
				_ = e.codeGraphManager.UpdateFile(context.Background(), ws, p)
			}
		}
	}

	// Emit completed step (STATE_DONE) with usage attached
	step.State = pb.StepUpdate_STATE_DONE
	step.Usage = usage
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
	startTime := time.Now()
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

	if ta, ok := tc.Args["ToolAction"].(string); ok {
		req.ToolAction = ta
	} else if ta, ok := tc.Args["toolAction"].(string); ok {
		req.ToolAction = ta
	}
	if ts, ok := tc.Args["ToolSummary"].(string); ok {
		req.ToolSummary = ts
	} else if ts, ok := tc.Args["toolSummary"].(string); ok {
		req.ToolSummary = ts
	}

	// Parse questions from tool args (support PascalCase and snake_case)
	questionsRaw, ok := tc.Args["Questions"]
	if !ok {
		questionsRaw = tc.Args["questions"]
	}
	if questionsRaw != nil {
		if questionsList, ok := questionsRaw.([]interface{}); ok {
			for _, qRaw := range questionsList {
				if qMap, ok := qRaw.(map[string]interface{}); ok {
					q := &pb.UserQuestion{}
					if text, ok := qMap["Question"].(string); ok {
						q.Question = text
					} else if text, ok := qMap["question"].(string); ok {
						q.Question = text
					}

					optionsRaw, ok := qMap["Options"]
					if !ok {
						optionsRaw = qMap["options"]
					}
					if options, ok := optionsRaw.([]interface{}); ok {
						for _, o := range options {
							if s, ok := o.(string); ok {
								q.Options = append(q.Options, s)
							}
						}
					}

					if multi, ok := qMap["IsMultiSelect"].(bool); ok {
						q.IsMultiSelect = multi
					} else if multi, ok := qMap["is_multi_select"].(bool); ok {
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

	// Build formatted output and JSON result for LLM
	completedTime := time.Now()
	timeFormat := "2006-01-02T15:04:05-07:00"
	var sb strings.Builder
	fmt.Fprintf(&sb, "Created At: %s\nCompleted At: %s\n\n", startTime.Format(timeFormat), completedTime.Format(timeFormat))

	var resultJSON string
	if resp.Skipped {
		resultJSON = `{"skipped": true, "reason": "user skipped the question"}`
		sb.WriteString("User skipped the question.")
	} else {
		// Format answers nicely
		type answerResult struct {
			QuestionIndex   int      `json:"question_index"`
			Question        string   `json:"question"`
			SelectedOptions []string `json:"selected_options,omitempty"`
			Text            string   `json:"text,omitempty"`
		}

		var results []answerResult
		sb.WriteString("User answered:\n")
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

			qText := ""
			if i < len(req.Questions) {
				qText = req.Questions[i].Question
			}
			ansStr := answer.Text
			if len(answer.SelectedOptions) > 0 {
				if ansStr != "" {
					ansStr = strings.Join(answer.SelectedOptions, ", ") + " (" + ansStr + ")"
				} else {
					ansStr = strings.Join(answer.SelectedOptions, ", ")
				}
			}
			if ansStr == "" {
				ansStr = "(no answer provided)"
			}
			if qText != "" {
				fmt.Fprintf(&sb, "%d. Question: %s\n   Answer: %s\n", i+1, qText, ansStr)
			} else {
				fmt.Fprintf(&sb, "%d. Answer: %s\n", i+1, ansStr)
			}
		}
		b, _ := json.Marshal(map[string]interface{}{
			"skipped": false,
			"answers": results,
		})
		resultJSON = string(b)
	}

	formattedOutput := strings.TrimSpace(sb.String())
	req.FormattedOutput = formattedOutput

	// Update step with answers
	uq := step.GetUserQuestion()
	if uq != nil {
		uq.Answers = resp.Answers
		uq.Skipped = resp.Skipped
		uq.FormattedOutput = formattedOutput
	}
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	// Add to conversation history
	outputMsg := formattedOutput
	if outputMsg == "" {
		outputMsg = resultJSON
	}
	e.history = append(e.history, toolResultMsg(tc, outputMsg, false))

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
	case "view_file", "find_by_name":
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

// isKnownTool returns true if name corresponds to a registered or engine-intercepted tool.
func (e *Engine) isKnownTool(name string) bool {
	if e.toolRegistry != nil && e.toolRegistry.HasTool(name) {
		return true
	}
	if e.hostToolNames != nil && e.hostToolNames[name] {
		return true
	}
	if e.mcpMgr != nil && e.mcpMgr.IsMCPTool(name) {
		return true
	}
	switch name {
	case "invoke_subagent", "define_subagent", "manage_subagents", "send_message",
		"browser_subagent", "desktop_subagent",
		"desktop_screenshot", "desktop_list_windows", "desktop_focus_window",
		"desktop_click", "desktop_type", "desktop_shortcut",
		"knowledge_read", "knowledge_write", "knowledge_replace", "knowledge_delete",
		"codegraph_search", "codegraph_find_references", "codegraph_call_hierarchy",
		"codegraph_get_impact", "codegraph_diff_branches",
		"publish", "ask_question", "ask_permission", "list_permissions", "finish":
		return true
	}
	return false
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
	targetPath := extractToolPath(tc)
	repoSuffix := ""
	if targetPath != "" {
		if repoRoot, isRepo := workspace.FindProjectRoot(targetPath); isRepo {
			repoSuffix = fmt.Sprintf(" (external repository '%s')", filepath.Base(repoRoot))
		}
	}

	switch tc.Name {
	case "run_command":
		cmd, _ := tc.Args["CommandLine"].(string)
		if cmd == "" {
			cmd, _ = tc.Args["command"].(string)
		}
		if cmd != "" {
			cwd, _ := tc.Args["Cwd"].(string)
			if cwd == "" {
				cwd, _ = tc.Args["cwd"].(string)
			}
			if cwd != "" {
				if repoRoot, isRepo := workspace.FindProjectRoot(cwd); isRepo {
					return fmt.Sprintf("Run command: %s (in %s [external repository '%s'])", cmd, cwd, filepath.Base(repoRoot))
				}
				return fmt.Sprintf("Run command: %s (in %s)", cmd, cwd)
			}
			return fmt.Sprintf("Run command: %s", cmd)
		}
	case "write_to_file":
		if path := extractToolPath(tc); path != "" {
			return fmt.Sprintf("Create file: %s%s", path, repoSuffix)
		}
	case "replace_file_content":
		if path := extractToolPath(tc); path != "" {
			return fmt.Sprintf("Edit file: %s%s", path, repoSuffix)
		}
	case "view_file":
		path, _ := tc.Args["AbsolutePath"].(string)
		if path == "" {
			path, _ = tc.Args["path"].(string)
		}
		if path != "" {
			return fmt.Sprintf("View file: %s%s", path, repoSuffix)
		}
	case "search_web":
		q, _ := tc.Args["Query"].(string)
		if q == "" {
			q, _ = tc.Args["query"].(string)
		}
		if q != "" {
			return fmt.Sprintf("Web search: %s", q)
		}
	case "read_url_content":
		u, _ := tc.Args["Url"].(string)
		if u == "" {
			u, _ = tc.Args["url"].(string)
		}
		if u != "" {
			return fmt.Sprintf("Fetch URL: %s", u)
		}
	case "generate_image":
		name, _ := tc.Args["ImageName"].(string)
		if name == "" {
			name, _ = tc.Args["image_name"].(string)
		}
		prompt, _ := tc.Args["Prompt"].(string)
		if prompt == "" {
			prompt, _ = tc.Args["prompt"].(string)
		}
		if name != "" {
			return fmt.Sprintf("Generate image: %s (%s)", name, prompt)
		}
		if prompt != "" {
			return fmt.Sprintf("Generate image: %s", prompt)
		}
	case "ask_question":
		return "Ask user question(s)"
	}
	argsJSON, _ := json.Marshal(tc.Args)
	return fmt.Sprintf("Tool: %s, Args: %s", tc.Name, string(argsJSON))
}

func generateDiffPreview(tc llm.ToolCall) string {
	switch tc.Name {
	case "write_to_file":
		path := extractToolPath(tc)
		content, _ := tc.Args["CodeContent"].(string)
		if content == "" {
			content, _ = tc.Args["code_content"].(string)
		}
		if content == "" {
			content, _ = tc.Args["content"].(string)
		}
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
		path := extractToolPath(tc)
		if path == "" {
			return ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		oldContent := string(data)
		lines := strings.Split(strings.ReplaceAll(oldContent, "\r\n", "\n"), "\n")

		type simpleChunk struct {
			target      string
			replacement string
			startLine   int
			endLine     int
		}
		var chunks []simpleChunk

		if tcStr, ok := tc.Args["TargetContent"].(string); ok && tcStr != "" {
			rcStr, _ := tc.Args["ReplacementContent"].(string)
			sl := 1
			if s, ok := tc.Args["StartLine"]; ok {
				sl = int(toInt64(s))
			}
			el := len(lines)
			if e, ok := tc.Args["EndLine"]; ok {
				el = int(toInt64(e))
			}
			chunks = append(chunks, simpleChunk{target: tcStr, replacement: rcStr, startLine: sl, endLine: el})
		} else if tcStr, ok := tc.Args["target_content"].(string); ok && tcStr != "" {
			rcStr, _ := tc.Args["replacement_content"].(string)
			if rcStr == "" {
				rcStr, _ = tc.Args["replacement"].(string)
			}
			sl := 1
			if s, ok := tc.Args["start_line"]; ok {
				sl = int(toInt64(s))
			}
			el := len(lines)
			if e, ok := tc.Args["end_line"]; ok {
				el = int(toInt64(e))
			}
			chunks = append(chunks, simpleChunk{target: tcStr, replacement: rcStr, startLine: sl, endLine: el})
		} else if chunksRaw, ok := tc.Args["chunks"].([]interface{}); ok {
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
				chunks = append(chunks, simpleChunk{target: target, replacement: replacement, startLine: startLine, endLine: endLine})
			}
		}

		for _, c := range chunks {
			if c.startLine <= len(lines) && c.startLine <= c.endLine {
				scopeStart := c.startLine - 1
				if scopeStart < 0 {
					scopeStart = 0
				}
				scopeEnd := c.endLine
				if scopeEnd > len(lines) {
					scopeEnd = len(lines)
				}
				scopedText := strings.Join(lines[scopeStart:scopeEnd], "\n")
				newScopedText := strings.Replace(scopedText, c.target, c.replacement, 1)
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
//   - Tracks research tool calls (view_file)
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
	case "view_file":
		e.researchToolCount.Add(1)
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
	targetPath := extractToolPath(tc)
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

	// If workspaces are defined, only block writes targeting paths inside a workspace.
	// Non-workspace files (scratch files, temp scripts, system directories) are not
	// workspace code changes and should not be blocked by the planning guard.
	if len(e.workspaces) > 0 {
		inWorkspace := false
		for _, ws := range e.workspaces {
			rel, err := filepath.Rel(ws, targetPath)
			if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				inWorkspace = true
				break
			}
		}
		if !inWorkspace {
			return false, ""
		}
	}

	// Research heuristic: only block if the agent has done 2+ research calls.
	// If the agent goes straight to writing without research, it's a simple
	// fix that doesn't need a plan.
	if e.researchToolCount.Load() < 2 {
		return false, ""
	}

	return true, "Planning mode is active. You have done research but have not created a plan. " +
		"You must create an implementation_plan.md artifact in the brain directory before " +
		"making workspace code changes. Use write_to_file to write your plan first, then " +
		"wait for user approval."
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case float32:
		return int64(val)
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	case json.Number:
		n, _ := val.Int64()
		return n
	default:
		return 0
	}
}

// extractXMLFallbackArgs parses XML-style tags from raw model strings when structured arguments
// are corrupted or embedded in tag form (e.g. <arg_key>path</arg_key><arg_value>...</arg_value>
// or <path>...</path>).
func extractXMLFallbackArgs(s string, args map[string]interface{}) {
	// Pattern 1: <arg_key>k</arg_key><arg_value>v</arg_value>
	keyValRegex := regexp.MustCompile(`(?s)<arg_key>\s*([^<]+?)\s*</arg_key>\s*<arg_value>\s*(.*?)\s*</arg_value>`)
	for _, m := range keyValRegex.FindAllStringSubmatch(s, -1) {
		k := strings.TrimSpace(m[1])
		v := strings.TrimSpace(m[2])
		if _, exists := args[k]; !exists {
			args[k] = v
		}
	}
	// Pattern 2: <key>value</key> for standard tool parameters
	for _, param := range []string{"path", "TargetFile", "command", "CommandLine", "content", "CodeContent"} {
		tagRegex := regexp.MustCompile(fmt.Sprintf(`(?s)<%s>\s*(.*?)\s*</%s>`, param, param))
		if m := tagRegex.FindStringSubmatch(s); len(m) > 1 {
			if _, exists := args[param]; !exists {
				args[param] = strings.TrimSpace(m[1])
			}
		}
	}
}

func (e *Engine) buildToolStep(tc llm.ToolCall, stepIdx int32) *pb.StepUpdate {
	step := &pb.StepUpdate{
		ConversationId: e.convID,
		TrajectoryId:   e.trajectoryID,
		StepIndex:      stepIdx,
	}

	// If tc.Args contains "raw" (due to upstream JSON truncation/escaping issues),
	// attempt to repair and unpack fields from the raw string.
	if rawVal, hasRaw := tc.Args["raw"]; hasRaw {
		if rawStr, ok := rawVal.(string); ok {
			if repaired, ok := llm.TryRepairJSON(rawStr); ok {
				for k, v := range repaired {
					if _, exists := tc.Args[k]; !exists {
						tc.Args[k] = v
					}
				}
			}
			extractXMLFallbackArgs(rawStr, tc.Args)
		}
	}

	argsJSON, _ := json.Marshal(tc.Args)

	switch tc.Name {
	case "view_file":
		action := &pb.ActionViewFile{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Path == "" {
			if p, ok := tc.Args["AbsolutePath"].(string); ok {
				action.Path = p
			} else if p, ok := tc.Args["target_file"].(string); ok {
				action.Path = p
			}
		}
		if action.StartLine == 0 {
			if sl, ok := tc.Args["StartLine"]; ok {
				action.StartLine = int32(toInt64(sl))
			}
		}
		if action.EndLine == 0 {
			if el, ok := tc.Args["EndLine"]; ok {
				action.EndLine = int32(toInt64(el))
			}
		}
		if action.ContentOffset == 0 {
			if co, ok := tc.Args["ContentOffset"]; ok {
				action.ContentOffset = int64(toInt64(co))
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_ViewFile{ViewFile: action}

	case "write_to_file":
		action := &pb.ActionWriteToFile{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Path == "" {
			if p, ok := tc.Args["TargetFile"].(string); ok {
				action.Path = p
			} else if p, ok := tc.Args["target_file"].(string); ok {
				action.Path = p
			} else if p, ok := tc.Args["path"].(string); ok {
				action.Path = p
			}
		}
		if action.Content == "" {
			if c, ok := tc.Args["CodeContent"].(string); ok {
				action.Content = c
			} else if c, ok := tc.Args["code_content"].(string); ok {
				action.Content = c
			} else if c, ok := tc.Args["content"].(string); ok {
				action.Content = c
			}
		}
		if !action.Overwrite {
			if ow, ok := tc.Args["Overwrite"].(bool); ok {
				action.Overwrite = ow
			} else if ow, ok := tc.Args["overwrite"].(bool); ok {
				action.Overwrite = ow
			}
		}
		if !action.Append {
			if ap, ok := tc.Args["Append"].(bool); ok {
				action.Append = ap
			} else if ap, ok := tc.Args["append"].(bool); ok {
				action.Append = ap
			}
		}
		if action.Description == "" {
			if d, ok := tc.Args["Description"].(string); ok {
				action.Description = d
			} else if d, ok := tc.Args["description"].(string); ok {
				action.Description = d
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		if action.ArtifactMetadata == nil {
			rawMeta := tc.Args["ArtifactMetadata"]
			if rawMeta == nil {
				rawMeta = tc.Args["artifact_metadata"]
			}
			if metaMap, ok := rawMeta.(map[string]interface{}); ok {
				meta := &pb.ArtifactMetadata{}
				if s, ok := metaMap["Summary"].(string); ok {
					meta.Summary = s
				} else if s, ok := metaMap["summary"].(string); ok {
					meta.Summary = s
				}
				if rf, ok := metaMap["RequestFeedback"].(bool); ok {
					meta.RequestFeedback = rf
				} else if rf, ok := metaMap["request_feedback"].(bool); ok {
					meta.RequestFeedback = rf
				}
				if uf, ok := metaMap["UserFacing"].(bool); ok {
					meta.UserFacing = uf
				} else if uf, ok := metaMap["user_facing"].(bool); ok {
					meta.UserFacing = uf
				}
				if at, ok := metaMap["artifact_type"].(string); ok {
					meta.ArtifactType = at
				}
				action.ArtifactMetadata = meta
				action.IsArtifact = true
			}
		}
		step.Action = &pb.StepUpdate_WriteToFile{WriteToFile: action}

	case "replace_file_content":
		action := &pb.ActionReplaceFileContent{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Path == "" {
			if p, ok := tc.Args["TargetFile"].(string); ok {
				action.Path = p
			} else if p, ok := tc.Args["target_file"].(string); ok {
				action.Path = p
			} else if p, ok := tc.Args["path"].(string); ok {
				action.Path = p
			}
		}
		if action.Instruction == "" {
			if ins, ok := tc.Args["Instruction"].(string); ok {
				action.Instruction = ins
			} else if ins, ok := tc.Args["instruction"].(string); ok {
				action.Instruction = ins
			}
		}
		if action.Description == "" {
			if d, ok := tc.Args["Description"].(string); ok {
				action.Description = d
			} else if d, ok := tc.Args["description"].(string); ok {
				action.Description = d
			}
		}
		if !action.AllowMultiple {
			if am, ok := tc.Args["AllowMultiple"].(bool); ok {
				action.AllowMultiple = am
			} else if am, ok := tc.Args["allow_multiple"].(bool); ok {
				action.AllowMultiple = am
			}
		}
		if action.TargetContent == "" {
			if tcStr, ok := tc.Args["TargetContent"].(string); ok {
				action.TargetContent = tcStr
			} else if tcStr, ok := tc.Args["target_content"].(string); ok {
				action.TargetContent = tcStr
			}
		}
		if action.ReplacementContent == "" {
			if rcStr, ok := tc.Args["ReplacementContent"].(string); ok {
				action.ReplacementContent = rcStr
			} else if rcStr, ok := tc.Args["replacement_content"].(string); ok {
				action.ReplacementContent = rcStr
			} else if rcStr, ok := tc.Args["replacement"].(string); ok {
				action.ReplacementContent = rcStr
			}
		}
		if action.StartLine == 0 {
			if sl, ok := tc.Args["StartLine"]; ok {
				action.StartLine = int32(toInt64(sl))
			} else if sl, ok := tc.Args["start_line"]; ok {
				action.StartLine = int32(toInt64(sl))
			}
		}
		if action.EndLine == 0 {
			if el, ok := tc.Args["EndLine"]; ok {
				action.EndLine = int32(toInt64(el))
			} else if el, ok := tc.Args["end_line"]; ok {
				action.EndLine = int32(toInt64(el))
			}
		}
		if len(action.TargetLintErrorIds) == 0 {
			if rawIds, ok := tc.Args["TargetLintErrorIds"].([]interface{}); ok {
				for _, id := range rawIds {
					if s, ok := id.(string); ok {
						action.TargetLintErrorIds = append(action.TargetLintErrorIds, s)
					}
				}
			} else if rawIds, ok := tc.Args["target_lint_error_ids"].([]interface{}); ok {
				for _, id := range rawIds {
					if s, ok := id.(string); ok {
						action.TargetLintErrorIds = append(action.TargetLintErrorIds, s)
					}
				}
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		if len(action.Chunks) == 0 && action.TargetContent != "" {
			action.Chunks = []*pb.EditChunk{
				{
					StartLine:     action.StartLine,
					EndLine:       action.EndLine,
					TargetContent: action.TargetContent,
					Replacement:   action.ReplacementContent,
					AllowMultiple: action.AllowMultiple,
				},
			}
		}
		step.Action = &pb.StepUpdate_ReplaceFileContent{ReplaceFileContent: action}

	case "run_command":
		action := &pb.ActionRunCommand{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Command == "" {
			if cmd, ok := tc.Args["CommandLine"].(string); ok {
				action.Command = cmd
			} else if cmd, ok := tc.Args["command"].(string); ok {
				action.Command = cmd
			}
		}
		if action.Cwd == "" {
			if cwd, ok := tc.Args["Cwd"].(string); ok {
				action.Cwd = cwd
			} else if cwd, ok := tc.Args["cwd"].(string); ok {
				action.Cwd = cwd
			}
		}
		if !action.IsDaemon {
			if d, ok := tc.Args["IsDaemon"].(bool); ok {
				action.IsDaemon = d
			} else if d, ok := tc.Args["is_daemon"].(bool); ok {
				action.IsDaemon = d
			}
		}
		if action.TerminalId == "" {
			if rtid, ok := tc.Args["RequestedTerminalID"].(string); ok {
				action.TerminalId = rtid
			} else if rtid, ok := tc.Args["requested_terminal_id"].(string); ok {
				action.TerminalId = rtid
			} else if rtid, ok := tc.Args["terminal_id"].(string); ok {
				action.TerminalId = rtid
			}
		}
		if !action.Persistent {
			if p, ok := tc.Args["RunPersistent"].(bool); ok {
				action.Persistent = p
			} else if p, ok := tc.Args["run_persistent"].(bool); ok {
				action.Persistent = p
			} else if p, ok := tc.Args["persistent"].(bool); ok {
				action.Persistent = p
			}
		}
		if action.WaitMsBeforeAsync == 0 {
			if w, ok := tc.Args["WaitMsBeforeAsync"]; ok {
				action.WaitMsBeforeAsync = int32(toInt64(w))
			} else if w, ok := tc.Args["wait_ms_before_async"]; ok {
				action.WaitMsBeforeAsync = int32(toInt64(w))
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_RunCommand{RunCommand: action}

	case "manage_task":
		action := &pb.ActionManageTask{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Action == "" {
			if a, ok := tc.Args["Action"].(string); ok {
				action.Action = a
			} else if a, ok := tc.Args["action"].(string); ok {
				action.Action = a
			}
		}
		if action.TaskId == "" {
			if tid, ok := tc.Args["TaskId"].(string); ok {
				action.TaskId = tid
			} else if tid, ok := tc.Args["task_id"].(string); ok {
				action.TaskId = tid
			}
		}
		if action.Input == "" {
			if in, ok := tc.Args["Input"].(string); ok {
				action.Input = in
			} else if in, ok := tc.Args["input"].(string); ok {
				action.Input = in
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_ManageTask{ManageTask: action}

	case "finish":
		action := &pb.ActionFinish{}
		_ = json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_Finish{Finish: action}

	case "invoke_subagent":
		action := &pb.ActionInvokeSubagent{}
		_ = json.Unmarshal(argsJSON, action)
		if len(action.Subagents) == 0 {
			rawSubs := tc.Args["Subagents"]
			if rawSubs == nil {
				rawSubs = tc.Args["subagents"]
			}
			if subsList, ok := rawSubs.([]interface{}); ok {
				for _, subItem := range subsList {
					if subMap, ok := subItem.(map[string]interface{}); ok {
						inv := &pb.SubagentInvocation{}
						if tn, ok := subMap["TypeName"].(string); ok {
							inv.TypeName = tn
						} else if tn, ok := subMap["type_name"].(string); ok {
							inv.TypeName = tn
						}
						if r, ok := subMap["Role"].(string); ok {
							inv.Role = r
						} else if r, ok := subMap["role"].(string); ok {
							inv.Role = r
						}
						if p, ok := subMap["Prompt"].(string); ok {
							inv.Prompt = p
						} else if p, ok := subMap["prompt"].(string); ok {
							inv.Prompt = p
						}
						if ws, ok := subMap["Workspace"].(string); ok {
							inv.Workspace = ws
						} else if ws, ok := subMap["workspace"].(string); ok {
							inv.Workspace = ws
						}
						if m, ok := subMap["Model"].(string); ok {
							inv.Model = m
						} else if m, ok := subMap["model"].(string); ok {
							inv.Model = m
						}
						action.Subagents = append(action.Subagents, inv)
					}
				}
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_InvokeSubagent{InvokeSubagent: action}

	case "define_subagent":
		action := &pb.ActionDefineSubagent{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Name == "" {
			if n, ok := tc.Args["Name"].(string); ok {
				action.Name = n
			} else if n, ok := tc.Args["name"].(string); ok {
				action.Name = n
			}
		}
		if action.Description == "" {
			if d, ok := tc.Args["Description"].(string); ok {
				action.Description = d
			} else if d, ok := tc.Args["description"].(string); ok {
				action.Description = d
			}
		}
		if action.SystemPrompt == "" {
			if sp, ok := tc.Args["SystemPrompt"].(string); ok {
				action.SystemPrompt = sp
			} else if sp, ok := tc.Args["system_prompt"].(string); ok {
				action.SystemPrompt = sp
			}
		}
		if !action.EnableWriteTools {
			if ew, ok := tc.Args["EnableWriteTools"].(bool); ok {
				action.EnableWriteTools = ew
			} else if ew, ok := tc.Args["enable_write_tools"].(bool); ok {
				action.EnableWriteTools = ew
			}
		}
		if !action.EnableMcpTools {
			if em, ok := tc.Args["EnableMcpTools"].(bool); ok {
				action.EnableMcpTools = em
			} else if em, ok := tc.Args["enable_mcp_tools"].(bool); ok {
				action.EnableMcpTools = em
			}
		}
		if !action.EnableSubagentTools {
			if es, ok := tc.Args["EnableSubagentTools"].(bool); ok {
				action.EnableSubagentTools = es
			} else if es, ok := tc.Args["enable_subagent_tools"].(bool); ok {
				action.EnableSubagentTools = es
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_DefineSubagent{DefineSubagent: action}

	case "manage_subagents":
		action := &pb.ActionManageSubagents{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Action == "" {
			if a, ok := tc.Args["Action"].(string); ok {
				action.Action = a
			} else if a, ok := tc.Args["action"].(string); ok {
				action.Action = a
			}
		}
		if len(action.ConversationIds) == 0 {
			if cids, ok := tc.Args["ConversationIds"].([]interface{}); ok {
				for _, cid := range cids {
					if s, ok := cid.(string); ok {
						action.ConversationIds = append(action.ConversationIds, s)
					}
				}
			} else if cids, ok := tc.Args["conversation_ids"].([]interface{}); ok {
				for _, cid := range cids {
					if s, ok := cid.(string); ok {
						action.ConversationIds = append(action.ConversationIds, s)
					}
				}
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_ManageSubagents{ManageSubagents: action}

	case "send_message":
		action := &pb.ActionSendMessage{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Recipient == "" {
			if r, ok := tc.Args["Recipient"].(string); ok {
				action.Recipient = r
			} else if r, ok := tc.Args["recipient"].(string); ok {
				action.Recipient = r
			}
		}
		if action.Message == "" {
			if m, ok := tc.Args["Message"].(string); ok {
				action.Message = m
			} else if m, ok := tc.Args["message"].(string); ok {
				action.Message = m
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_SendMessageAction{SendMessageAction: action}

	case "browser_subagent":
		action := &pb.ActionBrowserSubagent{}
		_ = json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_BrowserSubagent{BrowserSubagent: action}

	case "desktop_subagent":
		action := &pb.ActionDesktopSubagent{}
		_ = json.Unmarshal(argsJSON, action)
		step.Action = &pb.StepUpdate_DesktopSubagent{DesktopSubagent: action}

	case "search_web":
		action := &pb.ActionSearchWeb{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Query == "" {
			if q, ok := tc.Args["Query"].(string); ok {
				action.Query = q
			} else if q, ok := tc.Args["query"].(string); ok {
				action.Query = q
			}
		}
		if action.Domain == "" {
			if d, ok := tc.Args["Domain"].(string); ok {
				action.Domain = d
			} else if d, ok := tc.Args["domain"].(string); ok {
				action.Domain = d
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_SearchWeb{SearchWeb: action}

	case "read_url_content":
		action := &pb.ActionReadUrlContent{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Url == "" {
			if u, ok := tc.Args["Url"].(string); ok {
				action.Url = u
			} else if u, ok := tc.Args["url"].(string); ok {
				action.Url = u
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_ReadUrlContent{ReadUrlContent: action}

	case "generate_image":
		action := &pb.ActionGenerateImage{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Prompt == "" {
			if p, ok := tc.Args["Prompt"].(string); ok {
				action.Prompt = p
			} else if p, ok := tc.Args["prompt"].(string); ok {
				action.Prompt = p
			}
		}
		if action.ImageName == "" {
			if in, ok := tc.Args["ImageName"].(string); ok {
				action.ImageName = in
			} else if in, ok := tc.Args["image_name"].(string); ok {
				action.ImageName = in
			}
		}
		if action.AspectRatio == "" {
			if ar, ok := tc.Args["AspectRatio"].(string); ok {
				action.AspectRatio = ar
			} else if ar, ok := tc.Args["aspect_ratio"].(string); ok {
				action.AspectRatio = ar
			}
		}
		if len(action.ImagePaths) == 0 {
			if ips, ok := tc.Args["ImagePaths"].([]interface{}); ok {
				for _, ip := range ips {
					if s, ok := ip.(string); ok {
						action.ImagePaths = append(action.ImagePaths, s)
					}
				}
			} else if ips, ok := tc.Args["image_paths"].([]interface{}); ok {
				for _, ip := range ips {
					if s, ok := ip.(string); ok {
						action.ImagePaths = append(action.ImagePaths, s)
					}
				}
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_GenerateImage{GenerateImage: action}

	case "ask_question":
		action := &pb.ActionUserQuestion{}
		_ = json.Unmarshal(argsJSON, action)
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
		step.Action = &pb.StepUpdate_UserQuestion{UserQuestion: action}

	case "schedule":
		action := &pb.ActionSchedule{}
		_ = json.Unmarshal(argsJSON, action)
		if action.Prompt == "" {
			if p, ok := tc.Args["Prompt"].(string); ok {
				action.Prompt = p
			} else if p, ok := tc.Args["prompt"].(string); ok {
				action.Prompt = p
			}
		}
		if action.DurationSeconds == 0 {
			if ds, ok := tc.Args["DurationSeconds"]; ok {
				action.DurationSeconds = int32(toInt64(ds))
			} else if ds, ok := tc.Args["duration_seconds"]; ok {
				action.DurationSeconds = int32(toInt64(ds))
			}
		}
		if action.CronExpression == "" {
			if ce, ok := tc.Args["CronExpression"].(string); ok {
				action.CronExpression = ce
			} else if ce, ok := tc.Args["cron_expression"].(string); ok {
				action.CronExpression = ce
			}
		}
		if action.MaxIterations == 0 {
			if mi, ok := tc.Args["MaxIterations"]; ok {
				action.MaxIterations = int32(toInt64(mi))
			} else if mi, ok := tc.Args["max_iterations"]; ok {
				action.MaxIterations = int32(toInt64(mi))
			}
		}
		if action.TimerCondition == "" {
			if tcVal, ok := tc.Args["TimerCondition"].(string); ok {
				action.TimerCondition = tcVal
			} else if tcVal, ok := tc.Args["timer_condition"].(string); ok {
				action.TimerCondition = tcVal
			}
		}
		if !action.IsDaemon {
			if id, ok := tc.Args["IsDaemon"].(bool); ok {
				action.IsDaemon = id
			} else if id, ok := tc.Args["is_daemon"].(bool); ok {
				action.IsDaemon = id
			}
		}
		if action.ToolAction == "" {
			if ta, ok := tc.Args["ToolAction"].(string); ok {
				action.ToolAction = ta
			} else if ta, ok := tc.Args["toolAction"].(string); ok {
				action.ToolAction = ta
			}
		}
		if action.ToolSummary == "" {
			if ts, ok := tc.Args["ToolSummary"].(string); ok {
				action.ToolSummary = ts
			} else if ts, ok := tc.Args["toolSummary"].(string); ok {
				action.ToolSummary = ts
			}
		}
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
		return a.ViewFile.Content
	case *pb.StepUpdate_WriteToFile:
		wf := a.WriteToFile
		if wf.FormattedOutput != "" {
			return wf.FormattedOutput
		}
		result = map[string]interface{}{"created": wf.Created}
	case *pb.StepUpdate_ReplaceFileContent:
		rfc := a.ReplaceFileContent
		if rfc.FormattedOutput != "" {
			return rfc.FormattedOutput
		}
		result = map[string]interface{}{
			"success": rfc.Success, "diff": rfc.DiffBlock,
		}
	case *pb.StepUpdate_RunCommand:
		rc := a.RunCommand
		if rc.FormattedOutput != "" {
			return rc.FormattedOutput
		}
		m := map[string]interface{}{
			"stdout": rc.Stdout, "stderr": rc.Stderr,
			"exit_code": rc.ExitCode, "timed_out": rc.TimedOut,
			"task_id": rc.TaskId, "assigned_terminal_id": rc.AssignedTerminalId,
		}
		if rc.LogPath != "" {
			m["log_path"] = rc.LogPath
		}
		if rc.LogUri != "" {
			m["log_uri"] = rc.LogUri
		}
		result = m
	case *pb.StepUpdate_ManageTask:
		mt := a.ManageTask
		if mt.FormattedOutput != "" {
			return mt.FormattedOutput
		}
		var tasks []map[string]interface{}
		for _, t := range mt.Tasks {
			tm := map[string]interface{}{
				"task_id": t.TaskId, "command": t.Command,
				"cwd": t.Cwd, "status": t.Status,
				"exit_code": t.ExitCode, "started_at": t.StartedAt,
				"completed_at": t.CompletedAt, "recent_output": t.RecentOutput,
				"terminal_id": t.TerminalId,
			}
			if t.LogPath != "" {
				tm["log_path"] = t.LogPath
			}
			if t.LogUri != "" {
				tm["log_uri"] = t.LogUri
			}
			tasks = append(tasks, tm)
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
		if sub.FormattedOutput != "" {
			return sub.FormattedOutput
		}
		resMap := map[string]interface{}{
			"error": sub.ErrorMessage,
		}
		if len(sub.LaunchResults) > 0 {
			resMap["subagents"] = sub.LaunchResults
		} else {
			//nolint:staticcheck // support legacy result fields
			resMap["result"] = sub.ResultText
			//nolint:staticcheck // support legacy result fields
			resMap["steps_executed"] = sub.StepsExecuted
			//nolint:staticcheck // support legacy result fields
			resMap["child_trajectory_id"] = sub.ChildTrajectoryId
		}
		result = resMap
	case *pb.StepUpdate_Schedule:
		sc := a.Schedule
		if sc.FormattedOutput != "" {
			return sc.FormattedOutput
		}
		result = map[string]interface{}{
			"task_id": sc.TaskId,
			"prompt":  sc.Prompt,
			"status":  "scheduled",
		}
	case *pb.StepUpdate_DefineSubagent:
		da := a.DefineSubagent
		if da.FormattedOutput != "" {
			return da.FormattedOutput
		}
		result = map[string]interface{}{
			"name":   da.Name,
			"status": "defined",
		}
	case *pb.StepUpdate_ManageSubagents:
		ms := a.ManageSubagents
		if ms.FormattedOutput != "" {
			return ms.FormattedOutput
		}
		result = map[string]interface{}{
			"action": ms.Action,
			"status": "done",
		}
	case *pb.StepUpdate_SendMessageAction:
		sm := a.SendMessageAction
		if sm.FormattedOutput != "" {
			return sm.FormattedOutput
		}
		result = map[string]interface{}{
			"recipient": sm.Recipient,
			"status":    "sent",
		}
	case *pb.StepUpdate_SearchWeb:
		ws := a.SearchWeb
		if ws.FormattedOutput != "" {
			return ws.FormattedOutput
		}
		var results []map[string]interface{}
		for _, res := range ws.Results {
			results = append(results, map[string]interface{}{
				"title": res.Title, "url": res.Url, "snippet": res.Snippet,
			})
		}
		result = map[string]interface{}{"results": results}
	case *pb.StepUpdate_ReadUrlContent:
		wf := a.ReadUrlContent
		if wf.FormattedOutput != "" {
			return wf.FormattedOutput
		}
		result = map[string]interface{}{
			"content": wf.Content, "content_type": wf.ContentType,
		}
	case *pb.StepUpdate_GenerateImage:
		gi := a.GenerateImage
		if gi.FormattedOutput != "" {
			return gi.FormattedOutput
		}
		result = map[string]interface{}{
			"artifact_path": gi.ArtifactPath,
			"mime_type":     gi.MimeType,
			"byte_size":     gi.ByteSize,
		}
	case *pb.StepUpdate_UserQuestion:
		uq := a.UserQuestion
		if uq.FormattedOutput != "" {
			return uq.FormattedOutput
		}
	case *pb.StepUpdate_McpTool:
		mt := a.McpTool
		result = map[string]interface{}{
			"server": mt.ServerName, "tool": mt.ToolName,
			"result": mt.ResultJson, "is_error": mt.IsError,
		}
	case *pb.StepUpdate_DesktopSubagent:
		da := a.DesktopSubagent
		result = map[string]interface{}{
			"conversation_id": da.ConversationId,
			"target_app":      da.TargetApplication,
			"error":           da.ErrorMessage,
		}
	default:
		if step.Text != "" {
			return step.Text
		}
		result = map[string]interface{}{"status": "done"}
	}

	b, _ := json.Marshal(result)
	return string(b)
}

// emitStep sends a step update to the client. Serialized with stepMu
// to protect against race conditions when multiple read-only tools run concurrently.
func (e *Engine) emitStep(step *pb.StepUpdate) {
	e.stepMu.Lock()
	defer e.stepMu.Unlock()
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
//
//nolint:unused
func (e *Engine) emitStructuredErrorStep(err error) {
	var hErr *errors.HarnessError
	if errors.As(err, &hErr) {
		// Convert HarnessError to ErrorInfo with structured context
		errorInfo := &pb.ErrorInfo{
			Message: hErr.Message,
			Code:    string(hErr.Code),
		}

		// Add structured context as additional metadata
		if len(hErr.Context) > 0 {
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

// NextStepIndex returns the next sequential step index.
func (e *Engine) NextStepIndex() int32 {
	return e.nextStepIndex()
}

func (e *Engine) nextStepIndex() int32 {
	return e.stepIndex.Add(1) - 1
}

// streamGenerate reads from a streaming LLM provider, debouncing and coalescing
// text and thinking deltas into batched STATE_STREAMING StepUpdates over a flush window,
// and returns the assembled GenerateResponse along with the allocated stream step index (-1 if none).
func (e *Engine) streamGenerate(ctx context.Context, sp llm.StreamingProvider, req *llm.GenerateRequest) (*llm.GenerateResponse, int32, error) {
	chunksCh, errCh := sp.GenerateStream(ctx, req)

	// Lazily allocate a step index on the first delta so non-text responses (or errors)
	// don't burn an unused step index and leave phantom gaps in trajectories.
	var streamStepIdx int32 = -1
	getStreamStepIdx := func() int32 {
		if streamStepIdx == -1 {
			streamStepIdx = e.nextStepIndex()
		}
		return streamStepIdx
	}

	var contentBuf, thinkingBuf strings.Builder
	var pendingText, pendingThinking strings.Builder
	var allToolCalls []llm.ToolCall
	var finalUsage llm.Usage
	var finishReason string
	var hasEmittedFirstThinking, hasEmittedFirstText bool

	flushThinking := func() {
		if pendingThinking.Len() == 0 {
			return
		}
		delta := pendingThinking.String()
		pendingThinking.Reset()
		e.emitStep(&pb.StepUpdate{
			ConversationId: e.convID,
			TrajectoryId:   e.trajectoryID,
			StepIndex:      getStreamStepIdx(),
			ThinkingDelta:  delta,
			Source:         pb.StepUpdate_SOURCE_MODEL,
			State:          pb.StepUpdate_STATE_STREAMING,
			Target:         pb.StepUpdate_TARGET_USER,
		})
	}

	flushText := func() {
		if pendingText.Len() == 0 {
			return
		}
		delta := pendingText.String()
		pendingText.Reset()
		e.emitStep(&pb.StepUpdate{
			ConversationId: e.convID,
			TrajectoryId:   e.trajectoryID,
			StepIndex:      getStreamStepIdx(),
			TextDelta:      delta,
			Source:         pb.StepUpdate_SOURCE_MODEL,
			State:          pb.StepUpdate_STATE_STREAMING,
			Target:         pb.StepUpdate_TARGET_USER,
		})
	}

	flushAll := func() {
		flushThinking()
		flushText()
	}
	defer flushAll()

	handleChunk := func(chunk llm.StreamChunk) {
		// Emit thinking delta
		if chunk.ThinkingDelta != "" {
			thinkingBuf.WriteString(chunk.ThinkingDelta)
			if e.streamFlushInterval <= 0 {
				e.emitStep(&pb.StepUpdate{
					ConversationId: e.convID,
					TrajectoryId:   e.trajectoryID,
					StepIndex:      getStreamStepIdx(),
					ThinkingDelta:  chunk.ThinkingDelta,
					Source:         pb.StepUpdate_SOURCE_MODEL,
					State:          pb.StepUpdate_STATE_STREAMING,
					Target:         pb.StepUpdate_TARGET_USER,
				})
			} else if !hasEmittedFirstThinking {
				// Fast-path: emit initial thinking token immediately for 0ms TTFT
				hasEmittedFirstThinking = true
				e.emitStep(&pb.StepUpdate{
					ConversationId: e.convID,
					TrajectoryId:   e.trajectoryID,
					StepIndex:      getStreamStepIdx(),
					ThinkingDelta:  chunk.ThinkingDelta,
					Source:         pb.StepUpdate_SOURCE_MODEL,
					State:          pb.StepUpdate_STATE_STREAMING,
					Target:         pb.StepUpdate_TARGET_USER,
				})
			} else {
				pendingThinking.WriteString(chunk.ThinkingDelta)
				if pendingThinking.Len() >= maxPendingStreamBytes {
					flushThinking()
				}
			}
		}

		// Emit text delta
		if chunk.TextDelta != "" {
			if pendingThinking.Len() > 0 {
				flushThinking()
			}
			contentBuf.WriteString(chunk.TextDelta)
			if e.streamFlushInterval <= 0 {
				e.emitStep(&pb.StepUpdate{
					ConversationId: e.convID,
					TrajectoryId:   e.trajectoryID,
					StepIndex:      getStreamStepIdx(),
					TextDelta:      chunk.TextDelta,
					Source:         pb.StepUpdate_SOURCE_MODEL,
					State:          pb.StepUpdate_STATE_STREAMING,
					Target:         pb.StepUpdate_TARGET_USER,
				})
			} else if !hasEmittedFirstText {
				// Fast-path: emit initial text token immediately for 0ms TTFT
				hasEmittedFirstText = true
				e.emitStep(&pb.StepUpdate{
					ConversationId: e.convID,
					TrajectoryId:   e.trajectoryID,
					StepIndex:      getStreamStepIdx(),
					TextDelta:      chunk.TextDelta,
					Source:         pb.StepUpdate_SOURCE_MODEL,
					State:          pb.StepUpdate_STATE_STREAMING,
					Target:         pb.StepUpdate_TARGET_USER,
				})
			} else {
				pendingText.WriteString(chunk.TextDelta)
				if pendingText.Len() >= maxPendingStreamBytes {
					flushText()
				}
			}
		}

		// Collect tool calls (typically only in the final chunk)
		if len(chunk.ToolCalls) > 0 {
			allToolCalls = append(allToolCalls, chunk.ToolCalls...)
			flushAll()
		}

		// Capture final metadata
		if chunk.Done {
			finalUsage = chunk.Usage
			finishReason = chunk.FinishReason
			flushAll()
		}
	}

	if e.streamFlushInterval <= 0 {
		for chunk := range chunksCh {
			handleChunk(chunk)
		}
	} else {
		ticker := time.NewTicker(e.streamFlushInterval)
		defer ticker.Stop()

		streamOpen := true
		for streamOpen {
			select {
			case <-ctx.Done():
				return nil, -1, ctx.Err()
			case <-ticker.C:
				flushAll()
			case err, ok := <-errCh:
				if !ok {
					errCh = nil
					continue
				}
				if err != nil {
					return nil, -1, err
				}
			case chunk, ok := <-chunksCh:
				if !ok {
					streamOpen = false
					break
				}
				handleChunk(chunk)
			}
		}
	}

	flushAll()

	// Check for stream error
	if errCh != nil {
		select {
		case err := <-errCh:
			if err != nil {
				return nil, -1, err
			}
		default:
		}
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
	}, streamStepIdx, nil
}

// agyToolOrder defines the canonical Antigravity tool declaration priority.
// Core primitives appear first (view_file #1, run_command #2) to optimize transformer
// attention bias and guide models toward reading and executing code first.
var agyToolOrder = map[string]int{
	"view_file":            1,
	"run_command":          2,
	"manage_task":          3,
	"send_message":         4,
	"schedule":             5,
	"invoke_subagent":      6,
	"define_subagent":      7,
	"manage_subagents":     8,
	"write_to_file":        9,
	"replace_file_content": 10,
	"generate_image":       11,
	"read_url_content":     12,
	"search_web":           13,
	"ask_question":         14,
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

	// Append browser subagent if enabled, depth allows, and browser capability is available
	if e.subagentsEnabled && e.depth < e.maxDepth && e.hasBrowserCapability() {
		decls = append(decls, browserSubagentDeclaration())
	}

	// Append desktop subagent if enabled, depth allows, and desktop capability is available
	if e.subagentsEnabled && e.depth < e.maxDepth && e.hasDesktopCapability() {
		decls = append(decls, desktopSubagentDeclaration())
	}

	// Append direct desktop tools if desktop capability is configured
	if e.hasDesktopConfig {
		decls = append(decls, desktopToolDeclarations()...)
	}

	// Sort all declarations according to AGY canonical priority order.
	// Tools in agyToolOrder come first in exact canonical order (view_file #1, run_command #2, etc.),
	// followed by any extension tools (MCP, host tools) sorted alphabetically for deterministic caching.
	sort.Slice(decls, func(i, j int) bool {
		orderI := agyToolOrder[decls[i].Name]
		orderJ := agyToolOrder[decls[j].Name]
		if orderI > 0 && orderJ > 0 {
			return orderI < orderJ
		}
		if orderI > 0 {
			return true
		}
		if orderJ > 0 {
			return false
		}
		return decls[i].Name < decls[j].Name
	})

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
		"knowledge_read", "knowledge_write", "knowledge_replace", "knowledge_delete",
		"publish", "finish", "browser_subagent", "desktop_subagent",
		"desktop_screenshot", "desktop_list_windows", "desktop_focus_window",
		"desktop_click", "desktop_type", "desktop_shortcut",
	} {
		names[name] = true
	}

	return names
}

func (e *Engine) hasBrowserCapability() bool {
	if e.hasBrowserConfig {
		return true
	}
	if e.mcpMgr != nil {
		for _, d := range e.mcpMgr.ToolDeclarations() {
			lower := strings.ToLower(d.Name)
			if strings.HasPrefix(lower, "browser_") || strings.HasPrefix(lower, "playwright_") || strings.Contains(lower, "browser") {
				return true
			}
		}
	}
	for name := range e.hostToolNames {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "browser_") || strings.HasPrefix(lower, "playwright_") || strings.Contains(lower, "browser") {
			return true
		}
	}
	return false
}

func (e *Engine) hasDesktopCapability() bool {
	if e.hasDesktopConfig {
		return true
	}
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return true
	default:
		return false
	}
}

func (e *Engine) notifyParent(msg tools.SystemMessage) {
	if e.notifySendCh != nil {
		select {
		case e.notifySendCh <- msg:
		default:
		}
	}
	if e.subagentTracker != nil {
		e.subagentTracker.NotifyParent(msg)
	}
}

// AddPendingMessages adds synthetic messages/notifications to be processed on the next turn.
func (e *Engine) AddPendingMessages(msgs ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pendingSyntheticMsgs = append(e.pendingSyntheticMsgs, msgs...)
}

// Close releases resources held by the engine, including open SQLite databases
// and active subagents.
func (e *Engine) Close() error {
	if e.subagentTracker != nil {
		e.subagentTracker.KillAll()
	}
	if e.codeGraphManager != nil {
		return e.codeGraphManager.Close()
	}
	return nil
}
