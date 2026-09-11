package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/divmora/localharness/cmd/lhctl/client"
	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/daemon"
)

// Model is the main Bubbletea TUI application model.
type Model struct {
	client            *client.Client
	textarea          textarea.Model
	spinner           spinner.Model
	history           *ChatHistory
	subagents         *SubagentViewManager
	tasks             *TasksViewManager
	completer         *FileCompleter
	customCommands    *CustomCommandManager
	autocompleteState AutocompleteState
	approval          *ActiveApproval
	question          *ActiveQuestion
	artifactReview    *ActiveArtifactReview
	showThinking      bool
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
	return InitialModelWithHistory(c, workspaces, yolo, nil)
}

// InitialModelWithHistory creates the TUI model with optional preloaded history.
func InitialModelWithHistory(c *client.Client, workspaces []string, yolo bool, initialState *pb.ConversationState) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask a question, issue a command, @file, or /help..."
	ta.Focus()
	ta.CharLimit = 16384
	ta.Prompt = "❯ "
	ta.SetPromptFunc(2, func(lineIdx int) string {
		if lineIdx == 0 {
			return "❯ "
		}
		return "  "
	})
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(ColorMuted)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.EndOfBuffer = lipgloss.NewStyle()
	ta.BlurredStyle.EndOfBuffer = lipgloss.NewStyle()
	ta.EndOfBufferCharacter = ' '
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.Unbind()
	ta.SetHeight(1)
	ta.SetWidth(80)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorWarning)

	if len(workspaces) == 0 {
		workspaces = []string{"."}
	}

	hist := NewChatHistory()
	hist.SetWorkspaces(workspaces)
	hist.SetShowThinking(false)
	if initialState != nil {
		hist.LoadFromState(initialState)
		if len(initialState.Messages) > 0 {
			hist.AddSystemMessage(fmt.Sprintf("Resumed conversation %s (%d messages)", initialState.ConversationId, len(initialState.Messages)))
		}
	}

	return Model{
		client:         c,
		textarea:       ta,
		spinner:        s,
		history:        hist,
		subagents:      NewSubagentViewManager(),
		tasks:          NewTasksViewManager(),
		completer:      NewFileCompleter(workspaces),
		customCommands: NewCustomCommandManager(workspaces),
		workspaces:     workspaces,
		yoloMode:       yolo,
		mode:           ModeDefault,
		status:         "IDLE",
		showThinking:   false,
		ready:          true,
	}
}

