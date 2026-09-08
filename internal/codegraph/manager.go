package codegraph

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// Manager coordinates code graph stores and indexers for multiple projects and workspaces.
type Manager struct {
	mu           sync.RWMutex
	knowledgeDir string // e.g. ~/.divmora/localharness/knowledge/
	stores       map[string]*Store
	indexers     map[string]*Indexer
	wsToProject  map[string]string // wsPath -> projectUUID
}

// NewManager creates a CodeGraph Manager rooted in the given knowledge base directory.
func NewManager(knowledgeDir string) *Manager {
	return &Manager{
		knowledgeDir: knowledgeDir,
		stores:       make(map[string]*Store),
		indexers:     make(map[string]*Indexer),
		wsToProject:  make(map[string]string),
	}
}

// RegisterWorkspace associates a workspace path with a project UUID.
func (m *Manager) RegisterWorkspace(wsPath, projectUUID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanWS := filepath.Clean(wsPath)
	m.wsToProject[cleanWS] = projectUUID
}

// GetStore returns or loads the Store for the given project UUID.
func (m *Manager) GetStore(projectUUID string) (*Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if store, ok := m.stores[projectUUID]; ok {
		return store, nil
	}

	dbPath := filepath.Join(m.knowledgeDir, projectUUID, "codegraph.duckdb")
	store := NewStore(dbPath)
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("load codegraph store for project %s: %w", projectUUID, err)
	}

	m.stores[projectUUID] = store
	return store, nil
}

// GetIndexer returns or creates an Indexer for the given project and workspace.
func (m *Manager) GetIndexer(projectUUID, wsPath string) (*Indexer, error) {
	store, err := m.GetStore(projectUUID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cleanWS := filepath.Clean(wsPath)
	key := fmt.Sprintf("%s:%s", projectUUID, cleanWS)
	if idx, ok := m.indexers[key]; ok {
		return idx, nil
	}

	indexer := NewIndexer(cleanWS, store)
	m.indexers[key] = indexer
	m.wsToProject[cleanWS] = projectUUID
	return indexer, nil
}

// IndexWorkspace triggers indexing for a project workspace.
func (m *Manager) IndexWorkspace(ctx context.Context, projectUUID, wsPath, branch string) (*IndexStats, error) {
	idx, err := m.GetIndexer(projectUUID, wsPath)
	if err != nil {
		return nil, err
	}
	return idx.IndexWorkspace(ctx, branch)
}

// UpdateFile finds the associated indexer for a workspace and incrementally updates the file.
func (m *Manager) UpdateFile(ctx context.Context, wsPath, relPath string) error {
	m.mu.RLock()
	cleanWS := filepath.Clean(wsPath)
	projectUUID := m.wsToProject[cleanWS]
	m.mu.RUnlock()

	if projectUUID == "" {
		return nil // Not registered
	}

	idx, err := m.GetIndexer(projectUUID, cleanWS)
	if err != nil {
		return err
	}

	return idx.UpdateFile(ctx, relPath, "")
}

// RemoveFile finds the associated indexer for a workspace and removes the file from the graph.
func (m *Manager) RemoveFile(ctx context.Context, wsPath, relPath string) error {
	m.mu.RLock()
	cleanWS := filepath.Clean(wsPath)
	projectUUID := m.wsToProject[cleanWS]
	m.mu.RUnlock()

	if projectUUID == "" {
		return nil // Not registered
	}

	idx, err := m.GetIndexer(projectUUID, cleanWS)
	if err != nil {
		return err
	}

	return idx.RemoveFile(ctx, relPath, "")
}
