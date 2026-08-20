package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/engine"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/llm"
	mcpbridge "github.com/divmora/localharness/internal/mcp"
	"github.com/divmora/localharness/internal/discovery"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/util"
	"github.com/divmora/localharness/internal/workspace"
)

// Session manages a single WebSocket connection from an SDK client.
type Session struct {
	conn               *websocket.Conn
	logger             *slog.Logger
	serverCfg          *config.ServerConfig
	mu                 sync.Mutex // Protects writes to conn
	engine             *engine.Engine
	conv               *conversation.Conversation
	cancel             context.CancelFunc
	toolRegistry       *tools.Registry
	mcpMgr             *mcpbridge.Manager                     // MCP server bridge
	pendingToolResults    map[string]chan *pb.ToolResult      // stepID → result channel
	pendingMu             sync.Mutex                         // Protects pendingToolResults
	pendingPermissions    map[string]chan *pb.PermissionResponse // requestID → response channel
	pendingPermissionsMu  sync.Mutex                         // Protects pendingPermissions
	pendingQuestions      map[string]chan *pb.QuestionResponse // requestID → response channel
	pendingQuestionsMu    sync.Mutex                          // Protects pendingQuestions
	notifyCh              <-chan tools.SystemMessage           // System notifications for auto-wake
	maxAutoWakeTurns      int                                 // Max synthetic turns before needing real user message (0 = disabled)
	autoWakeCount         int                                 // Current count of consecutive auto-wake turns
	turnWg                sync.WaitGroup                      // Tracks in-flight handleUserMessage goroutines
	earlyUserMessages     []*pb.UserMessage                   // User messages received before engine initialization
	earlyUserMessagesMu   sync.Mutex                          // Protects earlyUserMessages
	wsMgr                 *workspace.Manager
	yoloMode              bool
	isRestricted          bool
	isDaemon              bool
	detached              bool
	ringBuffer            *EventRingBuffer
	approvalQueue         *ApprovalQueue
}

// NewSession creates a new session for a WebSocket connection.
func NewSession(conn *websocket.Conn, serverCfg *config.ServerConfig, logger *slog.Logger) *Session {
	return &Session{
		conn:               conn,
		logger:             logger,
		serverCfg:          serverCfg,
		pendingToolResults: make(map[string]chan *pb.ToolResult),
		pendingPermissions: make(map[string]chan *pb.PermissionResponse),
		pendingQuestions:   make(map[string]chan *pb.QuestionResponse),
		earlyUserMessages:  make([]*pb.UserMessage, 0),
		ringBuffer:         NewEventRingBuffer(200),
		approvalQueue:      NewApprovalQueue(),
	}
}

// Status returns the current high-level state of the session for the /status endpoint.
func (s *Session) Status() string {
	if s.engine == nil {
		return "INITIALIZING"
	}

	// Check if waiting for SDK responses (Blocked)
	s.pendingMu.Lock()
	pendingTools := len(s.pendingToolResults)
	s.pendingMu.Unlock()

	s.pendingPermissionsMu.Lock()
	pendingPerms := len(s.pendingPermissions)
	s.pendingPermissionsMu.Unlock()

	s.pendingQuestionsMu.Lock()
	pendingQs := len(s.pendingQuestions)
	s.pendingQuestionsMu.Unlock()

	if pendingTools > 0 || pendingPerms > 0 || pendingQs > 0 {
		return "BLOCKED"
	}

	// If engine is idle
	if s.engine.IsIdle() {
		return "IDLE"
	}

	return "RUNNING"
}

const (
	// wsPingInterval is how often the server sends WebSocket ping frames.
	// During long LLM calls (e.g., Workers AI free tier), the WebSocket
	// connection can be killed by the OS/middleware if there's no traffic.
	// Pings keep the connection alive.
	wsPingInterval = 30 * time.Second

	// wsPongTimeout is how long the server waits for a pong response
	// before considering the client disconnected. Must be > wsPingInterval.
	wsPongTimeout = 45 * time.Second
)

// Run is the main session loop — reads messages from the client and processes them.
// It uses a select loop to handle both WebSocket messages and system notifications
// (timer fires, task completions). When the engine is idle and a notification arrives,
// an auto-wake synthetic turn is started.
func (s *Session) Run() {
	defer s.conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	defer cancel()

	// Configure WebSocket keepalive: server pings, client pongs.
	// Set initial read deadline; the pong handler resets it on each pong.
	s.conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
		return nil
	})

	// Start ping sender goroutine
	go s.pingLoop(ctx)

	// Read WebSocket messages in a goroutine → channel
	clientMsgs := make(chan *pb.ClientMessage, 10)
	go s.readLoop(clientMsgs)


	for {
		// If we have a notification channel (engine initialized), use select.
		// Otherwise, just read client messages (pre-init phase).
		if s.notifyCh != nil {
			select {
			case <-ctx.Done():
				s.cleanup()
				return
			case msg, ok := <-clientMsgs:
				if !ok {
					if s.isDaemon {
						s.Detach()
						clientMsgs = nil
						continue
					}
					s.cleanup()
					return
				}
				s.dispatchClientMessage(ctx, msg)
			case notif := <-s.notifyCh:
				// Auto-wake: if engine is idle and within limit, start a synthetic turn
				if s.engine != nil && s.engine.IsIdle() && s.canAutoWake() {
					s.handleAutoWake(ctx, notif)
				}
				// If engine is busy, the notification was drained from the channel.
				// It won't be re-queued, but the engine will drain any remaining
				// notifications at the start of its next turn.
			}
		} else {
			select {
			case <-ctx.Done():
				s.cleanup()
				return
			case msg, ok := <-clientMsgs:
				if !ok {
					if s.isDaemon {
						s.Detach()
						clientMsgs = nil
						continue
					}
					s.cleanup()
					return
				}
				s.dispatchClientMessage(ctx, msg)
			}
		}
	}
}
// pingLoop sends periodic WebSocket ping frames to keep the connection alive.
// Without this, idle connections during long LLM calls (10-60s) can be dropped
// by the OS TCP keepalive or intermediate proxies.
func (s *Session) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			err := s.conn.WriteMessage(websocket.PingMessage, []byte("keepalive"))
			s.mu.Unlock()
			if err != nil {
				s.logger.Debug("ping write failed, connection likely closed", "error", err)
				return
			}
		}
	}
}

// readLoop reads WebSocket messages in a goroutine and sends them to a channel.
func (s *Session) readLoop(ch chan<- *pb.ClientMessage) {
	defer close(ch)

	for {
		msgType, data, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Error("WebSocket read error", "error", err)
			}
			return
		}

		if msgType != websocket.BinaryMessage {
			s.logger.Warn("received non-binary message, ignoring", "type", msgType)
			continue
		}

		var clientMsg pb.ClientMessage
		if err := proto.Unmarshal(data, &clientMsg); err != nil {
			s.logger.Error("protobuf unmarshal error", "error", err)
			s.sendError("PROTO_ERROR", fmt.Sprintf("invalid protobuf: %v", err), false)
			continue
		}

		ch <- &clientMsg
	}
}

