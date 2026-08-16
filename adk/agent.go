package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/adk/connection"
	"github.com/divmora/localharness/adk/hooks"
	"github.com/divmora/localharness/adk/middleware"
	"github.com/divmora/localharness/adk/policy"
)

// Agent is the high-level API for interacting with a localharness agent.
//
// Usage:
//
//	cfg := adk.NewLocalAgentConfig()
//	cfg.LitellmAPIKey = os.Getenv("LITELLM_API_KEY")
//
//	agent, err := adk.NewAgent(cfg)
//	if err != nil { ... }
//	defer agent.Close()
//
//	if err := agent.Start(ctx); err != nil { ... }
//
//	resp, err := agent.Chat(ctx, "List the files in the current directory")
//	fmt.Println(resp.Text)
type Agent struct {
	config         *LocalAgentConfig
	hookRunner     *hooks.HookRunner
	mwChain        *middleware.Chain
	conn           connection.Connection
	logger         *slog.Logger
	started        bool
	totalTokens    int
	seenUsageSteps map[int32]bool           // tracks step indices whose Usage has been counted
	toolHandlers   map[string]ToolHandlerFunc // host tool name → handler
}

// NewAgent creates a new Agent with the given configuration.
// Call Start() to launch the harness binary and establish the connection.
func NewAgent(config *LocalAgentConfig) (*Agent, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize Logger with level control
	logger := config.Logger
	if logger == nil {
		level := slog.LevelInfo
		if config.Verbose || os.Getenv("LOCALHARNESS_VERBOSE") == "true" || os.Getenv("LOCALHARNESS_LOG_LEVEL") == "debug" {
			level = slog.LevelDebug
		}
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	}

	runner := hooks.NewHookRunner()

	// Register user-provided hooks
	for _, h := range config.Hooks {
		if err := runner.RegisterHook(h); err != nil {
			return nil, fmt.Errorf("register hook: %w", err)
		}
	}

	// Apply policies: workspace_only auto-prepended, then user policies.
	// Note: appDataDir is NOT added to allowed paths here because the engine
	// auto-approves paths inside appDataDir via isAppDataDirPath() and never
	// sends permission requests for them to the SDK.
	activePolicies := config.Policies
	if len(config.Workspaces) > 0 {
		wsDirs := workspaceDirs(config.Workspaces)
		wsPolicies := policy.WorkspaceOnly(wsDirs)
		activePolicies = append(wsPolicies, activePolicies...)
	}

	if len(activePolicies) > 0 {
		policyHook, err := policy.Enforce(activePolicies, policy.WithLogger(logger))
		if err != nil {
			return nil, fmt.Errorf("policy enforcement: %w", err)
		}
		runner.RegisterHook(policyHook)
	}

	// Build middleware chain
	mwChain := middleware.NewChain(logger, config.Middlewares...)

	// Build host tool handler map
	toolHandlers := make(map[string]ToolHandlerFunc, len(config.HostTools))
	for _, ht := range config.HostTools {
		toolHandlers[ht.Name] = ht.Handler
	}

	return &Agent{
		config:         config,
		hookRunner:     runner,
		mwChain:        mwChain,
		logger:         logger,
		seenUsageSteps: make(map[int32]bool),
		toolHandlers:   toolHandlers,
	}, nil
}

// Start launches the localharness binary and establishes the WebSocket connection.
// This must be called before Chat().
func (a *Agent) Start(ctx context.Context) error {
	if a.started {
		return fmt.Errorf("agent already started")
	}

	a.logger.Info("starting localharness agent",
		"binary", a.config.BinaryPath,
		"workspaces", workspaceDirs(a.config.Workspaces),
	)

	// 1. Build HarnessConfig protobuf payload
	harnessCfg := buildHarnessConfig(a.config)

	// 2. Instantiate and start local connection layer (pipe-based handshake)
	localCfg := connection.LocalConfig{
		BinaryPath: a.config.BinaryPath,
		Workspaces: workspaceDirs(a.config.Workspaces),
		Config:     harnessCfg,
		Debug:      a.config.Verbose,
	}

	conn, err := connection.NewLocalConnection(ctx, localCfg, a.logger)
	if err != nil {
		a.logger.Error("failed to start agent connection", "error", err)
		return fmt.Errorf("failed to establish local connection: %w", err)
	}

	a.conn = conn
	a.started = true
	a.hookRunner.DispatchSessionStart()
	a.logger.Debug("agent session started successfully", "conversation_id", conn.ConversationID())
	return nil
}

