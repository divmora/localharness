package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/divmora/localharness/cmd/lhctl/client"
	"github.com/divmora/localharness/cmd/lhctl/tui"
	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
)

type runFlags struct {
	model      string
	workspaces []string
	yolo       bool
	detach     bool
	prompt     string
	sessionID  string
	ephemeral  bool
}

func parseRunFlags(args []string) runFlags {
	f := runFlags{}
	for _, a := range args {
		switch {
		case a == "--yolo" || a == "--dangerously-skip-permissions":
			f.yolo = true
		case a == "--detach":
			f.detach = true
		case a == "--ephemeral":
			f.ephemeral = true
		case strings.HasPrefix(a, "--model="):
			f.model = strings.TrimPrefix(a, "--model=")
		case strings.HasPrefix(a, "--workspace="):
			f.workspaces = append(f.workspaces, strings.TrimPrefix(a, "--workspace="))
		case strings.HasPrefix(a, "--prompt="):
			f.prompt = strings.TrimPrefix(a, "--prompt=")
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

	cl, err := client.ConnectOrStartDaemon(logger)
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
		return nil
	}

	p := tea.NewProgram(
		tui.InitialModel(cl, flags.workspaces, flags.yolo),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}

func runAttach(dataDir string, sessionID string, args []string) {
	fullID, err := resolveConversationID(dataDir, sessionID)
	if err != nil {
		fullID = sessionID
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cl, err := client.ConnectOrStartDaemon(logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to daemon: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	workspaces := []string{cwd}

	p := tea.NewProgram(
		tui.InitialModel(cl, workspaces, false),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
	_ = fullID
}
