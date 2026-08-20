package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/divmora/localharness/cmd/lhctl/client"
	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// Model is the main Bubbletea TUI application model.
type Model struct {
	client            *client.Client
	viewport          viewport.Model
	textInput         textinput.Model
	spinner           spinner.Model
	history           *ChatHistory
	subagents         *SubagentViewManager
	completer         *FileCompleter
	autocompleteState AutocompleteState
	approval          *ActiveApproval
	showHelp          bool
	showSubagents     bool
	yoloMode          bool
	modelName         string
	workspaces        []string
	promptTokens      int
	completionTokens  int
	totalTokens       int
	status            string // IDLE, RUNNING, STREAMING, BLOCKED, WAITING
	mode              AgentMode
	width             int
	height            int
	ready             bool
	quitting          bool
	lastInterrupt     time.Time
}


// InitialModel creates the TUI model.
func InitialModel(c *client.Client, workspaces []string, yolo bool) Model {
	ta := textinput.New()
	ta.Placeholder = "Ask a question, issue a command, @file, or /help..."
	ta.Focus()
	ta.CharLimit = 4096
	ta.Width = 80

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorWarning)

	if len(workspaces) == 0 {
		workspaces = []string{"."}
	}

	return Model{
		client:     c,
		textInput:  ta,
		spinner:    s,
		history:    NewChatHistory(),
		subagents:  NewSubagentViewManager(),
		completer:  NewFileCompleter(workspaces),
		workspaces: workspaces,
		yoloMode:   yolo,
		mode:       ModeDefault,
		status:     "IDLE",
	}
}


// Init initializes Bubbletea subscriptions.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spinner.Tick,
		listenForEvents(m.client),
		listenForErrors(m.client),
	)
}

func listenForEvents(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		if c == nil {
			return nil
		}
		msg, ok := <-c.Events()
		if !ok {
			return WSErrorMsg{Err: fmt.Errorf("connection closed by server")}
		}
		return ServerEventMsg{Msg: msg}
	}
}

func listenForErrors(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		if c == nil {
			return nil
		}
		err, ok := <-c.Errors()
		if !ok {
			return nil
		}
		return WSErrorMsg{Err: err}
	}
}

