// Package engine — subagent tool implementations.
//
// Implements four engine-intercepted tools for the subagent system:
//
//   - define_subagent: Register a named subagent type for the conversation
//   - invoke_subagent: Launch one or more subagents concurrently in background
//   - manage_subagents: List active / kill specific / kill all subagents
//   - send_message: Send a message to another agent by conversation ID
//
// Key properties:
//   - Fresh context: child engine gets only the prompt, no parent history
//   - Shared resources: same LLM provider, workspace, permission handler
//   - Tool filtering: child tools filtered by SubagentTypeDef flags
//   - Async: invoke_subagent returns immediately, children run in goroutines
//   - Depth limited: prevents infinite recursion (default: 3 levels)
//   - Concurrency limited: prevents fork bombs (default: 5 concurrent children)
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/util"
)

const (
	defaultMaxDepth     = 3
	defaultMaxSubagents = 5
	subagentMaxTurns    = 30

	defaultSubagentSystemPrompt = `You are a focused coding assistant working on a specific subtask.
You have access to the same tools as the parent agent. Complete the task and provide a clear, concise summary of what you did and found.
Be thorough but efficient — your response will be fed back to the parent agent as context.`

	subagentInboxSize = 32 // Buffered channel capacity for inter-agent messages
)

// ═══════════════════════════════════════════════════════════════════════
// TOOL DECLARATIONS
// ═══════════════════════════════════════════════════════════════════════

// defineSubagentDeclaration returns the FunctionDeclaration for define_subagent.
func defineSubagentDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "define_subagent",
		Description: `Defines a new type of subagent that can be invoked via invoke_subagent.

	Guidelines:
	* Use this tool if you need a specialized subagent for a task and none of the existing subagents are suitable.
	* Once the subagent is defined, it can be invoked repeatedly using invoke_subagent without calling this tool again.
	* The subagent will be defined with the specified name, description, system prompt, and tool groups.
	* By default, all subagents have read tools to research the codebase, and tools to communicate with other agents.`,
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []string{"name", "description", "system_prompt"},
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Unique name for this subagent type. Used when invoking via invoke_subagent.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable description of what this subagent does.",
				},
				"system_prompt": map[string]interface{}{
					"type":        "string",
					"description": "System prompt for the subagent. Defines its role, capabilities, and behavior.",
				},
				"enable_write_tools": map[string]interface{}{
					"type":        "boolean",
					"description": "Set true to enable the subagent to create/edit files and run commands.",
				},
				"enable_mcp_tools": map[string]interface{}{
					"type":        "boolean",
					"description": "Set true to enable the subagent to call MCP tools.",
				},
				"enable_subagent_tools": map[string]interface{}{
					"type":        "boolean",
					"description": "Set true to enable the subagent to define and invoke its own subagents.",
				},
			},
		},
	}
}

// invokeSubagentDeclaration returns the FunctionDeclaration for invoke_subagent.
func invokeSubagentDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "invoke_subagent",
		Description: `Launch one or more subagents concurrently in the background. Each subagent runs in its own conversation context with a fresh history.

The tool returns immediately with conversation IDs for each launched subagent. You do NOT need to poll — the system will automatically notify you when a subagent completes or sends a message.

Use invoke_subagent when:
1. A task benefits from a clean context (no history pollution)
2. Work can be parallelized (e.g., research different areas)
3. Tasks that might fail without affecting the parent
4. Delegating specialized work (e.g., "write tests for X")`,
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []string{"Subagents"},
			"properties": map[string]interface{}{
				"Subagents": map[string]interface{}{
					"type":        "array",
					"description": "Array of subagents to launch concurrently.",
					"items": map[string]interface{}{
						"type":     "object",
						"required": []string{"TypeName", "Role", "Prompt"},
						"properties": map[string]interface{}{
							"TypeName": map[string]interface{}{
								"type":        "string",
								"description": "Name of the subagent type to invoke (from available subagents or define_subagent).",
							},
							"Role": map[string]interface{}{
								"type":        "string",
								"description": "Brief 2-5 word job title for this subagent instance (e.g., 'Codebase Researcher').",
							},
							"Prompt": map[string]interface{}{
								"type":        "string",
								"description": "Task description for the subagent. Must be specific and self-contained.",
							},
						},
					},
				},
			},
		},
	}
}

