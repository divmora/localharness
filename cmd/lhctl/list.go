package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// listFlags holds parsed flags for the list command.
type listFlags struct {
	recent int // 0 = show all
}

func parseListFlags(args []string) listFlags {
	f := listFlags{}
	for _, a := range args {
		if strings.HasPrefix(a, "--recent=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(a, "--recent=")); err == nil && n > 0 {
				f.recent = n
			}
		}
	}
	return f
}

// convSummary holds summary data for a conversation listing entry.
type convSummary struct {
	ID        string
	CreatedAt string
	UpdatedAt string
	Messages  int
	Status    string
	SizeBytes int64

	// Agent lineage
	ParentID  string
	AgentType string
	AgentRole string
	Depth     int32
}

func runList(dataDir string, extraArgs []string) {
	flags := parseListFlags(extraArgs)

	convDir := filepath.Join(dataDir, "conversations")
	entries, err := os.ReadDir(convDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read %s: %v\n", convDir, err)
		os.Exit(1)
	}

	var summaries []convSummary

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pb" {
			continue
		}

		id := strings.TrimSuffix(e.Name(), ".pb")
		pbPath := filepath.Join(convDir, e.Name())

		// Get file size
		info, err := e.Info()
		if err != nil {
			continue
		}

		// Load state (quick — we only need header fields)
		data, err := os.ReadFile(pbPath)
		if err != nil {
			continue
		}

		state := &pb.ConversationState{}
		if err := proto.Unmarshal(data, state); err != nil {
			continue
		}

		summaries = append(summaries, convSummary{
			ID:        id,
			CreatedAt: state.CreatedAt,
			UpdatedAt: state.UpdatedAt,
			Messages:  len(state.Messages),
			Status:    formatStatus(state.Status),
			SizeBytes: info.Size(),
			ParentID:  state.ParentConversationId,
			AgentType: state.AgentTypeName,
			AgentRole: state.AgentRole,
			Depth:     state.AgentDepth,
		})
	}

	if len(summaries) == 0 {
		fmt.Println("No conversations found.")
		return
	}

	// Sort by UpdatedAt descending (most recent first)
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt > summaries[j].UpdatedAt
	})

	// Apply --recent limit
	if flags.recent > 0 && flags.recent < len(summaries) {
		summaries = summaries[:flags.recent]
	}

	// Print table
	fmt.Printf("%-38s  %-20s  %8s  %-8s  %-14s  %6s\n", "ID", "Updated", "Messages", "Status", "Agent", "Size")
	fmt.Println(strings.Repeat("─", 102))
	for _, s := range summaries {
		// Truncate timestamp to just date+time
		updated := s.UpdatedAt
		if len(updated) > 19 {
			updated = updated[:19]
		}

		// Agent column: "root" for top-level, "type (depth N)" for subagents
		agentCol := "root"
		if s.ParentID != "" {
			agentCol = s.AgentType
			if agentCol == "" {
				agentCol = "sub"
			}
			agentCol = fmt.Sprintf("%s (d%d)", agentCol, s.Depth)
		}

		fmt.Printf("%-38s  %-20s  %8d  %-8s  %-14s  %5dK\n",
			s.ID, updated, s.Messages, s.Status, agentCol, s.SizeBytes/1024)
	}
	fmt.Printf("\nTotal: %d conversations\n", len(summaries))
}

// formatStatus converts proto enum to short display string.
func formatStatus(s pb.ConversationState_ConversationStatus) string {
	switch s {
	case pb.ConversationState_STATUS_ACTIVE:
		return "ACTIVE"
	case pb.ConversationState_STATUS_COMPLETED:
		return "DONE"
	case pb.ConversationState_STATUS_ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
