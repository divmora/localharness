package engine

import (
	"context"
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

const browserSubagentSystemPrompt = `You are a specialized browser automation agent modeled after Jetski-grade browser automation.
Your objective is to accomplish the user's task by navigating the web, interacting with pages, and gathering information.
You have access to browser automation tools via MCP. Use them to navigate, click, type, and extract data.

Capabilities & Best Practices:
1. Persistent Profiles: You operate with a persistent browser profile by default. Logins, cookies, session storage, and state persist across runs.
2. Visual Elements & Bounding Boxes: Tools output element bounding boxes [box=x,y,width,height] and visual snapshots. Use these coordinates and landmarks for precise targeting.
3. Human-in-the-Loop Handoff: If you encounter a CAPTCHA, Cloudflare verification, 2FA/MFA challenge, or manual login requirement:
   - Do NOT repeatedly fail or loop endlessly.
   - Use the ask_question tool to notify the user and ask them to complete the verification or sign in in the visible browser window.
   - Once the user confirms completion, continue your task with the authenticated session intact.
4. Action Verification: After key navigation or submission steps, verify the page state or snapshot before concluding.
5. Return a clear and concise summary of your actions, findings, and any saved recording/artifacts.`

// browserSubagentDeclaration returns the FunctionDeclaration for browser_subagent.
func browserSubagentDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "browser_subagent",
		Description: "Start a specialized browser subagent to interact with web applications, websites, web pages, localhost servers, or SPAs. " +
			"The subagent has tools to navigate URLs, click elements, fill forms, extract data, take DOM snapshots with bounding boxes, and handle human-in-the-loop auth/CAPTCHA. " +
			"Always use browser_subagent (NOT desktop_subagent) whenever the task involves a URL, web app, or browser testing.",
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []string{"TaskName", "Task", "TaskSummary", "RecordingName"},
			"properties": map[string]interface{}{
				"TaskName": map[string]interface{}{
					"type":        "string",
					"description": "Name of the task that the browser subagent is performing.",
				},
				"Task": map[string]interface{}{
					"type":        "string",
					"description": "A clear, actionable task description for the browser subagent.",
				},
				"TaskSummary": map[string]interface{}{
					"type":        "string",
					"description": "A short, user-friendly summary of the task (1-2 sentences max).",
				},
				"RecordingName": map[string]interface{}{
					"type":        "string",
					"description": "Name of the browser recording that is created with the actions of the subagent.",
				},
				"ReusedSubagentId": map[string]interface{}{
					"type":        "string",
					"description": "ID of a previous subagent to resume from.",
				},
				"MediaPaths": map[string]interface{}{
					"type":        "array",
					"description": "Optional absolute paths to media files to provide as context.",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"Mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"auto", "headed", "headless", "connect"},
					"description": "Browser execution mode: 'auto' starts headless and escalates to headed if captcha/auth is required; 'headed' displays the browser window; 'headless' runs invisibly; 'connect' attaches to an existing browser via CDP URL.",
				},
				"ConnectUrl": map[string]interface{}{
					"type":        "string",
					"description": "Optional CDP WebSocket or HTTP URL to attach to an already running browser (e.g. http://localhost:9222). Used when Mode is 'connect'.",
				},
				"ProfileDir": map[string]interface{}{
					"type":        "string",
					"description": "Optional custom directory path for persistent browser profile. If omitted, uses default ~/.divmora/localharness/browser_profile.",
				},
				"Isolated": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, runs in an ephemeral in-memory profile without saving cookies or state across runs. Default is false.",
				},
			},
		},
	}
}

