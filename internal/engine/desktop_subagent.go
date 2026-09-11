package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/llm"
	"github.com/divmora/localharness/internal/tools"
	"github.com/divmora/localharness/internal/util"
)

const desktopSubagentSystemPrompt = `You are a specialized desktop computer use agent capable of interacting directly with host applications, windows, and desktop GUI interfaces.
Your objective is to accomplish the user's task by inspecting windows, capturing screenshots, focusing applications, clicking elements, typing text, and triggering keyboard shortcuts.
You have access to desktop automation tools: desktop_screenshot, desktop_list_windows, desktop_focus_window, desktop_click, desktop_type, desktop_shortcut.

CRITICAL OPERATIONAL RULES:
1. Do NOT spend turns reading source code files or exploring directories unless explicitly instructed. Focus directly on the desktop GUI.
2. For web applications, websites, web pages, or browser tasks, DO NOT use desktop_subagent — use browser_subagent instead. browser_subagent has native DOM access, element targeting, and Playwright automation.
3. Always begin by listing windows or taking a screenshot to understand the current visual state.
4. After taking actions, verify the state with a new screenshot before completing your task.
5. Return a clear and concise summary of your actions, observations, and results.`

// desktopSubagentDeclaration returns the FunctionDeclaration for desktop_subagent.
func desktopSubagentDeclaration() llm.FunctionDeclaration {
	return llm.FunctionDeclaration{
		Name: "desktop_subagent",
		Description: "Start a desktop subagent to interact with native desktop applications and windows on the host OS (e.g. native apps, system utilities, desktop windows). " +
			"The subagent has tools to inspect windows, capture screenshots, focus windows, click coordinates, type text, and use keyboard shortcuts. " +
			"IMPORTANT: For web applications, websites, web pages, or browser testing, DO NOT use desktop_subagent — use browser_subagent instead.",
		Parameters: map[string]interface{}{
			"type":     "object",
			"required": []string{"TaskName", "Task", "TaskSummary"},
			"properties": map[string]interface{}{
				"TaskName": map[string]interface{}{
					"type":        "string",
					"description": "Name of the task that the desktop subagent is performing.",
				},
				"Task": map[string]interface{}{
					"type":        "string",
					"description": "A clear, actionable task description for the desktop subagent.",
				},
				"TaskSummary": map[string]interface{}{
					"type":        "string",
					"description": "A short, user-friendly summary of the task (1-2 sentences max).",
				},
				"TargetApplication": map[string]interface{}{
					"type":        "string",
					"description": "Name or title of the target desktop application (e.g. 'Slack', 'Terminal', 'VS Code', 'Notes').",
				},
				"ReusedSubagentId": map[string]interface{}{
					"type":        "string",
					"description": "ID of a previous desktop subagent to resume from.",
				},
				"MediaPaths": map[string]interface{}{
					"type":        "array",
					"description": "Optional absolute paths to media or image files to provide as visual context.",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

// executeDesktopSubagent handles the desktop_subagent tool call.
func (e *Engine) executeDesktopSubagent(ctx context.Context, tc llm.ToolCall, step *pb.StepUpdate) error {
	taskName, _ := tc.Args["TaskName"].(string)
	task, _ := tc.Args["Task"].(string)
	taskSummary, _ := tc.Args["TaskSummary"].(string)
	targetApp, _ := tc.Args["TargetApplication"].(string)
	reusedSubagentId, _ := tc.Args["ReusedSubagentId"].(string)

	var mediaPaths []string
	if mp, ok := tc.Args["MediaPaths"].([]interface{}); ok {
		for _, v := range mp {
			if s, ok := v.(string); ok {
				mediaPaths = append(mediaPaths, s)
			}
		}
	}

	if task == "" {
		e.feedToolError(tc, step, "Task is required for desktop_subagent")
		return nil
	}

	if len(mediaPaths) > 0 {
		task += "\n\nMedia Paths provided for context:\n"
		for _, p := range mediaPaths {
			task += fmt.Sprintf("- %s\n", p)
		}
	}

	// Populate step action
	if action := step.GetDesktopSubagent(); action != nil {
		action.TaskName = taskName
		action.Task = task
		action.TaskSummary = taskSummary
		action.TargetApplication = targetApp
		action.ReusedSubagentId = reusedSubagentId
		action.MediaPaths = mediaPaths
	}

	// Depth check
	if e.depth >= e.maxDepth {
		errMsg := fmt.Sprintf("max subagent depth (%d) exceeded — cannot spawn desktop subagent", e.maxDepth)
		e.feedSubagentError(tc, step, errMsg)
		return nil
	}

	// Concurrency check
	if atomic.LoadInt32(&e.activeSubagents) >= int32(e.maxSubagents) {
		errMsg := fmt.Sprintf("max concurrent subagents (%d) reached — cannot launch desktop subagent", e.maxSubagents)
		e.feedSubagentError(tc, step, errMsg)
		return nil
	}

	childConvID := util.NewUUID()
	var initialHistory []llm.Message
	if reusedSubagentId != "" {
		childConvID = reusedSubagentId
	}

	childTrajID := fmt.Sprintf("%s/sub_%d_desktop", e.trajectoryID, step.StepIndex)

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
				e.logger.Warn("failed to resume desktop subagent conversation", "id", reusedSubagentId, "error", err)
			}
		} else {
			childConv, err = e.convMgr.CreateWithID(childConvID, &pb.HarnessConfig{})
			if err != nil {
				e.logger.Warn("failed to create desktop subagent conversation", "error", err)
			} else {
				childConv.State.ParentConversationId = e.convID
				childConv.State.AgentRole = "Desktop Agent"
				childConv.State.AgentTypeName = "desktop"
				childConv.State.AgentDepth = int32(e.depth + 1)
			}
		}
	}

	atomic.AddInt32(&e.activeSubagents, 1)

	// Create child engine
	childEngine := NewEngine(Config{
		Provider:             e.provider,
		ToolRegistry:         e.toolRegistry,
		SystemPrompt:         desktopSubagentSystemPrompt,
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
		Logger:               e.logger.With("subagent", "desktop", "role", "Desktop Agent"),
		HostToolHandler:      e.hostToolHandler,
		HostToolNames:        e.hostToolNames,
		HostToolDecls:        e.hostToolDecls,
		PermissionHandler:    e.permissionHandler,
		QuestionHandler:      e.questionHandler,
		SubagentsEnabled:     false,
		ExcludeMCPTools:      false,
		ExcludeToolGroups:    map[tools.ToolGroup]bool{tools.ToolGroupWrite: true},
		MCPManager:           e.mcpMgr,
		AgentBus:             e.agentBus,
		ConversationManager:  e.convMgr,
		ParentConversationID: e.convID,
		AgentRole:            "Desktop Agent",
		AgentTypeName:        "desktop",
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
		TypeName:       "desktop",
		Role:           "Desktop Agent",
		State:          SubagentStateRunning,
		Engine:         childEngine,
		Cancel:         cancel,
		Inbox:          make(chan string, subagentInboxSize),
	}
	e.subagentTracker.Register(instance)

	// Launch in background
	go func(inst *SubagentInstance, prompt string, target string) {
		defer atomic.AddInt32(&e.activeSubagents, -1)

		// Focus target app and take initial visual context snapshot if available
		driver := getDesktopDriver()
		var initialScreenshot string
		if target != "" {
			_ = driver.FocusWindow(childCtx, target)
		}
		if inst.Engine.brainDir != "" {
			initialPath := filepath.Join(inst.Engine.brainDir, "scratch", fmt.Sprintf("initial_%d.png", time.Now().UnixMilli()))
			if p, err := driver.CaptureScreen(childCtx, target, initialPath); err == nil {
				initialScreenshot = p
			}
		}

		subagentPrompt := prompt
		if initialScreenshot != "" {
			subagentPrompt = fmt.Sprintf("%s\n\nInitial Screen State: %s\n![Initial Screenshot](%s)", prompt, initialScreenshot, initialScreenshot)
		}

		childErr := inst.Engine.Run(childCtx, subagentPrompt)

		resultText := extractFinalResponse(inst.Engine.History())

		// Capture final verification snapshot
		var finalScreenshot string
		if inst.Engine.brainDir != "" {
			finalPath := filepath.Join(inst.Engine.brainDir, "scratch", fmt.Sprintf("final_%d.png", time.Now().UnixMilli()))
			if p, err := driver.CaptureScreen(context.Background(), target, finalPath); err == nil {
				finalScreenshot = p
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

		notifyContent := fmt.Sprintf("Desktop subagent completed.\nConversation ID: %s\nTarget Application: %s\nArtifact Directory: %s\n",
			inst.ConversationID, target, inst.Engine.brainDir)
		if finalScreenshot != "" {
			notifyContent += fmt.Sprintf("Final Screenshot: %s\n", finalScreenshot)
		}
		notifyContent += fmt.Sprintf("\nResult:\n%s", resultText)

		if childErr != nil {
			notifyContent = fmt.Sprintf("Desktop subagent failed: %v\nConversation ID: %s\nTarget Application: %s\nArtifact Directory: %s\n\nPartial result:\n%s",
				childErr, inst.ConversationID, target, inst.Engine.brainDir, resultText)
		}

		e.notifyParent(tools.SystemMessage{
			Source:  "desktop_subagent_complete",
			TaskID:  inst.ConversationID,
			Content: notifyContent,
		})

		if e.agentBus != nil {
			e.agentBus.Unsubscribe(inst.ConversationID)
		}
		e.subagentTracker.Remove(inst.ConversationID)

	}(instance, task, targetApp)

	if action := step.GetDesktopSubagent(); action != nil {
		action.ConversationId = childConvID
	}

	resultMsg := fmt.Sprintf("Launched desktop subagent (Conversation ID: %s) for application %q. It will run in the background and you will be notified when it completes.", childConvID, targetApp)
	step.State = pb.StepUpdate_STATE_DONE
	e.emitStep(step)
	e.history = append(e.history, toolResultMsg(tc, resultMsg, false))

	return nil
}