// Chat sends a prompt and returns the agent's response after the full turn completes.
//
// This is a convenience wrapper around ChatStream() that blocks until done.
// Use ChatStream() for real-time step delivery.
func (a *Agent) Chat(ctx context.Context, prompt string) (*ChatResponse, error) {
	return a.ChatWithContext(ctx, prompt, nil)
}

// ChatWithContext sends a prompt with additional host context and blocks until done.
// The MessageContext (active file, cursor, open files) is injected into the user message
// so the LLM has awareness of the user's current state.
func (a *Agent) ChatWithContext(ctx context.Context, prompt string, msgCtx *MessageContext) (*ChatResponse, error) {
	events, err := a.ChatStreamWithContext(ctx, prompt, msgCtx)
	if err != nil {
		return nil, err
	}

	var resp *ChatResponse
	var lastErr error
	for event := range events {
		if event.Type == EventTurnComplete {
			resp = event.Response
			lastErr = event.Error
		}
	}

	if lastErr != nil {
		// Enhance error with structured information if available
		if resp != nil && len(resp.Steps) > 0 {
			// Check if there's an error step with structured error information
			for _, step := range resp.Steps {
				if step.State == StateError && step.ErrorCode != "" {
					a.logger.Error("agent chat completed with structured error",
						"error_code", step.ErrorCode,
						"error_message", step.ErrorMessage,
						"error_metadata", step.ErrorMetadata)
					// Return enhanced error with code and context
					return resp, fmt.Errorf("agent error [%s]: %s", step.ErrorCode, step.ErrorMessage)
				}
			}
		}
		return resp, lastErr
	}
	if resp == nil {
		return nil, fmt.Errorf("stream ended without TurnComplete event")
	}
	return resp, nil
}

// ChatStream sends a prompt and returns a channel that emits StreamEvent values
// in real-time as the agentic loop progresses. The channel is closed when the
// turn completes.
//
// The final event is always EventTurnComplete with the full ChatResponse.
// If the turn ends due to an error, EventTurnComplete.Error is set and
// Response may be nil.
//
// Usage:
//
//	events, err := agent.ChatStream(ctx, "build the project")
//	if err != nil { ... }
//
//	for event := range events {
//	    switch event.Type {
//	    case adk.EventTextDelta:
//	        fmt.Print(event.TextDelta)
//	    case adk.EventToolCallStart:
//	        fmt.Printf("\n🔧 %s\n", event.Step.ToolName)
//	    case adk.EventToolCallDone:
//	        fmt.Printf("   ✅ done\n")
//	    case adk.EventError:
//	        fmt.Printf("   ❌ %s\n", event.Step.ErrorMessage)
//	    case adk.EventTurnComplete:
//	        fmt.Printf("\nDone! Steps: %d\n", len(event.Response.Steps))
//	    }
//	}
func (a *Agent) ChatStream(ctx context.Context, prompt string) (<-chan StreamEvent, error) {
	return a.ChatStreamWithContext(ctx, prompt, nil)
}