// executeBrowserSubagent handles the browser_subagent tool call.
func (e *Engine) executeBrowserSubagent(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	if !e.hasBrowserCapability() {
		e.feedToolError(tc, step, "browser capability is not available. Ensure Node.js and npx are installed in PATH, or configure a browser MCP server in mcp_config.json.")
		return nil
	}

	taskName, _ := tc.Args["TaskName"].(string)
	task, _ := tc.Args["Task"].(string)
	taskSummary, _ := tc.Args["TaskSummary"].(string)
	recordingName, _ := tc.Args["RecordingName"].(string)
	reusedSubagentId, _ := tc.Args["ReusedSubagentId"].(string)
	mode, _ := tc.Args["Mode"].(string)
	if mode == "" {
		mode = "auto"
	}
	connectUrl, _ := tc.Args["ConnectUrl"].(string)

	profileDir, _ := tc.Args["ProfileDir"].(string)
	isolated, _ := tc.Args["Isolated"].(bool)

	var mediaPaths []string
	if mp, ok := tc.Args["MediaPaths"].([]interface{}); ok {
		for _, v := range mp {
			if s, ok := v.(string); ok {
				mediaPaths = append(mediaPaths, s)
			}
		}
	}

	if task == "" {
		e.feedToolError(tc, step, "Task is required for browser_subagent")
		return nil
	}

	if len(mediaPaths) > 0 {
		task += "\n\nMedia Paths provided for context:\n"
		for _, p := range mediaPaths {
			task += fmt.Sprintf("- %s\n", p)
		}
	}

	task += fmt.Sprintf("\n\nBrowser Mode: %s", mode)
	if connectUrl != "" {
		task += fmt.Sprintf("\nCDP Connect URL: %s", connectUrl)
	}
	if profileDir != "" {
		task += fmt.Sprintf("\nBrowser Profile Directory: %s", profileDir)
	}
	if isolated {
		task += "\nBrowser Isolation: Ephemeral (in-memory)"
	}

	// Populate step action
	if action := step.GetBrowserSubagent(); action != nil {
		action.TaskName = taskName
		action.Task = task
		action.TaskSummary = taskSummary
		action.RecordingName = recordingName
		action.ReusedSubagentId = reusedSubagentId
		action.MediaPaths = mediaPaths
		action.Mode = mode
		action.ConnectUrl = connectUrl
		action.ProfileDir = profileDir
		action.Isolated = isolated
	}

	// Depth check
	if e.depth >= e.maxDepth {
		errMsg := fmt.Sprintf("max subagent depth (%d) exceeded — cannot spawn browser subagent", e.maxDepth)
		e.feedSubagentError(tc, step, errMsg)
		return nil
	}

	// Concurrency check
	if atomic.LoadInt32(&e.activeSubagents) >= int32(e.maxSubagents) {
		errMsg := fmt.Sprintf("max concurrent subagents (%d) reached — cannot launch browser subagent", e.maxSubagents)
		e.feedSubagentError(tc, step, errMsg)
		return nil
	}

	childConvID := util.NewUUID()
	var initialHistory []llm.Message
	if reusedSubagentId != "" {
		childConvID = reusedSubagentId
	}

	childTrajID := fmt.Sprintf("%s/sub_%d_browser", e.trajectoryID, step.StepIndex)

	// Create flat brain directory
	var childBrainDir string
	if e.appDataDir != "" {
		childBrainDir = filepath.Join(e.appDataDir, "brain", childConvID)
		for _, d := range []string{
			childBrainDir,
			filepath.Join(childBrainDir, "scratch"),
			filepath.Join(childBrainDir, ".system_generated", "logs"),
			filepath.Join(childBrainDir, ".system_generated", "traces"),
		} {
			_ = os.MkdirAll(d, 0755)
		}
	}

	// Create child conversation
	var childConv *conversation.Conversation
	if e.convMgr != nil {
		var err error
		if reusedSubagentId != "" {
			childConv, err = e.convMgr.Resume(reusedSubagentId)
			if err == nil && childConv.State != nil {
				for _, m := range childConv.State.Messages {
					initialHistory = append(initialHistory, mapProtoMessageToLLM(m))
				}
			} else {
				e.logger.Warn("failed to resume browser subagent conversation", "id", reusedSubagentId, "error", err)
			}
		} else {
			childConv, err = e.convMgr.CreateWithID(childConvID, &pb.HarnessConfig{})
			if err != nil {
				e.logger.Warn("failed to create browser subagent conversation", "error", err)
			} else {
				childConv.State.ParentConversationId = e.convID
				childConv.State.AgentRole = "Browser Agent"
				childConv.State.AgentTypeName = "browser"
				childConv.State.AgentDepth = int32(e.depth + 1)
			}
		}
	}

	atomic.AddInt32(&e.activeSubagents, 1)

	// Create child engine
	childEngine := NewEngine(Config{
		Provider:             e.provider,
		ToolRegistry:         e.toolRegistry,
		SystemPrompt:         browserSubagentSystemPrompt,
		InitialHistory:       initialHistory,
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
		Logger:               e.logger.With("subagent", "browser", "role", "Browser Agent"),
		HostToolHandler:      e.hostToolHandler,
		HostToolNames:        e.hostToolNames,
		HostToolDecls:        e.hostToolDecls,
		PermissionHandler:    e.permissionHandler,
		QuestionHandler:      e.questionHandler,
		SubagentsEnabled:     false,
		ExcludeMCPTools:      false,                                                // Need MCP tools for Playwright
		ExcludeToolGroups:    map[tools.ToolGroup]bool{tools.ToolGroupWrite: true}, // Typically read-only on filesystem
		MCPManager:           e.mcpMgr,
		AgentBus:             e.agentBus,
		ConversationManager:  e.convMgr,
		ParentConversationID: e.convID,
		AgentRole:            "Browser Agent",
		AgentTypeName:        "browser",
		Workspaces:           e.workspaces,
		WorkspaceInfos:       e.workspaceInfos,
		UserRules:            e.userRules,
		YoloMode:             e.yoloMode,
		Skills:               e.msgCtx.Skills,
		Plugins:              e.msgCtx.Plugins,
		NotifySendCh:         e.notifySendCh,
	})
	childEngine.conv = childConv

	childCtx, cancel := context.WithCancel(ctx)
	instance := &SubagentInstance{
		ConversationID: childConvID,
		TypeName:       "browser",
		Role:           "Browser Agent",
		State:          SubagentStateRunning,
		Engine:         childEngine,
		Cancel:         cancel,
		Inbox:          make(chan string, subagentInboxSize),
	}
	e.subagentTracker.Register(instance)

	// Launch in background
	go func(inst *SubagentInstance, prompt string) {
		defer atomic.AddInt32(&e.activeSubagents, -1)

		if e.appDataDir != "" {
			// Suggest video recording location for playwright if they use it
			os.Setenv("PLAYWRIGHT_RECORD_VIDEO_DIR", filepath.Join(inst.Engine.brainDir, "scratch"))
		}

		childErr := inst.Engine.Run(childCtx, prompt)

		resultText := extractFinalResponse(inst.Engine.History())

		// Scan for any recorded videos, traces, or session artifacts
		var recordingPath string
		if inst.Engine.brainDir != "" {
			for _, searchDir := range []string{
				filepath.Join(inst.Engine.brainDir, "artifacts"),
				filepath.Join(inst.Engine.brainDir, "scratch"),
			} {
				if entries, err := os.ReadDir(searchDir); err == nil {
					for _, entry := range entries {
						ext := filepath.Ext(entry.Name())
						if ext == ".webm" || ext == ".mp4" || ext == ".trace" || ext == ".zip" {
							recordingPath = filepath.Join(searchDir, entry.Name())
							break
						}
					}
				}
				if recordingPath != "" {
					break
				}
			}
		}

		if childErr != nil {
			inst.SetState(SubagentStateError, childErr)
		} else {
			inst.SetState(SubagentStateIdle, nil)
		}

		if inst.Engine.conv != nil {
			history := inst.Engine.History()
			var protoMsgs []*pb.ConversationMessage
			for _, m := range history {
				protoMsgs = append(protoMsgs, mapHistoryMessageToProto(m))
			}
			inst.Engine.conv.SetMessages(protoMsgs)
			_ = inst.Engine.conv.SaveAll()
		}

		notifyContent := fmt.Sprintf("Browser subagent completed.\nConversation ID: %s\nArtifact Directory: %s\n",
			inst.ConversationID, inst.Engine.brainDir)
		if recordingPath != "" {
			notifyContent += fmt.Sprintf("Recording: %s\n", recordingPath)
		}
		notifyContent += fmt.Sprintf("\nResult:\n%s", resultText)

		if childErr != nil {
			notifyContent = fmt.Sprintf("Browser subagent failed: %v\nConversation ID: %s\nArtifact Directory: %s\n",
				childErr, inst.ConversationID, inst.Engine.brainDir)
			if recordingPath != "" {
				notifyContent += fmt.Sprintf("Recording: %s\n", recordingPath)
			}
			notifyContent += fmt.Sprintf("\nPartial result:\n%s", resultText)
		}

		e.notifyParent(tools.SystemMessage{
			Source:  "browser_subagent_complete",
			TaskID:  inst.ConversationID,
			Content: notifyContent,
		})

		if e.agentBus != nil {
			e.agentBus.Unsubscribe(inst.ConversationID)
		}
		e.subagentTracker.Remove(inst.ConversationID)

	}(instance, task)

	if action := step.GetBrowserSubagent(); action != nil {
		action.ConversationId = childConvID
	}

	resultMsg := fmt.Sprintf("Launched browser subagent (Conversation ID: %s). It will run in the background and you will be notified when it completes.", childConvID)
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultMsg, false))

	return nil
}
