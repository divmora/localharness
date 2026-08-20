package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"

	"github.com/divmora/localharness/cmd/lhctl/client"
	"github.com/divmora/localharness/cmd/lhctl/tui"
	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
)

type runFlags struct {
	model              string
	workspaces         []string
	explicitWorkspaces []string
	yolo               bool
	detach             bool
	prompt             string
	sessionID          string
	ephemeral          bool
}

// formatResumeCommand builds the CLI command to resume the given conversation session.
func formatResumeCommand(sessionID string, flags runFlags) string {
	var parts []string
	parts = append(parts, "lhctl", "-c", sessionID)
	if flags.model != "" {
		parts = append(parts, fmt.Sprintf("--model=%s", flags.model))
	}
	for _, ws := range flags.explicitWorkspaces {
		parts = append(parts, fmt.Sprintf("--workspace=%s", ws))
	}
	if flags.yolo {
		parts = append(parts, "--yolo")
	}
	return strings.Join(parts, " ")
}

func parseRunFlags(args []string) runFlags {
	f := runFlags{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--yolo" || a == "--dangerously-skip-permissions":
			f.yolo = true
		case a == "--detach":
			f.detach = true
		case a == "--ephemeral":
			f.ephemeral = true
		case strings.HasPrefix(a, "--model="):
			f.model = strings.TrimPrefix(a, "--model=")
		case a == "-m" && i+1 < len(args):
			i++
			f.model = args[i]
		case strings.HasPrefix(a, "--workspace="):
			ws := strings.TrimPrefix(a, "--workspace=")
			f.workspaces = append(f.workspaces, ws)
			f.explicitWorkspaces = append(f.explicitWorkspaces, ws)
		case a == "-w" && i+1 < len(args):
			i++
			f.workspaces = append(f.workspaces, args[i])
			f.explicitWorkspaces = append(f.explicitWorkspaces, args[i])
		case strings.HasPrefix(a, "--prompt="):
			f.prompt = strings.TrimPrefix(a, "--prompt=")
		case a == "-p" && i+1 < len(args):
			i++
			f.prompt = args[i]
		case a == "-c" || a == "--continue" || a == "--resume":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				f.sessionID = args[i]
			} else {
				f.sessionID = "latest"
			}
		case strings.HasPrefix(a, "-c="):
			f.sessionID = strings.TrimPrefix(a, "-c=")
		case strings.HasPrefix(a, "--conversation="):
			f.sessionID = strings.TrimPrefix(a, "--conversation=")
		case strings.HasPrefix(a, "--continue="):
			f.sessionID = strings.TrimPrefix(a, "--continue=")
		case strings.HasPrefix(a, "--resume="):
			f.sessionID = strings.TrimPrefix(a, "--resume=")
		case strings.HasPrefix(a, "--session-id="):
			f.sessionID = strings.TrimPrefix(a, "--session-id=")
		}
	}
	if len(f.workspaces) == 0 {
		cwd, err := os.Getwd()
		if err == nil {
			f.workspaces = []string{cwd}
		} else {
			f.workspaces = []string{"."}
		}
	}
	return f
}

