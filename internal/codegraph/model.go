package codegraph

import (
	"time"
)

// Node represents an indexed code symbol in the repository graph.
type Node struct {
	SymbolID  string `json:"symbol_id"`  // e.g. "pkg/auth:ValidateToken"
	BlobHash  string `json:"blob_hash"`  // Content-addressed SHA-256 hash of the file
	Kind      string `json:"kind"`       // "function", "method", "struct", "interface", "type", "class", "package"
	Name      string `json:"name"`       // e.g. "ValidateToken"
	FilePath  string `json:"file_path"`  // Workspace-relative path
	StartLine int    `json:"start_line"` // 1-indexed
	EndLine   int    `json:"end_line"`   // 1-indexed
	Signature string `json:"signature"`  // Full signature
	Docstring string `json:"docstring"`  // Comments/docstring
	Exported  bool   `json:"exported"`   // True if public/exported symbol
}

// Edge represents a relationship between two code symbols.
type Edge struct {
	BlobHash     string `json:"blob_hash"`     // Content-addressed file hash
	SourceSymbol string `json:"source_symbol"` // SymbolID of the caller / referencing entity
	TargetSymbol string `json:"target_symbol"` // SymbolID or name of the referenced entity
	Relation     string `json:"relation"`      // "calls", "imports", "implements", "references", "defines"
	FilePath     string `json:"file_path"`     // Source file path
	Line         int    `json:"line"`          // Source line number
}

// FileManifest maps a workspace file path to its content blob hash on a given branch.
type FileManifest struct {
	BranchName string    `json:"branch_name"`
	FilePath   string    `json:"file_path"`
	BlobHash   string    `json:"blob_hash"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Branch represents a tracked git branch or workspace snapshot.
type Branch struct {
	Name       string    `json:"name"`
	HeadCommit string    `json:"head_commit"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IndexStats holds statistics from an indexing run.
type IndexStats struct {
	TotalFiles   int           `json:"total_files"`
	ParsedFiles  int           `json:"parsed_files"`
	CachedFiles  int           `json:"cached_files"`
	TotalNodes   int           `json:"total_nodes"`
	TotalEdges   int           `json:"total_edges"`
	Duration     time.Duration `json:"duration"`
	Branch       string        `json:"branch"`
}

// CallHierarchy represents incoming or outgoing calls for a symbol.
type CallHierarchy struct {
	RootSymbol string     `json:"root_symbol"`
	Direction  string     `json:"direction"` // "incoming" (callers) or "outgoing" (callees)
	Calls      []CallItem `json:"calls"`
}

// CallItem is a single hop in a call hierarchy.
type CallItem struct {
	CallerNode   Node   `json:"caller_node"`
	CalleeSymbol string `json:"callee_symbol"`
	FilePath     string `json:"file_path"`
	Line         int    `json:"line"`
	Depth        int    `json:"depth"`
}

// ImpactResult computes the downstream blast radius when a symbol or file is changed.
type ImpactResult struct {
	Target            string   `json:"target"`             // Symbol or file inspected
	Branch            string   `json:"branch"`
	DownstreamCallers []Node   `json:"downstream_callers"` // Functions/methods calling this symbol directly or indirectly
	Implementations   []Node   `json:"implementations"`    // Types implementing this interface
	AffectedFiles     []string `json:"affected_files"`     // Deduplicated list of affected file paths
	MaxDepth          int      `json:"max_depth"`
}

// BranchDiff represents differences in symbols and dependencies between two branches.
type BranchDiff struct {
	BaseBranch   string `json:"base_branch"`
	TargetBranch string `json:"target_branch"`
	AddedNodes   []Node `json:"added_nodes"`
	RemovedNodes []Node `json:"removed_nodes"`
	AddedEdges   []Edge `json:"added_edges"`
	RemovedEdges []Edge `json:"removed_edges"`
}