// manageSubagentsDeclaration returns the FunctionDeclaration for manage_subagents.
func manageSubagentsDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "manage_subagents",
		Description: `Manage background subagents. Use this tool to list running subagents or interact with them.

Actions:
- 'list': List all currently running subagents
- 'kill': Cancel specific subagents by conversation ID
- 'kill_all': Cancel all running subagents`,
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []string{"Action"},
			"properties": map[string]interface{}{
				"Action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"list", "kill", "kill_all"},
					"description": "The action to perform.",
				},
				"ConversationIds": map[string]interface{}{
					"type":        "array",
					"description": "Conversation IDs of subagents to kill. Required when Action is 'kill'.",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

// sendMessageDeclaration returns the FunctionDeclaration for send_message.
func sendMessageDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "send_message",
		Description: `Send a message to another agent by its conversation ID (returned by invoke_subagent). This tool is ONLY for communicating with other agents.

**Do NOT use send_message to communicate with the user.** Instead, output visible text to communicate with the user.`,
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []string{"Recipient", "Message"},
			"properties": map[string]interface{}{
				"Recipient": map[string]interface{}{
					"type":        "string",
					"description": "The conversation ID of the recipient agent.",
				},
				"Message": map[string]interface{}{
					"type":        "string",
					"description": "The message content to send.",
				},
			},
		},
	}
}

// subagentToolDeclarations returns all subagent tool declarations.
// Called from buildToolDeclarations() when subagents are enabled.
func subagentToolDeclarations() []llm.FunctionDeclaration {
	return []llm.FunctionDeclaration{
		defineSubagentDeclaration(),
		invokeSubagentDeclaration(),
		manageSubagentsDeclaration(),
		sendMessageDeclaration(),
	}
}

// ═══════════════════════════════════════════════════════════════════════
// TOOL EXECUTION — define_subagent
// ═══════════════════════════════════════════════════════════════════════

// executeDefineSubagent handles the define_subagent tool call.
func (e *Engine) executeDefineSubagent(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	name, _ := tc.Args["name"].(string)
	description, _ := tc.Args["description"].(string)
	systemPrompt, _ := tc.Args["system_prompt"].(string)
	enableWrite, _ := tc.Args["enable_write_tools"].(bool)
	enableMCP, _ := tc.Args["enable_mcp_tools"].(bool)
	enableSubagent, _ := tc.Args["enable_subagent_tools"].(bool)

	if name == "" {
		e.feedToolError(tc, step, "name is required for define_subagent")
		return nil
	}
	if description == "" {
		e.feedToolError(tc, step, "description is required for define_subagent")
		return nil
	}

	typeDef := SubagentTypeDef{
		Name:                name,
		Description:         description,
		SystemPrompt:        systemPrompt,
		EnableWriteTools:    enableWrite,
		EnableMCPTools:      enableMCP,
		EnableSubagentTools: enableSubagent,
	}

	if err := e.subagentRegistry.Define(typeDef); err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to define subagent: %v", err))
		return nil
	}

	// Populate step action
	if action := step.GetDefineSubagent(); action != nil {
		action.Name = name
		action.Description = description
		action.SystemPrompt = systemPrompt
		action.EnableWriteTools = enableWrite
		action.EnableMcpTools = enableMCP
		action.EnableSubagentTools = enableSubagent
	}

	e.logger.Info("subagent type defined",
		"name", name,
		"write_tools", enableWrite,
		"mcp_tools", enableMCP,
		"subagent_tools", enableSubagent,
	)

	// Feed success result
	resultMsg := fmt.Sprintf("Subagent type '%s' defined successfully. You can now invoke it using invoke_subagent with TypeName='%s'.", name, name)
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	e.history = append(e.history, toolResultMsg(tc, resultMsg, false))

	return nil
}