// Update handles state transitions and events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 2
		footerHeight := 3
		vpHeight := msg.Height - headerHeight - footerHeight
		if vpHeight < 4 {
			vpHeight = 4
		}

		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.textInput.Width = msg.Width - 6

	case spinner.TickMsg:
		m.spinner, spCmd = m.spinner.Update(msg)
		cmds = append(cmds, spCmd)
		if m.status == "RUNNING" || m.status == "STREAMING" {
			m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
		}

	case tea.KeyMsg:
		// Modal Key Handling
		if m.approval != nil {
			switch msg.String() {
			case "y", "Y", "enter":
				_ = m.client.SendPermissionResponse(m.approval.RequestID, true, "")
				m.approval = nil
				m.status = "RUNNING"
				return m, nil
			case "n", "N", "esc":
				_ = m.client.SendPermissionResponse(m.approval.RequestID, false, "denied by user in TUI")
				m.approval = nil
				m.status = "RUNNING"
				return m, nil
			case "yolo":
				m.yoloMode = true
				_ = m.client.SendSetYoloMode(true)
				_ = m.client.SendPermissionResponse(m.approval.RequestID, true, "")
				m.approval = nil
				m.status = "RUNNING"
				return m, nil
			}
			return m, nil
		}

		if m.showSubagents {
			switch msg.String() {
			case "up", "k":
				m.subagents.NavigateUp()
			case "down", "j":
				m.subagents.NavigateDown()
			case "enter":
				if m.subagents.IsDrillDown() {
					m.subagents.ExitDrillDown()
				} else {
					m.subagents.SelectDrillDown()
				}
			case "esc", "q":
				if m.subagents.IsDrillDown() {
					m.subagents.ExitDrillDown()
				} else {
					m.showSubagents = false
				}
			}
			return m, nil
		}

		if m.showHelp {
			if msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
				m.showHelp = false
			}
			return m, nil
		}

		// Autocomplete Dropdown Navigation
		if m.autocompleteState.Active {
			switch msg.String() {
			case "up":
				if m.autocompleteState.SelectedIndex > 0 {
					m.autocompleteState.SelectedIndex--
				} else {
					m.autocompleteState.SelectedIndex = len(m.autocompleteState.Candidates) - 1
				}
				return m, nil
			case "down":
				if m.autocompleteState.SelectedIndex < len(m.autocompleteState.Candidates)-1 {
					m.autocompleteState.SelectedIndex++
				} else {
					m.autocompleteState.SelectedIndex = 0
				}
				return m, nil
			case "tab", "enter":
				if len(m.autocompleteState.Candidates) > 0 {
					selected := m.autocompleteState.Candidates[m.autocompleteState.SelectedIndex]
					val := m.textInput.Value()
					if m.autocompleteState.Type == AutocompleteSlashCommand {
						m.textInput.SetValue(selected.Value + " ")
						m.textInput.SetCursor(len(m.textInput.Value()))
					} else {
						prefix := val[:m.autocompleteState.CursorPos]
						m.textInput.SetValue(prefix + "@" + selected.Value + " ")
						m.textInput.SetCursor(len(m.textInput.Value()))
					}
					m.autocompleteState.Active = false
				}
				return m, nil
			case "esc":
				m.autocompleteState.Active = false
				return m, nil
			}
		}



		// Shift+Tab Mode Cycling (default -> accept-edits -> plan -> default)
		if msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab" {
			m.mode = m.mode.Next()
			switch m.mode {
			case ModeDefault:
				m.history.AddSystemMessage("Mode: DEFAULT (Safe mode — prompts for file edits and shell commands)")
			case ModeAcceptEdits:
				m.history.AddSystemMessage("Mode: ACCEPT-EDITS (Auto-approves file edits; shell commands require confirmation)")
			case ModePlan:
				m.history.AddSystemMessage("Mode: PLAN (Plan-before-act mode — enforces research & implementation_plan.md before code changes)")
			}
			m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
			m.viewport.GotoBottom()
			return m, nil
		}

		// Global Shortcuts
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.status == "RUNNING" || m.status == "STREAMING" {
				if time.Since(m.lastInterrupt) < 2*time.Second {
					m.quitting = true
					if m.client != nil {
						_ = m.client.Close()
					}
					return m, tea.Quit
				}
				_ = m.client.SendInterrupt()
				m.lastInterrupt = time.Now()
				m.status = "IDLE"
				m.history.AddSystemMessage("Turn interrupted. Press Ctrl+C again to exit.")
				m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
				m.viewport.GotoBottom()
				return m, nil
			}
			m.quitting = true
			if m.client != nil {
				_ = m.client.Close()
			}
			return m, tea.Quit

		case tea.KeyCtrlD:
			m.quitting = true
			if m.client != nil {
				_ = m.client.Close()
			}
			return m, tea.Quit

		case tea.KeyEnter:
			input := strings.TrimSpace(m.textInput.Value())
			if input == "" {
				return m, nil
			}

			m.textInput.Reset()
			m.autocompleteState.Active = false

			if cmd, isCmd := ParseCommand(input); isCmd {
				teaCmd := m.handleSlashCommand(cmd)
				m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
				m.viewport.GotoBottom()
				return m, teaCmd
			}

			m.history.AddUserMessage(input)
			m.status = "RUNNING"

			promptToSend := input
			if m.mode == ModePlan && !strings.HasPrefix(strings.ToLower(input), "/plan") {
				promptToSend = fmt.Sprintf("%s\n\n[Mode: PLAN] Please research the codebase using read tools and write implementation_plan.md in the brain directory before making any code modifications.", input)
			}
			_ = m.client.SendUserMessage(promptToSend, nil, nil)
			m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
			m.viewport.GotoBottom()
			return m, nil
		}


	case ServerEventMsg:
		m.handleServerEvent(msg.Msg)
		m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
		m.viewport.GotoBottom()
		cmds = append(cmds, listenForEvents(m.client))

	case WSErrorMsg:
		if m.quitting {
			return m, tea.Quit
		}
		m.history.AddSystemMessage(fmt.Sprintf("Server disconnected: %v", msg.Err))
		m.status = "IDLE"
		m.viewport.SetContent(m.history.RenderView(m.spinner, m.width))
		m.viewport.GotoBottom()
	}


	// Update text input and check for @ autocomplete trigger
	m.textInput, tiCmd = m.textInput.Update(msg)
	cmds = append(cmds, tiCmd)

	slashQ, isSlash := DetectSlashCommandQuery(m.textInput.Value(), m.textInput.Position())
	if isSlash {
		matches := MatchSlashCommands(slashQ)
		if len(matches) > 0 {
			selIndex := 0
			if m.autocompleteState.Active && m.autocompleteState.Type == AutocompleteSlashCommand && m.autocompleteState.Query == slashQ {
				selIndex = m.autocompleteState.SelectedIndex
				if selIndex >= len(matches) {
					selIndex = len(matches) - 1
				}
			}
			m.autocompleteState = AutocompleteState{
				Active:        true,
				Type:          AutocompleteSlashCommand,
				Query:         slashQ,
				Candidates:    matches,
				SelectedIndex: selIndex,
				CursorPos:     0,
			}
		} else {
			m.autocompleteState.Active = false
		}
	} else {
		query, startPos, found := DetectFileQuery(m.textInput.Value(), m.textInput.Position())
		if found {
			matches := m.completer.Match(query, 8)
			if len(matches) > 0 {
				var candidates []AutocompleteCandidate
				for _, match := range matches {
					candidates = append(candidates, AutocompleteCandidate{
						Value:       match,
						DisplayText: "@" + match,
					})
				}
				selIndex := 0
				if m.autocompleteState.Active && m.autocompleteState.Type == AutocompleteFile && m.autocompleteState.Query == query {
					selIndex = m.autocompleteState.SelectedIndex
					if selIndex >= len(candidates) {
						selIndex = len(candidates) - 1
					}
				}
				m.autocompleteState = AutocompleteState{
					Active:        true,
					Type:          AutocompleteFile,
					Query:         query,
					Candidates:    candidates,
					SelectedIndex: selIndex,
					CursorPos:     startPos,
				}
			} else {
				m.autocompleteState.Active = false
			}
		} else {
			m.autocompleteState.Active = false
		}
	}



	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) handleSlashCommand(cmd *Command) tea.Cmd {
	switch cmd.Name {
	case "help":
		m.showHelp = true
		return nil

	case "mode":
		if len(cmd.Args) > 0 {
			switch strings.ToLower(cmd.Args[0]) {
			case "default", "safe":
				m.mode = ModeDefault
			case "accept-edits", "accept_edits", "edits", "edit":
				m.mode = ModeAcceptEdits
			case "plan", "planning":
				m.mode = ModePlan
			default:
				m.history.AddSystemMessage("Usage: /mode [default | accept-edits | plan]")
				return nil
			}
		} else {
			m.mode = m.mode.Next()
		}
		switch m.mode {
		case ModeDefault:
			m.history.AddSystemMessage("Mode switched to: DEFAULT (Safe mode — prompts for file edits and shell execution)")
		case ModeAcceptEdits:
			m.history.AddSystemMessage("Mode switched to: ACCEPT-EDITS (Auto-approves file edits; shell commands require confirmation)")
		case ModePlan:
			m.history.AddSystemMessage("Mode switched to: PLAN (Plan-before-act mode — requires research & implementation_plan.md before code changes)")
		}
		return nil


	case "plan":
		goal := strings.Join(cmd.Args, " ")
		var prompt string
		if goal != "" {
			prompt = fmt.Sprintf("Please create a comprehensive implementation plan for: %s.\n\nFirst, research the codebase using read tools, then create implementation_plan.md in the brain directory before modifying any code.", goal)
			m.history.AddUserMessage("/plan " + goal)
		} else {
			prompt = "Please create a comprehensive implementation plan for the current task. First, research the codebase, then create implementation_plan.md in the brain directory before modifying any code."
			m.history.AddUserMessage("/plan")
		}
		m.status = "RUNNING"
		_ = m.client.SendUserMessage(prompt, nil, nil)
		return nil

	case "pause", "interrupt":
		if m.status == "RUNNING" || m.status == "STREAMING" {
			_ = m.client.SendInterrupt()
			m.status = "IDLE"
			m.history.AddSystemMessage("Turn interrupted. Use /resume to continue.")
		} else {
			m.history.AddSystemMessage("Agent is not currently running a turn.")
		}
		return nil

	case "resume":
		msg := strings.Join(cmd.Args, " ")
		_ = m.client.SendResume(msg)
		m.status = "RUNNING"
		if msg != "" {
			m.history.AddUserMessage("/resume " + msg)
		} else {
			m.history.AddSystemMessage("Resumed agent execution.")
		}
		return nil



	case "subagents":
		m.showSubagents = true
		return nil

	case "yolo":
		m.yoloMode = !m.yoloMode
		_ = m.client.SendSetYoloMode(m.yoloMode)
		if m.yoloMode {
			m.history.AddSystemMessage("YOLO Mode ENABLED: All tool actions will auto-execute without prompts.")
		} else {
			m.history.AddSystemMessage("YOLO Mode DISABLED: Safe mode active with approval prompts.")
		}
		return nil

	case "workspace":
		if len(cmd.Args) == 0 || cmd.Args[0] == "list" {
			_ = m.client.SendWorkspaceRequest("list", "", "", "")
		} else if len(cmd.Args) >= 2 && cmd.Args[0] == "add" {
			_ = m.client.SendWorkspaceRequest("add", cmd.Args[1], "", "")
		} else if len(cmd.Args) >= 2 && cmd.Args[0] == "remove" {
			_ = m.client.SendWorkspaceRequest("remove", cmd.Args[1], "", "")
		} else {
			m.history.AddSystemMessage("Usage: /workspace [list | add <path> | remove <path>]")
		}
		return nil

	case "compact":
		m.history.AddSystemMessage("Compacting conversation context...")
		_ = m.client.SendUserMessage("[Compact Context]", nil, []string{"Please compact previous messages and summarize progress."})
		return nil

	case "model":
		if len(cmd.Args) > 0 {
			m.modelName = cmd.Args[0]
			m.history.AddSystemMessage(fmt.Sprintf("Switched model target to: %s", m.modelName))
		} else {
			m.history.AddSystemMessage(fmt.Sprintf("Current Model: %s", m.modelName))
		}
		return nil

	case "status":
		statusMsg := fmt.Sprintf("Session Status: %s | YOLO: %v | Workspaces: %d | Subagents: %d active | Tokens: %d",
			m.status, m.yoloMode, len(m.workspaces), m.subagents.RunningCount(), m.totalTokens)
		m.history.AddSystemMessage(statusMsg)
		return nil

	case "clear":
		m.history.Clear()
		return nil

	case "detach":
		_ = m.client.Close()
		return tea.Quit

	case "exit", "quit":
		_ = m.client.Close()
		return tea.Quit
	}
	return nil
}