// dispatchClientMessage routes a client message to the appropriate handler.
func (s *Session) dispatchClientMessage(ctx context.Context, msg *pb.ClientMessage) {
	switch payload := msg.Payload.(type) {
	case *pb.ClientMessage_Init:
		s.handleInit(ctx, payload.Init)
	case *pb.ClientMessage_UserMessage:
		// Real user message — reset auto-wake counter
		s.autoWakeCount = 0
		s.turnWg.Add(1)
		go s.handleUserMessage(ctx, payload.UserMessage)
	case *pb.ClientMessage_Cancel:
		s.handleCancel()
	case *pb.ClientMessage_HostToolResult:
		s.handleToolResult(payload.HostToolResult)
	case *pb.ClientMessage_PermissionResponse:
		s.handlePermissionResponse(payload.PermissionResponse)
	case *pb.ClientMessage_QuestionResponse:
		s.handleQuestionResponse(payload.QuestionResponse)
	case *pb.ClientMessage_Interrupt:
		s.handleInterrupt()
	case *pb.ClientMessage_Resume:
		s.handleResume(payload.Resume)
	case *pb.ClientMessage_WorkspaceRequest:
		s.handleWorkspaceRequest(ctx, payload.WorkspaceRequest)
	case *pb.ClientMessage_SetYoloMode:
		s.handleSetYoloMode(payload.SetYoloMode)
	default:
		s.logger.Warn("unknown client message type")
	}
}

func (s *Session) handleInterrupt() {
	if s.engine != nil {
		s.engine.Interrupt()
	}
}

func (s *Session) handleResume(req *pb.ResumeRequest) {
	if s.engine != nil {
		s.engine.Resume(req.Message)
	}
}

// canAutoWake checks whether another synthetic turn is allowed.
// Returns false when auto-wake is disabled (limit=0) or the limit is reached.
func (s *Session) canAutoWake() bool {
	if s.maxAutoWakeTurns <= 0 {
		return false // Auto-wake disabled
	}
	return s.autoWakeCount < s.maxAutoWakeTurns
}

// handleAutoWake starts a synthetic agent turn triggered by a system notification.
// The notification content is passed as the user message — the engine will also
// drain any additional pending notifications via its notifyCh.
func (s *Session) handleAutoWake(ctx context.Context, notif tools.SystemMessage) {
	s.autoWakeCount++
	s.logger.Info("auto-wake: starting synthetic turn",
		"source", notif.Source,
		"task_id", notif.TaskID,
		"auto_wake_count", s.autoWakeCount,
		"max_auto_wake_turns", s.maxAutoWakeTurns,
	)

	// Construct a synthetic user message from the notification
	syntheticMsg := &pb.UserMessage{
		Content: fmt.Sprintf("[System notification: %s] %s", notif.Source, notif.Content),
	}

	s.turnWg.Add(1)
	go s.handleUserMessage(ctx, syntheticMsg)
}

// cleanup persists state and shuts down resources on disconnect.
// Waits for any in-flight handleUserMessage goroutines to complete their
// post-run save (SetMessages + SaveAll) before doing final cleanup.
func (s *Session) cleanup() {
	s.logger.Info("session cleanup: waiting for in-flight turns to complete")
	s.turnWg.Wait()
	s.logger.Info("session cleanup: all turns complete, saving state")

	if s.conv != nil {
		if err := s.conv.SaveAll(); err != nil {
			s.logger.Error("failed to save conversation state during cleanup", "error", err)
		}
	}
	if s.toolRegistry != nil {
		s.toolRegistry.Shutdown()
	}
	if s.mcpMgr != nil {
		s.mcpMgr.Close()
	}
}