// ═══════════════════════════════════════════════════════════════════════
// TOOL EXECUTION — invoke_subagent (async, multi-launch)
// ═══════════════════════════════════════════════════════════════════════

// subagentInvocationArgs represents one subagent to launch.
type subagentInvocationArgs struct {
	TypeName string `json:"TypeName"`
	Role     string `json:"Role"`
	Prompt   string `json:"Prompt"`
	AllocatedBudget float64 `json:"AllocatedBudget,omitempty"`
}

// executeSubagent handles the invoke_subagent tool call.
// Launches subagents concurrently in background and returns immediately.
func (e *Engine) executeSubagent(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	// ── Depth check ──
	if e.depth >= e.maxDepth {
		errMsg := fmt.Sprintf("max subagent depth (%d) exceeded — cannot spawn further children", e.maxDepth)
		e.feedSubagentError(tc, step, errMsg)
		return nil // Not fatal to the parent — LLM can adapt
	}

	// ── Parse args ──
	// Support both new (Subagents array) and legacy (prompt string) formats
	var invocations []subagentInvocationArgs

	if subagentsRaw, ok := tc.Args["Subagents"]; ok {
		// New format: array of invocations
		subJSON, err := json.Marshal(subagentsRaw)
		if err != nil {
			e.feedSubagentError(tc, step, fmt.Sprintf("invalid Subagents format: %v", err))
			return nil
		}
		if err := json.Unmarshal(subJSON, &invocations); err != nil {
			e.feedSubagentError(tc, step, fmt.Sprintf("invalid Subagents format: %v", err))
			return nil
		}
	} else if prompt, ok := tc.Args["prompt"].(string); ok && prompt != "" {
		// Legacy format: single prompt-based invocation
		sysInstructions, _ := tc.Args["system_instructions"].(string)
		invocations = []subagentInvocationArgs{
			{
				TypeName: "self", // Legacy behavior = self type
				Role:     "Subagent",
				Prompt:   prompt,
			},
			// Store system_instructions for legacy path
		}
		_ = sysInstructions // Legacy system_instructions handled below
	}

	if len(invocations) == 0 {
		e.feedSubagentError(tc, step, "at least one subagent must be specified (Subagents array or prompt)")
		return nil
	}

	// ── Launch each subagent ──
	type launchResult struct {
		ConversationID string `json:"conversation_id"`
		TypeName       string `json:"type_name"`
		Role           string `json:"role"`
	}
	var results []launchResult

	for _, inv := range invocations {
		// Concurrency check
		if atomic.LoadInt32(&e.activeSubagents) >= int32(e.maxSubagents) {
			errMsg := fmt.Sprintf("max concurrent subagents (%d) reached — cannot launch '%s'", e.maxSubagents, inv.TypeName)
			e.logger.Warn(errMsg)
			continue
		}

		// Look up type
		typeDef, ok := e.subagentRegistry.Get(inv.TypeName)
		if !ok {
			e.logger.Warn("unknown subagent type", "type", inv.TypeName)
			results = append(results, launchResult{
				ConversationID: "",
				TypeName:       inv.TypeName,
				Role:           fmt.Sprintf("ERROR: unknown subagent type '%s'", inv.TypeName),
			})
			continue
		}

		if inv.Prompt == "" {
			results = append(results, launchResult{
				ConversationID: "",
				TypeName:       inv.TypeName,
				Role:           "ERROR: prompt is required",
			})
			continue
		}

		// Create child trajectory & conversation IDs.
		// Conv IDs are UUIDs for flat brain dirs under brain/<uuid>/.
		childTrajID := fmt.Sprintf("%s/sub_%d_%s", e.trajectoryID, step.StepIndex, inv.TypeName)
		childConvID := util.NewUUID()

		// Create flat brain directory at brain/<uuid>/ (not nested under parent).
		var childBrainDir string
		if e.appDataDir != "" {
			childBrainDir = filepath.Join(e.appDataDir, "brain", childConvID)
			for _, d := range []string{
				childBrainDir,
				filepath.Join(childBrainDir, "scratch"),
				filepath.Join(childBrainDir, ".system_generated", "logs"),
				filepath.Join(childBrainDir, ".system_generated", "traces"),
			} {
				os.MkdirAll(d, 0755)
			}
		}

		// Create child conversation for .pb persistence (if ConversationManager available).
		var childConv *conversation.Conversation
		if e.convMgr != nil {
			var err error
			childConv, err = e.convMgr.CreateWithID(childConvID, &pb.HarnessConfig{})
			if err != nil {
				e.logger.Warn("failed to create subagent conversation", "error", err)
			} else {
				// Set lineage fields for tree reconstruction
				childConv.State.ParentConversationId = e.convID
				childConv.State.AgentRole = inv.Role
				childConv.State.AgentTypeName = inv.TypeName
				childConv.State.AgentDepth = int32(e.depth + 1)
				childConv.State.BudgetAllocated = inv.AllocatedBudget
			}
		}

		// Determine system prompt
		sysPrompt := typeDef.SystemPrompt
		if sysPrompt == "" {
			sysPrompt = defaultSubagentSystemPrompt
		}

		atomic.AddInt32(&e.activeSubagents, 1)

		// Build tool group filter from type definition flags.
		// If write tools are disabled, exclude the "write" group and host tools.
		var excludeGroups map[tools.ToolGroup]bool
		excludeHostTools := false
		if !typeDef.EnableWriteTools {
			excludeGroups = map[tools.ToolGroup]bool{
				tools.ToolGroupWrite: true,
			}
			// Host tools are typically write-capable (SDK-registered custom actions),
			// so exclude them when write tools are disabled.
			excludeHostTools = true
		}

		// Create child engine with its own flat brain dir and shared bus.
		childEngine := NewEngine(Config{
			Provider:             e.provider,
			ToolRegistry:         e.toolRegistry,
			SystemPrompt:         sysPrompt,
			ConversationID:       childConvID,
			TrajectoryID:         childTrajID,
			ParentTrajectoryID:   e.trajectoryID,
			Depth:                e.depth + 1,
			MaxDepth:             e.maxDepth,
			MaxSubagents:         e.maxSubagents,
			OnStep:               e.stepCB,
			OnTrajectory:         e.trajCB,
			MaxTurns:             subagentMaxTurns,
			CompactionThreshold:  e.compactionThreshold,
			KeepRecentMessages:   e.keepRecentMessages,
			BrainDir:             childBrainDir,
			AppDataDir:           e.appDataDir,
			Logger:               e.logger.With("subagent", inv.TypeName, "role", inv.Role),
			HostToolHandler:      e.hostToolHandler,
			HostToolNames:        e.hostToolNames,
			HostToolDecls:        e.hostToolDecls,
			PermissionHandler:    e.permissionHandler,
			SubagentsEnabled:     typeDef.EnableSubagentTools,
			ExcludeToolGroups:    excludeGroups,
			ExcludeHostTools:     excludeHostTools,
			ExcludeMCPTools:      !typeDef.EnableMCPTools,
			AgentBus:             e.agentBus,
			ConversationManager:  e.convMgr,
			ParentConversationID: e.convID,
			AgentRole:            inv.Role,
			AgentTypeName:        inv.TypeName,
		})
		childEngine.conv = childConv

		// Register instance in tracker
		childCtx, cancel := context.WithCancel(ctx)
		instance := &SubagentInstance{
			ConversationID: childConvID,
			TypeName:       inv.TypeName,
			Role:           inv.Role,
			State:          SubagentStateRunning,
			Engine:         childEngine,
			Cancel:         cancel,
			Inbox:          make(chan string, subagentInboxSize),
		}
		e.subagentTracker.Register(instance)

		// Launch in background goroutine
		go func(inst *SubagentInstance, prompt string) {
			defer atomic.AddInt32(&e.activeSubagents, -1)

			childErr := inst.Engine.Run(childCtx, prompt)

			// Extract result
			resultText := extractFinalResponse(inst.Engine.History())

			if childErr != nil {
				inst.SetState(SubagentStateError, childErr)
			} else {
				inst.SetState(SubagentStateIdle, nil)
			}

			// Save child conversation state (.pb) if available.
			if inst.Engine.conv != nil {
				history := inst.Engine.History()
				var protoMsgs []*pb.ConversationMessage
				for _, m := range history {
					protoMsgs = append(protoMsgs, mapHistoryMessageToProto(m))
				}
				inst.Engine.conv.SetMessages(protoMsgs)
				if saveErr := inst.Engine.conv.SaveAll(); saveErr != nil {
					e.logger.Warn("failed to save subagent conversation", "error", saveErr)
				}
			}

			// Notify parent — include artifact directory for cross-agent reads.
			notifyContent := fmt.Sprintf("Subagent '%s' (%s) completed.\nConversation ID: %s\nArtifact Directory: %s\n\nResult:\n%s",
				inst.TypeName, inst.Role, inst.ConversationID, inst.Engine.brainDir, resultText)
			if childErr != nil {
				notifyContent = fmt.Sprintf("Subagent '%s' (%s) failed: %v\nConversation ID: %s\nArtifact Directory: %s\n\nPartial result:\n%s",
					inst.TypeName, inst.Role, childErr, inst.ConversationID, inst.Engine.brainDir, resultText)
			}

			e.subagentTracker.NotifyParent(tools.SystemMessage{
				Source:  "subagent_complete",
				TaskID:  inst.ConversationID,
				Content: notifyContent,
			})

			// Clean up bus listener and tracker.
			if e.agentBus != nil {
				e.agentBus.Unsubscribe(inst.ConversationID)
			}
			e.subagentTracker.Remove(inst.ConversationID)

			e.logger.Info("subagent completed",
				"type", inst.TypeName,
				"role", inst.Role,
				"conversation_id", inst.ConversationID,
				"brain_dir", inst.Engine.brainDir,
				"result_len", len(resultText),
				"error", childErr,
			)
		}(instance, inv.Prompt)

		results = append(results, launchResult{
			ConversationID: childConvID,
			TypeName:       inv.TypeName,
			Role:           inv.Role,
		})

		e.logger.Info("launched subagent",
			"type", inv.TypeName,
			"role", inv.Role,
			"conversation_id", childConvID,
			"depth", e.depth+1,
		)
	}

	// ── Return immediately with launch results ──
	resultJSON, _ := json.MarshalIndent(results, "", "  ")
	resultMsg := fmt.Sprintf("Launched %d subagent(s). They will run in the background and you will be notified when they complete.\n\n%s",
		len(results), string(resultJSON))

	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	e.history = append(e.history, toolResultMsg(tc, resultMsg, false))

	return nil
}