func (m *Model) handleServerEvent(srvMsg *pb.ServerMessage) {
	if srvMsg == nil {
		return
	}

	if srvMsg.GetInitResponse() != nil {
		m.history.AddSystemMessage(fmt.Sprintf("Connected to LocalHarness session %s (v%s)",
			srvMsg.GetInitResponse().ConversationId, srvMsg.GetInitResponse().HarnessVersion))
	}

	if step := srvMsg.GetStepUpdate(); step != nil {
		// Update tokens if present
		if step.Usage != nil {
			m.promptTokens = int(step.Usage.PromptTokens)
			m.completionTokens = int(step.Usage.CompletionTokens)
			m.totalTokens = int(step.Usage.TotalTokens)
		}

		// Handle streaming text
		if step.State == pb.StepUpdate_STATE_STREAMING {
			m.status = "STREAMING"
			if step.TextDelta != "" {
				m.history.AppendStreamingText(step.TextDelta)
			}
			if step.ThinkingDelta != "" {
				m.history.AppendThinkingText(step.ThinkingDelta)
			}
			return
		}

		// Handle Permission Prompt (WAITING)
		if step.State == pb.StepUpdate_STATE_WAITING && step.GetPermissionRequest() != nil {
			pr := step.GetPermissionRequest()
			if m.yoloMode {
				_ = m.client.SendPermissionResponse(pr.RequestId, true, "")
				return
			}
			if m.mode == ModeAcceptEdits && (pr.ToolName == "write_to_file" || pr.ToolName == "replace_file_content") {
				_ = m.client.SendPermissionResponse(pr.RequestId, true, "")
				return
			}
			m.status = "WAITING"
			m.approval = &ActiveApproval{
				RequestID:   pr.RequestId,
				ToolName:    pr.ToolName,
				Description: pr.ArgsSummary,
				DiffPreview: pr.DiffPreview,
			}
			return
		}



		// Handle active tool execution
		if step.State == pb.StepUpdate_STATE_ACTIVE && step.Action != nil {
			m.status = "RUNNING"
			name, args := extractActionDetails(step)
			m.history.StartToolCall(name, args)
			return
		}

		// Handle tool execution finished
		if step.State == pb.StepUpdate_STATE_DONE || step.State == pb.StepUpdate_STATE_ERROR {
			isErr := step.State == pb.StepUpdate_STATE_ERROR
			name, diff, res := extractActionResult(step)
			m.history.FinishToolCall(name, res, isErr, diff)
			return
		}
	}

	if traj := srvMsg.GetTrajectoryState(); traj != nil {
		if traj.ParentTrajectoryId != "" || traj.Depth > 0 {
			// Subagent event
			stateStr := traj.State.String()
			m.subagents.AddOrUpdate(&SubagentState{
				ConversationID: traj.TrajectoryId,
				ParentID:       traj.ParentTrajectoryId,
				Depth:          int(traj.Depth),
				State:          stateStr,
			})
		}

		if traj.State == pb.TrajectoryState_TRAJ_IDLE {
			m.status = "IDLE"
			m.history.FlushStreaming()
		} else if traj.State == pb.TrajectoryState_TRAJ_RUNNING {
			m.status = "RUNNING"
		}
	}

	if wsResp := srvMsg.GetWorkspaceResponse(); wsResp != nil {
		var wsDirs []string
		for _, ws := range wsResp.Workspaces {
			wsDirs = append(wsDirs, ws.Directory)
		}
		m.workspaces = wsDirs
		m.completer.SetWorkspaces(wsDirs)
		m.history.AddSystemMessage(fmt.Sprintf("📂 %s (Total: %d)", wsResp.Message, len(wsResp.Workspaces)))
	}

	if rc := srvMsg.GetReplayComplete(); rc != nil {
		m.history.AddSystemMessage(fmt.Sprintf("🔄 Replayed %d historical events from buffer", rc.EventCount))
	}

	if errEv := srvMsg.GetError(); errEv != nil {
		m.history.AddSystemMessage(fmt.Sprintf("❌ Error [%s]: %s", errEv.Code, errEv.Message))
	}
}

