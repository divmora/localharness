package codegraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store manages the code graph for a single project across all branches.
// It stores AST nodes and edges indexed by content blob hash (de-duplicated),
// and branch manifests mapping (branch, file_path) -> blob_hash.
type Store struct {
	mu           sync.RWMutex
	dbPath       string // e.g. ~/.divmora/localharness/knowledge/<project-uuid>/codegraph.duckdb
	activeBranch string

	branches  map[string]*Branch           // keyed by branch_name
	manifests map[string]map[string]string // [branch_name][file_path] -> blob_hash
	nodes     map[string][]Node            // keyed by blob_hash
	edges     map[string][]Edge            // keyed by blob_hash

	// In-memory caches per branch
	ftsIndexes   map[string]*FTSIndex    // [branch_name] -> cached FTS index
	branchGraphs map[string]*branchGraph // [branch_name] -> indexed nodes and edges
}

// branchGraph indexes nodes and edges for O(1) lookups on a branch.
type branchGraph struct {
	nodeByID        map[string]Node
	nodesByName     map[string][]Node
	nodesByFile     map[string][]Node
	callersByTarget map[string][]Edge // incoming call edges (Relation == "calls")
	calleesBySource map[string][]Edge // outgoing call edges (Relation == "calls")
	refsByTarget    map[string][]Edge // all referencing edges (calls, implements, imports, etc.)
	implsByTarget   map[string][]Edge // implements edges (Relation == "implements")
}

func (bg *branchGraph) lookupNode(symbol string, fallbackFilePath string) Node {
	if n, ok := bg.nodeByID[symbol]; ok {
		return n
	}
	if list := bg.nodesByName[symbol]; len(list) > 0 {
		return list[0]
	}
	return Node{
		SymbolID: symbol,
		Name:     symbol,
		FilePath: fallbackFilePath,
	}
}