// ═══════════════════════════════════════════════════════════════════════
// TOOL EXECUTION — manage_subagents
// ═══════════════════════════════════════════════════════════════════════

// executeManageSubagents handles the manage_subagents tool call.
func (e *Engine) executeManageSubagents(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	action, _ := tc.Args["Action"].(string)

	var resultMsg string

	switch action {
	case "list":
		instances := e.subagentTracker.List()
		if len(instances) == 0 {
			resultMsg = "No active subagents."
		} else {
			type info struct {
				ConversationID string `json:"conversation_id"`
				TypeName       string `json:"type_name"`
				Role           string `json:"role"`
				State          string `json:"state"`
			}
			var infos []info
			for _, inst := range instances {
				infos = append(infos, info{
					ConversationID: inst.ConversationID,
					TypeName:       inst.TypeName,
					Role:           inst.Role,
					State:          inst.GetState().String(),
				})
			}
			infoJSON, _ := json.MarshalIndent(infos, "", "  ")
			resultMsg = fmt.Sprintf("Active subagents (%d):\n%s", len(infos), string(infoJSON))
		}

	case "kill":
		idsRaw, _ := tc.Args["ConversationIds"]
		idsJSON, _ := json.Marshal(idsRaw)
		var ids []string
		json.Unmarshal(idsJSON, &ids)

		if len(ids) == 0 {
			e.feedToolError(tc, step, "ConversationIds required for 'kill' action")
			return nil
		}

		var killed, notFound []string
		for _, id := range ids {
			if err := e.subagentTracker.Kill(id); err != nil {
				notFound = append(notFound, id)
			} else {
				killed = append(killed, id)
			}
		}
		resultMsg = fmt.Sprintf("Killed %d subagent(s).", len(killed))
		if len(notFound) > 0 {
			resultMsg += fmt.Sprintf(" Not found: %v", notFound)
		}

	case "kill_all":
		count := e.subagentTracker.KillAll()
		resultMsg = fmt.Sprintf("Killed all %d active subagent(s).", count)

	default:
		e.feedToolError(tc, step, fmt.Sprintf("unknown action '%s' — use 'list', 'kill', or 'kill_all'", action))
		return nil
	}

	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	e.history = append(e.history, toolResultMsg(tc, resultMsg, false))

	return nil
}