func extractActionDetails(step *pb.StepUpdate) (string, string) {
	if step.Action == nil {
		return "tool", ""
	}
	switch a := step.Action.(type) {
	case *pb.StepUpdate_ViewFile:
		return "view_file", a.ViewFile.Path
	case *pb.StepUpdate_WriteToFile:
		return "write_to_file", a.WriteToFile.Path
	case *pb.StepUpdate_ReplaceFileContent:
		return "replace_file_content", a.ReplaceFileContent.Path
	case *pb.StepUpdate_RunCommand:
		return "run_command", a.RunCommand.Command
	case *pb.StepUpdate_ListDir:
		return "list_dir", a.ListDir.Path
	case *pb.StepUpdate_GrepSearch:
		return "grep_search", a.GrepSearch.Query
	case *pb.StepUpdate_FindFile:
		return "find_file", a.FindFile.Pattern
	case *pb.StepUpdate_InvokeSubagent:
		return "invoke_subagent", fmt.Sprintf("%d subagents", len(a.InvokeSubagent.Subagents))
	case *pb.StepUpdate_SearchWeb:
		return "search_web", a.SearchWeb.Query
	case *pb.StepUpdate_ReadUrlContent:
		return "read_url_content", a.ReadUrlContent.Url
	case *pb.StepUpdate_Schedule:
		return "schedule", a.Schedule.Prompt
	case *pb.StepUpdate_Finish:
		return "finish", ""
	default:
		return "tool", ""
	}
}