// handleInit processes the InitRequest and sets up the engine.
func (s *Session) handleInit(ctx context.Context, req *pb.InitRequest) {
	cfg := req.Config
	if cfg == nil {
		s.sendError("INIT_ERROR", "config is required", true)
		return
	}

	s.logger.Info("initializing session")

	// Set up workspace manager
	var workspaceDirs []string
	var workspaceInfos []engine.WorkspaceInfo
	for _, ws := range cfg.Workspaces {
		workspaceDirs = append(workspaceDirs, ws.Directory)
		workspaceInfos = append(workspaceInfos, engine.WorkspaceInfo{
			Directory:  ws.Directory,
			CorpusName: ws.CorpusName,
		})
	}
	if len(workspaceDirs) == 0 {
		workspaceDirs = []string{s.serverCfg.Workspace}
		workspaceInfos = []engine.WorkspaceInfo{{Directory: s.serverCfg.Workspace}}
	}

	wsMgr, err := workspace.NewManager(workspaceDirs)
	if err != nil {
		s.sendError("INIT_ERROR", fmt.Sprintf("workspace error: %v", err), true)
		return
	}
	s.wsMgr = wsMgr
	s.yoloMode = cfg.YoloMode

	// Workspace trust verification
	settings := config.LoadGlobalSettings(s.logger)
	allTrusted := true
	for _, dir := range workspaceDirs {
		if !settings.IsWorkspaceTrusted(dir) {
			allTrusted = false
			break
		}
	}
	if cfg.Trusted {
		allTrusted = true
	}
	s.isRestricted = !allTrusted
	if s.isRestricted {
		s.logger.Warn("workspace is untrusted: operating in restricted read-only mode")
	}

	// Set up conversation manager — always uses the binary's resolved data dir
	appDataDir := s.serverCfg.AppDataDir

	convMgr, err := conversation.NewManager(appDataDir)
	if err != nil {
		s.sendError("INIT_ERROR", fmt.Sprintf("conversation manager error: %v", err), true)
		return
	}

	// Create or resume conversation
	if !s.serverCfg.IsNewSession {
		// Resume existing conversation
		s.conv, err = convMgr.Resume(s.serverCfg.SessionID)
		if err != nil {
			s.sendError("INIT_ERROR", fmt.Sprintf("cannot resume conversation: %v", err), true)
			return
		}
		s.logger.Info("resumed conversation", "id", s.conv.ID)
	} else {
		// Create new conversation
		s.conv, err = convMgr.CreateWithID(s.serverCfg.SessionID, cfg)
		if err != nil {
			s.sendError("INIT_ERROR", fmt.Sprintf("cannot create conversation: %v", err), true)
			return
		}
		s.logger.Info("created new conversation", "id", s.conv.ID)


	}

	// Allow only the brain and knowledge subdirectories so agents can write
	// artifacts, logs, and knowledge items — but NOT conversations/, plugins/,
	// skills/, projects.json, or other harness-internal paths.
	brainRoot := filepath.Join(appDataDir, "brain")
	knowledgeRoot := filepath.Join(appDataDir, "knowledge")
	if err := wsMgr.AddAllowedPath(brainRoot); err != nil {
		s.logger.Warn("failed to allow brain dir in workspace manager", "error", err)
	}
	if err := wsMgr.AddAllowedPath(knowledgeRoot); err != nil {
		s.logger.Warn("failed to allow knowledge dir in workspace manager", "error", err)
	}

	// Set up LLM provider
	provider, err := s.createProvider(cfg)
	if err != nil {
		s.sendError("INIT_ERROR", fmt.Sprintf("LLM provider error: %v", err), true)
		return
	}



	// Set up tool registry
	toolRegistry := tools.NewRegistry(wsMgr, s.logger)
	builtinCfg := cfg.BuiltinTools
	if builtinCfg == nil {
		builtinCfg = config.DefaultBuiltinTools()
	}
	if s.isRestricted {
		builtinCfg.CreateFile = false
		builtinCfg.EditFile = false
		builtinCfg.RunCommand = false
		builtinCfg.ManageTask = false
	}
	tools.RegisterBuiltinTools(toolRegistry, builtinCfg)
	s.toolRegistry = toolRegistry

	// Build host tool declarations and name set from config
	var hostToolDecls []llm.FunctionDeclaration
	hostToolNames := make(map[string]bool)
	for _, td := range cfg.HostTools {
		var params map[string]interface{}
		if td.ParametersJsonSchema != "" {
			if err := json.Unmarshal([]byte(td.ParametersJsonSchema), &params); err != nil {
				s.logger.Warn("invalid host tool parameter schema", "tool", td.Name, "error", err)
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
		} else {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		hostToolDecls = append(hostToolDecls, llm.FunctionDeclaration{
			Name:        td.Name,
			Description: td.Description,
			Parameters:  params,
		})
		hostToolNames[td.Name] = true
		s.logger.Info("registered host tool", "name", td.Name)
	}

	// Load initial history from conversation state
	var initialHistory []llm.Message
	for _, m := range s.conv.Messages() {
		initialHistory = append(initialHistory, mapProtoMessageToLLM(m))
	}

	// Set up MCP servers (global config + agent-level merge)
	var mcpMgr *mcpbridge.Manager
	if !s.isRestricted {
		globalMcpServers := config.LoadGlobalMcpConfig(s.logger)
		mergedMcpServers := config.MergeMcpConfigs(globalMcpServers, cfg.McpServers)

		// Auto-inject Playwright MCP server when browser capability is enabled
		if cfg.BuiltinTools != nil && cfg.BuiltinTools.Browser {
			if _, err := exec.LookPath("npx"); err != nil {
				s.logger.Warn("Browser capability enabled but npx not found in PATH — install Node.js to use browser tools")
			} else {
				// Check if user already configured a "playwright" MCP server
				hasPlaywright := false
				for _, srv := range mergedMcpServers {
					if srv.Name == "playwright" {
						hasPlaywright = true
						break
					}
				}
				if !hasPlaywright {
					playwrightCfg := &pb.McpServerConfig{
						Name: "playwright",
						Transport: &pb.McpServerConfig_Stdio{
							Stdio: &pb.McpStdioTransport{
								Command: "npx",
								Args:    []string{"-y", "@playwright/mcp@latest"},
							},
						},
					}
					mergedMcpServers = append(mergedMcpServers, playwrightCfg)
					s.logger.Info("auto-injecting Playwright MCP server for browser capability")
				}
			}
		}

		if len(mergedMcpServers) > 0 {
			mcpMgr = mcpbridge.NewManager(s.logger)
			if err := mcpMgr.Connect(ctx, mergedMcpServers); err != nil {
				s.sendError("INIT_ERROR", fmt.Sprintf("MCP connection error: %v", err), false)
				// Non-fatal: continue without MCP tools
				s.logger.Error("MCP connection failed, continuing without MCP tools", "error", err)
				mcpMgr = nil
			} else {
				s.mcpMgr = mcpMgr
				s.logger.Info("MCP servers connected",
					"servers", mcpMgr.ServerCount(),
					"tools", mcpMgr.ToolCount(),
				)
			}
		}
	}

	// Create trajectory
	trajID := s.conv.NextTrajectoryID()

	// Load user rules: ADK-injected (from config) + auto-discovered (AGENTS.md from workspaces).
	// SDK rules come first, then workspace rules.
	var userRules []config.UserRule
	for _, r := range cfg.UserRules {
		if r.Content != "" {
			userRules = append(userRules, config.UserRule{
				Filename: r.Label,
				Content:  r.Content,
			})
		}
	}
	discoveredRules := config.LoadAgentsRules(workspaceDirs, s.logger)
	userRules = append(userRules, discoveredRules...)
	if len(userRules) > 0 {
		s.logger.Info("loaded user rules", "sdk", len(userRules)-len(discoveredRules), "agents_md", len(discoveredRules))
	}

	// Wire system notification channel: timers, cron, and task completions
	// all push to the same channel so the engine can drain them before each turn.
	var notifyCh <-chan tools.SystemMessage
	if toolRegistry.TaskManager() != nil {
		schedMgr := toolRegistry.TaskManager().ScheduleManager()
		notifyCh = schedMgr.Notifications()
		// TaskManager pushes completions to the same underlying channel
		toolRegistry.TaskManager().SetNotifyChannel(schedMgr.NotifyChannel())
	}

	// Discover skills and plugins from filesystem (global + workspace)
	// then merge with ADK-injected definitions (SDK > workspace > global)
	var allSkills []engine.SkillDef
	var allPlugins []engine.PluginDef
	if !s.isRestricted {
		adkSkills := protoSkillsToEngine(cfg.Skills)
		adkPlugins := protoPluginsToEngine(cfg.Plugins)
		allSkills, allPlugins = discovery.DiscoverAll(appDataDir, workspaceDirs, adkSkills, adkPlugins, s.logger)
	}

	// Create project registry for workspace → project UUID mapping (Knowledge Items)
	projectRegistry := engine.NewProjectRegistry(appDataDir)
	if err := projectRegistry.Load(); err != nil {
		s.logger.Warn("failed to load project registry", "error", err)
	}

	s.engine = engine.NewEngine(engine.Config{
		Provider:              provider,
		ToolRegistry:          toolRegistry,
		SystemPrompt:          cfg.SystemInstructions,
		StructuredInstructions: cfg.StructuredInstructions,
		ConversationID:        s.conv.ID,
		TrajectoryID:          trajID,
		OnStep:                s.onStep,
		OnTrajectory:          s.onTrajectory,
		CompactionThreshold:   resolveCompactionThreshold(int(cfg.CompactionThreshold)),
		KeepRecentMessages:    int(cfg.KeepRecentMessages),
		BrainDir:              s.conv.BrainDir,
		AppDataDir:            appDataDir,
		Logger:                s.logger,
		HostToolHandler:       s.hostToolHandler,
		HostToolNames:         hostToolNames,
		HostToolDecls:         hostToolDecls,
		PermissionHandler:     s.permissionHandler,
		QuestionHandler:       s.questionHandler,
		MCPManager:            mcpMgr,
		Workspaces:            workspaceDirs,
		WorkspaceInfos:        workspaceInfos,
		UserRules:             userRules,
		MaxDepth:              int(cfg.MaxSubagentDepth),
		MaxSubagents:          int(cfg.MaxConcurrentSubagents),
		SubagentsEnabled:      builtinCfg.InvokeSubagent,
		InitialHistory:        initialHistory,
		EnableWebDev:          cfg.PromptModules != nil && cfg.PromptModules.EnableWebDevelopment,
		EnablePlanningMode:    cfg.PromptModules != nil && cfg.PromptModules.EnablePlanning,
		EnableSlashCommands:   cfg.PromptModules != nil && cfg.PromptModules.EnableSlashCommands,
		SlashCommands:         protoSlashCommandsToEngine(cfg.SlashCommands),
		EnableKnowledgeItems:  cfg.PromptModules != nil && cfg.PromptModules.EnableKnowledgeItems,
		Skills:                allSkills,
		Plugins:               allPlugins,
		NotifyCh:              notifyCh,
		ProjectRegistry:       projectRegistry,
		ConversationManager:   convMgr,
		YoloMode:              s.yoloMode,
	})


	// Store notification channel on session for auto-wake in Run() select loop
	s.notifyCh = notifyCh
	s.maxAutoWakeTurns = int(cfg.MaxAutoWakeTurns)

	// Register pre-completion hook to save conversation state BEFORE
	// TRAJ_IDLE is emitted. This prevents a race condition where the SDK
	// receives TRAJ_IDLE, Chat() returns, and agent.Close() kills the
	// harness before handleUserMessage can reach SetMessages/SaveAll.
	s.engine.SetPreCompletionHook(func() {
		history := s.engine.History()
		var protoMsgs []*pb.ConversationMessage
		for _, m := range history {
			protoMsgs = append(protoMsgs, mapLLMMessageToProto(m))
		}
		s.conv.SetMessages(protoMsgs)
		if err := s.conv.SaveAll(); err != nil {
			s.logger.Error("pre-completion save failed", "error", err)
		} else {
			s.logger.Debug("pre-completion save complete",
				"messages", len(protoMsgs),
				"conv_id", s.conv.ID,
			)
		}
	})

	// Send init response
	s.sendServerMessage(&pb.ServerMessage{
		Payload: &pb.ServerMessage_InitResponse{
			InitResponse: &pb.InitResponse{
				ConversationId: s.conv.ID,
				HarnessVersion: config.HarnessVersion,
			},
		},
	})

	s.logger.Info("session initialized",
		"conversation_id", s.conv.ID,
		"conversation_dir", s.conv.BrainDir,
		"model", provider.ModelName(),
		"workspaces", workspaceDirs,
	)

	// Process any user messages that arrived before initialization
	s.earlyUserMessagesMu.Lock()
	earlyMessages := s.earlyUserMessages
	s.earlyUserMessages = nil
	s.earlyUserMessagesMu.Unlock()

	if len(earlyMessages) > 0 {
		s.logger.Info("processing early user messages", "count", len(earlyMessages))
		for _, msg := range earlyMessages {
			s.turnWg.Add(1)
			go s.handleUserMessage(ctx, msg)
		}
	}
}

// handleUserMessage processes a user prompt and runs the agentic loop.
// Caller must call turnWg.Add(1) before launching this in a goroutine.
func (s *Session) handleUserMessage(ctx context.Context, msg *pb.UserMessage) {
	if s.engine == nil {
		// Queue the message for processing after initialization
		s.earlyUserMessagesMu.Lock()
		s.earlyUserMessages = append(s.earlyUserMessages, msg)
		s.earlyUserMessagesMu.Unlock()
		s.logger.Info("user message received before initialization, queued", "content_len", len(msg.Content), "queued_count", len(s.earlyUserMessages))
		s.turnWg.Done() // We're done with this goroutine
		return
	}

	defer s.turnWg.Done()

	s.logger.Info("user message received", "content_len", len(msg.Content))

	// Log user message to conversation
	s.conv.AddMessage(&pb.ConversationMessage{
		Role:    "user",
		Content: msg.Content,
	})

	// Run the agentic loop with host context and ephemeral messages (blocking)
	if len(msg.EphemeralMessages) > 0 {
		s.engine.SetEphemeralMessages(msg.EphemeralMessages)
	}
	if len(msg.SettingsChanges) > 0 {
		changes := make([]engine.SettingsChange, len(msg.SettingsChanges))
		for i, sc := range msg.SettingsChanges {
			changes[i] = engine.SettingsChange{
				Setting:  sc.Setting,
				OldValue: sc.OldValue,
				NewValue: sc.NewValue,
				Hint:     sc.Hint,
			}
		}
		s.engine.SetSettingsChanges(changes)
	}
	if err := s.engine.RunWithContext(ctx, msg.Content, msg.Context); err != nil {
		s.logger.Error("engine error", "error", err)
		s.sendError("ENGINE_ERROR", err.Error(), false)
	}

	// Sync engine history back to conversation
	history := s.engine.History()
	var protoMsgs []*pb.ConversationMessage
	for _, m := range history {
		protoMsgs = append(protoMsgs, mapLLMMessageToProto(m))
	}
	s.conv.SetMessages(protoMsgs)

	// Persist state after each turn
	if err := s.conv.SaveAll(); err != nil {
		s.logger.Error("failed to save conversation state", "error", err)
	} else {
		s.logger.Debug("conversation state saved",
			"messages", len(protoMsgs),
			"conv_id", s.conv.ID,
		)
	}
}

// handleCancel aborts the current turn.
func (s *Session) handleCancel() {
	if s.cancel != nil {
		s.logger.Info("cancelling current turn")
		s.cancel()
	}
}

// hostToolHandler is the engine.HostToolHandler callback.
// It creates a channel, stores it in pendingToolResults, and blocks until
// the SDK client sends back a ToolResult (or timeout/context cancellation).
func (s *Session) hostToolHandler(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) (string, bool, error) {
	stepID := fmt.Sprintf("%d", step.StepIndex)
	ch := make(chan *pb.ToolResult, 1)

	s.pendingMu.Lock()
	s.pendingToolResults[stepID] = ch
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pendingToolResults, stepID)
		s.pendingMu.Unlock()
	}()

	// Wait for client response (or timeout/cancel)
	select {
	case result := <-ch:
		return result.ResultJson, result.IsError, nil
	case <-ctx.Done():
		return "", true, ctx.Err()
	case <-time.After(5 * time.Minute):
		return "", true, errors.New(errors.ErrCodeToolTimeout,
			"host tool timed out waiting for result").
			WithContext("tool", tc.Name).
			WithContext("timeout", "5m").
			WithContext("conversation_id", s.conv.ID).
			WithComponent("session")
	}
}