// ChatStreamWithContext sends a prompt with host context and returns a streaming event channel.
func (a *Agent) ChatStreamWithContext(ctx context.Context, prompt string, msgCtx *MessageContext) (<-chan StreamEvent, error) {
	if !a.started {
		return nil, fmt.Errorf("agent not started — call Start() first")
	}
	if a.conn == nil {
		return nil, fmt.Errorf("no connection established")
	}

	// Enforce session token budget
	if a.config.MaxTotalTokens > 0 && a.totalTokens >= a.config.MaxTotalTokens {
		return nil, fmt.Errorf("session token budget exceeded: %d >= %d", a.totalTokens, a.config.MaxTotalTokens)
	}

	// --- Middleware: PreTurn ---
	actualPrompt := prompt
	if a.mwChain.HasPreTurn() {
		req := &middleware.TurnRequest{
			Prompt:   prompt,
			Metadata: make(map[string]any),
		}
		result, err := a.mwChain.RunPreTurn(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("middleware PreTurn: %w", err)
		}
		actualPrompt = result.Prompt
	}

	a.logger.Debug("sending chat prompt to localharness", "prompt_len", len(actualPrompt))

	// Pre-turn hook check
	turnResult := a.hookRunner.DispatchPreTurn(actualPrompt)
	if !turnResult.Allow {
		a.logger.Warn("chat turn denied by hook", "reason", turnResult.Message)
		return nil, fmt.Errorf("turn denied by hook: %s", turnResult.Message)
	}

	// Send prompt (with optional message context and ephemeral messages)
	var ephemeralMsgs []string
	if msgCtx != nil {
		ephemeralMsgs = msgCtx.EphemeralMessages
	}
	if err := a.conn.SendWithContext(ctx, actualPrompt, messageContextToProto(msgCtx), ephemeralMsgs); err != nil {
		return nil, fmt.Errorf("send prompt: %w", err)
	}

	// Receive and process steps
	stepsCh, err := a.conn.ReceiveSteps(ctx)
	if err != nil {
		return nil, fmt.Errorf("receive steps: %w", err)
	}

	// Create the event channel (buffered to avoid blocking the processing goroutine)
	eventCh := make(chan StreamEvent, 32)

	go a.driveStream(ctx, stepsCh, eventCh)

	return eventCh, nil
}

// driveStream reads steps from the connection, processes them (policies, hooks),
// emits StreamEvent values, and sends the final TurnComplete event.
func (a *Agent) driveStream(ctx context.Context, stepsCh <-chan connection.Step, eventCh chan<- StreamEvent) {
	defer close(eventCh)

	var response ChatResponse

	// Accumulate streaming text/thinking deltas as fallback for response.Text.
	// When the model streams its final response via TextDelta fragments,
	// the IsFinal step may have an empty Text field. We use the accumulated
	// deltas to ensure response.Text is always populated.
	var accumulatedText strings.Builder
	var accumulatedThinking strings.Builder

	for step := range stepsCh {
		a.logger.Debug("received step update",
			"index", step.Index,
			"state", step.State,
			"source", step.Source,
			"tool", step.ToolName,
		)

		processedStep := a.processStep(ctx, step)
		response.Steps = append(response.Steps, processedStep)

		// Emit streaming events based on step type
		a.emitStreamEvents(eventCh, step, &processedStep)

		// Accumulate streaming text deltas for fallback
		if step.TextDelta != "" {
			accumulatedText.WriteString(step.TextDelta)
		}
		if step.ThinkingDelta != "" {
			accumulatedThinking.WriteString(step.ThinkingDelta)
		}

		// Accumulate final response from explicit IsFinal step
		if step.IsFinal && step.Text != "" {
			response.Text = step.Text
			response.Thinking = step.Thinking
		}

		if step.Usage != nil {
			response.Usage = &UsageMetadata{
				PromptTokens:     step.Usage.PromptTokens,
				CompletionTokens: step.Usage.CompletionTokens,
				ThinkingTokens:   step.Usage.ThinkingTokens,
				TotalTokens:      step.Usage.TotalTokens,
				CachedTokens:     step.Usage.CachedTokens,
			}
			// Deduplicate: the harness may send Usage on multiple sub-step
			// updates for the same API call (Active → Permission → Done).
			// Only count each step index once.
			if !a.seenUsageSteps[step.Index] {
				a.seenUsageSteps[step.Index] = true
				a.totalTokens += step.Usage.TotalTokens
				a.logger.Debug("accumulated token usage",
					"step_index", step.Index,
					"step_tokens", step.Usage.TotalTokens,
					"prompt_tokens", step.Usage.PromptTokens,
					"completion_tokens", step.Usage.CompletionTokens,
					"cached_tokens", step.Usage.CachedTokens,
					"cumulative_tokens", a.totalTokens,
				)
			}
		}
	}

	// Fallback: if no IsFinal step contained text, use accumulated deltas.
	// This handles the case where the model's final response was delivered
	// entirely via streaming TextDelta fragments.
	if response.Text == "" && accumulatedText.Len() > 0 {
		response.Text = accumulatedText.String()
	}
	if response.Thinking == "" && accumulatedThinking.Len() > 0 {
		response.Thinking = accumulatedThinking.String()
	}

	a.logger.Info("chat turn completed",
		"steps", len(response.Steps),
		"cumulative_tokens", a.totalTokens,
	)

	// Post-turn hook
	a.hookRunner.DispatchPostTurn(response.Text)

	// --- Middleware: PostTurn ---
	if a.mwChain.HasPostTurn() {
		turnTokens := 0
		if response.Usage != nil {
			turnTokens = response.Usage.TotalTokens
		}
		mwResp := &middleware.TurnResponse{
			Text:        response.Text,
			Thinking:    response.Thinking,
			TotalTokens: turnTokens,
			StepCount:   len(response.Steps),
			Metadata:    make(map[string]any),
		}
		result, err := a.mwChain.RunPostTurn(ctx, mwResp)
		if err != nil {
			a.logger.Warn("PostTurn middleware error", "error", err)
			// Emit the error as the final event
			eventCh <- StreamEvent{
				Type:  EventTurnComplete,
				Error: err,
			}
			return
		}
		// Apply any response transformations
		if result.Text != "" {
			response.Text = result.Text
		}
		if result.Thinking != "" {
			response.Thinking = result.Thinking
		}
	}

	// Final event
	eventCh <- StreamEvent{
		Type:     EventTurnComplete,
		Response: &response,
	}
}