// Init initializes Bubbletea subscriptions.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textarea.Blink,
		m.spinner.Tick,
		listenForEvents(m.client),
		listenForErrors(m.client),
	}
	if len(m.history.items) > 0 {
		initial := m.history.FormatInitialHistory(m.getWidth())
		if initial != "" {
			cmds = append(cmds, tea.Println(initial))
		}
	}
	return tea.Batch(cmds...)
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
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateDimensions()
		return m, nil

	case spinner.TickMsg:
		m.spinner, spCmd = m.spinner.Update(msg)
		cmds = append(cmds, spCmd)
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		// Inline Approval Handling
		if m.approval != nil {
			targetLabel := m.approval.DisplayTarget()

			// Check if pressing 1-9 to toggle a sub-command
			if len(m.approval.SubCommands) > 1 && len(msg.String()) == 1 && msg.String()[0] >= '1' && msg.String()[0] <= '9' {
				idx := int(msg.String()[0] - '1')
				if idx < len(m.approval.SubCommands) {
					m.approval.ToggleSubcommand(idx)
					return m, nil
				}
			}

			hasCompound := len(m.approval.SubCommands) > 1
			approvedSubs := m.approval.ApprovedSubcommands()
			deniedSubs := m.approval.DeniedSubcommands()

			switch msg.String() {
			case "y", "Y", "enter":
				var sysMsg string
				if hasCompound && len(approvedSubs) < len(m.approval.SubCommands) {
					// Partial approval
					if len(approvedSubs) == 0 {
						_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, "all sub-commands denied by user in TUI", pb.PermissionResponse_SCOPE_ONCE, nil, deniedSubs)
						sysMsg = fmt.Sprintf("✗ Denied all sub-commands: %s", targetLabel)
					} else {
						reason := fmt.Sprintf("User approved sub-commands: [%s], but denied: [%s]. Approved sub-commands have been granted permission; please execute approved commands individually if needed.", strings.Join(approvedSubs, ", "), strings.Join(deniedSubs, ", "))
						_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, reason, pb.PermissionResponse_SCOPE_ONCE, approvedSubs, deniedSubs)
						sysMsg = fmt.Sprintf("✓ Partial approval: Allowed once [%s] | Denied [%s]", strings.Join(approvedSubs, ", "), strings.Join(deniedSubs, ", "))
					}
				} else {
					_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, true, "", pb.PermissionResponse_SCOPE_ONCE, approvedSubs, nil)
					sysMsg = fmt.Sprintf("✓ Allowed once: %s", targetLabel)
				}
				item := m.history.AddSystemMessage(sysMsg)
				m.approval = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

			case "c", "C", "a", "A":
				var sysMsg string
				if hasCompound && len(approvedSubs) < len(m.approval.SubCommands) {
					// Partial approval for conversation
					if len(approvedSubs) == 0 {
						_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, "all sub-commands denied by user in TUI", pb.PermissionResponse_SCOPE_CONVERSATION, nil, deniedSubs)
						sysMsg = fmt.Sprintf("✗ Denied all sub-commands: %s", targetLabel)
					} else {
						reason := fmt.Sprintf("User approved sub-commands for conversation: [%s], but denied: [%s]. Approved sub-commands have been granted conversation permission; please execute approved commands individually if needed.", strings.Join(approvedSubs, ", "), strings.Join(deniedSubs, ", "))
						_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, reason, pb.PermissionResponse_SCOPE_CONVERSATION, approvedSubs, deniedSubs)
						sysMsg = fmt.Sprintf("✓ Allowed for conversation: [%s] | Denied: [%s]", strings.Join(approvedSubs, ", "), strings.Join(deniedSubs, ", "))
					}
				} else {
					_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, true, "", pb.PermissionResponse_SCOPE_CONVERSATION, approvedSubs, nil)
					sysMsg = fmt.Sprintf("✓ Allowed for this conversation: %s", targetLabel)
				}
				item := m.history.AddSystemMessage(sysMsg)
				m.approval = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

			case "g", "G":
				var sysMsg string
				if hasCompound && len(approvedSubs) < len(m.approval.SubCommands) {
					// Partial approval globally
					if len(approvedSubs) == 0 {
						_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, "all sub-commands denied by user in TUI", pb.PermissionResponse_SCOPE_GLOBAL, nil, deniedSubs)
						sysMsg = fmt.Sprintf("✗ Denied all sub-commands: %s", targetLabel)
					} else {
						reason := fmt.Sprintf("User approved sub-commands globally: [%s], but denied: [%s]. Approved sub-commands have been permanently allowed; please execute approved commands individually if needed.", strings.Join(approvedSubs, ", "), strings.Join(deniedSubs, ", "))
						_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, reason, pb.PermissionResponse_SCOPE_GLOBAL, approvedSubs, deniedSubs)
						sysMsg = fmt.Sprintf("✓ Allowed globally: [%s] | Denied: [%s]", strings.Join(approvedSubs, ", "), strings.Join(deniedSubs, ", "))
					}
				} else {
					_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, true, "", pb.PermissionResponse_SCOPE_GLOBAL, approvedSubs, nil)
					sysMsg = fmt.Sprintf("✓ Allowed globally in ~/.divmora/config/settings.json: %s", targetLabel)
				}
				item := m.history.AddSystemMessage(sysMsg)
				m.approval = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

			case "n", "N", "esc":
				_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, false, "denied by user in TUI", pb.PermissionResponse_SCOPE_ONCE, nil, m.approval.SubCommands)
				item := m.history.AddSystemMessage(fmt.Sprintf("✗ Denied: %s", targetLabel))
				m.approval = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

			case "yolo":
				m.yoloMode = true
				_ = m.client.SendSetYoloMode(true)
				_ = m.client.SendPermissionResponseWithSubcommands(m.approval.RequestID, true, "", pb.PermissionResponse_SCOPE_ONCE, approvedSubs, nil)
				item := m.history.AddSystemMessage("YOLO Mode ENABLED: All tool actions will auto-execute without prompts.")
				m.approval = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
			}
			return m, nil
		}

		// Interactive Question Card Handling (Sequential Step-by-Step with Write-In)
		if m.question != nil {
			if m.question.IsWritingText {
				switch msg.String() {
				case "enter":
					m.question.ConfirmWriteIn()
					return m, nil
				case "esc":
					m.question.CancelWriteIn()
					return m, nil
				default:
					var cmd tea.Cmd
					m.question.TextInput, cmd = m.question.TextInput.Update(msg)
					return m, cmd
				}
			}

			switch msg.String() {
			case "o", "O":
				m.question.StartWriteIn()
				return m, nil
			case "up", "k":
				m.question.MoveCursorUp()
				return m, nil
			case "down", "j":
				m.question.MoveCursorDown()
				return m, nil
			case " ":
				if m.question.IsOtherFocused() {
					m.question.StartWriteIn()
				} else {
					m.question.ToggleFocused()
				}
				return m, nil
			case "b", "B", "p", "P":
				if m.question.HasPrev() {
					m.question.PrevQuestion()
					return m, nil
				}
			case "enter":
				if m.question.IsOtherFocused() && m.question.CustomText[m.question.CurrentQuestion] == "" {
					m.question.StartWriteIn()
					return m, nil
				}

				curSummary := m.question.CurrentAnswerSummary()
				if m.question.HasNext() {
					var item ChatItem
					if len(m.question.Questions) > 1 {
						item = m.history.AddSystemMessage(fmt.Sprintf("✓ [%d/%d] %s", m.question.CurrentQuestion+1, len(m.question.Questions), curSummary))
					}
					m.question.NextQuestion()
					if item.Content != "" {
						return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
					}
					return m, nil
				}

				// Final question answered - submit all answers
				var sysMsg string
				if len(m.question.Questions) > 1 {
					sysMsg = fmt.Sprintf("✓ [%d/%d] %s", m.question.CurrentQuestion+1, len(m.question.Questions), curSummary)
				} else {
					sysMsg = fmt.Sprintf("✓ Answered: %s", curSummary)
				}
				item := m.history.AddSystemMessage(sysMsg)

				answers := m.question.BuildAnswers()
				_ = m.client.SendQuestionResponse(m.question.RequestID, answers, false)
				m.question = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

			case "s", "S", "esc":
				_ = m.client.SendQuestionResponse(m.question.RequestID, nil, true)
				item := m.history.AddSystemMessage("↷ Skipped question")
				m.question = nil
				m.status = "RUNNING"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

			default:
				if len(msg.String()) == 1 && msg.String()[0] >= '1' && msg.String()[0] <= '9' {
					optIdx := int(msg.String()[0] - '1')
					m.question.ToggleOption(optIdx)
					return m, nil
				}
			}
			return m, nil
		}

		// Interactive Artifact Review Card Handling
		if m.artifactReview != nil {
			if m.artifactReview.IsWritingFeedback {
				switch msg.String() {
				case "enter":
					feedback := strings.TrimSpace(m.artifactReview.TextInput.Value())
					if feedback == "" {
						feedback = "Proceed"
					}
					uItem := m.history.AddUserMessage(feedback)
					sItem := m.history.AddSystemMessage(fmt.Sprintf("✓ Feedback submitted for %s", m.artifactReview.Filename))
					var extraItem ChatItem
					if m.mode == ModePlan {
						m.mode = ModeAcceptEdits
						extraItem = m.history.AddSystemMessage("Plan approved — switched mode to ACCEPT-EDITS.")
					}
					_ = m.client.SendUserMessage(feedback, nil, nil)
					m.artifactReview = nil
					m.status = "RUNNING"
					var printCmds []tea.Cmd
					printCmds = append(printCmds, tea.Println(m.history.RenderItem(uItem, m.getWidth())))
					printCmds = append(printCmds, tea.Println(m.history.RenderItem(sItem, m.getWidth())))
					if extraItem.Content != "" {
						printCmds = append(printCmds, tea.Println(m.history.RenderItem(extraItem, m.getWidth())))
					}
					return m, tea.Batch(printCmds...)
				case "esc":
					m.artifactReview.CancelFeedback()
					return m, nil
				default:
					var cmd tea.Cmd
					m.artifactReview.TextInput, cmd = m.artifactReview.TextInput.Update(msg)
					return m, cmd
				}
			}

			switch msg.String() {
			case "enter", "p", "P":
				proceedMsg := "Proceed with the plan."
				uItem := m.history.AddUserMessage(proceedMsg)
				sItem := m.history.AddSystemMessage(fmt.Sprintf("✓ Approved artifact: %s", m.artifactReview.Filename))
				var extraItem ChatItem
				if m.mode == ModePlan {
					m.mode = ModeAcceptEdits
					extraItem = m.history.AddSystemMessage("Plan approved — switched mode to ACCEPT-EDITS.")
				}
				_ = m.client.SendUserMessage(proceedMsg, nil, nil)
				m.artifactReview = nil
				m.status = "RUNNING"
				var printCmds []tea.Cmd
				printCmds = append(printCmds, tea.Println(m.history.RenderItem(uItem, m.getWidth())))
				printCmds = append(printCmds, tea.Println(m.history.RenderItem(sItem, m.getWidth())))
				if extraItem.Content != "" {
					printCmds = append(printCmds, tea.Println(m.history.RenderItem(extraItem, m.getWidth())))
				}
				return m, tea.Batch(printCmds...)
			case "f", "F", "e", "E":
				m.artifactReview.StartFeedback()
				return m, nil
			case "v", "V":
				m.artifactReview.ToggleView()
				return m, nil
			case "esc", "q", "Q":
				item := m.history.AddSystemMessage(fmt.Sprintf("↷ Dismissed review for %s", m.artifactReview.Filename))
				m.artifactReview = nil
				m.status = "IDLE"
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
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
					val := m.textarea.Value()
					if m.autocompleteState.Type == AutocompleteSlashCommand {
						m.textarea.SetValue(selected.Value + " ")
						m.textarea.CursorEnd()
					} else {
						prefix := val[:m.autocompleteState.CursorPos]
						m.textarea.SetValue(prefix + "@" + selected.Value + " ")
						m.textarea.CursorEnd()
					}
					m.autocompleteState.Active = false
					m.updateInputDimensions()
				}
				return m, nil
			case "esc":
				m.autocompleteState.Active = false
				return m, nil
			}
		}

		// Bracketed paste handling: terminal sends pasted block in a single KeyMsg with Paste=true
		if msg.Paste {
			m.textarea.InsertString(string(msg.Runes))
			m.updateInputDimensions()
			return m, nil
		}

		// Shift+Tab Mode Cycling (default -> accept-edits -> plan -> default)
		if msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab" {
			m.mode = m.mode.Next()
			var item ChatItem
			switch m.mode {
			case ModeDefault:
				item = m.history.AddSystemMessage("Mode: DEFAULT (Safe mode — prompts for file edits and shell commands)")
			case ModeAcceptEdits:
				item = m.history.AddSystemMessage("Mode: ACCEPT-EDITS (Auto-approves file edits; shell commands require confirmation)")
			case ModePlan:
				item = m.history.AddSystemMessage("Mode: PLAN (Plan-before-act mode — enforces research & implementation_plan.md before code changes)")
			}
			return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
		}

		// Global Shortcuts
		switch msg.Type {
		case tea.KeyCtrlO:
			m.showThinking = !m.showThinking
			m.history.SetShowThinking(m.showThinking)
			var item ChatItem
			if m.showThinking {
				item = m.history.AddSystemMessage("Thinking expanded (CoT visible). Press Ctrl+O to collapse.")
			} else {
				item = m.history.AddSystemMessage("Thinking collapsed. Press Ctrl+O to expand.")
			}
			return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

		case tea.KeyCtrlY:
			toCopy := m.history.LastAssistantResponse()
			if toCopy == "" {
				item := m.history.AddSystemMessage("No assistant response found to copy.")
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
			}
			_ = CopyToClipboard(toCopy)
			item := m.history.AddSystemMessage(fmt.Sprintf("✓ Copied last response to clipboard (%d characters)", len(toCopy)))
			return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

		case tea.KeyCtrlV:
			clip, err := PasteFromClipboard()
			if err == nil && clip != "" {
				m.textarea.InsertString(clip)
				m.updateInputDimensions()
				return m, nil
			}

		case tea.KeyCtrlJ:
			m.textarea.InsertRune('\n')
			m.updateInputDimensions()
			return m, nil

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
				item := m.history.AddSystemMessage("Turn interrupted. Press Ctrl+C again to exit.")
				return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
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
			// Alt+Enter / Option+Enter manually inserts a newline into the prompt
			if msg.Alt {
				m.textarea.InsertRune('\n')
				m.updateInputDimensions()
				return m, nil
			}

			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				return m, nil
			}

			m.textarea.Reset()
			m.textarea.SetHeight(1)
			m.autocompleteState.Active = false
			m.updateDimensions()

			if cmd, isCmd := ParseCommand(input); isCmd {
				teaCmd := m.handleSlashCommand(cmd)
				return m, teaCmd
			}

			item := m.history.AddUserMessage(input)
			m.status = "RUNNING"

			promptToSend := input
			if m.mode == ModePlan && !strings.HasPrefix(strings.ToLower(input), "/plan") {
				promptToSend = fmt.Sprintf("%s\n\n[Mode: PLAN] Please research the codebase using read tools and write implementation_plan.md in the brain directory before making any code modifications.", input)
			}
			if m.client != nil {
				_ = m.client.SendUserMessage(promptToSend, nil, nil)
			}
			return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
		}

	case ServerEventMsg:
		evtCmd := m.handleServerEvent(msg.Msg)
		if evtCmd != nil {
			cmds = append(cmds, evtCmd)
		}
		cmds = append(cmds, listenForEvents(m.client))
		return m, tea.Batch(cmds...)

	case SideQuestionResultMsg:
		item := m.history.AddSideQuestion(msg.Question, msg.Answer)
		return m, tea.Println(m.history.RenderItem(item, m.getWidth()))

	case WSErrorMsg:
		if m.quitting {
			return m, tea.Quit
		}
		item := m.history.AddSystemMessage(fmt.Sprintf("Server disconnected: %v", msg.Err))
		m.status = "IDLE"
		return m, tea.Println(m.history.RenderItem(item, m.getWidth()))
	}

	// Update text input and check for @ autocomplete trigger
	m.textarea, tiCmd = m.textarea.Update(msg)
	cmds = append(cmds, tiCmd)
	m.updateInputDimensions()

	pos := m.cursorPos()
	slashQ, isSlash := DetectSlashCommandQuery(m.textarea.Value(), pos)
	if isSlash {
		matches := MatchAllSlashCommands(slashQ, m.customCommands)
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
		query, startPos, found := DetectFileQuery(m.textarea.Value(), pos)
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

	return m, tea.Batch(cmds...)
}

