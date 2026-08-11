// lhctl — LocalHarness CLI utility for inspecting conversations and debugging.
//
// This is a lightweight, offline-only tool that reads conversation state files
// from disk. It has zero runtime dependencies — no WebSocket, no LLM providers.
//
// Usage:
//
//	lhctl conversation inspect <id>           # Message-level analysis
//	lhctl conversation inspect <id> --json    # Machine-readable output
//	lhctl conversation list                   # List all conversations
//	lhctl conversation list --recent=5        # Last 5 conversations
//	lhctl --help                              # Show help
//
// The data directory defaults to ~/.divmora/localharness/ and can be
// overridden with --data-dir.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usage = `lhctl — LocalHarness CLI Debugger

Usage:
  lhctl conversation inspect <id> [flags]   Inspect a conversation's messages
  lhctl conversation trace <id> [flags]     Show tool call timeline
  lhctl conversation list [flags]           List all conversations
  lhctl conversation tree <id>              Show agent tree for a conversation
  lhctl conv inspect <id> [flags]           (alias for conversation)
  lhctl --help                              Show this help

Global Flags:
  --data-dir=<path>   Override data directory (default: ~/.divmora/localharness/)

Inspect Flags:
  --json              Output as JSON (for piping to jq)
  --top=<N>           Show top N largest messages (default: 3)
  --steps             Show full tool args, paths, and error details
  --step=<N>          Deep-dive into a single step (dump full args and result)
  --errors            Show only steps that errored (policy denials, tool failures)

Trace Flags:
  --watch             Live-tail mode — poll for new trace files as agent runs
  --commands          Show full command lines for run_command calls

List Flags:
  --recent=<N>        Show only the N most recent conversations

Examples:
  lhctl conv inspect 3c1a5fa1                    # Overview with size analysis
  lhctl conv inspect 3c1a5fa1 --steps            # Full step trace with paths
  lhctl conv inspect 3c1a5fa1 --errors           # Only errors and denials
  lhctl conv inspect 3c1a5fa1 --step=1           # Deep-dive into step 1
  lhctl conv inspect 3c1a5fa1 --json | jq '.'    # Machine-readable output
  lhctl conv trace d2784f60                       # Tool call timeline
  lhctl conv trace d2784f60 --watch              # Live-tail while agent runs
  lhctl conv tree 253aacfb                        # Show agent family tree
`

func main() {
	args := os.Args[1:]

	if len(args) == 0 || contains(args, "--help") || contains(args, "-h") {
		fmt.Print(usage)
		os.Exit(0)
	}

	// Resolve data directory
	dataDir := resolveDataDir(args)

	// Strip --data-dir from args
	args = stripFlag(args, "--data-dir")

	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "Error: no subcommand provided. Run 'lhctl --help' for usage.\n")
		os.Exit(1)
	}

	// Subcommand routing
	subcmd := args[0]
	switch subcmd {
	case "conversation", "conv":
		if len(args) < 2 {
			fmt.Fprint(os.Stderr, "Error: missing sub-subcommand. Use 'inspect' or 'list'.\n")
			os.Exit(1)
		}
		switch args[1] {
		case "inspect":
			if len(args) < 3 {
				fmt.Fprint(os.Stderr, "Error: missing conversation ID.\nUsage: lhctl conversation inspect <id>\n")
				os.Exit(1)
			}
			runInspect(dataDir, args[2], args[3:])
		case "list":
			runList(dataDir, args[2:])
		case "tree":
			if len(args) < 3 {
				fmt.Fprint(os.Stderr, "Error: missing conversation ID.\nUsage: lhctl conversation tree <id>\n")
				os.Exit(1)
			}
			runTree(dataDir, args[2])
		case "trace":
			if len(args) < 3 {
				fmt.Fprint(os.Stderr, "Error: missing conversation ID.\nUsage: lhctl conversation trace <id> [--watch] [--commands]\n")
				os.Exit(1)
			}
			runTrace(dataDir, args[2], args[3:])
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown sub-subcommand %q. Use 'inspect' or 'list'.\n", args[1])
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown subcommand %q. Run 'lhctl --help' for usage.\n", subcmd)
		os.Exit(1)
	}
}

// resolveDataDir finds the data directory from --data-dir flag or default.
func resolveDataDir(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--data-dir=") {
			return strings.TrimPrefix(a, "--data-dir=")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(home, ".divmora", "localharness")
}

// resolveConversationID resolves a partial ID to a full UUID by matching
// against existing .pb files in the conversations directory.
func resolveConversationID(dataDir, partialID string) (string, error) {
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