// emitStreamEvents maps a raw connection Step to typed StreamEvent values.
func (a *Agent) emitStreamEvents(ch chan<- StreamEvent, raw connection.Step, processed *Step) {
	// Streaming text deltas
	if raw.TextDelta != "" {
		a.emitEvent(ch, StreamEvent{
			Type:      EventTextDelta,
			TextDelta: raw.TextDelta,
		}, raw)
	}

	// Streaming thinking deltas
	if raw.ThinkingDelta != "" {
		a.emitEvent(ch, StreamEvent{
			Type:          EventThinkingDelta,
			ThinkingDelta: raw.ThinkingDelta,
		}, raw)
	}

	// Tool call lifecycle
	if raw.ToolName != "" {
		switch connection.StepState(raw.State) {
		case connection.StateActive:
			a.emitEvent(ch, StreamEvent{
				Type: EventToolCallStart,
				Step: processed,
			}, raw)
		case connection.StateDone:
			a.emitEvent(ch, StreamEvent{
				Type: EventToolCallDone,
				Step: processed,
			}, raw)
			// Dispatch PostToolCall hook
			a.hookRunner.DispatchPostToolCall(hooks.ToolResult{
				Name:    raw.ToolName,
				Content: raw.ToolResultContent,
				IsError: false,
				CallID:  raw.ToolArgsJSON, // best available ID
			})
		case connection.StateError:
			a.logger.Error("tool execution error",
				"tool", raw.ToolName,
				"error_code", raw.ErrorCode,
				"error_message", raw.ErrorMessage,
				"error_metadata", raw.ErrorMetadata,
				"step_index", raw.Index)
			a.emitEvent(ch, StreamEvent{
				Type: EventError,
				Step: processed,
			}, raw)
			// Dispatch ToolError hook — may provide recovery
			errResult := a.hookRunner.DispatchToolError(hooks.ToolError{
				ToolName:      raw.ToolName,
				Error:         fmt.Errorf("%s", raw.ErrorMessage),
				Args:          raw.ToolArgs,
				ErrorCode:     raw.ErrorCode,
				ErrorMetadata: raw.ErrorMetadata,
			})
			if errResult.Handled {
				a.logger.Info("tool error recovered by hook",
					"tool", raw.ToolName,
					"recovery_content", errResult.RecoveryContent,
				)
			}
		}
	} else if raw.State == connection.StateError {
		// Non-tool errors (e.g., model errors)
		a.logger.Error("non-tool error",
			"error_code", raw.ErrorCode,
			"error_message", raw.ErrorMessage,
			"error_metadata", raw.ErrorMetadata,
			"step_index", raw.Index)
		a.emitEvent(ch, StreamEvent{
			Type: EventError,
			Step: processed,
		}, raw)
	}

	// Compaction event
	if raw.IsCompaction() {
		a.emitEvent(ch, StreamEvent{
			Type: EventCompaction,
			Step: processed,
		}, raw)
		// Dispatch CompactionHook
		a.hookRunner.DispatchCompaction(hooks.CompactionEvent{
			OriginalTokens:  raw.CompactionOriginalTokens,
			CompactedTokens: raw.CompactionCompactedTokens,
			MessagesRemoved: raw.CompactionMessagesRemoved,
			Summary:         raw.CompactionSummary,
		})
	}
}