// handleToolResult routes an incoming ToolResult from the SDK client
// to the pending channel that the engine's hostToolHandler is blocking on.
func (s *Session) handleToolResult(result *pb.ToolResult) {
	s.logger.Info("received host tool result", "tool", result.ToolName, "step_id", result.StepId)

	s.pendingMu.Lock()
	ch, ok := s.pendingToolResults[result.StepId]
	s.pendingMu.Unlock()

	if !ok {
		s.logger.Warn("received tool result for unknown step",
			"step_id", result.StepId,
			"tool", result.ToolName,
		)
		return
	}

	// Non-blocking send (channel is buffered with capacity 1)
	select {
	case ch <- result:
	default:
		s.logger.Warn("duplicate tool result ignored", "step_id", result.StepId)
	}
}

// permissionHandler is the engine.PermissionHandler callback.
// It creates a channel, stores it in pendingPermissions, and blocks until
// the SDK client sends back a PermissionResponse (or timeout/context cancellation).
func (s *Session) permissionHandler(ctx context.Context, req *pb.ActionPermissionRequest) (bool, string, error) {
	if s.yoloMode {
		s.logger.Info("yolo mode active: auto-approving permission request", "tool", req.ToolName, "request_id", req.RequestId)
		return true, "", nil
	}

	s.mu.Lock()
	isDetached := s.detached || s.conn == nil
	s.mu.Unlock()

	if isDetached {
		s.logger.Info("client detached: queuing pending approval", "tool", req.ToolName, "request_id", req.RequestId)
		respCh := s.approvalQueue.Enqueue(req.RequestId, req.ToolName, req.ArgsSummary, req.DiffPreview)
		select {
		case resp, ok := <-respCh:
			if !ok || resp == nil {
				return false, "approval cancelled or queue cleared", nil
			}
			return resp.Approved, resp.DenialReason, nil
		case <-ctx.Done():
			s.approvalQueue.Resolve(req.RequestId, &pb.PermissionResponse{
				RequestId:    req.RequestId,
				Approved:     false,
				DenialReason: "context cancelled",
			})
			return false, "cancelled", ctx.Err()
		}
	}

	ch := make(chan *pb.PermissionResponse, 1)

	s.pendingPermissionsMu.Lock()
	s.pendingPermissions[req.RequestId] = ch
	s.pendingPermissionsMu.Unlock()

	defer func() {
		s.pendingPermissionsMu.Lock()
		delete(s.pendingPermissions, req.RequestId)
		s.pendingPermissionsMu.Unlock()
	}()

	// Wait for client response (or timeout/cancel)
	select {
	case resp := <-ch:
		if resp.Scope == pb.PermissionResponse_SCOPE_CONVERSATION || resp.Scope == pb.PermissionResponse_SCOPE_GLOBAL {
			var toGrant []string
			if len(resp.ApprovedSubcommands) > 0 {
				toGrant = append(toGrant, resp.ApprovedSubcommands...)
				if resp.Approved {
					target := extractTargetFromPermissionReq(req)
					if target != "" {
						toGrant = append(toGrant, target)
					}
				}
			} else if resp.Approved {
				target := extractTargetFromPermissionReq(req)
				if target != "" {
					toGrant = append(toGrant, target)
					if req.ToolName == "run_command" {
						subCmds, _ := util.SplitShellCommands(target)
						toGrant = append(toGrant, subCmds...)
					}
				} else if req.ToolName != "" {
					toGrant = append(toGrant, "*")
				}
			}

			if len(toGrant) > 0 {
				if s.engine != nil {
					for _, item := range toGrant {
						s.engine.AddPermissionGrant(engine.PermissionGrant{
							Action: req.ToolName,
							Target: item,
							Reason: "Approved by user in session",
							Scope:  resp.Scope.String(),
						})
					}
				}
				if resp.Scope == pb.PermissionResponse_SCOPE_GLOBAL {
					for _, item := range toGrant {
						if req.ToolName == "run_command" && item != "*" {
							_ = config.AddAllowedCommand(item, s.logger)
						} else {
							_ = config.AddAllowedTool(req.ToolName, s.logger)
						}
					}
					if s.engine != nil {
						s.engine.ReloadGlobalSettings()
					}
				}
			}
		}
		return resp.Approved, resp.DenialReason, nil
	case <-ctx.Done():
		return false, "cancelled", ctx.Err()
	case <-time.After(5 * time.Minute):
		return false, "timed out waiting for permission response (5m)", nil
	}
}