func extractActionResult(step *pb.StepUpdate) (string, string, string) {
	if step.Action == nil {
		return "tool", "", ""
	}
	switch a := step.Action.(type) {
	case *pb.StepUpdate_WriteToFile:
		return "write_to_file", a.WriteToFile.DiffBlock, fmt.Sprintf("Created %s", a.WriteToFile.Path)
	case *pb.StepUpdate_ReplaceFileContent:
		return "replace_file_content", a.ReplaceFileContent.DiffBlock, fmt.Sprintf("Updated %s", a.ReplaceFileContent.Path)
	case *pb.StepUpdate_RunCommand:
		out := a.RunCommand.Stdout
		if out == "" {
			out = a.RunCommand.Stderr
		}
		return "run_command", "", out
	case *pb.StepUpdate_ViewFile:
		return "view_file", "", fmt.Sprintf("%d lines read", a.ViewFile.TotalLines)
	case *pb.StepUpdate_ListDir:
		return "list_dir", "", fmt.Sprintf("%d items found", len(a.ListDir.Entries))
	case *pb.StepUpdate_GrepSearch:
		return "grep_search", "", fmt.Sprintf("%d matches found", a.GrepSearch.TotalMatches)
	case *pb.StepUpdate_FindFile:
		return "find_file", "", fmt.Sprintf("%d files matched", len(a.FindFile.Matches))
	case *pb.StepUpdate_InvokeSubagent:
		return "invoke_subagent", "", fmt.Sprintf("Launched %d subagents", len(a.InvokeSubagent.LaunchResults))
	default:
		return "tool", "", ""
	}
}

