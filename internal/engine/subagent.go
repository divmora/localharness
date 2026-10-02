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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/util"
	"github.com/divmora/localharness/internal/workspace"
)

const (
	defaultMaxDepth     = 3
	defaultMaxSubagents = 5
	subagentMaxTurns    = 30

	defaultSubagentSystemPrompt = `You are a focused specialized coding subagent working on a specific subtask within a team.
You have access to tools to research, edit files, run commands, and communicate with peer agents.

## Handoff Briefing Requirements
When you complete your assigned task or before ending your turn, your final response MUST provide a structured **Handoff Briefing** following this exact format:

### Handoff Briefing
- **Task & Goal**: Brief summary of the original objective assigned to you.
- **Status**: [COMPLETED | PARTIALLY_COMPLETED | BLOCKED]
- **Files Created & Modified**:
  - List every touched file with full paths and a 1-line explanation of changes.
- **Key Changes & Decisions**: Architectural choices, new functions/structs/APIs exposed, or data models.
- **Verification & Tests**: Exact commands executed (e.g. go test ./...) and verification results.
- **Next Steps**: Specific recommendations for the Lead Orchestrator or peer agents on how to use your work.

Be thorough, precise, and professional — this briefing is delivered directly to the Lead Orchestrator and peer agents for immediate handoff.`

	subagentInboxSize = 32 // Buffered channel capacity for inter-agent messages
)

// ═══════════════════════════════════════════════════════════════════════
// TOOL DECLARATIONS
// ═══════════════════════════════════════════════════════════════════════

// defineSubagentDeclaration returns the FunctionDeclaration for define_subagent.
func defineSubagentDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "define_subagent",
		Description: "\tDefines a new type of subagent that can be invoked via invoke_subagent.\n\n" +
			"\tGuidelines:\n" +
			"\t* Use this tool if you need a specialized subagent for a task and none of the existing subagents are suitable.\n" +
			"\t* Once the subagent is defined, it can be invoked repeatedly using invoke_subagent without calling this tool again.\n" +
			"\t* The subagent will be defined with the specified name, description, system prompt, and tool groups.\n" +
			"\t* By default, all subagents have read tools to research the codebase, and tools to communicate with other agents.\n\t",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Description": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable description of what this subagent does and when it should be used.",
				},
				"EnableMcpTools": map[string]interface{}{
					"type":        "boolean",
					"description": "Set true to enable the subagent to call MCP tools.",
				},
				"EnableSubagentTools": map[string]interface{}{
					"type":        "boolean",
					"description": "Set true to equip the subagent with tools to define and invoke its own subagents",
				},
				"EnableWriteTools": map[string]interface{}{
					"type":        "boolean",
					"description": "Set true to equip the subagent with tools to create and edit files, and run commands.",
				},
				"Name": map[string]interface{}{
					"type":        "string",
					"description": "Unique name for the subagent. Used to invoke it via invoke_subagent. Must start with a letter or digit and contain only letters, digits, '_', '-', and '.'.",
				},
				"SystemPrompt": map[string]interface{}{
					"type":        "string",
					"description": "A detailed system prompt for this subagent.",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"Name", "Description", "SystemPrompt", "ToolSummary", "ToolAction"},
		},
	}
}