func extractTargetFromPermissionReq(req *pb.ActionPermissionRequest) string {
	if req.ToolName == "run_command" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(req.ArgsJson), &args); err == nil {
			if cmd, ok := args["command"].(string); ok && cmd != "" {
				return cmd
			}
			if cmd, ok := args["CommandLine"].(string); ok && cmd != "" {
				return cmd
			}
		}
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(req.ArgsJson), &args); err == nil {
		for _, key := range []string{"path", "AbsolutePath", "DirectoryPath", "SearchPath", "TargetFile", "file_path", "target_file"} {
			if p, ok := args[key].(string); ok && p != "" {
				return p
			}
		}
	}
	return ""
}

// handlePermissionResponse routes an incoming PermissionResponse from the SDK client
// to the pending channel that the engine's permissionHandler is blocking on.
func (s *Session) handlePermissionResponse(resp *pb.PermissionResponse) {
	s.logger.Info("received permission response", "request_id", resp.RequestId, "approved", resp.Approved)

	// First try resolving from approvalQueue
	if s.approvalQueue.Resolve(resp.RequestId, resp) {
		return
	}

	s.pendingPermissionsMu.Lock()
	ch, ok := s.pendingPermissions[resp.RequestId]
	s.pendingPermissionsMu.Unlock()

	if !ok {
		s.logger.Warn("received permission response for unknown request",
			"request_id", resp.RequestId,
		)
		return
	}

	// Non-blocking send (channel is buffered with capacity 1)
	select {
	case ch <- resp:
	default:
		s.logger.Warn("duplicate permission response ignored", "request_id", resp.RequestId)
	}
}

// questionHandler is the engine.QuestionHandler callback.
// It creates a channel, stores it in pendingQuestions, and blocks until
// the SDK client sends back a QuestionResponse (or timeout/context cancellation).
func (s *Session) questionHandler(ctx context.Context, req *pb.ActionUserQuestion) (*pb.QuestionResponse, error) {
	ch := make(chan *pb.QuestionResponse, 1)

	s.pendingQuestionsMu.Lock()
	s.pendingQuestions[req.RequestId] = ch
	s.pendingQuestionsMu.Unlock()

	defer func() {
		s.pendingQuestionsMu.Lock()
		delete(s.pendingQuestions, req.RequestId)
		s.pendingQuestionsMu.Unlock()
	}()

	// Wait for client response (or timeout/cancel)
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return &pb.QuestionResponse{Skipped: true}, ctx.Err()
	case <-time.After(5 * time.Minute):
		return &pb.QuestionResponse{Skipped: true}, nil
	}
}

// handleQuestionResponse routes an incoming QuestionResponse from the SDK client
// to the pending channel that the engine's questionHandler is blocking on.
func (s *Session) handleQuestionResponse(resp *pb.QuestionResponse) {
	s.logger.Info("received question response", "request_id", resp.RequestId, "skipped", resp.Skipped)

	s.pendingQuestionsMu.Lock()
	ch, ok := s.pendingQuestions[resp.RequestId]
	s.pendingQuestionsMu.Unlock()

	if !ok {
		s.logger.Warn("received question response for unknown request",
			"request_id", resp.RequestId,
		)
		return
	}

	// Non-blocking send (channel is buffered with capacity 1)
	select {
	case ch <- resp:
	default:
		s.logger.Warn("duplicate question response ignored", "request_id", resp.RequestId)
	}
}

// createProvider creates the LLM provider from the HarnessConfig.
// It loads the configuration from ~/.divmora/config/litellm.json.
func (s *Session) createProvider(cfg *pb.HarnessConfig) (llm.Provider, error) {
	liteLLMCfg := config.LoadGlobalLiteLLMConfig(s.logger)

	var endpointName string
	if cfg.LitellmEndpoint != "" {
		endpointName = cfg.LitellmEndpoint
	} else if liteLLMCfg.DefaultEndpoint != "" {
		endpointName = liteLLMCfg.DefaultEndpoint
	}

	var baseURL, apiKey, model string

	// Load from global config if an endpoint is resolved
	if endpointName != "" {
		if endpoint, ok := liteLLMCfg.Endpoints[endpointName]; ok {
			baseURL = endpoint.BaseURL
			apiKey = endpoint.APIKey
			model = endpoint.DefaultModel
			s.logger.Info("using LiteLLM endpoint from global config", "endpoint", endpointName)
		} else if cfg.LitellmEndpoint != "" {
			return nil, errors.New(errors.ErrCodeConfiguration,
				"LiteLLM endpoint not found in configuration").
				WithContext("endpoint", endpointName).
				WithContext("config_file", "~/.divmora/config/litellm.json").
				WithComponent("session")
		}
	}

	// ADK inline config overrides global config
	if cfg.LitellmBaseUrl != "" {
		baseURL = cfg.LitellmBaseUrl
	}
	if cfg.LitellmApiKey != "" {
		apiKey = cfg.LitellmApiKey
	}
	if cfg.LitellmModel != "" {
		model = cfg.LitellmModel
	}

	if baseURL == "" {
		return nil, errors.New(errors.ErrCodeConfiguration,
			"no LiteLLM base URL configured").
			WithContext("config_source", "ADK config and ~/.divmora/config/litellm.json").
			WithComponent("session")
	}

	primary, err := llm.NewOpenAIProvider(llm.OpenAIConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		ModelName: model,
	}, s.logger)

	if err != nil {
		return nil, err
	}

	return primary, nil
}

// ─── Callbacks ──────────────────────────────────────────────────────────

func (s *Session) onStep(step *pb.StepUpdate) {
	// Log to transcript — skip streaming text deltas to avoid noisy repeated entries.
	// Streaming steps have no new information worth persisting; the final DONE state
	// captures the complete text.
	isStreamingDelta := step.State == pb.StepUpdate_STATE_STREAMING && step.Action == nil
	if s.conv != nil && !isStreamingDelta {
		source := "MODEL"
		switch step.Source {
		case pb.StepUpdate_SOURCE_USER:
			source = "USER_EXPLICIT"
		case pb.StepUpdate_SOURCE_SYSTEM:
			source = "SYSTEM"
		}

		status := "DONE"
		switch step.State {
		case pb.StepUpdate_STATE_ACTIVE:
			status = "ACTIVE"
		case pb.StepUpdate_STATE_ERROR:
			status = "ERROR"
		case pb.StepUpdate_STATE_STREAMING:
			status = "RUNNING"
		case pb.StepUpdate_STATE_WAITING:
			status = "WAITING"
		}

		// Determine step type — use tool-specific types matching Antigravity format
		stepType := stepTypeFromAction(step)

		entry := &conversation.TranscriptJSONEntry{
			StepIndex: step.StepIndex,
			Source:    source,
			Type:      stepType,
			Status:    status,
		}

		// Include content from the step result (truncated for large outputs)
		content := extractStepContent(step)
		if content != "" {
			entry.Content = truncate(content, 2000)
		}

		// Include thinking text
		if step.Thinking != "" {
			entry.Thinking = truncate(step.Thinking, 500)
		}

		// Include tool calls on model response entries (PLANNER_RESPONSE)
		// These are not available on individual step callbacks in our architecture,
		// but we encode the current tool's args as a single-element tool_calls array.
		if step.Action != nil && step.State == pb.StepUpdate_STATE_ACTIVE {
			tc := extractToolCall(step)
			if tc != nil {
				entry.ToolCalls = []conversation.TranscriptToolCall{*tc}
			}
		}

		// Include error details
		if step.ErrorInfo != nil && step.ErrorInfo.Message != "" {
			entry.Error = truncate(step.ErrorInfo.Message, 500)
		}

		s.conv.LogStep(entry)

		// Save step content to steps/<N>/content.md
		if step.Text != "" {
			s.conv.SaveStepContent(step.StepIndex, step.Text)
		}
	}

	// Send to client via WebSocket
	s.sendServerMessage(&pb.ServerMessage{
		Payload: &pb.ServerMessage_StepUpdate{
			StepUpdate: step,
		},
	})
}