// emitEvent sends a StreamEvent through the StepMiddleware chain, then to the
// channel. If any middleware sets ShouldFilter, the event is suppressed.
func (a *Agent) emitEvent(ch chan<- StreamEvent, event StreamEvent, raw connection.Step) {
	if !a.mwChain.HasStep() {
		ch <- event
		return
	}

	// Build StepEvent for middleware
	toolState := ""
	if raw.ToolName != "" {
		switch connection.StepState(raw.State) {
		case connection.StateActive:
			toolState = "active"
		case connection.StateDone:
			toolState = "done"
		case connection.StateError:
			toolState = "error"
		}
	}

	stepEvent := &middleware.StepEvent{
		EventType:     int(event.Type),
		TextDelta:     event.TextDelta,
		ThinkingDelta: event.ThinkingDelta,
		ToolName:      raw.ToolName,
		ToolState:     toolState,
		ErrorMessage:  raw.ErrorMessage,
		Metadata: map[string]any{
			"tool_args_json": raw.ToolArgsJSON,
			"step_index":     raw.Index,
		},
	}

	result, err := a.mwChain.RunStep(context.Background(), stepEvent)
	if err != nil {
		a.logger.Warn("StepMiddleware error", "error", err)
		// Still emit the original event on middleware error
		ch <- event
		return
	}

	if result.ShouldFilter {
		return // Middleware suppressed this event
	}

	ch <- event
}

// processStep handles a single step from the harness.
// For tool calls, it evaluates policies. For permission requests from the engine,
// it responds based on its policy evaluation.
func (a *Agent) processStep(ctx context.Context, step connection.Step) Step {
	result := Step{
		Index:        step.Index,
		Text:         step.Text,
		ToolName:     step.ToolName,
		State:        StepState(step.State),
		Source:       StepSource(step.Source),
		ErrorMessage: step.ErrorMessage,
	}

	// Parse tool args if present
	if step.ToolArgsJSON != "" {
		var args map[string]any
		if err := json.Unmarshal([]byte(step.ToolArgsJSON), &args); err == nil {
			result.ToolArgs = args
		}
	}

	// Handle permission requests from the engine
	if step.IsPermissionRequest() {
		a.handlePermissionRequest(ctx, step)
	}

	// Handle question requests from the engine
	if step.IsQuestionRequest() {
		a.handleQuestionRequest(ctx, step)
	}

	// Handle host tool calls — auto-dispatch to registered handlers
	if step.IsHostToolCall && step.State == connection.StateWaiting {
		a.handleHostToolCall(ctx, step)
	}

	return result
}

// handlePermissionRequest evaluates policies for a tool call that the engine
// is asking about, and sends the response back.
func (a *Agent) handlePermissionRequest(ctx context.Context, step connection.Step) {
	// Parse tool args for policy evaluation
	var args map[string]any
	if step.ToolArgsJSON != "" {
		json.Unmarshal([]byte(step.ToolArgsJSON), &args)
	}

	// Evaluate policies
	tc := hooks.ToolCall{
		Name:     step.PermissionToolName,
		Args:     args,
		ArgsJSON: step.ToolArgsJSON,
	}

	hookResult := a.hookRunner.DispatchPreToolCall(tc)

	// Send response
	if err := a.conn.SendPermissionResponse(
		ctx,
		step.PermissionRequestID,
		hookResult.Allow,
		hookResult.Message,
	); err != nil {
		a.logger.Error("failed to send permission response",
			"request_id", step.PermissionRequestID,
			"error", err,
		)
	}
}

// handleQuestionRequest processes a question from the engine.
// If the user registered a QuestionHandler, it is called to present the questions.
// Otherwise, the question is auto-skipped.
func (a *Agent) handleQuestionRequest(ctx context.Context, step connection.Step) {
	var answers []*connection.QuestionAnswer
	skipped := true

	if a.config.QuestionHandler != nil {
		// Convert connection questions to SDK questions
		questions := make([]Question, len(step.Questions))
		for i, q := range step.Questions {
			questions[i] = Question{
				Text:          q.Question,
				Options:       q.Options,
				IsMultiSelect: q.IsMultiSelect,
			}
		}

		// Call user's handler
		resp, err := a.config.QuestionHandler(ctx, questions)
		if err != nil {
			a.logger.Error("question handler error", "error", err)
		} else if resp != nil {
			skipped = resp.Skipped
			answers = make([]*connection.QuestionAnswer, len(resp.Answers))
			for i, ans := range resp.Answers {
				answers[i] = &connection.QuestionAnswer{
					SelectedIndices: ans.SelectedIndices,
					SelectedOptions: ans.SelectedOptions,
					Text:            ans.Text,
				}
			}
		}
	}

	if err := a.conn.SendQuestionResponse(
		ctx,
		step.QuestionRequestID,
		answers,
		skipped,
	); err != nil {
		a.logger.Error("failed to send question response",
			"request_id", step.QuestionRequestID,
			"error", err,
		)
	}
}