// invokeSubagentDeclaration returns the FunctionDeclaration for invoke_subagent.
func invokeSubagentDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "invoke_subagent",
		Description: "Invokes one or more subagents by name with a single tool call. Each subagent runs in the background with its own prompt and reports back when done.\n\n" +
			"Specify the Subagents array with one or more entries. Each entry defines a subagent to launch.\n\n" +
			"Communicate with subagents using the send_message tool. Examples of when to do this:\n" +
			"* To check on the status of a subagent.\n" +
			"* To send a running subagent further instructions.\n" +
			"* To send an idle subagent new instructions.\n\n" +
			"Guidelines:\n" +
			"* Each invoked subagent will be uniquely identified by its conversationID.\n" +
			"* Multiple subagents with the same type name can be invoked, with each subagent receiving a unique conversationID.\n" +
			"* If a task is a natural continuation of an existing subagent's work, send a message to that subagent with the task rather than invoking a new subagent to conserve resources.\n" +
			"* When selecting the Model argument, default to 'inherit' unless the user explicitly requests a different model.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Subagents": map[string]interface{}{
					"type":        "array",
					"description": "Array of subagents to invoke. Each entry specifies a separate subagent to launch concurrently.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"Model": map[string]interface{}{
								"type":        "string",
								"enum":        []string{"inherit", "flash_lite", "flash", "pro"},
								"description": "Model to use for the subagent. 'inherit' (default) uses the calling agent's model. 'flash_lite' uses a very light model. 'flash' uses a smaller, faster model suited for simple tasks like research lookups, file reading, or quick searches. 'pro' uses a larger, more capable model suited for complex tasks requiring deep reasoning, large refactors, or multi-step planning.",
							},
							"Prompt": map[string]interface{}{
								"type":        "string",
								"description": "A clear, actionable task description for the subagent. Be specific about what the subagent should do and what information it should return.",
							},
							"Role": map[string]interface{}{
								"type":        "string",
								"description": "A 2-5 word description of the subagent's role. Should read similar to a job title, e.g. 'Codebase Researcher', 'Database Debugger', etc. Should also be detailed enough to distinguish between different subagents who might share similar purposes.",
							},
							"TypeName": map[string]interface{}{
								"type":        "string",
								"description": "Type name of the subagent to invoke.",
							},
							"Workspace": map[string]interface{}{
								"type":        "string",
								"description": "Workspace mode for the subagent. 'inherit' (default) uses the same workspace as the parent. 'branch' creates a new isolated workspace branched or cloned from the parent. 'share' creates a new workspace sharing the parent's underlying repository directory (similar to a git worktree or Mercurial 'hg share'), allowing independent branching without duplicating storage. If omitted, defaults to 'inherit'.",
							},
						},
						"required": []string{"TypeName", "Role", "Prompt"},
					},
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"Subagents", "ToolSummary", "ToolAction"},
		},
	}
}

// manageSubagentsDeclaration returns the FunctionDeclaration for manage_subagents.
func manageSubagentsDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "manage_subagents",
		Description: "\tManage existing subagents.\n" +
			"\tActions:\n" +
			"\t* 'list': List active direct subagents with their conversation IDs and live state. Each subagent is reported as a JSON object with this schema:\n" +
			"\t{\"$schema\":\"https://json-schema.org/draft/2020-12/schema\",\"properties\":{\"role\":{\"type\":\"string\",\"description\":\"The subagent's role, or its type name if no role was set.\"},\"type\":{\"type\":\"string\",\"description\":\"The subagent's type name.\"},\"conversationId\":{\"type\":\"string\",\"description\":\"The subagent's conversation ID, used to message or kill it.\"},\"transcript\":{\"type\":\"string\",\"description\":\"Absolute URI of the subagent's transcript log, if available.\"},\"state\":{\"type\":\"string\",\"enum\":[\"running\",\"idle\",\"waiting_for_input\",\"waiting_for_dependents\",\"waiting_for_message\",\"canceling\",\"errored\",\"unspecified\"],\"description\":\"The subagent's current lifecycle state.\"},\"stateDetail\":{\"type\":\"string\",\"description\":\"Context for State: the current tool call (name and summary) when running or waiting_for_input, or the failure message when errored.\"}},\"additionalProperties\":false,\"type\":\"object\"}\n" +
			"\t* 'kill': Terminate specific subagents and all their descendants.\n" +
			"\t* 'kill_all': Terminate all subagents and all their descendants.\n\n" +
			"\tWhen a subagent is killed, its branched workspaces will be deleted, but its logs and artifacts will be preserved.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"list", "kill", "kill_all"},
					"description": "The action to perform. Must be 'list' (list active direct subagents with their live state), 'kill' (terminate specific subagents and all their descendants), or 'kill_all' (terminate all subagents and all their descendants).",
				},
				"ConversationIds": map[string]interface{}{
					"type":        "array",
					"description": "The IDs of the subagents to kill. Required for 'kill'.",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"Action", "ToolSummary", "ToolAction"},
		},
	}
}