// View renders the TUI interface.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Initializing LocalHarness TUI..."
	}


	var overlay string
	if m.approval != nil {
		overlay = RenderApprovalModal(m.approval, m.width)
	} else if m.showSubagents {
		overlay = m.subagents.Render(m.width, m.height)
	} else if m.showHelp {
		overlay = RenderHelpView(m.width)
	}

	statusBar := RenderStatusBar(StatusBarState{
		Status:           m.status,
		Mode:             m.mode,
		ModelName:        m.modelName,
		PromptTokens:     m.promptTokens,
		CompletionTokens: m.completionTokens,
		TotalTokens:      m.totalTokens,
		RunningSubagents: m.subagents.RunningCount(),
		TotalSubagents:   m.subagents.TotalCount(),
		YoloMode:         m.yoloMode,
		WorkspaceCount:   len(m.workspaces),
	}, m.width)


	autocompleteView := ""
	if m.autocompleteState.Active {
		autocompleteView = RenderAutocomplete(&m.autocompleteState, m.width) + "\n"
	}

	inputLine := lipgloss.NewStyle().Padding(0, 1).Render(
		lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true).Render("❯ ") + m.textInput.View(),
	)

	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		autocompleteView+inputLine,
		statusBar,
	)

	if overlay != "" {
		// Place overlay in the center
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	}

	return mainView
}