// ═══════════════════════════════════════════════════════════════════════
// TOOL EXECUTION — send_message
// ═══════════════════════════════════════════════════════════════════════

// executeSendMessage handles the send_message tool call.
func (e *Engine) executeSendMessage(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	recipient, _ := tc.Args["Recipient"].(string)
	message, _ := tc.Args["Message"].(string)

	if recipient == "" {
		e.feedToolError(tc, step, "Recipient is required for send_message")
		return nil
	}
	if message == "" {
		e.feedToolError(tc, step, "Message is required for send_message")
		return nil
	}

	if err := e.subagentTracker.SendMessage(recipient, message); err != nil {
		e.feedToolError(tc, step, fmt.Sprintf("failed to send message: %v", err))
		return nil
	}

	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

	resultMsg := fmt.Sprintf("Message sent to %s.", recipient)
	e.history = append(e.history, toolResultMsg(tc, resultMsg, false))

	return nil
}

// ═══════════════════════════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════════════════════════

// feedSubagentError feeds an error result back to the LLM so it can adapt.
func (e *Engine) feedSubagentError(tc llm.ToolCall, step *pb.StepUpdate, errMsg string) {
	e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf("Error: %s", errMsg), true))

	if action := step.GetInvokeSubagent(); action != nil {
		action.ErrorMessage = errMsg
	}
	step.State = pb.StepUpdate_STATE_ERROR
	step.ErrorInfo = &pb.ErrorInfo{
		Message: errMsg,
		Code:    "SUBAGENT_ERROR",
	}
	e.emitStep(step)
}