func (m *Model) handleSlashCommand(cmd *Command) tea.Cmd {
	switch cmd.Name {
	case "help":
		rendered := RenderHelpViewWithCustom(m.getWidth(), m.customCommands)
		return tea.Println("\n" + rendered)

	case "new", "reset":
		m.history.Clear()
		m.subagents = NewSubagentViewManager()
		m.promptTokens = 0
		m.completionTokens = 0
		m.totalTokens = 0
		m.status = "IDLE"
		item := m.history.AddSystemMessage(fmt.Sprintf("✨ Started a new session in workspace: %s", strings.Join(m.workspaces, ", ")))
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

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
				item := m.history.AddSystemMessage("Usage: /mode [default | accept-edits | plan]")
				return tea.Println(m.history.RenderItem(item, m.getWidth()))
			}
		} else {
			m.mode = m.mode.Next()
		}
		var item ChatItem
		switch m.mode {
		case ModeDefault:
			item = m.history.AddSystemMessage("Mode switched to: DEFAULT (Safe mode — prompts for file edits and shell execution)")
		case ModeAcceptEdits:
			item = m.history.AddSystemMessage("Mode switched to: ACCEPT-EDITS (Auto-approves file edits; shell commands require confirmation)")
		case ModePlan:
			item = m.history.AddSystemMessage("Mode switched to: PLAN (Plan-before-act mode — requires research & implementation_plan.md before code changes)")
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "plan":
		goal := strings.Join(cmd.Args, " ")
		var prompt string
		var item ChatItem
		if goal != "" {
			prompt = fmt.Sprintf("Please create a comprehensive implementation plan for: %s.\n\nFirst, research the codebase using read tools, then create implementation_plan.md in the brain directory before modifying any code.", goal)
			item = m.history.AddUserMessage("/plan " + goal)
		} else {
			prompt = "Please create a comprehensive implementation plan for the current task. First, research the codebase, then create implementation_plan.md in the brain directory before modifying any code."
			item = m.history.AddUserMessage("/plan")
		}
		m.status = "RUNNING"
		if m.client != nil {
			_ = m.client.SendUserMessage(prompt, nil, nil)
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "teamwork", "team", "teamwork-preview":
		goal := strings.Join(cmd.Args, " ")
		var prompt string
		var item ChatItem
		if goal != "" {
			prompt = fmt.Sprintf("Please coordinate a team of autonomous specialized subagents to accomplish: %s.\n\nDefine any specialized subagent types needed (using define_subagent), launch parallel subagents (using invoke_subagent) with clear focused prompts, and coordinate their structured Handoff Briefings (original goal, files touched, decisions, and tests) until the goal is fully achieved.", goal)
			item = m.history.AddUserMessage("/teamwork " + goal)
		} else {
			prompt = "Please analyze the current task and coordinate a team of autonomous specialized subagents to work on it. Define any specialized subagent types needed (using define_subagent), launch parallel subagents (using invoke_subagent) with clear focused prompts, and coordinate their structured Handoff Briefings until completion."
			item = m.history.AddUserMessage("/teamwork")
		}
		m.status = "RUNNING"
		if m.client != nil {
			_ = m.client.SendUserMessage(prompt, nil, nil)
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "pause", "interrupt":
		var item ChatItem
		if m.status == "RUNNING" || m.status == "STREAMING" {
			if m.client != nil {
				_ = m.client.SendInterrupt()
			}
			m.status = "IDLE"
			item = m.history.AddSystemMessage("Turn interrupted. Use /resume to continue.")
		} else {
			item = m.history.AddSystemMessage("Agent is not currently running a turn.")
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "resume":
		msg := strings.Join(cmd.Args, " ")
		if m.client != nil {
			_ = m.client.SendResume(msg)
		}
		m.status = "RUNNING"
		var item ChatItem
		if msg != "" {
			item = m.history.AddUserMessage("/resume " + msg)
		} else {
			item = m.history.AddSystemMessage("Resumed agent execution.")
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "subagents":
		rendered := m.subagents.Render(m.getWidth(), 24)
		return tea.Println("\n" + rendered)

	case "tasks", "ps":
		if len(cmd.Args) > 0 && cmd.Args[0] == "list" {
			item := m.history.AddSystemMessage(fmt.Sprintf("⚙️ Background Tasks: %d total (%d running).", m.tasks.TotalCount(), m.tasks.RunningCount()))
			return tea.Println(m.history.RenderItem(item, m.getWidth()))
		}
		if len(cmd.Args) >= 2 && cmd.Args[0] == "kill" {
			if m.client != nil {
				_ = m.client.SendUserMessage(fmt.Sprintf("manage_task kill %s", cmd.Args[1]), nil, nil)
			}
			item := m.history.AddSystemMessage(fmt.Sprintf("Sent kill request for task %s", cmd.Args[1]))
			return tea.Println(m.history.RenderItem(item, m.getWidth()))
		}
		rendered := m.tasks.Render(m.getWidth(), 24)
		return tea.Println("\n" + rendered)

	case "yolo":
		m.yoloMode = !m.yoloMode
		if m.client != nil {
			_ = m.client.SendSetYoloMode(m.yoloMode)
		}
		var item ChatItem
		if m.yoloMode {
			item = m.history.AddSystemMessage("YOLO Mode ENABLED: All tool actions will auto-execute without prompts.")
		} else {
			item = m.history.AddSystemMessage("YOLO Mode DISABLED: Safe mode active with approval prompts.")
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "thinking":
		if len(cmd.Args) > 0 {
			switch strings.ToLower(cmd.Args[0]) {
			case "on", "true", "show", "expand":
				m.showThinking = true
			case "off", "false", "hide", "collapse":
				m.showThinking = false
			default:
				item := m.history.AddSystemMessage("Usage: /thinking [on | off]")
				return tea.Println(m.history.RenderItem(item, m.getWidth()))
			}
		} else {
			m.showThinking = !m.showThinking
		}
		m.history.SetShowThinking(m.showThinking)
		var item ChatItem
		if m.showThinking {
			item = m.history.AddSystemMessage("Thinking expanded (CoT visible). Type /thinking or press Ctrl+O to collapse.")
		} else {
			item = m.history.AddSystemMessage("Thinking collapsed. Type /thinking or press Ctrl+O to expand.")
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "workspace":
		if len(cmd.Args) == 0 || cmd.Args[0] == "list" {
			if m.client != nil {
				_ = m.client.SendWorkspaceRequest("list", "", "", "")
			}
		} else if len(cmd.Args) >= 2 && cmd.Args[0] == "add" {
			if m.client != nil {
				_ = m.client.SendWorkspaceRequest("add", cmd.Args[1], "", "")
			}
		} else if len(cmd.Args) >= 2 && cmd.Args[0] == "remove" {
			if m.client != nil {
				_ = m.client.SendWorkspaceRequest("remove", cmd.Args[1], "", "")
			}
		} else {
			item := m.history.AddSystemMessage("Usage: /workspace [list | add <path> | remove <path>]")
			return tea.Println(m.history.RenderItem(item, m.getWidth()))
		}
		return nil

	case "compact":
		item := m.history.AddSystemMessage("Compacting conversation context...")
		if m.client != nil {
			_ = m.client.SendUserMessage("[Compact Context]", nil, []string{"Please compact previous messages and summarize progress."})
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "context":
		sessionID := ""
		if m.client != nil {
			sessionID = m.client.SessionID()
		}
		ctxInfo := ContextInfo{
			ModelName:        m.modelName,
			PromptTokens:     m.promptTokens,
			CompletionTokens: m.completionTokens,
			TotalTokens:      m.totalTokens,
			Workspaces:       m.workspaces,
			SessionID:        sessionID,
		}
		rendered := RenderContextView(ctxInfo, m.getWidth())
		return tea.Println("\n" + rendered)

	case "btw":
		if len(cmd.Args) == 0 {
			item := m.history.AddSystemMessage("Usage: /btw <question> (Ask a side question without interrupting current task)")
			return tea.Println(m.history.RenderItem(item, m.getWidth()))
		}
		sideQ := strings.Join(cmd.Args, " ")
		m.history.AddSideQuestion(sideQ, "Analyzing side question...")
		return AskSideQuestionCmd(sideQ, m.history, m.modelName)

	case "model":
		var item ChatItem
		if len(cmd.Args) > 0 {
			m.modelName = cmd.Args[0]
			item = m.history.AddSystemMessage(fmt.Sprintf("Switched model target to: %s", m.modelName))
		} else {
			item = m.history.AddSystemMessage(fmt.Sprintf("Current Model: %s", m.modelName))
		}
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "status":
		statusMsg := fmt.Sprintf("Session Status: %s | YOLO: %v | Workspaces: %d | Subagents: %d active | Tokens: %d",
			m.status, m.yoloMode, len(m.workspaces), m.subagents.RunningCount(), m.totalTokens)
		item := m.history.AddSystemMessage(statusMsg)
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "version":
		ver := config.HarnessVersion
		if !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		running, info, _ := daemon.IsDaemonRunning()
		daemonStr := "not running"
		if running && info != nil {
			dVer := info.Version
			if !strings.HasPrefix(dVer, "v") {
				dVer = "v" + dVer
			}
			daemonStr = fmt.Sprintf("running (PID %d, Port %d, %s)", info.PID, info.Port, dVer)
		}
		versionMsg := fmt.Sprintf("lhctl version %s (%s/%s, %s) | Daemon: %s", ver, runtime.GOOS, runtime.GOARCH, runtime.Version(), daemonStr)
		item := m.history.AddSystemMessage(versionMsg)
		return tea.Println(m.history.RenderItem(item, m.getWidth()))

	case "clear":
		m.history.Clear()
		return tea.ClearScreen

	case "detach":
		if m.client != nil {
			_ = m.client.Close()
		}
		return tea.Quit

	case "exit", "quit":
		if m.client != nil {
			_ = m.client.Close()
		}
		return tea.Quit

	default:
		// Check for user-defined custom slash command (.agents/commands/ or ~/.divmora/commands/)
		if m.customCommands != nil {
			if customCmd, ok := m.customCommands.Find(cmd.Name); ok {
				prompt := customCmd.Expand(cmd.Args)
				userDisplay := "/" + cmd.Name
				if len(cmd.Args) > 0 {
					userDisplay += " " + strings.Join(cmd.Args, " ")
				}
				item := m.history.AddUserMessage(userDisplay)
				m.status = "RUNNING"
				if m.client != nil {
					_ = m.client.SendUserMessage(prompt, nil, nil)
				}
				return tea.Println(m.history.RenderItem(item, m.getWidth()))
			}
		}
		item := m.history.AddSystemMessage(fmt.Sprintf("Unknown command '/%s'. Type /help for available commands.", cmd.Name))
		return tea.Println(m.history.RenderItem(item, m.getWidth()))
	}
}

func (m *Model) handleServerEvent(srvMsg *pb.ServerMessage) tea.Cmd {
	if srvMsg == nil {
		return nil
	}

	var printCmds []tea.Cmd

	if srvMsg.GetInitResponse() != nil {
		item := m.history.AddSystemMessage(fmt.Sprintf("Connected to LocalHarness session %s (v%s)",
			srvMsg.GetInitResponse().ConversationId, srvMsg.GetInitResponse().HarnessVersion))
		printCmds = append(printCmds, tea.Println(m.history.RenderItem(item, m.getWidth())))
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
			return nil
		}

		// Handle Permission Prompt (WAITING)
		if step.State == pb.StepUpdate_STATE_WAITING && step.GetPermissionRequest() != nil {
			pr := step.GetPermissionRequest()
			if m.yoloMode {
				_ = m.client.SendPermissionResponse(pr.RequestId, true, "", pb.PermissionResponse_SCOPE_ONCE)
				return nil
			}
			if m.mode == ModeAcceptEdits && (pr.ToolName == "write_to_file" || pr.ToolName == "replace_file_content" || pr.ToolName == "multi_replace_file_content") {
				_ = m.client.SendPermissionResponse(pr.RequestId, true, "", pb.PermissionResponse_SCOPE_ONCE)
				return nil
			}
			m.status = "WAITING"
			m.approval = &ActiveApproval{
				RequestID:   pr.RequestId,
				ToolName:    pr.ToolName,
				Description: pr.ArgsSummary,
				DiffPreview: pr.DiffPreview,
				ArgsJSON:    pr.ArgsJson,
			}
			m.approval.InitSubcommands()
			return nil
		}

		// Handle User Question (WAITING)
		if step.State == pb.StepUpdate_STATE_WAITING && step.GetUserQuestion() != nil {
			m.status = "WAITING"
			m.question = NewActiveQuestion(step.GetUserQuestion())
			return nil
		}

		// Handle active tool execution
		if step.State == pb.StepUpdate_STATE_ACTIVE && step.Action != nil {
			m.status = "RUNNING"
			name, args := extractActionDetails(step)
			flushed := m.history.StartToolCall(name, args)
			for _, it := range flushed {
				printCmds = append(printCmds, tea.Println(m.history.RenderItem(it, m.getWidth())))
			}

			// Track background tasks
			if rc := step.GetRunCommand(); rc != nil && rc.TaskId != "" {
				m.tasks.AddOrUpdate(&TaskItemState{
					TaskID:    rc.TaskId,
					Command:   rc.Command,
					Cwd:       rc.Cwd,
					Status:    "RUNNING",
					StartedAt: time.Now(),
				})
			}
			if sch := step.GetSchedule(); sch != nil && sch.TaskId != "" {
				m.tasks.AddOrUpdate(&TaskItemState{
					TaskID:     sch.TaskId,
					Command:    sch.Prompt,
					Status:     "RUNNING",
					StartedAt:  time.Now(),
					IsSchedule: true,
				})
			}
			if len(printCmds) > 0 {
				return tea.Batch(printCmds...)
			}
			return nil
		}

		// Handle tool execution finished
		if step.State == pb.StepUpdate_STATE_DONE || step.State == pb.StepUpdate_STATE_ERROR {
			isErr := step.State == pb.StepUpdate_STATE_ERROR
			name, diff, res := extractActionResult(step)
			doneItem := m.history.FinishToolCall(name, res, isErr, diff)
			if doneItem != nil {
				printCmds = append(printCmds, tea.Println(m.history.RenderItem(*doneItem, m.getWidth())))
			}

			// Surface interactive review card when an artifact requesting feedback is written/updated
			if !isErr {
				if wtf := step.GetWriteToFile(); wtf != nil && wtf.ArtifactMetadata != nil && wtf.ArtifactMetadata.RequestFeedback {
					m.artifactReview = NewActiveArtifactReview(wtf.Path, wtf.ArtifactMetadata.ArtifactType, wtf.ArtifactMetadata.Summary)
					m.status = "WAITING"
				} else if rfc := step.GetReplaceFileContent(); rfc != nil && rfc.ArtifactMetadata != nil && rfc.ArtifactMetadata.RequestFeedback {
					m.artifactReview = NewActiveArtifactReview(rfc.Path, rfc.ArtifactMetadata.ArtifactType, rfc.ArtifactMetadata.Summary)
					m.status = "WAITING"
				}
			}

			// Update task state upon completion
			if rc := step.GetRunCommand(); rc != nil && rc.TaskId != "" {
				if t := m.tasks.tasks[rc.TaskId]; t != nil {
					if isErr {
						t.Status = "FAILED"
						t.RecentOutput = rc.Stderr
					} else {
						t.Status = "COMPLETED"
						t.RecentOutput = rc.Stdout
					}
					t.ExitCode = int(rc.ExitCode)
					t.CompletedAt = time.Now()
				}
			}
			if mt := step.GetManageTask(); mt != nil && len(mt.Tasks) > 0 {
				m.tasks.UpdateFromProto(mt.Tasks)
			}
			if len(printCmds) > 0 {
				return tea.Batch(printCmds...)
			}
			return nil
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
			flushed := m.history.FlushStreaming()
			for _, it := range flushed {
				printCmds = append(printCmds, tea.Println(m.history.RenderItem(it, m.getWidth())))
			}
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
		m.history.SetWorkspaces(wsDirs)
		item := m.history.AddSystemMessage(fmt.Sprintf("📂 %s (Total: %d)", wsResp.Message, len(wsResp.Workspaces)))
		printCmds = append(printCmds, tea.Println(m.history.RenderItem(item, m.getWidth())))
	}

	if rc := srvMsg.GetReplayComplete(); rc != nil {
		item := m.history.AddSystemMessage(fmt.Sprintf("🔄 Replayed %d historical events from buffer", rc.EventCount))
		printCmds = append(printCmds, tea.Println(m.history.RenderItem(item, m.getWidth())))
	}

	if errEv := srvMsg.GetError(); errEv != nil {
		item := m.history.AddSystemMessage(fmt.Sprintf("❌ Error [%s]: %s", errEv.Code, errEv.Message))
		printCmds = append(printCmds, tea.Println(m.history.RenderItem(item, m.getWidth())))
	}

	if len(printCmds) > 0 {
		return tea.Batch(printCmds...)
	}
	return nil
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
	case *pb.StepUpdate_BrowserSubagent:
		return "browser_subagent", a.BrowserSubagent.Task
	case *pb.StepUpdate_DesktopSubagent:
		return "desktop_subagent", a.DesktopSubagent.Task
	case *pb.StepUpdate_InvokeSubagent:
		var roles []string
		for _, sub := range a.InvokeSubagent.Subagents {
			roles = append(roles, fmt.Sprintf("%s (%s)", sub.Role, sub.TypeName))
		}
		if len(roles) > 0 {
			return "invoke_subagent", strings.Join(roles, ", ")
		}
		return "invoke_subagent", fmt.Sprintf("%d subagents", len(a.InvokeSubagent.Subagents))
	case *pb.StepUpdate_DefineSubagent:
		return "define_subagent", fmt.Sprintf("%s: %s", a.DefineSubagent.Name, a.DefineSubagent.Description)
	case *pb.StepUpdate_ManageSubagents:
		return "manage_subagents", fmt.Sprintf("action=%s", a.ManageSubagents.Action)
	case *pb.StepUpdate_SendMessageAction:
		return "send_message", fmt.Sprintf("to %s", a.SendMessageAction.Recipient)
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
		return "write_to_file", a.WriteToFile.DiffBlock, ""
	case *pb.StepUpdate_ReplaceFileContent:
		return "replace_file_content", a.ReplaceFileContent.DiffBlock, ""
	case *pb.StepUpdate_RunCommand:
		if a.RunCommand.ExitCode == 0 {
			return "run_command", "", "ok"
		}
		return "run_command", "", fmt.Sprintf("exit code %d", a.RunCommand.ExitCode)
	case *pb.StepUpdate_ViewFile:
		if a.ViewFile.TotalLines > 0 {
			return "view_file", "", fmt.Sprintf("%d lines", a.ViewFile.TotalLines)
		}
		return "view_file", "", "ok"
	case *pb.StepUpdate_ListDir:
		return "list_dir", "", fmt.Sprintf("%d items", len(a.ListDir.Entries))
	case *pb.StepUpdate_GrepSearch:
		return "grep_search", "", fmt.Sprintf("%d matches", a.GrepSearch.TotalMatches)
	case *pb.StepUpdate_FindFile:
		return "find_file", "", fmt.Sprintf("%d files", len(a.FindFile.Matches))
	case *pb.StepUpdate_BrowserSubagent:
		return "browser_subagent", "", a.BrowserSubagent.TaskSummary
	case *pb.StepUpdate_DesktopSubagent:
		return "desktop_subagent", "", a.DesktopSubagent.TaskSummary
	case *pb.StepUpdate_InvokeSubagent:
		var summaries []string
		for _, res := range a.InvokeSubagent.LaunchResults {
			summaries = append(summaries, fmt.Sprintf("%s (%s)", res.Role, res.ConversationId))
		}
		if len(summaries) > 0 {
			return "invoke_subagent", "", fmt.Sprintf("Spawned: %s", strings.Join(summaries, ", "))
		}
		return "invoke_subagent", "", fmt.Sprintf("Launched %d subagents", len(a.InvokeSubagent.LaunchResults))
	case *pb.StepUpdate_DefineSubagent:
		return "define_subagent", "", fmt.Sprintf("Registered persona '%s'", a.DefineSubagent.Name)
	case *pb.StepUpdate_SendMessageAction:
		return "send_message", "", fmt.Sprintf("Message delivered to %s", a.SendMessageAction.Recipient)
	default:
		return "tool", "", ""
	}
}

// View renders the dynamic inline dock at the bottom of the terminal.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	w := m.getWidth()

	var sections []string

	// 1. In-flight action line (live tool or LLM streaming indicator)
	if activeItem := m.history.ActiveToolItem(); activeItem != nil {
		dur := time.Since(m.history.ToolStartTime()).Round(100 * time.Millisecond)
		action := activeItem.SemanticAction
		if action == ActionUnknown {
			action = inferSemanticAction(activeItem.ToolName)
		}
		target := activeItem.Target
		if target == "" {
			target = extractTargetFromArgs(activeItem.ToolName, activeItem.ToolArgs)
		}
		target = formatRelativePath(target, m.workspaces)
		maxTargetLen := max(15, w-35)
		if len(target) > maxTargetLen {
			target = target[:maxTargetLen-3] + "..."
		}
		verb := action.Verb()
		if action == ActionUnknown {
			verb = "Running " + activeItem.ToolName
		}
		line := fmt.Sprintf("  %s %s %s [%s]",
			m.spinner.View(),
			action.BadgeStyle().Render(verb),
			ActionTargetStyle.Render(target),
			lipgloss.NewStyle().Foreground(ColorWarning).Render(dur.String()),
		)
		sections = append(sections, line)
	} else if m.status == "STREAMING" {
		if m.history.thinkingText.Len() > 0 {
			dur := time.Since(m.history.thinkingStartTime).Round(100 * time.Millisecond)
			sections = append(sections, "  "+m.spinner.View()+" "+ThinkingCollapsedStyle.Render(fmt.Sprintf("Thinking... [%s]", dur.String())))
		} else if m.history.streamingText.Len() > 0 {
			sections = append(sections, "  "+m.spinner.View()+" "+lipgloss.NewStyle().Foreground(ColorHighlight).Render("Generating response..."))
		}
	}

	// 2. Interactive modals or textarea input
	if m.approval != nil {
		sections = append(sections, RenderApprovalInline(m.approval, w))
	} else if m.question != nil {
		sections = append(sections, RenderQuestionInline(m.question, w))
	} else if m.artifactReview != nil {
		sections = append(sections, RenderArtifactReviewInline(m.artifactReview, w))
	} else {
		if m.autocompleteState.Active {
			sections = append(sections, RenderAutocomplete(&m.autocompleteState, w))
		}

		inputLine := lipgloss.NewStyle().Padding(0, 1).Render(m.textarea.View())
		if m.textarea.LineCount() > 1 {
			hint := lipgloss.NewStyle().Faint(true).Render(
				fmt.Sprintf(" [%d lines • Enter to submit, Alt+Enter for newline]", m.textarea.LineCount()),
			)
			inputLine = inputLine + "\n" + lipgloss.NewStyle().Padding(0, 1).Render(hint)
		}
		sections = append(sections, inputLine)

		activityStrip := RenderActiveBackgroundStrip(m.tasks, m.subagents, w)
		if activityStrip != "" {
			sections = append(sections, activityStrip)
		}
	}

	// 3. Bottom status bar
	statusBar := RenderStatusBar(StatusBarState{
		Status:           m.status,
		Mode:             m.mode,
		ModelName:        m.modelName,
		PromptTokens:     m.promptTokens,
		CompletionTokens: m.completionTokens,
		TotalTokens:      m.totalTokens,
		RunningSubagents: m.subagents.RunningCount(),
		TotalSubagents:   m.subagents.TotalCount(),
		RunningTasks:     m.tasks.RunningCount(),
		YoloMode:         m.yoloMode,
		WorkspaceCount:   len(m.workspaces),
	}, w)
	sections = append(sections, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// getWidth returns the terminal width or detects it from stdout if not yet set by window size message.
func (m Model) getWidth() int {
	if m.width > 0 {
		return m.width
	}
	if tWidth, _, err := term.GetSize(os.Stdout.Fd()); err == nil && tWidth > 0 {
		return tWidth
	}
	return 100
}

// cursorPos calculates the byte offset in m.textarea.Value() where the cursor is currently located.
func (m *Model) cursorPos() int {
	val := m.textarea.Value()
	line := m.textarea.Line()
	col := m.textarea.LineInfo().ColumnOffset
	lines := strings.Split(val, "\n")
	runePos := 0
	for i := 0; i < line && i < len(lines); i++ {
		runePos += len([]rune(lines[i])) + 1
	}
	if line < len(lines) {
		lineRunes := []rune(lines[line])
		if col > len(lineRunes) {
			col = len(lineRunes)
		}
		runePos += col
	}

	runes := []rune(val)
	if runePos > len(runes) {
		runePos = len(runes)
	}
	return len(string(runes[:runePos]))
}

// updateDimensions recalculates textarea dimensions to fit current window size.
func (m *Model) updateDimensions() {
	m.ready = true
	w := m.getWidth()
	if w > 6 {
		m.textarea.SetWidth(w - 6)
	}
}

// updateInputDimensions dynamically expands or contracts the textarea height based on line count.
func (m *Model) updateInputDimensions() {
	lines := m.textarea.LineCount()
	h := min(max(1, lines), 6)
	if h != m.textarea.Height() {
		m.textarea.SetHeight(h)
	}
}

// RenderConsoleHistory renders the conversation history for display in the terminal console upon exit.
func (m Model) RenderConsoleHistory() string {
	if m.history == nil {
		return ""
	}
	m.history.FlushStreaming()
	if len(m.history.items) == 0 {
		return ""
	}
	return strings.TrimSpace(m.history.RenderView(spinner.Model{}, m.getWidth()))
}
