package codegraph

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages the code graph for a single project across all branches.
// It stores AST nodes and edges in an embedded SQLite database indexed by content blob hash,
// with branch manifests mapping (branch, file_path) -> blob_hash.
type Store struct {
	mu           sync.RWMutex
	dbPath       string
	activeBranch string
	db           *sql.DB

	// In-memory caches per branch for hot queries and FTS
	ftsIndexes   map[string]*FTSIndex
	branchGraphs map[string]*branchGraph
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

// diskSnapshot is the legacy JSON structure for backward-compatible migration.
type diskSnapshot struct {
	Version      string                       `json:"version"`
	ActiveBranch string                       `json:"active_branch"`
	Branches     map[string]*Branch           `json:"branches"`
	Manifests    map[string]map[string]string `json:"manifests"`
	Nodes        map[string][]Node            `json:"nodes"`
	Edges        map[string][]Edge            `json:"edges"`
	SavedAt      time.Time                    `json:"saved_at"`
}

// NewStore creates a new Store backed by the specified SQLite database path.
func NewStore(dbPath string) *Store {
	return &Store{
		dbPath:       dbPath,
		activeBranch: "main",
		ftsIndexes:   make(map[string]*FTSIndex),
		branchGraphs: make(map[string]*branchGraph),
	}
}

// DBPath returns the underlying database file path.
func (s *Store) DBPath() string {
	return s.dbPath
}

// Close closes the underlying SQLite database connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// initSchema initializes the SQLite tables, indexes, and pragmas.
func (s *Store) initSchema() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := s.db.Exec(p); err != nil {
			return fmt.Errorf("codegraph set pragma %q: %w", p, err)
		}
	}

	schema := `
	CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT
	);

	CREATE TABLE IF NOT EXISTS branches (
		name TEXT PRIMARY KEY,
		head_commit TEXT,
		updated_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS file_manifest (
		branch_name TEXT,
		file_path TEXT,
		blob_hash TEXT NOT NULL,
		updated_at TIMESTAMP,
		PRIMARY KEY (branch_name, file_path)
	);
	CREATE INDEX IF NOT EXISTS idx_manifest_blob ON file_manifest(blob_hash);
	CREATE INDEX IF NOT EXISTS idx_manifest_branch ON file_manifest(branch_name);

	CREATE TABLE IF NOT EXISTS nodes (
		blob_hash TEXT NOT NULL,
		symbol_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		file_path TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		signature TEXT,
		docstring TEXT,
		exported BOOLEAN NOT NULL DEFAULT 0,
		PRIMARY KEY (blob_hash, symbol_id)
	);
	CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
	CREATE INDEX IF NOT EXISTS idx_nodes_symbol_id ON nodes(symbol_id);
	CREATE INDEX IF NOT EXISTS idx_nodes_file_path ON nodes(file_path);

	CREATE TABLE IF NOT EXISTS edges (
		blob_hash TEXT NOT NULL,
		source_symbol TEXT NOT NULL,
		target_symbol TEXT NOT NULL,
		relation TEXT NOT NULL,
		file_path TEXT NOT NULL,
		line INTEGER NOT NULL,
		PRIMARY KEY (blob_hash, source_symbol, target_symbol, relation, line)
	);
	CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_symbol, relation);
	CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_symbol, relation);
	CREATE INDEX IF NOT EXISTS idx_edges_blob ON edges(blob_hash);

	CREATE VIRTUAL TABLE IF NOT EXISTS fts_nodes USING fts5(
		blob_hash UNINDEXED,
		symbol_id,
		name,
		signature,
		docstring,
		tokenize = 'unicode61 remove_diacritics 2'
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("codegraph init schema: %w", err)
	}
	return nil
}

// Load opens the SQLite database and initializes schema or migrates legacy JSON snapshots.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.dbPath), 0755); err != nil {
		return fmt.Errorf("create codegraph dir: %w", err)
	}

	// Check if the file is a legacy JSON snapshot
	if fi, err := os.Stat(s.dbPath); err == nil && fi.Size() > 0 {
		prefix := make([]byte, 16)
		f, rErr := os.Open(s.dbPath)
		if rErr == nil {
			n, _ := f.Read(prefix)
			f.Close()
			trimmed := bytes.TrimSpace(prefix[:n])
			if len(trimmed) > 0 && trimmed[0] == '{' {
				// Legacy JSON snapshot detected; migrate to SQLite
				if err := s.migrateLegacyJSON(); err != nil {
					return fmt.Errorf("migrate legacy json: %w", err)
				}
			}
		}
	}

	if s.db == nil {
		db, err := sql.Open("sqlite", s.dbPath)
		if err != nil {
			return fmt.Errorf("open codegraph sqlite %s: %w", s.dbPath, err)
		}
		s.db = db
	}

	if err := s.initSchema(); err != nil {
		return err
	}

	// Load active branch from metadata
	var active string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key = 'active_branch'").Scan(&active)
	if err == nil && active != "" {
		s.activeBranch = active
	} else {
		if s.activeBranch == "" {
			s.activeBranch = "main"
		}
		_, _ = s.db.Exec("INSERT OR REPLACE INTO metadata(key, value) VALUES('active_branch', ?)", s.activeBranch)
	}

	return nil
}

// migrateLegacyJSON reads old JSON snapshot data and writes it into a fresh SQLite database.
func (s *Store) migrateLegacyJSON() error {
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return err
	}

	var snap diskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		// Not valid JSON, let SQLite try opening it
		return nil
	}

	// Rename old JSON to .bak
	bakPath := s.dbPath + ".json.bak"
	_ = os.Rename(s.dbPath, bakPath)

	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return err
	}
	s.db = db

	if err := s.initSchema(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if snap.ActiveBranch != "" {
		s.activeBranch = snap.ActiveBranch
	} else {
		s.activeBranch = "main"
	}
	_, _ = tx.Exec("INSERT OR REPLACE INTO metadata(key, value) VALUES('active_branch', ?)", s.activeBranch)

	for name, b := range snap.Branches {
		up := b.UpdatedAt
		if up.IsZero() {
			up = time.Now().UTC()
		}
		_, _ = tx.Exec("INSERT OR REPLACE INTO branches(name, head_commit, updated_at) VALUES(?, ?, ?)", name, b.HeadCommit, up)
	}

	for branch, m := range snap.Manifests {
		now := time.Now().UTC()
		for path, hash := range m {
			_, _ = tx.Exec("INSERT OR REPLACE INTO file_manifest(branch_name, file_path, blob_hash, updated_at) VALUES(?, ?, ?, ?)", branch, path, hash, now)
		}
	}

	for hash, nodes := range snap.Nodes {
		for _, n := range nodes {
			_, _ = tx.Exec(`INSERT OR IGNORE INTO nodes(blob_hash, symbol_id, kind, name, file_path, start_line, end_line, signature, docstring, exported)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				hash, n.SymbolID, n.Kind, n.Name, n.FilePath, n.StartLine, n.EndLine, n.Signature, n.Docstring, n.Exported)
			_, _ = tx.Exec(`INSERT INTO fts_nodes(blob_hash, symbol_id, name, signature, docstring) VALUES(?, ?, ?, ?, ?)`,
				hash, n.SymbolID, n.Name, n.Signature, n.Docstring)
		}
	}

	for hash, edges := range snap.Edges {
		for _, e := range edges {
			_, _ = tx.Exec(`INSERT OR IGNORE INTO edges(blob_hash, source_symbol, target_symbol, relation, file_path, line)
				VALUES(?, ?, ?, ?, ?, ?)`,
				hash, e.SourceSymbol, e.TargetSymbol, e.Relation, e.FilePath, e.Line)
		}
	}

	return tx.Commit()
}