// stepTypeFromAction maps a StepUpdate's Action to an Antigravity-compatible type string.
// When no action is present, returns "PLANNER_RESPONSE" for model text responses.
func stepTypeFromAction(step *pb.StepUpdate) string {
	if step.Action == nil {
		if step.Text != "" {
			return "PLANNER_RESPONSE"
		}
		return "MODEL_RESPONSE"
	}

	switch step.Action.(type) {
	case *pb.StepUpdate_ViewFile:
		return "VIEW_FILE"
	case *pb.StepUpdate_WriteToFile:
		return "CODE_ACTION"
	case *pb.StepUpdate_ReplaceFileContent:
		return "CODE_ACTION"
	case *pb.StepUpdate_ListDir:
		return "LIST_DIRECTORY"
	case *pb.StepUpdate_GrepSearch:
		return "GREP_SEARCH"
	case *pb.StepUpdate_FindFile:
		return "FIND_FILE"
	case *pb.StepUpdate_RunCommand:
		return "RUN_COMMAND"
	case *pb.StepUpdate_ManageTask:
		return "MANAGE_TASK"
	case *pb.StepUpdate_Finish:
		return "FINISH"
	case *pb.StepUpdate_InvokeSubagent:
		return "INVOKE_SUBAGENT"
	case *pb.StepUpdate_SearchWeb:
		return "WEB_SEARCH"
	case *pb.StepUpdate_ReadUrlContent:
		return "WEB_FETCH"
	case *pb.StepUpdate_Schedule:
		return "SCHEDULE"
	case *pb.StepUpdate_McpTool:
		return "MCP_TOOL"
	case *pb.StepUpdate_HostToolCall:
		return "HOST_TOOL"
	case *pb.StepUpdate_UserQuestion:
		return "USER_QUESTION"
	case *pb.StepUpdate_PermissionRequest:
		return "PERMISSION_REQUEST"
	case *pb.StepUpdate_DefineSubagent:
		return "DEFINE_SUBAGENT"
	case *pb.StepUpdate_ManageSubagents:
		return "MANAGE_SUBAGENTS"
	case *pb.StepUpdate_SendMessageAction:
		return "SEND_MESSAGE"
	default:
		return "GENERIC"
	}
}

// extractToolCall extracts tool name and key args from a step for the tool_calls array.
func extractToolCall(step *pb.StepUpdate) *conversation.TranscriptToolCall {
	if step.Action == nil {
		return nil
	}

	tc := &conversation.TranscriptToolCall{}
	args := make(map[string]string)

	switch a := step.Action.(type) {
	case *pb.StepUpdate_ViewFile:
		tc.Name = "view_file"
		args["path"] = a.ViewFile.Path
		if a.ViewFile.StartLine > 0 || a.ViewFile.EndLine > 0 {
			args["lines"] = fmt.Sprintf("%d-%d", a.ViewFile.StartLine, a.ViewFile.EndLine)
		}
	case *pb.StepUpdate_WriteToFile:
		tc.Name = "create_file"
		args["path"] = a.WriteToFile.Path
		args["overwrite"] = fmt.Sprintf("%v", a.WriteToFile.Overwrite)
	case *pb.StepUpdate_ReplaceFileContent:
		tc.Name = "edit_file"
		args["path"] = a.ReplaceFileContent.Path
	case *pb.StepUpdate_ListDir:
		tc.Name = "list_dir"
		args["path"] = a.ListDir.Path
	case *pb.StepUpdate_GrepSearch:
		tc.Name = "grep_search"
		args["path"] = a.GrepSearch.Path
		args["query"] = a.GrepSearch.Query
	case *pb.StepUpdate_FindFile:
		tc.Name = "find_file"
		args["pattern"] = a.FindFile.Pattern
	case *pb.StepUpdate_RunCommand:
		tc.Name = "run_command"
		args["command"] = truncate(a.RunCommand.Command, 200)
	case *pb.StepUpdate_ManageTask:
		tc.Name = "manage_task"
	case *pb.StepUpdate_Finish:
		tc.Name = "finish"
	case *pb.StepUpdate_InvokeSubagent:
		tc.Name = "invoke_subagent"
	case *pb.StepUpdate_SearchWeb:
		tc.Name = "search_web"
		args["query"] = a.SearchWeb.Query
	case *pb.StepUpdate_ReadUrlContent:
		tc.Name = "read_url_content"
		args["url"] = a.ReadUrlContent.Url
	case *pb.StepUpdate_Schedule:
		tc.Name = "schedule"
		if a.Schedule.DurationSeconds > 0 {
			args["duration"] = fmt.Sprintf("%ds", a.Schedule.DurationSeconds)
		}
		if a.Schedule.CronExpression != "" {
			args["cron"] = a.Schedule.CronExpression
		}
	case *pb.StepUpdate_McpTool:
		tc.Name = a.McpTool.ToolName
		args["server"] = a.McpTool.ServerName
	case *pb.StepUpdate_HostToolCall:
		tc.Name = a.HostToolCall.ToolName
	case *pb.StepUpdate_UserQuestion:
		tc.Name = "ask_question"
	default:
		return nil
	}

	if len(args) > 0 {
		tc.Args = args
	}
	return tc
}