// handleHostToolCall dispatches a host tool call to the registered handler
// and sends the result back to the harness.
func (a *Agent) handleHostToolCall(ctx context.Context, step connection.Step) {
	stepID := fmt.Sprintf("%d", step.Index)
	toolName := step.ToolName

	handler, ok := a.toolHandlers[toolName]
	if !ok {
		a.logger.Error("no handler registered for host tool", "tool", toolName)
		a.conn.SendToolResult(ctx, stepID, toolName,
			fmt.Sprintf(`{"error": "no handler registered for tool %q"}`, toolName), true)
		return
	}

	// Parse tool arguments
	var args map[string]any
	if step.ToolArgsJSON != "" {
		if err := json.Unmarshal([]byte(step.ToolArgsJSON), &args); err != nil {
			a.logger.Error("failed to parse host tool args", "tool", toolName, "error", err)
			a.conn.SendToolResult(ctx, stepID, toolName,
				fmt.Sprintf(`{"error": "failed to parse arguments: %s"}`, err), true)
			return
		}
	}

	// Call the handler
	result, err := handler(ctx, args)
	if err != nil {
		a.logger.Warn("host tool handler returned error", "tool", toolName, "error", err)
		a.conn.SendToolResult(ctx, stepID, toolName,
			fmt.Sprintf(`{"error": %q}`, err.Error()), true)
		return
	}

	// Marshal the result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		a.logger.Error("failed to marshal host tool result", "tool", toolName, "error", err)
		a.conn.SendToolResult(ctx, stepID, toolName,
			fmt.Sprintf(`{"error": "failed to marshal result: %s"}`, err), true)
		return
	}

	if err := a.conn.SendToolResult(ctx, stepID, toolName, string(resultJSON), false); err != nil {
		a.logger.Error("failed to send host tool result",
			"tool", toolName,
			"error", err,
		)
	}
}

func (a *Agent) Close() error {
	a.logger.Debug("closing agent session")
	if a.started {
		a.hookRunner.DispatchSessionEnd()
		a.started = false
	}
	if a.conn != nil {
		return a.conn.Close()
	}
	return nil
}

// IsStarted returns true if the agent session has been started.
func (a *Agent) IsStarted() bool {
	return a.started
}

// ConversationID returns the active conversation identifier.
// Returns empty string before the session is started.
func (a *Agent) ConversationID() string {
	if a.conn != nil {
		return a.conn.ConversationID()
	}
	return ""
}

// FetchAgentCard retrieves the A2A agent card from the running harness.
// The agent card is a machine-readable discovery document that describes
// the agent's capabilities, skills, and interaction requirements.
// Requires the agent to be started.
func (a *Agent) FetchAgentCard(ctx context.Context) (*AgentCardInfo, error) {
	if a.conn == nil {
		return nil, fmt.Errorf("agent not started — call Start() first")
	}
	card, err := a.conn.FetchAgentCard(ctx)
	if err != nil {
		return nil, err
	}
	// Map connection types to SDK types
	info := &AgentCardInfo{
		Name:               card.Name,
		Description:        card.Description,
		Version:            card.Version,
		URL:                card.URL,
		DocumentationURL:   card.DocumentationURL,
		DefaultInputModes:  card.DefaultInputModes,
		DefaultOutputModes: card.DefaultOutputModes,
		Capabilities: AgentCardCapabilities{
			Streaming:              card.Capabilities.Streaming,
			PushNotifications:      card.Capabilities.PushNotifications,
			StateTransitionHistory: card.Capabilities.StateTransitionHistory,
			A2AVersion:             card.Capabilities.A2AVersion,
		},
	}
	if card.Provider != nil {
		info.Provider = &AgentCardProviderInfo{
			Name: card.Provider.Name,
			URL:  card.Provider.URL,
		}
	}
	for _, s := range card.Skills {
		info.Skills = append(info.Skills, AgentCardSkillInfo{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Tags:        s.Tags,
			Examples:    s.Examples,
		})
	}
	return info, nil
}