// ensureDB ensures the SQLite database connection is open and schema initialized.
func (s *Store) ensureDB() error {
	if s.db != nil {
		return nil
	}
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return fmt.Errorf("open codegraph sqlite %s: %w", s.dbPath, err)
	}
	s.db = db
	return s.initSchema()
}

// Save is a lightweight operation in SQLite since point mutations are committed immediately.
// It checkpoints WAL and ensures active branch metadata is up to date.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.ensureDB(); err != nil {
		return err
	}

	_, _ = s.db.Exec("INSERT OR REPLACE INTO metadata(key, value) VALUES('active_branch', ?)", s.activeBranch)
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(PASSIVE);")
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

	if err := s.ensureDB(); err == nil {
		_, _ = s.db.Exec("INSERT OR REPLACE INTO metadata(key, value) VALUES('active_branch', ?)", branch)
		_, _ = s.db.Exec("INSERT OR IGNORE INTO branches(name, updated_at) VALUES(?, ?)", branch, time.Now().UTC())
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

	if err := s.ensureDB(); err != nil {
		return nil
	}

	rows, err := s.db.Query("SELECT name, COALESCE(head_commit, ''), updated_at FROM branches ORDER BY name")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []Branch
	for rows.Next() {
		var b Branch
		var up time.Time
		if err := rows.Scan(&b.Name, &b.HeadCommit, &up); err == nil {
			b.UpdatedAt = up
			result = append(result, b)
		}
	}
	return result
}