// sendMessageDeclaration returns the FunctionDeclaration for send_message.
func sendMessageDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name:        "send_message",
		Description: "Send a message to another agent. This tool can be used to communicate with subagents, peer agents, etc. Do not use this tool to communicate with the user.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Message": map[string]interface{}{
					"type":        "string",
					"description": "The message content.",
				},
				"Recipient": map[string]interface{}{
					"type":        "string",
					"description": "The recipient ID to send the message to, e.g. a subagent conversation ID.",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"Recipient", "Message", "ToolSummary", "ToolAction"},
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
	name, _ := tc.Args["Name"].(string)
	if name == "" {
		name, _ = tc.Args["name"].(string)
	}
	description, _ := tc.Args["Description"].(string)
	if description == "" {
		description, _ = tc.Args["description"].(string)
	}
	systemPrompt, _ := tc.Args["SystemPrompt"].(string)
	if systemPrompt == "" {
		systemPrompt, _ = tc.Args["system_prompt"].(string)
	}
	enableWrite, ok := tc.Args["EnableWriteTools"].(bool)
	if !ok {
		enableWrite, _ = tc.Args["enable_write_tools"].(bool)
	}
	enableMCP, ok := tc.Args["EnableMcpTools"].(bool)
	if !ok {
		enableMCP, _ = tc.Args["enable_mcp_tools"].(bool)
	}
	enableSubagent, ok := tc.Args["EnableSubagentTools"].(bool)
	if !ok {
		enableSubagent, _ = tc.Args["enable_subagent_tools"].(bool)
	}
	defaultModel, _ := tc.Args["DefaultModel"].(string)
	if defaultModel == "" {
		defaultModel, _ = tc.Args["default_model"].(string)
	}

	if name == "" {
		e.feedToolError(tc, step, "Name is required for define_subagent")
		return nil
	}
	if description == "" {
		e.feedToolError(tc, step, "Description is required for define_subagent")
		return nil
	}
	if systemPrompt == "" {
		e.feedToolError(tc, step, "SystemPrompt is required for define_subagent")
		return nil
	}

	typeDef := SubagentTypeDef{
		Name:                name,
		Description:         description,
		SystemPrompt:        systemPrompt,
		EnableWriteTools:    enableWrite,
		EnableMCPTools:      enableMCP,
		EnableSubagentTools: enableSubagent,
		DefaultModelTier:    defaultModel,
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
	if def := step.GetDefineSubagent(); def != nil {
		def.FormattedOutput = resultMsg
	}
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
	TypeName  string `json:"TypeName"`
	Role      string `json:"Role"`
	Prompt    string `json:"Prompt"`
	Workspace string `json:"Workspace,omitempty"`
	Model     string `json:"Model,omitempty"`
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
		Model          string `json:"model,omitempty"`
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
		} else if e.brainDir != "" {
			childBrainDir = filepath.Join(e.brainDir, "subagents", childConvID)
		}
		if childBrainDir != "" {
			for _, d := range []string{
				childBrainDir,
				filepath.Join(childBrainDir, "scratch"),
				filepath.Join(childBrainDir, ".system_generated", "logs"),
				filepath.Join(childBrainDir, ".system_generated", "traces"),
			} {
				_ = os.MkdirAll(d, 0755)
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
			}
		}

		// Determine system prompt
		sysPrompt := typeDef.SystemPrompt
		if sysPrompt == "" {
			if inv.TypeName == "self" {
				sysPrompt = e.sysPrompt
			} else {
				sysPrompt = defaultSubagentSystemPrompt
			}
		}

		atomic.AddInt32(&e.activeSubagents, 1)

		// Build tool group filter from type definition flags and capability inheritance.
		enableWrite := typeDef.EnableWriteTools
		enableMCP := typeDef.EnableMCPTools
		enableSubagents := typeDef.EnableSubagentTools

		if typeDef.InheritCapabilities || e.inheritSubagentCapabilities {
			enableWrite = (e.excludeToolGroups == nil || !e.excludeToolGroups[tools.ToolGroupWrite])
			enableMCP = !e.excludeMCPTools
			enableSubagents = e.subagentsEnabled
		}

		excludeGroups := make(map[tools.ToolGroup]bool)
		for k, v := range e.excludeToolGroups {
			excludeGroups[k] = v
		}
		excludeHostTools := false
		if !enableWrite {
			excludeGroups[tools.ToolGroupWrite] = true
			excludeHostTools = true
		} else {
			delete(excludeGroups, tools.ToolGroupWrite)
			excludeHostTools = e.excludeHostTools
		}

		// Resolve child engine model provider
		reqModel := inv.Model
		if reqModel == "" {
			if typeDef.DefaultModelTier != "" {
				reqModel = typeDef.DefaultModelTier
			} else if inv.TypeName == "research" {
				reqModel = string(ModelTierFlash)
			}
		}
		childProvider := ResolveSubagentProvider(e.provider, reqModel, e.modelTierResolver)

		// Setup isolated workspace for "branch" mode if requested
		childWorkspaces := e.workspaces
		childWorkspaceInfos := e.workspaceInfos
		childRegistry := e.toolRegistry
		var branchedWorkspaceDir string

		if inv.Workspace == "branch" && len(e.workspaces) > 0 && childBrainDir != "" {
			branchedWorkspaceDir = filepath.Join(childBrainDir, "workspace_branch")
			if err := copyWorkspaceSnapshot(e.workspaces[0], branchedWorkspaceDir); err != nil {
				e.logger.Warn("failed to snapshot workspace for branch mode", "error", err)
				branchedWorkspaceDir = ""
			} else {
				childWorkspaces = []string{branchedWorkspaceDir}
				var corpus string
				if len(e.workspaceInfos) > 0 {
					corpus = e.workspaceInfos[0].CorpusName
				}
				childWorkspaceInfos = []WorkspaceInfo{
					{
						Directory:  branchedWorkspaceDir,
						CorpusName: corpus,
					},
				}
				branchWsMgr, err := workspace.NewManagerWithAccessMode([]string{branchedWorkspaceDir}, e.accessMode)
				if err == nil {
					if childBrainDir != "" {
						_ = branchWsMgr.AddAllowedPath(childBrainDir)
					}
					if e.appDataDir != "" {
						_ = branchWsMgr.AddAllowedPath(e.appDataDir)
					}
					if e.toolRegistry != nil {
						childRegistry = e.toolRegistry.CloneWithWorkspaceManager(branchWsMgr)
					}
				}
			}
		}

		// Create child engine with its own flat brain dir and shared bus.
		childEngine := NewEngine(Config{
			Provider:                 childProvider,
			SummarizerProvider:       e.summarizerProvider,
			ModelTierResolver:        e.modelTierResolver,
			ToolRegistry:             childRegistry,
			SystemPrompt:             sysPrompt,
			ConversationID:           childConvID,
			TrajectoryID:             childTrajID,
			ParentTrajectoryID:       e.trajectoryID,
			Depth:                    e.depth + 1,
			MaxDepth:                 e.maxDepth,
			MaxSubagents:             e.maxSubagents,
			OnStep:                   e.stepCB,
			OnTrajectory:             e.trajCB,
			MaxTurns:                 subagentMaxTurns,
			CompactionThreshold:      e.compactionThreshold,
			KeepRecentMessages:       e.keepRecentMessages,
			BrainDir:                 childBrainDir,
			AppDataDir:               e.appDataDir,
			Logger:                   e.logger.With("subagent", inv.TypeName, "role", inv.Role),
			StreamFlushInterval:      e.streamFlushInterval,
			MaxConcurrentToolWorkers: e.maxConcurrentToolWorkers,
			HostToolHandler:          e.hostToolHandler,
			HostToolNames:            e.hostToolNames,
			HostToolDecls:            e.hostToolDecls,
			PermissionHandler:        e.permissionHandler,
			QuestionHandler:          e.questionHandler,
			SubagentsEnabled:         enableSubagents,
			ExcludeToolGroups:        excludeGroups,
			ExcludeHostTools:         excludeHostTools,
			ExcludeMCPTools:          !enableMCP,
			MCPManager:               e.mcpMgr,
			AgentBus:                 e.agentBus,
			ConversationManager:      e.convMgr,
			ParentConversationID:     e.convID,
			AgentRole:                inv.Role,
			AgentTypeName:            inv.TypeName,
			Workspaces:               childWorkspaces,
			WorkspaceInfos:           childWorkspaceInfos,
			UserRules:                e.userRules,
			YoloMode:                 e.yoloMode,
			Conversation:             childConv,
			Skills:                   e.msgCtx.Skills,
			Plugins:                  e.msgCtx.Plugins,
			Env:                      e.Env(),
		})

		// Register instance in tracker with a long-lived context tied to the tracker lifecycle
		// (not the ephemeral parent turn context, which cancels when the turn finishes)
		childCtx, cancel := context.WithCancel(context.Background())
		instance := &SubagentInstance{
			ConversationID: childConvID,
			TypeName:       inv.TypeName,
			Role:           inv.Role,
			Model:          childProvider.ModelName(),
			State:          SubagentStateRunning,
			Engine:         childEngine,
			Cancel:         cancel,
			Inbox:          make(chan string, subagentInboxSize),
		}
		e.subagentTracker.Register(instance)

		// Launch in background goroutine
		go func(inst *SubagentInstance, prompt string, branchedDir string) {
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

			// Workspace branch synchronization and diff generation
			var branchSyncInfo string
			if branchedDir != "" && len(e.workspaces) > 0 {
				primaryWS := e.workspaces[0]
				applySync := (childErr == nil)
				patchContent, changedFiles, syncErr := diffAndSyncBranch(primaryWS, branchedDir, inst.Engine.brainDir, applySync)
				if syncErr != nil {
					e.logger.Warn("failed to diff/sync branched workspace", "error", syncErr)
				}
				if len(changedFiles) > 0 {
					patchPath := ""
					if inst.Engine.brainDir != "" && patchContent != "" {
						patchPath = filepath.Join(inst.Engine.brainDir, "patch.diff")
					}
					var sb strings.Builder
					sb.WriteString("\n\n### Branched Workspace Changes\n")
					if applySync {
						sb.WriteString(fmt.Sprintf("**Merged %d changed file(s) into primary workspace:**\n", len(changedFiles)))
					} else {
						sb.WriteString(fmt.Sprintf("**Subagent failed. %d changed file(s) NOT merged to primary workspace:**\n", len(changedFiles)))
					}
					for _, cf := range changedFiles {
						sb.WriteString(fmt.Sprintf("- `%s`\n", cf))
					}
					if patchPath != "" {
						sb.WriteString(fmt.Sprintf("\nUnified patch saved to: [%s](file://%s)\n", filepath.Base(patchPath), filepath.ToSlash(patchPath)))
					}
					branchSyncInfo = sb.String()
				}
			}

			// Save handoff briefing artifact in child brain directory if available
			handoffPath := ""
			if inst.Engine.brainDir != "" {
				handoffPath = filepath.Join(inst.Engine.brainDir, "handoff_briefing.md")
				briefingContent := resultText
				if briefingContent == "" && childErr != nil {
					briefingContent = fmt.Sprintf("Error: %v\n", childErr)
				}
				if branchSyncInfo != "" {
					briefingContent += branchSyncInfo
				}
				if briefingContent != "" {
					_ = os.WriteFile(handoffPath, []byte(briefingContent), 0644)
				}
			}

			// Notify parent — compact 3-line notification with markdown link to handoff briefing.
			statusStr := "completed"
			if childErr != nil {
				statusStr = fmt.Sprintf("failed: %v", childErr)
			}

			summary := extractHandoffSummary(resultText, 250)
			if summary == "" {
				if childErr != nil {
					summary = childErr.Error()
				} else {
					summary = "Task completed successfully."
				}
			}

			briefingRef := "N/A"
			if handoffPath != "" {
				briefingRef = fmt.Sprintf("[handoff_briefing.md](file://%s)", filepath.ToSlash(handoffPath))
			}

			notifyContent := fmt.Sprintf("Subagent '%s' (%s) %s (Conversation ID: %s).\nHandoff Briefing: %s\nSummary: %s",
				inst.TypeName, inst.Role, statusStr, inst.ConversationID, briefingRef, summary)
			if branchSyncInfo != "" {
				notifyContent += branchSyncInfo
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
		}(instance, inv.Prompt, branchedWorkspaceDir)

		results = append(results, launchResult{
			ConversationID: childConvID,
			TypeName:       inv.TypeName,
			Role:           inv.Role,
			Model:          childProvider.ModelName(),
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

	if inv := step.GetInvokeSubagent(); inv != nil {
		inv.FormattedOutput = resultMsg
	}
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
	if action == "" {
		action, _ = tc.Args["action"].(string)
	}

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
				Model          string `json:"model,omitempty"`
				State          string `json:"state"`
			}
			var infos []info
			for _, inst := range instances {
				infos = append(infos, info{
					ConversationID: inst.ConversationID,
					TypeName:       inst.TypeName,
					Role:           inst.Role,
					Model:          inst.Model,
					State:          inst.GetState().String(),
				})
			}
			infoJSON, _ := json.MarshalIndent(infos, "", "  ")
			resultMsg = fmt.Sprintf("Active subagents (%d):\n%s", len(infos), string(infoJSON))
		}

	case "kill":
		idsRaw := tc.Args["ConversationIds"]
		if idsRaw == nil {
			idsRaw = tc.Args["conversation_ids"]
		}
		idsJSON, _ := json.Marshal(idsRaw)
		var ids []string
		_ = json.Unmarshal(idsJSON, &ids)

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

	if ms := step.GetManageSubagents(); ms != nil {
		ms.FormattedOutput = resultMsg
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
	if recipient == "" {
		recipient, _ = tc.Args["recipient"].(string)
	}
	message, _ := tc.Args["Message"].(string)
	if message == "" {
		message, _ = tc.Args["message"].(string)
	}

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

	resultMsg := fmt.Sprintf("Message sent to %s.", recipient)
	if sm := step.GetSendMessageAction(); sm != nil {
		sm.FormattedOutput = resultMsg
	}
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)

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

// extractHandoffSummary extracts a concise 1-2 sentence excerpt from the subagent's result text.
func extractHandoffSummary(text string, maxLen int) string {
	if text == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = 250
	}
	lines := strings.Split(text, "\n")
	var meaningful []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		// Skip markdown headings and horizontal dividers
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "===") {
			continue
		}
		// Strip leading list bullet markers
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			meaningful = append(meaningful, trimmed)
			if len(strings.Join(meaningful, " ")) >= maxLen {
				break
			}
		}
	}
	combined := strings.Join(meaningful, " ")
	if len(combined) > maxLen {
		runes := []rune(combined)
		if len(runes) > maxLen {
			truncated := string(runes[:maxLen])
			if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxLen/2 {
				truncated = truncated[:lastSpace]
			}
			combined = strings.TrimRight(truncated, ",.; ") + "..."
		}
	}
	return combined
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

var branchSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	".venv": true, "vendor": true, ".idea": true, ".vscode": true,
	"dist": true, "build": true, ".next": true, "target": true,
	"bin": true, ".gemini": true, ".divmora": true, ".cache": true,
	".turbo": true, ".agents": true,
}

func copyWorkspaceSnapshot(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if branchSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		if d.Type().IsRegular() {
			srcData, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			info, err := d.Info()
			mode := os.FileMode(0644)
			if err == nil {
				mode = info.Mode()
			}
			targetPath := filepath.Join(dst, rel)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return nil
			}
			return os.WriteFile(targetPath, srcData, mode)
		}
		return nil
	})
}

func diffAndSyncBranch(primaryWS, branchWS, brainDir string, applySync bool) (string, []string, error) {
	var patches []string
	var changedFiles []string
	branchFiles := make(map[string]bool)

	// Walk branch workspace to find modified or added files
	err := filepath.WalkDir(branchWS, func(bPath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(branchWS, bPath)
		if err != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if branchSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		branchFiles[rel] = true
		bContent, err := os.ReadFile(bPath)
		if err != nil {
			return nil
		}

		pPath := filepath.Join(primaryWS, rel)
		pContent, pErr := os.ReadFile(pPath)
		if pErr != nil {
			// New file added in branch
			diff := util.UnifiedDiff("a/"+rel, "b/"+rel, "", string(bContent))
			if diff != "" {
				patches = append(patches, diff)
				changedFiles = append(changedFiles, rel+" (created)")
				if applySync {
					_ = os.MkdirAll(filepath.Dir(pPath), 0755)
					info, _ := d.Info()
					mode := os.FileMode(0644)
					if info != nil {
						mode = info.Mode()
					}
					_ = os.WriteFile(pPath, bContent, mode)
				}
			}
		} else if !bytes.Equal(pContent, bContent) {
			// File modified in branch
			diff := util.UnifiedDiff("a/"+rel, "b/"+rel, string(pContent), string(bContent))
			if diff != "" {
				patches = append(patches, diff)
				changedFiles = append(changedFiles, rel)
				if applySync {
					info, _ := d.Info()
					mode := os.FileMode(0644)
					if info != nil {
						mode = info.Mode()
					}
					_ = os.WriteFile(pPath, bContent, mode)
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}

	// Walk primary workspace to find deleted files
	_ = filepath.WalkDir(primaryWS, func(pPath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(primaryWS, pPath)
		if err != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if branchSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		if !branchFiles[rel] {
			// File was deleted in branch
			pContent, err := os.ReadFile(pPath)
			if err == nil {
				diff := util.UnifiedDiff("a/"+rel, "b/"+rel, string(pContent), "")
				if diff != "" {
					patches = append(patches, diff)
					changedFiles = append(changedFiles, rel+" (deleted)")
					if applySync {
						_ = os.Remove(pPath)
					}
				}
			}
		}
		return nil
	})

	fullPatch := strings.Join(patches, "\n")
	if brainDir != "" && fullPatch != "" {
		_ = os.WriteFile(filepath.Join(brainDir, "patch.diff"), []byte(fullPatch), 0644)
	}
	return fullPatch, changedFiles, nil
}