// buildHarnessConfig translates LocalAgentConfig into a pb.HarnessConfig.
func buildHarnessConfig(cfg *LocalAgentConfig) *pb.HarnessConfig {
	builtin := &pb.BuiltinToolsConfig{
		ViewFile:       cfg.Capabilities.ViewFile,
		CreateFile:     cfg.Capabilities.CreateFile,
		EditFile:       cfg.Capabilities.EditFile,
		ListDir:        cfg.Capabilities.ListDir,
		SearchDir:      cfg.Capabilities.SearchDir,
		FindFile:       cfg.Capabilities.FindFile,
		RunCommand:     cfg.Capabilities.RunCommand,
		Finish:         cfg.Capabilities.Finish,
		ManageTask:     cfg.Capabilities.ManageTask,
		InvokeSubagent: cfg.Capabilities.InvokeSubagent,
		WebSearch:      cfg.Capabilities.WebSearch,
		WebFetch:       cfg.Capabilities.WebFetch,
		Schedule:       cfg.Capabilities.Schedule,
		Browser:        cfg.Capabilities.Browser,
	}

	harnessCfg := &pb.HarnessConfig{
		SystemInstructions:     cfg.SystemInstructions,
		StructuredInstructions: structuredPromptToProto(cfg.StructuredPrompt),
		BuiltinTools:           builtin,
		ConversationId:         cfg.ConversationID,
		CompactionThreshold:    int32(cfg.CompactionThreshold),
		MaxSubagentDepth:       int32(cfg.MaxSubagentDepth),
		MaxConcurrentSubagents: int32(cfg.MaxConcurrentSubagents),
		MaxAutoWakeTurns:       int32(cfg.MaxAutoWakeTurns),
		LitellmEndpoint:        cfg.LitellmEndpoint,
		LitellmApiKey:          cfg.LitellmAPIKey,
		LitellmBaseUrl:         cfg.LitellmBaseURL,
		LitellmModel:           cfg.LitellmModel,
	}

	// Prompt modules — only set if any module is enabled
	if cfg.EnablePlanningMode || cfg.EnableWebDevelopment || cfg.EnableSlashCommands || cfg.EnableKnowledgeItems {
		harnessCfg.PromptModules = &pb.PromptModules{
			EnablePlanning:       cfg.EnablePlanningMode,
			EnableWebDevelopment: cfg.EnableWebDevelopment,
			EnableSlashCommands:  cfg.EnableSlashCommands,
			EnableKnowledgeItems: cfg.EnableKnowledgeItems,
		}
	}

	// Host tools — SDK-registered custom tools forwarded to SDK handlers
	for _, ht := range cfg.HostTools {
		schemaJSON, _ := json.Marshal(ht.Parameters)
		harnessCfg.HostTools = append(harnessCfg.HostTools, &pb.ToolDef{
			Name:                 ht.Name,
			Description:          ht.Description,
			ParametersJsonSchema: string(schemaJSON),
		})
	}

	// Slash command definitions (sent alongside PromptModules)
	for _, cmd := range cfg.SlashCommands {
		harnessCfg.SlashCommands = append(harnessCfg.SlashCommands, &pb.SlashCommandDef{
			Name:        cmd.Name,
			Description: cmd.Description,
		})
	}

	// Skill definitions (data-driven — presence = enabled)
	for _, s := range cfg.Skills {
		harnessCfg.Skills = append(harnessCfg.Skills, &pb.SkillDef{
			Name:        s.Name,
			Description: s.Description,
			SkillPath:   s.SkillPath,
		})
	}

	// Plugin definitions (data-driven — presence = enabled)
	for _, p := range cfg.Plugins {
		pbPlugin := &pb.PluginDef{
			Name:        p.Name,
			Description: p.Description,
			Path:        p.Path,
		}
		for _, s := range p.Skills {
			pbPlugin.Skills = append(pbPlugin.Skills, &pb.SkillDef{
				Name:        s.Name,
				Description: s.Description,
				SkillPath:   s.SkillPath,
			})
		}
		harnessCfg.Plugins = append(harnessCfg.Plugins, pbPlugin)
	}

	// Subagent type definitions (data-driven — presence = enabled)
	for _, st := range cfg.SubagentTypes {
		harnessCfg.SubagentTypes = append(harnessCfg.SubagentTypes, &pb.SubagentTypeConfig{
			Name:                st.Name,
			Description:         st.Description,
			SystemPrompt:        st.SystemPrompt,
			EnableWriteTools:    st.EnableWriteTools,
			EnableMcpTools:      st.EnableMCPTools,
			EnableSubagentTools: st.EnableSubagentTools,
		})
	}

	// Built-in subagent exclusions
	harnessCfg.ExcludeBuiltinSubagents = cfg.ExcludeBuiltinSubagents

	// ADK-injected user rules
	for _, r := range cfg.UserRules {
		harnessCfg.UserRules = append(harnessCfg.UserRules, &pb.UserRuleConfig{
			Label:   r.Label,
			Content: r.Content,
		})
	}

	for _, ws := range cfg.Workspaces {
		harnessCfg.Workspaces = append(harnessCfg.Workspaces, &pb.Workspace{
			Directory:  ws.Directory,
			Name:       "workspace",
			CorpusName: ws.CorpusName,
		})
	}

	// Translate MCP server configs
	for _, mcp := range cfg.McpServers {
		server := &pb.McpServerConfig{
			Name:         mcp.Name,
			EnabledTools: mcp.Tools,
			Env:          mcp.Env,
		}
		if mcp.Command != "" {
			server.Transport = &pb.McpServerConfig_Stdio{
				Stdio: &pb.McpStdioTransport{
					Command: mcp.Command,
					Args:    mcp.Args,
				},
			}
		} else if mcp.URL != "" {
			server.Transport = &pb.McpServerConfig_Http{
				Http: &pb.McpHttpTransport{
					Url:     mcp.URL,
					Headers: mcp.Headers,
				},
			}
		}
		harnessCfg.McpServers = append(harnessCfg.McpServers, server)
	}

	harnessCfg.LitellmEndpoint = cfg.LitellmEndpoint
	if cfg.LitellmAPIKey != "" || cfg.LitellmBaseURL != "" || cfg.LitellmModel != "" {
		harnessCfg.LitellmApiKey = cfg.LitellmAPIKey
		harnessCfg.LitellmBaseUrl = cfg.LitellmBaseURL
		harnessCfg.LitellmModel = cfg.LitellmModel
	}

	return harnessCfg
}

