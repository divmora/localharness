// lhctl — LocalHarness CLI and Interactive Terminal Interface.
//
// Features:
// - Interactive Terminal UI (TUI) with real-time token streaming and syntax highlighting.
// - Multi-Client daemon connection, background detachment, and attach replay.
// - Offline conversation inspection, trace visualization, and agent hierarchy trees.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

var (
	globalDataDir string
)

func newRootCommand() *cobra.Command {
	var (
		modelFlag      string
		workspacesFlag []string
		yoloFlag       bool
		detachFlag     bool
		promptFlag     string
		convFlag       string
	)

	rootCmd := &cobra.Command{
		Use:   "lhctl",
		Short: "LocalHarness CLI & Interactive TUI",
		Long: `lhctl is the command-line interface and interactive terminal for LocalHarness.

It allows you to run interactive multi-agent coding sessions, manage background daemons,
detach and attach to headless sessions, and inspect conversation state and traces offline.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := convFlag
			if sessionID == "" && len(args) > 0 {
				sessionID = args[0]
			} else if sessionID == "latest" && len(args) > 0 {
				sessionID = args[0]
			}
			flags := runFlags{
				model:              modelFlag,
				workspaces:         workspacesFlag,
				explicitWorkspaces: workspacesFlag,
				yolo:               yoloFlag,
				detach:             detachFlag,
				prompt:             promptFlag,
				sessionID:          sessionID,
			}
			return runInteractiveWithOptions(flags)
		},
	}

	// Persistent flags (available across all subcommands)
	rootCmd.PersistentFlags().StringVar(&globalDataDir, "data-dir", getDefaultDataDir(), "Override data directory")

	// Run / Interactive flags on root
	addRunFlags(rootCmd, &modelFlag, &workspacesFlag, &yoloFlag, &detachFlag, &promptFlag, &convFlag)

	// Subcommand: run
	var (
		runModelFlag      string
		runWorkspacesFlag []string
		runYoloFlag       bool
		runDetachFlag     bool
		runPromptFlag     string
		runConvFlag       string
	)
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start an interactive session in TUI or launch a detached task",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := runConvFlag
			if sessionID == "" && len(args) > 0 {
				sessionID = args[0]
			} else if sessionID == "latest" && len(args) > 0 {
				sessionID = args[0]
			}
			flags := runFlags{
				model:              runModelFlag,
				workspaces:         runWorkspacesFlag,
				explicitWorkspaces: runWorkspacesFlag,
				yolo:               runYoloFlag,
				detach:             runDetachFlag,
				prompt:             runPromptFlag,
				sessionID:          sessionID,
			}
			return runInteractiveWithOptions(flags)
		},
	}
	addRunFlags(runCmd, &runModelFlag, &runWorkspacesFlag, &runYoloFlag, &runDetachFlag, &runPromptFlag, &runConvFlag)
	rootCmd.AddCommand(runCmd)

	// Subcommand: attach
	attachCmd := &cobra.Command{
		Use:   "attach <session-id>",
		Short: "Attach to a running background daemon session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runAttach(getDataDir(), args[0], nil)
			return nil
		},
	}
	rootCmd.AddCommand(attachCmd)

	// Subcommand: daemon
	daemonCmd := &cobra.Command{
		Use:   "daemon [start|stop|status]",
		Short: "Manage the background LocalHarness daemon runtime",
	}

	daemonStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the LocalHarness daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			return startDaemon()
		},
	}
	daemonStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running LocalHarness daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDaemon()
		},
	}
	daemonStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check the status of the LocalHarness daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusDaemon()
		},
	}
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)

	// Subcommand: conversation (alias: conv)
	convCmd := &cobra.Command{
		Use:     "conversation",
		Aliases: []string{"conv"},
		Short:   "Inspect, trace, and visualize conversation state offline",
	}

	// conv inspect
	var (
		inspectJSON       bool
		inspectTopN       int
		inspectSteps      bool
		inspectStepN      int
		inspectErrorsOnly bool
	)
	inspectCmd := &cobra.Command{
		Use:   "inspect <id>",
		Short: "Inspect conversation messages and token usage offline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := []string{}
			if inspectJSON {
				flags = append(flags, "--json")
			}
			if inspectSteps {
				flags = append(flags, "--steps")
			}
			if inspectErrorsOnly {
				flags = append(flags, "--errors")
			}
			if inspectTopN != 3 {
				flags = append(flags, fmt.Sprintf("--top=%d", inspectTopN))
			}
			if inspectStepN != -1 {
				flags = append(flags, fmt.Sprintf("--step=%d", inspectStepN))
			}
			runInspect(getDataDir(), args[0], flags)
			return nil
		},
	}
	inspectCmd.Flags().BoolVar(&inspectJSON, "json", false, "Output as JSON (for piping to jq)")
	inspectCmd.Flags().IntVar(&inspectTopN, "top", 3, "Show top N largest messages")
	inspectCmd.Flags().BoolVar(&inspectSteps, "steps", false, "Show full tool args, paths, and error details")
	inspectCmd.Flags().IntVar(&inspectStepN, "step", -1, "Deep-dive into a single step (dump full args and result)")
	inspectCmd.Flags().BoolVar(&inspectErrorsOnly, "errors", false, "Show only steps that errored")
	convCmd.AddCommand(inspectCmd)

	// conv trace
	var (
		traceWatchMode bool
		traceCommands  bool
	)
	traceCmd := &cobra.Command{
		Use:   "trace <id>",
		Short: "Show LLM API call timeline and tool execution trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := []string{}
			if traceWatchMode {
				flags = append(flags, "--watch")
			}
			if traceCommands {
				flags = append(flags, "--commands")
			}
			runTrace(getDataDir(), args[0], flags)
			return nil
		},
	}
	traceCmd.Flags().BoolVar(&traceWatchMode, "watch", false, "Live-tail mode — poll for new trace files as agent runs")
	traceCmd.Flags().BoolVar(&traceCommands, "commands", false, "Show full command lines for run_command calls")
	convCmd.AddCommand(traceCmd)

	// conv tree
	treeCmd := &cobra.Command{
		Use:   "tree <id>",
		Short: "Display the subagent hierarchy tree for a conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runTree(getDataDir(), args[0])
			return nil
		},
	}
	convCmd.AddCommand(treeCmd)

	// conv list
	var listRecent int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all recorded conversations",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := []string{}
			if listRecent > 0 {
				flags = append(flags, fmt.Sprintf("--recent=%d", listRecent))
			}
			runList(getDataDir(), flags)
			return nil
		},
	}
	listCmd.Flags().IntVar(&listRecent, "recent", 0, "Show only the N most recent conversations")
	convCmd.AddCommand(listCmd)

	rootCmd.AddCommand(convCmd)

	return rootCmd
}

func addRunFlags(cmd *cobra.Command, model *string, workspaces *[]string, yolo *bool, detach *bool, prompt *string, conv *string) {
	cmd.Flags().StringVarP(model, "model", "m", "", "Target LLM model (e.g. gpt-4o, claude-3-5-sonnet)")
	cmd.Flags().StringArrayVarP(workspaces, "workspace", "w", nil, "Attach workspace directory (repeatable)")
	cmd.Flags().BoolVarP(yolo, "yolo", "y", false, "Enable YOLO Mode (dangerously skip permission checks)")
	cmd.Flags().BoolVarP(detach, "detach", "d", false, "Launch prompt in background daemon without blocking")
	cmd.Flags().StringVarP(prompt, "prompt", "p", "", "Initial prompt to execute immediately")
	cmd.Flags().StringVarP(conv, "conversation", "c", "", "Resume an existing conversation by ID (or latest if omitted)")
	cmd.Flags().Lookup("conversation").NoOptDefVal = "latest"
	cmd.Flags().StringVar(conv, "resume", "", "Alias for --conversation")
	cmd.Flags().Lookup("resume").NoOptDefVal = "latest"
	cmd.Flags().StringVar(conv, "continue", "", "Alias for --conversation")
	cmd.Flags().Lookup("continue").NoOptDefVal = "latest"
}

func getDefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".divmora", "localharness")
	}
	return filepath.Join(home, ".divmora", "localharness")
}

func getDataDir() string {
	if globalDataDir != "" {
		return globalDataDir
	}
	return getDefaultDataDir()
}

func main() {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// getLatestConversationID finds the most recently updated conversation ID.
func getLatestConversationID(dataDir string) (string, error) {
	convDir := filepath.Join(dataDir, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		return "", fmt.Errorf("cannot read conversations directory %s: %w", convDir, err)
	}

	type convEntry struct {
		id        string
		updatedAt string
		modTime   time.Time
	}
	var list []convEntry

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pb" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".pb")
		pbPath := filepath.Join(convDir, e.Name())

		info, err := e.Info()
		if err != nil {
			continue
		}

		data, err := os.ReadFile(pbPath)
		if err != nil {
			continue
		}
		state := &pb.ConversationState{}
		if err := proto.Unmarshal(data, state); err != nil {
			continue
		}
		list = append(list, convEntry{
			id:        id,
			updatedAt: state.UpdatedAt,
			modTime:   info.ModTime(),
		})
	}

	if len(list) == 0 {
		return "", fmt.Errorf("no existing conversations found to resume")
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].updatedAt != "" && list[j].updatedAt != "" && list[i].updatedAt != list[j].updatedAt {
			return list[i].updatedAt > list[j].updatedAt
		}
		return list[i].modTime.After(list[j].modTime)
	})

	return list[0].id, nil
}

// resolveConversationID resolves a partial ID, UUID, or "latest"/"recent"/"last" alias
// to a full conversation ID.
func resolveConversationID(dataDir, partialID string) (string, error) {
	if strings.ToLower(partialID) == "latest" || strings.ToLower(partialID) == "recent" || strings.ToLower(partialID) == "last" {
		return getLatestConversationID(dataDir)
	}

	convDir := filepath.Join(dataDir, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		return "", fmt.Errorf("cannot read conversations directory %s: %w", convDir, err)
	}

	var matches []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pb" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".pb")
		if name == partialID {
			return name, nil
		}
		if strings.HasPrefix(name, partialID) {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no conversation found matching %q", partialID)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous ID %q matches %d conversations: %s",
			partialID, len(matches), strings.Join(matches, ", "))
	}
}

// contains checks if a string slice contains a value.
func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// stripFlag removes a flag and its value from args (supports --flag=value format).
func stripFlag(args []string, prefix string) []string {
	var result []string
	for _, a := range args {
		if strings.HasPrefix(a, prefix+"=") || a == prefix {
			continue
		}
		result = append(result, a)
	}
	return result
}