// symbolLookupKeys returns all suffix keys under which a symbol should be indexed
// for O(1) matching of exact, ":" and "." suffixes.
func symbolLookupKeys(sym string) []string {
	sym = strings.TrimSpace(sym)
	if sym == "" {
		return nil
	}
	keys := []string{sym}
	seen := map[string]bool{sym: true}

	for i := 0; i < len(sym); i++ {
		if (sym[i] == ':' || sym[i] == '.') && i+1 < len(sym) {
			k := sym[i+1:]
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	return keys
}

func buildBranchGraph(nodes []Node, edges []Edge) *branchGraph {
	bg := &branchGraph{
		nodeByID:        make(map[string]Node, len(nodes)),
		nodesByName:     make(map[string][]Node, len(nodes)),
		nodesByFile:     make(map[string][]Node),
		callersByTarget: make(map[string][]Edge),
		calleesBySource: make(map[string][]Edge),
		refsByTarget:    make(map[string][]Edge),
		implsByTarget:   make(map[string][]Edge),
	}

	for _, n := range nodes {
		bg.nodeByID[n.SymbolID] = n
		bg.nodesByName[n.Name] = append(bg.nodesByName[n.Name], n)
		if n.FilePath != "" {
			bg.nodesByFile[n.FilePath] = append(bg.nodesByFile[n.FilePath], n)
		}
	}

	for _, e := range edges {
		// Index for FindReferences (any relation)
		for _, k := range symbolLookupKeys(e.TargetSymbol) {
			bg.refsByTarget[k] = append(bg.refsByTarget[k], e)
		}

		// Index for GetCallHierarchy & GetImpactRadius
		if e.Relation == "calls" {
			for _, k := range symbolLookupKeys(e.TargetSymbol) {
				bg.callersByTarget[k] = append(bg.callersByTarget[k], e)
			}
			for _, k := range symbolLookupKeys(e.SourceSymbol) {
				bg.calleesBySource[k] = append(bg.calleesBySource[k], e)
			}
		}

		if e.Relation == "implements" {
			for _, k := range symbolLookupKeys(e.TargetSymbol) {
				bg.implsByTarget[k] = append(bg.implsByTarget[k], e)
			}
		}
	}

	return bg
}

// diskSnapshot is the persisted JSON structure inside codegraph.duckdb
type diskSnapshot struct {
	Version      string                       `json:"version"`
	ActiveBranch string                       `json:"active_branch"`
	Branches     map[string]*Branch           `json:"branches"`
	Manifests    map[string]map[string]string `json:"manifests"`
	Nodes        map[string][]Node            `json:"nodes"`
	Edges        map[string][]Edge            `json:"edges"`
	SavedAt      time.Time                    `json:"saved_at"`
}

// NewStore creates a new Store backed by the specified DuckDB file path.
func NewStore(dbPath string) *Store {
	return &Store{
		dbPath:       dbPath,
		activeBranch: "main",
		branches:     make(map[string]*Branch),
		manifests:    make(map[string]map[string]string),
		nodes:        make(map[string][]Node),
		edges:        make(map[string][]Edge),
		ftsIndexes:   make(map[string]*FTSIndex),
		branchGraphs: make(map[string]*branchGraph),
	}
}

// DBPath returns the underlying file path of the database.
func (s *Store) DBPath() string {
	return s.dbPath
}

// Load reads the stored graph state from disk.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Fresh store
		}
		return fmt.Errorf("codegraph load %s: %w", s.dbPath, err)
	}

	var snap diskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		// If corrupted or non-JSON format, start empty
		return nil
	}

	s.activeBranch = snap.ActiveBranch
	if s.activeBranch == "" {
		s.activeBranch = "main"
	}
	s.branches = snap.Branches
	if s.branches == nil {
		s.branches = make(map[string]*Branch)
	}
	s.manifests = snap.Manifests
	if s.manifests == nil {
		s.manifests = make(map[string]map[string]string)
	}
	s.nodes = snap.Nodes
	if s.nodes == nil {
		s.nodes = make(map[string][]Node)
	}
	s.edges = snap.Edges
	if s.edges == nil {
		s.edges = make(map[string][]Edge)
	}

	s.ftsIndexes = make(map[string]*FTSIndex)
	s.branchGraphs = make(map[string]*branchGraph)

	return nil
}

// Save writes the current graph state to disk atomically.
func (s *Store) Save() error {
	s.mu.RLock()
	snap := diskSnapshot{
		Version:      "1.0",
		ActiveBranch: s.activeBranch,
		Branches:     s.branches,
		Manifests:    s.manifests,
		Nodes:        s.nodes,
		Edges:        s.edges,
		SavedAt:      time.Now().UTC(),
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("codegraph marshal: %w", err)
	}

	dir := filepath.Dir(s.dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("codegraph mkdir %s: %w", dir, err)
	}

	tmpPath := s.dbPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("codegraph write temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.dbPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("codegraph rename: %w", err)
	}

	return nil
}

// SetActiveBranch sets the current active branch name.
func (s *Store) SetActiveBranch(branch string) {
	if branch == "" {
		branch = "main"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeBranch = branch
	if _, ok := s.branches[branch]; !ok {
		s.branches[branch] = &Branch{
			Name:      branch,
			UpdatedAt: time.Now().UTC(),
		}
	}
	if _, ok := s.manifests[branch]; !ok {
		s.manifests[branch] = make(map[string]string)
	}
}

// ActiveBranch returns the current active branch.
func (s *Store) ActiveBranch() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeBranch == "" {
		return "main"
	}
	return s.activeBranch
}