// structuredPromptToProto translates SDK StructuredPrompt to proto.
func structuredPromptToProto(sp *StructuredPrompt) *pb.StructuredSystemInstructions {
	if sp == nil {
		return nil
	}

	proto := &pb.StructuredSystemInstructions{
		Identity:           sp.Identity,
		Guidelines:         sp.Guidelines,
		CommunicationStyle: sp.CommunicationStyle,
	}

	for _, s := range sp.Sections {
		proto.Sections = append(proto.Sections, &pb.SystemSection{
			Tag:      s.Tag,
			Content:  s.Content,
			Priority: int32(s.Priority),
		})
	}

	return proto
}

// messageContextToProto translates SDK MessageContext to proto UserContext.
func messageContextToProto(mc *MessageContext) *pb.UserContext {
	if mc == nil {
		return nil
	}

	ctx := &pb.UserContext{
		CursorLine: int32(mc.CursorLine),
		Extra:      mc.Extra,
	}

	if mc.ActiveFile != nil {
		ctx.ActiveFile = &pb.FileInfo{
			Path:     mc.ActiveFile.Path,
			Language: mc.ActiveFile.Language,
		}
	}

	for _, f := range mc.OpenFiles {
		ctx.OpenFiles = append(ctx.OpenFiles, &pb.FileInfo{
			Path:     f.Path,
			Language: f.Language,
		})
	}

	return ctx
}

// workspaceDirs extracts directory paths from WorkspaceDef slices.
// Used by policy enforcement and connection layers that only need path strings.
func workspaceDirs(defs []WorkspaceDef) []string {
	dirs := make([]string, len(defs))
	for i, ws := range defs {
		dirs[i] = ws.Directory
	}
	return dirs
}