func runInteractive(args []string) {
	flags := parseRunFlags(args)
	if err := runInteractiveWithOptions(flags); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runInteractiveWithOptions(flags runFlags) error {
	dataDir := getDataDir()

	// If resuming existing session requested via -c / --conversation / --continue / --resume
	var initialState *pb.ConversationState
	if flags.sessionID != "" {
		fullID, err := resolveConversationID(dataDir, flags.sessionID)
		if err != nil {
			return fmt.Errorf("cannot resume conversation: %w", err)
		}
		flags.sessionID = fullID

		// Attempt to load existing state for TUI history
		pbPath := filepath.Join(dataDir, "conversations", fullID+".pb")
		if data, err := os.ReadFile(pbPath); err == nil {
			var state pb.ConversationState
			if err := proto.Unmarshal(data, &state); err == nil {
				initialState = &state
			}
		}
	}

	if len(flags.workspaces) == 0 {
		cwd, err := os.Getwd()
		if err == nil {
			flags.workspaces = []string{cwd}
		} else {
			flags.workspaces = []string{"."}
		}
	}

	// Workspace trust verification on first launch
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	settings := config.LoadGlobalSettings(logger)

	for _, ws := range flags.workspaces {
		absWS, err := filepath.Abs(ws)
		if err == nil && !settings.IsWorkspaceTrusted(absWS) {
			if !flags.yolo {
				fmt.Printf("Workspace %q is not in the trusted list.\n", absWS)
				fmt.Print("Do you trust this workspace and its authors? [y/N]: ")
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(strings.TrimSpace(answer)) == "y" || strings.ToLower(strings.TrimSpace(answer)) == "yes" {
					_ = config.AddTrustedWorkspace(absWS, logger)
					fmt.Printf("Added %s to trusted workspaces.\n", absWS)
				} else {
					fmt.Println("Proceeding in restricted read-only mode.")
				}
			} else {
				_ = config.AddTrustedWorkspace(absWS, logger)
			}
		}
	}

	cl, err := client.ConnectOrStartDaemonWithSession(logger, flags.sessionID)
	if err != nil {
		return fmt.Errorf("connecting to daemon: %w", err)
	}

	var pbWorkspaces []*pb.Workspace
	for _, dir := range flags.workspaces {
		absDir, _ := filepath.Abs(dir)
		pbWorkspaces = append(pbWorkspaces, &pb.Workspace{
			Directory: absDir,
			Name:      filepath.Base(absDir),
		})
	}

	harnessCfg := &pb.HarnessConfig{
		LitellmModel: flags.model,
		Workspaces:   pbWorkspaces,
		YoloMode:     flags.yolo,
		BuiltinTools: &pb.BuiltinToolsConfig{
			ViewFile:        true,
			CreateFile:      true,
			EditFile:        true,
			ListDir:         true,
			SearchDir:       true,
			FindFile:        true,
			RunCommand:      true,
			Finish:          true,
			ManageTask:      true,
			InvokeSubagent:  true,
			WebSearch:       true,
			WebFetch:        true,
			Schedule:        true,
			DefineSubagent:  true,
			ManageSubagents: true,
			SendMessage:     true,
		},
		PromptModules: &pb.PromptModules{
			EnableWebDevelopment: true,
			EnablePlanning:       true,
			EnableSlashCommands:  true,
			EnableKnowledgeItems: true,
		},
	}

	if err := cl.Init(harnessCfg); err != nil {
		return fmt.Errorf("initializing session: %w", err)
	}

	if flags.prompt != "" {
		_ = cl.SendUserMessage(flags.prompt, nil, nil)
	}

	if flags.detach {
		fmt.Printf("Task running in background daemon (Session ID: %s)\n", cl.SessionID())
		fmt.Printf("Attach anytime with: lhctl attach %s\n", cl.SessionID())
		fmt.Printf("To resume this conversation, run:\n  %s\n", formatResumeCommand(cl.SessionID(), flags))
		return nil
	}

	p := tea.NewProgram(
		tui.InitialModelWithHistory(cl, flags.workspaces, flags.yolo, initialState),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	sessionID := cl.SessionID()
	if sessionID == "" {
		sessionID = flags.sessionID
	}
	if sessionID != "" {
		resumeCmd := formatResumeCommand(sessionID, flags)
		fmt.Printf("\nTo resume this conversation, run:\n  %s\n\n", resumeCmd)
	}

	return nil
}

func runAttach(dataDir string, sessionID string, args []string) {
	fullID, err := resolveConversationID(dataDir, sessionID)
	if err != nil {
		fullID = sessionID
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cl, err := client.ConnectOrStartDaemonWithSession(logger, fullID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to daemon: %v\n", err)
		os.Exit(1)
	}

	var initialState *pb.ConversationState
	pbPath := filepath.Join(dataDir, "conversations", fullID+".pb")
	if data, err := os.ReadFile(pbPath); err == nil {
		var state pb.ConversationState
		if err := proto.Unmarshal(data, &state); err == nil {
			initialState = &state
		}
	}

	cwd, _ := os.Getwd()
	workspaces := []string{cwd}

	p := tea.NewProgram(
		tui.InitialModelWithHistory(cl, workspaces, false, initialState),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	sessID := cl.SessionID()
	if sessID == "" {
		sessID = fullID
	}
	if sessID != "" {
		resumeCmd := formatResumeCommand(sessID, runFlags{sessionID: sessID})
		fmt.Printf("\nTo resume this conversation, run:\n  %s\n\n", resumeCmd)
	}
}