// HasBlob checks if AST nodes and edges for this blob hash are already cached.
func (s *Store) HasBlob(blobHash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.ensureDB(); err != nil {
		return false
	}

	var exists int
	err := s.db.QueryRow("SELECT 1 FROM nodes WHERE blob_hash = ? LIMIT 1", blobHash).Scan(&exists)
	return err == nil && exists == 1
}

// AddFile registers or updates a file in the active branch.
// It inserts new nodes and edges indexed by content blob hash (de-duplicated),
// updates the branch file manifest, and updates in-memory caches.
func (s *Store) AddFile(branch, filePath, blobHash string, nodes []Node, edges []Edge) {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDB(); err != nil {
		return
	}

	now := time.Now().UTC()

	// Check old hash for incremental FTS update
	var oldHash string
	_ = s.db.QueryRow("SELECT blob_hash FROM file_manifest WHERE branch_name = ? AND file_path = ?", branch, filePath).Scan(&oldHash)

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck

	_, _ = tx.Exec("INSERT OR IGNORE INTO branches(name, updated_at) VALUES(?, ?)", branch, now)
	_, _ = tx.Exec("UPDATE branches SET updated_at = ? WHERE name = ?", now, branch)
	_, _ = tx.Exec("INSERT OR REPLACE INTO file_manifest(branch_name, file_path, blob_hash, updated_at) VALUES(?, ?, ?, ?)", branch, filePath, blobHash, now)

	// Check if this blob hash is already known in nodes
	var nodeCount int
	_ = tx.QueryRow("SELECT COUNT(*) FROM nodes WHERE blob_hash = ?", blobHash).Scan(&nodeCount)

	if nodeCount == 0 && len(nodes) > 0 {
		nodeStmt, err := tx.Prepare(`INSERT OR IGNORE INTO nodes(blob_hash, symbol_id, kind, name, file_path, start_line, end_line, signature, docstring, exported)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err == nil {
			defer nodeStmt.Close()
			ftsStmt, _ := tx.Prepare(`INSERT INTO fts_nodes(blob_hash, symbol_id, name, signature, docstring) VALUES(?, ?, ?, ?, ?)`)
			if ftsStmt != nil {
				defer ftsStmt.Close()
			}
			for _, n := range nodes {
				_, _ = nodeStmt.Exec(blobHash, n.SymbolID, n.Kind, n.Name, n.FilePath, n.StartLine, n.EndLine, n.Signature, n.Docstring, n.Exported)
				if ftsStmt != nil {
					_, _ = ftsStmt.Exec(blobHash, n.SymbolID, n.Name, n.Signature, n.Docstring)
				}
			}
		}
	}

	// Check if this blob hash is already known in edges
	var edgeCount int
	_ = tx.QueryRow("SELECT COUNT(*) FROM edges WHERE blob_hash = ?", blobHash).Scan(&edgeCount)

	if edgeCount == 0 && len(edges) > 0 {
		edgeStmt, err := tx.Prepare(`INSERT OR IGNORE INTO edges(blob_hash, source_symbol, target_symbol, relation, file_path, line)
			VALUES(?, ?, ?, ?, ?, ?)`)
		if err == nil {
			defer edgeStmt.Close()
			for _, e := range edges {
				_, _ = edgeStmt.Exec(blobHash, e.SourceSymbol, e.TargetSymbol, e.Relation, e.FilePath, e.Line)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return
	}

	// Incremental in-memory FTS update
	if fts, ok := s.ftsIndexes[branch]; ok && fts != nil {
		if oldHash != "" && oldHash != blobHash {
			oldNodes := s.getNodesByBlobLocked(oldHash)
			for _, n := range oldNodes {
				fts.RemoveNode(n.SymbolID)
			}
		}
		if oldHash == "" || oldHash != blobHash {
			newNodes := nodes
			if len(newNodes) == 0 {
				newNodes = s.getNodesByBlobLocked(blobHash)
			}
			for _, n := range newNodes {
				fts.IndexNode(n)
			}
		}
	}

	// Invalidate branchGraph cache on content change
	if oldHash == "" || oldHash != blobHash {
		delete(s.branchGraphs, branch)
	}
}

// RemoveFile removes a file from a branch manifest.
func (s *Store) RemoveFile(branch, filePath string) {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDB(); err != nil {
		return
	}

	var oldHash string
	_ = s.db.QueryRow("SELECT blob_hash FROM file_manifest WHERE branch_name = ? AND file_path = ?", branch, filePath).Scan(&oldHash)

	if oldHash != "" {
		if fts, ok := s.ftsIndexes[branch]; ok && fts != nil {
			oldNodes := s.getNodesByBlobLocked(oldHash)
			for _, n := range oldNodes {
				fts.RemoveNode(n.SymbolID)
			}
		}
		_, _ = s.db.Exec("DELETE FROM file_manifest WHERE branch_name = ? AND file_path = ?", branch, filePath)
		delete(s.branchGraphs, branch)
		_, _ = s.db.Exec("UPDATE branches SET updated_at = ? WHERE name = ?", time.Now().UTC(), branch)
	}
}

// PruneDeletedFiles removes manifest entries for files no longer in the active file list.
func (s *Store) PruneDeletedFiles(branch string, activeFiles map[string]bool) {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDB(); err != nil {
		return
	}

	rows, err := s.db.Query("SELECT file_path, blob_hash FROM file_manifest WHERE branch_name = ?", branch)
	if err != nil {
		return
	}
	defer rows.Close()

	type toDelete struct {
		path string
		hash string
	}
	var deleted []toDelete

	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err == nil {
			if !activeFiles[path] {
				deleted = append(deleted, toDelete{path: path, hash: hash})
			}
		}
	}
	rows.Close()

	if len(deleted) == 0 {
		return
	}

	fts := s.ftsIndexes[branch]
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck

	delStmt, _ := tx.Prepare("DELETE FROM file_manifest WHERE branch_name = ? AND file_path = ?")
	if delStmt != nil {
		defer delStmt.Close()
	}

	for _, item := range deleted {
		if fts != nil {
			oldNodes := s.getNodesByBlobLocked(item.hash)
			for _, n := range oldNodes {
				fts.RemoveNode(n.SymbolID)
			}
		}
		if delStmt != nil {
			_, _ = delStmt.Exec(branch, item.path)
		}
	}

	_, _ = tx.Exec("UPDATE branches SET updated_at = ? WHERE name = ?", time.Now().UTC(), branch)
	_ = tx.Commit()

	delete(s.branchGraphs, branch)
}

func (s *Store) getNodesByBlobLocked(blobHash string) []Node {
	rows, err := s.db.Query(`SELECT symbol_id, kind, name, file_path, start_line, end_line, signature, docstring, exported
		FROM nodes WHERE blob_hash = ?`, blobHash)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		n.BlobHash = blobHash
		if err := rows.Scan(&n.SymbolID, &n.Kind, &n.Name, &n.FilePath, &n.StartLine, &n.EndLine, &n.Signature, &n.Docstring, &n.Exported); err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (s *Store) getBranchNodesLocked(branch string) []Node {
	if err := s.ensureDB(); err != nil {
		return nil
	}

	query := `SELECT n.blob_hash, n.symbol_id, n.kind, n.name, n.file_path, n.start_line, n.end_line, n.signature, n.docstring, n.exported
		FROM nodes n
		JOIN file_manifest m ON n.blob_hash = m.blob_hash
		WHERE m.branch_name = ?
		ORDER BY n.file_path, n.start_line`

	rows, err := s.db.Query(query, branch)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.BlobHash, &n.SymbolID, &n.Kind, &n.Name, &n.FilePath, &n.StartLine, &n.EndLine, &n.Signature, &n.Docstring, &n.Exported); err == nil {
			results = append(results, n)
		}
	}
	return results
}

func (s *Store) getBranchEdgesLocked(branch string) []Edge {
	if err := s.ensureDB(); err != nil {
		return nil
	}

	query := `SELECT e.blob_hash, e.source_symbol, e.target_symbol, e.relation, e.file_path, e.line
		FROM edges e
		JOIN file_manifest m ON e.blob_hash = m.blob_hash
		WHERE m.branch_name = ?`

	rows, err := s.db.Query(query, branch)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.BlobHash, &e.SourceSymbol, &e.TargetSymbol, &e.Relation, &e.FilePath, &e.Line); err == nil {
			results = append(results, e)
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

// GetBranchNodes returns all indexed AST nodes on the given branch.
func (s *Store) GetBranchNodes(branch string) []Node {
	if branch == "" {
		branch = s.ActiveBranch()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getBranchNodesLocked(branch)
}

// GetBranchEdges returns all relationship edges on the given branch.
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

// ExportDuckDBSchema returns the standard SQL DDL schema.
func (s *Store) ExportDuckDBSchema() string {
	return `
-- Code Graph Relational Schema
CREATE TABLE IF NOT EXISTS branches (
    name VARCHAR PRIMARY KEY,
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
    PRIMARY KEY (blob_hash, source_symbol, target_symbol, relation, line)
);
`
}