// extractStepContent builds a human-readable content summary from a step's result.
// For text-only steps, returns the model text. For tool results, returns a brief summary.
func extractStepContent(step *pb.StepUpdate) string {
	// Model text response
	if step.Action == nil {
		return step.Text
	}

	switch a := step.Action.(type) {
	case *pb.StepUpdate_ViewFile:
		if a.ViewFile.Content != "" {
			return fmt.Sprintf("File Path: %s\nTotal Lines: %d\n%s",
				a.ViewFile.Path, a.ViewFile.TotalLines, truncate(a.ViewFile.Content, 1500))
		}
		return fmt.Sprintf("File Path: %s", a.ViewFile.Path)
	case *pb.StepUpdate_WriteToFile:
		if a.WriteToFile.Created {
			return fmt.Sprintf("Created file %s", a.WriteToFile.Path)
		}
		return fmt.Sprintf("Create file %s (overwrite=%v)", a.WriteToFile.Path, a.WriteToFile.Overwrite)
	case *pb.StepUpdate_ReplaceFileContent:
		if a.ReplaceFileContent.Success {
			return fmt.Sprintf("Edited %s\n%s", a.ReplaceFileContent.Path, truncate(a.ReplaceFileContent.DiffBlock, 1500))
		}
		return fmt.Sprintf("Edit %s", a.ReplaceFileContent.Path)
	case *pb.StepUpdate_ListDir:
		return fmt.Sprintf("Listed %s (%d entries)", a.ListDir.Path, len(a.ListDir.Entries))
	case *pb.StepUpdate_GrepSearch:
		return fmt.Sprintf("Searched %s for %q (%d matches)",
			a.GrepSearch.Path, a.GrepSearch.Query, a.GrepSearch.TotalMatches)
	case *pb.StepUpdate_FindFile:
		return fmt.Sprintf("Find %q (%d matches)", a.FindFile.Pattern, len(a.FindFile.Matches))
	case *pb.StepUpdate_RunCommand:
		rc := a.RunCommand
		parts := fmt.Sprintf("Command: %s\nExit code: %d", truncate(rc.Command, 200), rc.ExitCode)
		if rc.TaskId != "" {
			parts += fmt.Sprintf("\nTask ID: %s", rc.TaskId)
		}
		if rc.Stdout != "" {
			parts += fmt.Sprintf("\nStdout: %s", truncate(rc.Stdout, 500))
		}
		if rc.Stderr != "" {
			parts += fmt.Sprintf("\nStderr: %s", truncate(rc.Stderr, 500))
		}
		return parts
	case *pb.StepUpdate_SearchWeb:
		return fmt.Sprintf("Search: %q (%d results)", a.SearchWeb.Query, len(a.SearchWeb.Results))
	case *pb.StepUpdate_ReadUrlContent:
		return fmt.Sprintf("Fetch: %s", a.ReadUrlContent.Url)
	case *pb.StepUpdate_InvokeSubagent:
		return fmt.Sprintf("Subagent result (%d steps): %s",
			a.InvokeSubagent.StepsExecuted, truncate(a.InvokeSubagent.ResultText, 500))
	case *pb.StepUpdate_McpTool:
		return fmt.Sprintf("MCP %s.%s", a.McpTool.ServerName, a.McpTool.ToolName)
	case *pb.StepUpdate_HostToolCall:
		return fmt.Sprintf("Host tool: %s", a.HostToolCall.ToolName)
	}

	return ""
}

// truncate returns the first n characters of s, appending "…" if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Session) onTrajectory(state *pb.TrajectoryState) {
	if s.conv != nil {
		s.conv.LogStep(&conversation.TranscriptJSONEntry{
			StepIndex: -1,
			Source:    "SYSTEM",
			Type:      "TRAJECTORY_STATE",
			Status:    state.State.String(),
		})
	}

	s.sendServerMessage(&pb.ServerMessage{
		Payload: &pb.ServerMessage_TrajectoryState{
			TrajectoryState: state,
		},
	})
}

// ─── Wire helpers ───────────────────────────────────────────────────────

func (s *Session) sendServerMessage(msg *pb.ServerMessage) {
	if s.ringBuffer != nil {
		s.ringBuffer.Push(msg)
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		s.logger.Error("protobuf marshal error", "error", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil && !s.detached {
		if err := s.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			s.logger.Error("WebSocket write error", "error", err)
		}
	}
}

// SetDaemon sets the daemon mode flag.
func (s *Session) SetDaemon(d bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isDaemon = d
}

// Attach connects a new client WebSocket connection to an active session.
func (s *Session) Attach(conn *websocket.Conn) {
	s.mu.Lock()
	s.conn = conn
	s.detached = false

	// Replay ring buffer
	replayed := s.ringBuffer.All()
	for _, msg := range replayed {
		data, err := proto.Marshal(msg)
		if err == nil {
			_ = conn.WriteMessage(websocket.BinaryMessage, data)
		}
	}

	// Emit ReplayComplete
	replayComplete := &pb.ServerMessage{
		Payload: &pb.ServerMessage_ReplayComplete{
			ReplayComplete: &pb.ReplayComplete{
				EventCount: int32(len(replayed)),
			},
		},
	}
	data, _ := proto.Marshal(replayComplete)
	_ = conn.WriteMessage(websocket.BinaryMessage, data)

	// Emit any pending approvals from queue
	pendingList := s.approvalQueue.List()
	for _, p := range pendingList {
		permMsg := &pb.ServerMessage{
			Payload: &pb.ServerMessage_StepUpdate{
				StepUpdate: &pb.StepUpdate{
					State:  pb.StepUpdate_STATE_WAITING,
					Target: pb.StepUpdate_TARGET_USER,
					Action: &pb.StepUpdate_PermissionRequest{
						PermissionRequest: &pb.ActionPermissionRequest{
							RequestId:   p.RequestID,
							ToolName:    p.ToolName,
							ArgsSummary: p.Description,
							DiffPreview: p.DiffPreview,
						},
					},
				},
			},
		}
		pData, _ := proto.Marshal(permMsg)
		_ = conn.WriteMessage(websocket.BinaryMessage, pData)
	}
	s.mu.Unlock()

	// Launch reader for this attached client
	go func() {
		clientMsgs := make(chan *pb.ClientMessage, 10)
		go s.readLoop(clientMsgs)
		for msg := range clientMsgs {
			s.dispatchClientMessage(context.Background(), msg)
		}
		s.Detach()
	}()
}

// Detach disconnects the current client without stopping background execution.
func (s *Session) Detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detached = true
	s.conn = nil
	s.logger.Info("client detached, background execution continues")
}

func (s *Session) handleSetYoloMode(req *pb.SetYoloModeRequest) {
	s.yoloMode = req.Enabled
	s.logger.Info("yolo mode toggled", "enabled", s.yoloMode)
	if s.engine != nil {
		s.engine.SetYoloMode(s.yoloMode)
	}
	if s.yoloMode {
		for _, p := range s.approvalQueue.List() {
			s.approvalQueue.Resolve(p.RequestID, &pb.PermissionResponse{
				RequestId: p.RequestID,
				Approved:  true,
			})
		}
	}
}