// ListBranches returns all known branches.
func (s *Store) ListBranches() []Branch {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Branch, 0, len(s.branches))
	for _, b := range s.branches {
		result = append(result, *b)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// HasBlob checks if AST nodes and edges for this blob hash are already cached.
func (s *Store) HasBlob(blobHash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.nodes[blobHash]
	return ok
}

// AddFile registers or updates a file in the active branch.
func (s *Store) AddFile(branch, filePath, blobHash string, nodes []Node, edges []Edge) {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure branch manifest
	if _, ok := s.branches[branch]; !ok {
		s.branches[branch] = &Branch{
			Name:      branch,
			UpdatedAt: time.Now().UTC(),
		}
	}
	if _, ok := s.manifests[branch]; !ok {
		s.manifests[branch] = make(map[string]string)
	}

	oldHash, hadOld := s.manifests[branch][filePath]

	// Update manifest
	s.manifests[branch][filePath] = blobHash

	// Store nodes & edges if not already present
	if _, ok := s.nodes[blobHash]; !ok && len(nodes) > 0 {
		s.nodes[blobHash] = nodes
	}
	if _, ok := s.edges[blobHash]; !ok && len(edges) > 0 {
		s.edges[blobHash] = edges
	}

	// Incremental FTS update
	if fts, ok := s.ftsIndexes[branch]; ok && fts != nil {
		if hadOld && oldHash != blobHash {
			if oldNodes, ok := s.nodes[oldHash]; ok {
				for _, n := range oldNodes {
					fts.RemoveNode(n.SymbolID)
				}
			}
		}
		if !hadOld || oldHash != blobHash {
			newNodes := nodes
			if len(newNodes) == 0 {
				newNodes = s.nodes[blobHash]
			}
			for _, n := range newNodes {
				fts.IndexNode(n)
			}
		}
	}

	// Invalidate branchGraph cache on content change
	if !hadOld || oldHash != blobHash {
		delete(s.branchGraphs, branch)
	}

	s.branches[branch].UpdatedAt = time.Now().UTC()
}

// RemoveFile removes a file from a branch manifest.
func (s *Store) RemoveFile(branch, filePath string) {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.manifests[branch]; ok {
		if oldHash, ok := m[filePath]; ok {
			if fts, ok := s.ftsIndexes[branch]; ok && fts != nil {
				if oldNodes, ok := s.nodes[oldHash]; ok {
					for _, n := range oldNodes {
						fts.RemoveNode(n.SymbolID)
					}
				}
			}
			delete(m, filePath)
			delete(s.branchGraphs, branch)
		}
	}
	if b, ok := s.branches[branch]; ok {
		b.UpdatedAt = time.Now().UTC()
	}
}

// PruneDeletedFiles removes manifest entries for files no longer in active file list.
func (s *Store) PruneDeletedFiles(branch string, activeFiles map[string]bool) {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.manifests[branch]
	if !ok {
		return
	}

	fts := s.ftsIndexes[branch]
	pruned := false

	for path, hash := range m {
		if !activeFiles[path] {
			if fts != nil {
				if oldNodes, ok := s.nodes[hash]; ok {
					for _, n := range oldNodes {
						fts.RemoveNode(n.SymbolID)
					}
				}
			}
			delete(m, path)
			pruned = true
		}
	}

	if pruned {
		delete(s.branchGraphs, branch)
		if b, ok := s.branches[branch]; ok {
			b.UpdatedAt = time.Now().UTC()
		}
	}
}

func (s *Store) getBranchNodesLocked(branch string) []Node {
	manifest, ok := s.manifests[branch]
	if !ok {
		return nil
	}

	var results []Node
	for _, hash := range manifest {
		if nodeList, ok := s.nodes[hash]; ok {
			results = append(results, nodeList...)
		}
	}
	return results
}

func (s *Store) getBranchEdgesLocked(branch string) []Edge {
	manifest, ok := s.manifests[branch]
	if !ok {
		return nil
	}

	var results []Edge
	for _, hash := range manifest {
		if edgeList, ok := s.edges[hash]; ok {
			results = append(results, edgeList...)
		}
	}
	return results
}

func (s *Store) getOrBuildFTS(branch string) *FTSIndex {
	s.mu.RLock()
	if fts, ok := s.ftsIndexes[branch]; ok && fts != nil {
		s.mu.RUnlock()
		return fts
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if fts, ok := s.ftsIndexes[branch]; ok && fts != nil {
		return fts
	}

	fts := NewFTSIndex()
	nodes := s.getBranchNodesLocked(branch)
	for _, n := range nodes {
		fts.IndexNode(n)
	}
	s.ftsIndexes[branch] = fts
	return fts
}

func (s *Store) getOrBuildGraph(branch string) *branchGraph {
	s.mu.RLock()
	if bg, ok := s.branchGraphs[branch]; ok && bg != nil {
		s.mu.RUnlock()
		return bg
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if bg, ok := s.branchGraphs[branch]; ok && bg != nil {
		return bg
	}

	nodes := s.getBranchNodesLocked(branch)
	edges := s.getBranchEdgesLocked(branch)
	bg := buildBranchGraph(nodes, edges)
	s.branchGraphs[branch] = bg
	return bg
}

// GetBranchNodes returns all active AST nodes on a given branch.
func (s *Store) GetBranchNodes(branch string) []Node {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getBranchNodesLocked(branch)
}

// GetBranchEdges returns all active relationship edges on a given branch.
func (s *Store) GetBranchEdges(branch string) []Edge {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getBranchEdgesLocked(branch)
}

// SearchSymbols finds symbols matching query pattern and optional kind filter using BM25 FTS ranking.
func (s *Store) SearchSymbols(branch, query, kind string, limit int) []Node {
	scored := s.SearchSymbolsFTS(branch, query, kind, limit)
	var nodes []Node
	for _, sn := range scored {
		nodes = append(nodes, sn.Node)
	}
	return nodes
}

// SearchSymbolsFTS finds and ranks symbols matching query using full-text search with BM25 scoring.
func (s *Store) SearchSymbolsFTS(branch, query, kind string, limit int) []ScoredNode {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	if limit <= 0 {
		limit = 50
	}

	query = strings.TrimSpace(query)
	kind = strings.ToLower(strings.TrimSpace(kind))

	// If empty query, return top nodes alphabetically
	if query == "" {
		nodes := s.GetBranchNodes(branch)
		if len(nodes) == 0 {
			return nil
		}
		var results []ScoredNode
		for _, n := range nodes {
			if kind != "" && strings.ToLower(n.Kind) != kind {
				continue
			}
			results = append(results, ScoredNode{Node: n, Score: 1.0})
			if len(results) >= limit {
				break
			}
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].Node.Name < results[j].Node.Name
		})
		return results
	}

	fts := s.getOrBuildFTS(branch)
	return fts.Search(query, kind, limit)
}

// FindReferences locates all callers and usages referencing targetSymbol on branch.
func (s *Store) FindReferences(branch, targetSymbol string) []Edge {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	target := strings.TrimSpace(targetSymbol)
	if target == "" {
		return nil
	}

	bg := s.getOrBuildGraph(branch)
	refs := bg.refsByTarget[target]
	if len(refs) == 0 {
		return nil
	}

	result := make([]Edge, len(refs))
	copy(result, refs)
	return result
}

// GetCallHierarchy traverses incoming callers or outgoing callees up to maxDepth.
func (s *Store) GetCallHierarchy(branch, symbolID, direction string, maxDepth int) *CallHierarchy {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	if maxDepth <= 0 {
		maxDepth = 3
	}
	if direction == "" {
		direction = "incoming"
	}

	bg := s.getOrBuildGraph(branch)

	hierarchy := &CallHierarchy{
		RootSymbol: symbolID,
		Direction:  direction,
		Calls:      make([]CallItem, 0),
	}

	visited := make(map[string]bool)
	var traverse func(curr string, depth int)

	traverse = func(curr string, depth int) {
		if depth > maxDepth || visited[curr] {
			return
		}
		visited[curr] = true

		if direction == "incoming" {
			for _, e := range bg.callersByTarget[curr] {
				callerNode := bg.lookupNode(e.SourceSymbol, e.FilePath)
				hierarchy.Calls = append(hierarchy.Calls, CallItem{
					CallerNode:   callerNode,
					CalleeSymbol: e.TargetSymbol,
					FilePath:     e.FilePath,
					Line:         e.Line,
					Depth:        depth,
				})
				traverse(e.SourceSymbol, depth+1)
			}
		} else {
			for _, e := range bg.calleesBySource[curr] {
				callerNode := bg.lookupNode(e.SourceSymbol, e.FilePath)
				hierarchy.Calls = append(hierarchy.Calls, CallItem{
					CallerNode:   callerNode,
					CalleeSymbol: e.TargetSymbol,
					FilePath:     e.FilePath,
					Line:         e.Line,
					Depth:        depth,
				})
				traverse(e.TargetSymbol, depth+1)
			}
		}
	}

	traverse(symbolID, 1)
	return hierarchy
}

// GetImpactRadius calculates all downstream callers, implementations, and affected files.
func (s *Store) GetImpactRadius(branch, symbolOrFile string, maxDepth int) *ImpactResult {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	if maxDepth <= 0 {
		maxDepth = 5
	}

	bg := s.getOrBuildGraph(branch)

	affectedFilesMap := make(map[string]bool)
	callersMap := make(map[string]Node)
	implMap := make(map[string]Node)

	// Determine starting symbols
	var startingSymbols []string
	if strings.HasSuffix(symbolOrFile, ".go") || strings.HasSuffix(symbolOrFile, ".py") || strings.HasSuffix(symbolOrFile, ".ts") || strings.Contains(symbolOrFile, "/") {
		affectedFilesMap[symbolOrFile] = true
		if nodes, ok := bg.nodesByFile[symbolOrFile]; ok {
			for _, n := range nodes {
				startingSymbols = append(startingSymbols, n.SymbolID)
			}
		} else {
			// Suffix match for relative file paths e.g. "user.go" matching "pkg/user.go"
			for file, nodes := range bg.nodesByFile {
				if strings.HasSuffix(file, "/"+symbolOrFile) {
					affectedFilesMap[file] = true
					for _, n := range nodes {
						startingSymbols = append(startingSymbols, n.SymbolID)
					}
				}
			}
		}
	} else {
		startingSymbols = append(startingSymbols, symbolOrFile)
		if n, ok := bg.nodeByID[symbolOrFile]; ok && n.FilePath != "" {
			affectedFilesMap[n.FilePath] = true
		}
		for _, n := range bg.nodesByName[symbolOrFile] {
			if n.FilePath != "" {
				affectedFilesMap[n.FilePath] = true
			}
		}
	}

	visited := make(map[string]bool)
	var traverse func(sym string, depth int)

	traverse = func(sym string, depth int) {
		if depth > maxDepth || visited[sym] {
			return
		}
		visited[sym] = true

		// Find callers
		for _, e := range bg.callersByTarget[sym] {
			affectedFilesMap[e.FilePath] = true
			if n, ok := bg.nodeByID[e.SourceSymbol]; ok {
				callersMap[n.SymbolID] = n
			} else {
				callersMap[e.SourceSymbol] = Node{
					SymbolID: e.SourceSymbol,
					Name:     e.SourceSymbol,
					FilePath: e.FilePath,
				}
			}
			traverse(e.SourceSymbol, depth+1)
		}

		// Find implementations
		for _, e := range bg.implsByTarget[sym] {
			affectedFilesMap[e.FilePath] = true
			if n, ok := bg.nodeByID[e.SourceSymbol]; ok {
				implMap[n.SymbolID] = n
			}
		}
	}

	for _, sym := range startingSymbols {
		traverse(sym, 1)
	}

	var callers []Node
	for _, c := range callersMap {
		callers = append(callers, c)
	}
	sort.Slice(callers, func(i, j int) bool {
		return callers[i].SymbolID < callers[j].SymbolID
	})

	var impls []Node
	for _, im := range implMap {
		impls = append(impls, im)
	}
	sort.Slice(impls, func(i, j int) bool {
		return impls[i].SymbolID < impls[j].SymbolID
	})

	var affectedFiles []string
	for f := range affectedFilesMap {
		affectedFiles = append(affectedFiles, f)
	}
	sort.Strings(affectedFiles)

	return &ImpactResult{
		Target:            symbolOrFile,
		Branch:            branch,
		DownstreamCallers: callers,
		Implementations:   impls,
		AffectedFiles:     affectedFiles,
		MaxDepth:          maxDepth,
	}
}

// DiffBranches computes structural additions and deletions between two branches.
func (s *Store) DiffBranches(baseBranch, targetBranch string) *BranchDiff {
	baseNodes := s.GetBranchNodes(baseBranch)
	targetNodes := s.GetBranchNodes(targetBranch)

	baseEdges := s.GetBranchEdges(baseBranch)
	targetEdges := s.GetBranchEdges(targetBranch)

	baseNodeMap := make(map[string]Node)
	for _, n := range baseNodes {
		baseNodeMap[n.SymbolID] = n
	}

	targetNodeMap := make(map[string]Node)
	for _, n := range targetNodes {
		targetNodeMap[n.SymbolID] = n
	}

	var addedNodes []Node
	for id, n := range targetNodeMap {
		if _, ok := baseNodeMap[id]; !ok {
			addedNodes = append(addedNodes, n)
		}
	}

	var removedNodes []Node
	for id, n := range baseNodeMap {
		if _, ok := targetNodeMap[id]; !ok {
			removedNodes = append(removedNodes, n)
		}
	}

	baseEdgeMap := make(map[string]Edge)
	for _, e := range baseEdges {
		key := fmt.Sprintf("%s->%s:%s", e.SourceSymbol, e.TargetSymbol, e.Relation)
		baseEdgeMap[key] = e
	}

	targetEdgeMap := make(map[string]Edge)
	for _, e := range targetEdges {
		key := fmt.Sprintf("%s->%s:%s", e.SourceSymbol, e.TargetSymbol, e.Relation)
		targetEdgeMap[key] = e
	}

	var addedEdges []Edge
	for k, e := range targetEdgeMap {
		if _, ok := baseEdgeMap[k]; !ok {
			addedEdges = append(addedEdges, e)
		}
	}

	var removedEdges []Edge
	for k, e := range baseEdgeMap {
		if _, ok := targetEdgeMap[k]; !ok {
			removedEdges = append(removedEdges, e)
		}
	}

	return &BranchDiff{
		BaseBranch:   baseBranch,
		TargetBranch: targetBranch,
		AddedNodes:   addedNodes,
		RemovedNodes: removedNodes,
		AddedEdges:   addedEdges,
		RemovedEdges: removedEdges,
	}
}

// ExportDuckDBSchema returns the standard DuckDB SQL DDL schema.
func (s *Store) ExportDuckDBSchema() string {
	return `
-- DuckDB Code Graph Schema
CREATE TABLE IF NOT EXISTS branches (
    branch_name VARCHAR PRIMARY KEY,
    head_commit VARCHAR,
    updated_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS file_manifest (
    branch_name VARCHAR,
    file_path VARCHAR,
    blob_hash VARCHAR,
    PRIMARY KEY (branch_name, file_path)
);

CREATE TABLE IF NOT EXISTS nodes (
    blob_hash VARCHAR,
    symbol_id VARCHAR,
    kind VARCHAR,
    name VARCHAR,
    file_path VARCHAR,
    start_line INTEGER,
    end_line INTEGER,
    signature VARCHAR,
    docstring VARCHAR,
    exported BOOLEAN,
    PRIMARY KEY (blob_hash, symbol_id)
);

CREATE TABLE IF NOT EXISTS edges (
    blob_hash VARCHAR,
    source_symbol VARCHAR,
    target_symbol VARCHAR,
    relation VARCHAR,
    file_path VARCHAR,
    line INTEGER,
    PRIMARY KEY (blob_hash, source_symbol, target_symbol, relation)
);
`
}