// feedToolError feeds a generic tool error result.
func (e *Engine) feedToolError(tc llm.ToolCall, step *pb.StepUpdate, errMsg string) {
	e.history = append(e.history, toolResultMsg(tc, fmt.Sprintf("Error: %s", errMsg), true))

	step.State = pb.StepUpdate_STATE_ERROR
	step.ErrorInfo = &pb.ErrorInfo{
		Message: errMsg,
		Code:    "TOOL_ERROR",
	}
	e.emitStep(step)
}

// extractFinalResponse finds the last model text response from a message history.
func extractFinalResponse(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role == "model" && msg.Content != "" && len(msg.ToolCalls) == 0 {
			return msg.Content
		}
	}
	return ""
}

// accumulateUsage sums up token usage estimates from model messages.
// This is a rough estimate — real usage is tracked per-call, but for the
// subagent summary we give a ballpark from what we can reconstruct.
func accumulateUsage(history []llm.Message) llm.Usage {
	// Token usage is not stored in messages (it's tracked per-call).
	// Return zero — the actual usage is captured via tracing.
	return llm.Usage{}
}

// mapHistoryMessageToProto converts an LLM message to proto format for .pb persistence.
func mapHistoryMessageToProto(msg llm.Message) *pb.ConversationMessage {
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

// mapProtoMessageToLLM converts a proto ConversationMessage to an LLM message.
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