func (s *Session) handleWorkspaceRequest(ctx context.Context, req *pb.WorkspaceRequest) {
	switch req.Action {
	case "list":
		var currentWorkspaces []*pb.Workspace
		if s.engine != nil {
			for _, dir := range s.engine.Workspaces() {
				currentWorkspaces = append(currentWorkspaces, &pb.Workspace{
					Directory: dir,
					Name:      filepath.Base(dir),
				})
			}
		}
		s.sendServerMessage(&pb.ServerMessage{
			Payload: &pb.ServerMessage_WorkspaceResponse{
				WorkspaceResponse: &pb.WorkspaceResponse{
					Success:    true,
					Message:    fmt.Sprintf("Loaded %d workspace(s)", len(currentWorkspaces)),
					Workspaces: currentWorkspaces,
				},
			},
		})

	case "add":
		if req.Path == "" {
			s.sendServerMessage(&pb.ServerMessage{
				Payload: &pb.ServerMessage_WorkspaceResponse{
					WorkspaceResponse: &pb.WorkspaceResponse{
						Success: false,
						Message: "workspace path is required",
					},
				},
			})
			return
		}
		absPath, err := filepath.Abs(req.Path)
		if err != nil {
			s.sendServerMessage(&pb.ServerMessage{
				Payload: &pb.ServerMessage_WorkspaceResponse{
					WorkspaceResponse: &pb.WorkspaceResponse{
						Success: false,
						Message: fmt.Sprintf("invalid workspace path: %v", err),
					},
				},
			})
			return
		}

		if err := s.wsMgr.AddWorkspace(absPath); err != nil {
			s.sendServerMessage(&pb.ServerMessage{
				Payload: &pb.ServerMessage_WorkspaceResponse{
					WorkspaceResponse: &pb.WorkspaceResponse{
						Success: false,
						Message: fmt.Sprintf("failed to add workspace: %v", err),
					},
				},
			})
			return
		}

		_ = config.AddTrustedWorkspace(absPath, s.logger)

		if s.engine != nil {
			s.engine.AddWorkspace(absPath, engine.WorkspaceInfo{
				Directory:  absPath,
				CorpusName: req.CorpusName,
			})
		}

		if s.conv != nil && s.conv.State != nil && s.conv.State.Config != nil {
			s.conv.State.Config.Workspaces = append(s.conv.State.Config.Workspaces, &pb.Workspace{
				Directory:  absPath,
				Name:       req.Name,
				CorpusName: req.CorpusName,
			})
			_ = s.conv.SaveAll()
		}

		var currentWorkspaces []*pb.Workspace
		if s.engine != nil {
			for _, dir := range s.engine.Workspaces() {
				currentWorkspaces = append(currentWorkspaces, &pb.Workspace{
					Directory: dir,
					Name:      filepath.Base(dir),
				})
			}
		}

		s.sendServerMessage(&pb.ServerMessage{
			Payload: &pb.ServerMessage_WorkspaceResponse{
				WorkspaceResponse: &pb.WorkspaceResponse{
					Success:    true,
					Message:    fmt.Sprintf("Added workspace %s", absPath),
					Workspaces: currentWorkspaces,
				},
			},
		})

	case "remove":
		if req.Path == "" {
			s.sendServerMessage(&pb.ServerMessage{
				Payload: &pb.ServerMessage_WorkspaceResponse{
					WorkspaceResponse: &pb.WorkspaceResponse{
						Success: false,
						Message: "workspace path is required",
					},
				},
			})
			return
		}
		absPath, err := filepath.Abs(req.Path)
		if err != nil {
			absPath = req.Path
		}

		if err := s.wsMgr.RemoveWorkspace(absPath); err != nil {
			s.sendServerMessage(&pb.ServerMessage{
				Payload: &pb.ServerMessage_WorkspaceResponse{
					WorkspaceResponse: &pb.WorkspaceResponse{
						Success: false,
						Message: fmt.Sprintf("failed to remove workspace: %v", err),
					},
				},
			})
			return
		}

		if s.engine != nil {
			s.engine.RemoveWorkspace(absPath)
		}

		if s.conv != nil && s.conv.State != nil && s.conv.State.Config != nil {
			var updated []*pb.Workspace
			for _, ws := range s.conv.State.Config.Workspaces {
				if ws.Directory != absPath {
					updated = append(updated, ws)
				}
			}
			s.conv.State.Config.Workspaces = updated
			_ = s.conv.SaveAll()
		}


		var currentWorkspaces []*pb.Workspace
		if s.engine != nil {
			for _, dir := range s.engine.Workspaces() {
				currentWorkspaces = append(currentWorkspaces, &pb.Workspace{
					Directory: dir,
					Name:      filepath.Base(dir),
				})
			}
		}

		s.sendServerMessage(&pb.ServerMessage{
			Payload: &pb.ServerMessage_WorkspaceResponse{
				WorkspaceResponse: &pb.WorkspaceResponse{
					Success:    true,
					Message:    fmt.Sprintf("Removed workspace %s", absPath),
					Workspaces: currentWorkspaces,
				},
			},
		})
	}
}

func (s *Session) sendError(code, message string, fatal bool) {
	s.sendServerMessage(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Error{
			Error: &pb.ErrorEvent{
				Code:    code,
				Message: message,
				Fatal:   fatal,
			},
		},
	})
}

// sendStructuredError sends a structured HarnessError as a protobuf ErrorEvent
func (s *Session) sendStructuredError(err error, fatal bool) {
	var hErr *errors.HarnessError
	if errors.As(err, &hErr) {
		// Convert HarnessError to protobuf ErrorEvent with metadata
		s.sendServerMessage(&pb.ServerMessage{
			Payload: &pb.ServerMessage_Error{
				Error: hErr.ToProto(),
			},
		})
	} else {
		// Fallback to legacy error format for non-structured errors
		s.sendError("UNKNOWN_ERROR", err.Error(), fatal)
	}
}

func mapProtoMessageToLLM(msg *pb.ConversationMessage) llm.Message {
	llmMsg := llm.Message{
		Role:    msg.Role,
		Content: msg.Content,
		Parts:   msg.Parts,
	}
	if len(msg.ToolCalls) > 0 {
		llmMsg.ToolCalls = make([]llm.ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			var args map[string]interface{}
			if tc.ArgsJson != "" {
				_ = json.Unmarshal([]byte(tc.ArgsJson), &args)
			}
			llmMsg.ToolCalls[i] = llm.ToolCall{
				ID:   tc.CallId,
				Name: tc.Name,
				Args: args,
			}
		}
	}
	if msg.ToolResult != nil {
		llmMsg.ToolResult = &llm.ToolCallResult{
			CallID:  msg.ToolResult.CallId,
			Name:    msg.ToolResult.Name,
			Content: msg.ToolResult.Content,
			IsError: msg.ToolResult.IsError,
		}
	}
	return llmMsg
}

func mapLLMMessageToProto(msg llm.Message) *pb.ConversationMessage {
	protoMsg := &pb.ConversationMessage{
		Role:    msg.Role,
		Content: msg.Content,
		Parts:   msg.Parts,
	}
	if len(msg.ToolCalls) > 0 {
		protoMsg.ToolCalls = make([]*pb.ToolCallRecord, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			argsBytes, _ := json.Marshal(tc.Args)
			protoMsg.ToolCalls[i] = &pb.ToolCallRecord{
				CallId:   tc.ID,
				Name:     tc.Name,
				ArgsJson: string(argsBytes),
			}
		}
	}
	if msg.ToolResult != nil {
		protoMsg.ToolResult = &pb.ToolResultRecord{
			CallId:  msg.ToolResult.CallID,
			Name:    msg.ToolResult.Name,
			Content: msg.ToolResult.Content,
			IsError: msg.ToolResult.IsError,
		}
	}
	return protoMsg
}

// protoSlashCommandsToEngine converts proto SlashCommandDef to engine SlashCommandDef.
func protoSlashCommandsToEngine(defs []*pb.SlashCommandDef) []engine.SlashCommandDef {
	if len(defs) == 0 {
		return nil
	}
	result := make([]engine.SlashCommandDef, len(defs))
	for i, d := range defs {
		result[i] = engine.SlashCommandDef{
			Name:        d.Name,
			Description: d.Description,
		}
	}
	return result
}

// protoSkillsToEngine converts proto SkillDef to engine SkillDef.
func protoSkillsToEngine(defs []*pb.SkillDef) []engine.SkillDef {
	if len(defs) == 0 {
		return nil
	}
	result := make([]engine.SkillDef, len(defs))
	for i, d := range defs {
		result[i] = engine.SkillDef{
			Name:        d.Name,
			Description: d.Description,
			SkillPath:   d.SkillPath,
		}
	}
	return result
}

// protoPluginsToEngine converts proto PluginDef to engine PluginDef.
func protoPluginsToEngine(defs []*pb.PluginDef) []engine.PluginDef {
	if len(defs) == 0 {
		return nil
	}
	result := make([]engine.PluginDef, len(defs))
	for i, d := range defs {
		result[i] = engine.PluginDef{
			Name:        d.Name,
			Description: d.Description,
			Path:        d.Path,
			Skills:      protoSkillsToEngine(d.Skills),
		}
	}
	return result
}

// resolveCompactionThreshold converts the SDK-provided threshold into the
// engine value using the convention:
//
//	0  → use DefaultCompactionThreshold (100K tokens)
//	-1 → disable compaction (engine value 0)
//	>0 → use the explicit value as-is
func resolveCompactionThreshold(adkValue int) int {
	switch {
	case adkValue < 0:
		return 0 // Disabled — engine treats 0 as "no compaction"
	case adkValue == 0:
		return config.DefaultCompactionThreshold
	default:
		return adkValue
	}
}
