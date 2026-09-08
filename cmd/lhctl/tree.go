package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// agentNode represents a conversation in the agent tree.
type agentNode struct {
	ID        string
	Role      string
	TypeName  string
	Depth     int32
	Messages  int
	Status    string
	SizeBytes int64
	Children  []*agentNode
}

// runTree displays the agent family tree for a given conversation.
// It finds the root conversation (following parent links upward),
// then builds and renders the full tree of descendants.
func runTree(dataDir, partialID string) {
	// Resolve partial ID
	convID, err := resolveConversationID(dataDir, partialID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Load all conversations into a map
	convDir := filepath.Join(dataDir, "conversations")
	allStates := loadAllConversations(convDir)

	if _, ok := allStates[convID]; !ok {
		fmt.Fprintf(os.Stderr, "Error: conversation %s not found\n", convID)
		os.Exit(1)
	}

	// Walk up to find the root
	rootID := convID
	for {
		state, ok := allStates[rootID]
		if !ok {
			break
		}
		if state.ParentConversationId == "" {
			break
		}
		// Check if parent exists
		if _, parentExists := allStates[state.ParentConversationId]; !parentExists {
			break
		}
		rootID = state.ParentConversationId
	}

	// Build tree from root downward
	// Build parent → children index
	childrenOf := make(map[string][]string) // parentID → []childID
	for id, state := range allStates {
		if state.ParentConversationId != "" {
			childrenOf[state.ParentConversationId] = append(
				childrenOf[state.ParentConversationId], id)
		}
	}

	root := buildNode(rootID, allStates, childrenOf, convDir)

	// Render
	fmt.Printf("🌳 Agent Tree (root: %s)\n", truncateID(rootID))
	fmt.Println(strings.Repeat("─", 80))
	renderTree(root, "", true, convID)
	fmt.Println(strings.Repeat("─", 80))

	// Summary
	total := countNodes(root)
	maxDepth := maxTreeDepth(root)
	fmt.Printf("\nAgents: %d | Max depth: %d\n", total, maxDepth)
}

// buildNode recursively builds an agentNode from conversation state.
func buildNode(id string, states map[string]*pb.ConversationState, children map[string][]string, convDir string) *agentNode {
	state := states[id]

	// Get file size
	pbPath := filepath.Join(convDir, id+".pb")
	var sizeBytes int64
	if info, err := os.Stat(pbPath); err == nil {
		sizeBytes = info.Size()
	}

	node := &agentNode{
		ID:        id,
		Role:      state.AgentRole,
		TypeName:  state.AgentTypeName,
		Depth:     state.AgentDepth,
		Messages:  len(state.Messages),
		Status:    formatStatus(state.Status),
		SizeBytes: sizeBytes,
	}

	for _, childID := range children[id] {
		node.Children = append(node.Children, buildNode(childID, states, children, convDir))
	}

	return node
}

// renderTree prints the tree with box-drawing connectors.
func renderTree(node *agentNode, prefix string, isLast bool, highlightID string) {
	// Connector
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	if prefix == "" {
		connector = "" // Root has no connector
	}

	// Format node label
	label := formatNodeLabel(node)

	// Highlight the requested conversation
	highlight := ""
	if node.ID == highlightID {
		highlight = " ◀"
	}

	fmt.Printf("%s%s%s%s\n", prefix, connector, label, highlight)

	// Recurse into children
	childPrefix := prefix
	if prefix != "" {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, child := range node.Children {
		isChildLast := i == len(node.Children)-1
		renderTree(child, childPrefix, isChildLast, highlightID)
	}
}

// formatNodeLabel formats a single node for tree display.
func formatNodeLabel(n *agentNode) string {
	shortID := truncateID(n.ID)

	if n.Depth == 0 {
		// Root node
		return fmt.Sprintf("🤖 %s [root] (%d msgs, %s)",
			shortID, n.Messages, formatStatus2(n.Status))
	}

	// Child node
	typeName := n.TypeName
	if typeName == "" {
		typeName = "sub"
	}
	role := n.Role
	if role == "" {
		role = "agent"
	}

	return fmt.Sprintf("🔹 %s [%s: %s] (d%d, %d msgs, %s)",
		shortID, typeName, role, n.Depth, n.Messages, formatStatus2(n.Status))
}

// truncateID shows first 8 chars of a UUID for readability.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// formatStatus2 returns a compact status with emoji.
func formatStatus2(status string) string {
	switch status {
	case "ACTIVE":
		return "✅"
	case "DONE":
		return "✅ done"
	case "ERROR":
		return "❌ error"
	default:
		return status
	}
}

// countNodes counts total nodes in the tree.
func countNodes(n *agentNode) int {
	count := 1
	for _, c := range n.Children {
		count += countNodes(c)
	}
	return count
}

// maxTreeDepth returns the maximum depth in the tree.
func maxTreeDepth(n *agentNode) int {
	max := int(n.Depth)
	for _, c := range n.Children {
		d := maxTreeDepth(c)
		if d > max {
			max = d
		}
	}
	return max
}

// loadAllConversations loads all .pb files from the conversations directory.
func loadAllConversations(convDir string) map[string]*pb.ConversationState {
	result := make(map[string]*pb.ConversationState)

	entries, err := os.ReadDir(convDir)
	if err != nil {
		return result
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pb" {
			continue
		}

		id := strings.TrimSuffix(e.Name(), ".pb")
		data, err := os.ReadFile(filepath.Join(convDir, e.Name()))
		if err != nil {
			continue
		}

		state := &pb.ConversationState{}
		if err := proto.Unmarshal(data, state); err != nil {
			continue
		}

		result[id] = state
	}

	return result
}
