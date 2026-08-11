package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// KnowledgeItem represents a single knowledge item within a project.
// KIs are directories containing a metadata.json and an artifacts/ subdirectory.
type KnowledgeItem struct {
	Name       string    `json:"name"`       // Directory name (unique within project)
	Summary    string    `json:"summary"`    // Short description for per-message injection
	References []string  `json:"references"` // Source files this KI was derived from
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Runtime fields — populated by Load/WriteArtifact, not persisted in metadata.json.
	Artifacts []string `json:"-"` // Filenames within artifacts/ dir
	BasePath  string   `json:"-"` // Absolute path to KI directory
	Stale     bool     `json:"-"` // True if referenced files changed since UpdatedAt
}

// KnowledgeStore manages Knowledge Items for a single project.
// Thread-safe for concurrent access.
type KnowledgeStore struct {
	mu      sync.RWMutex
	baseDir string                   // <appDataDir>/knowledge/<project-uuid>/
	items   map[string]*KnowledgeItem // keyed by KI name
}

// kiNameRegex validates KI names: lowercase letters, digits, hyphens.
var kiNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// NewKnowledgeStore creates a store rooted at the given base directory.
// Call Load() to populate from disk.
func NewKnowledgeStore(baseDir string) *KnowledgeStore {
	return &KnowledgeStore{
		baseDir: baseDir,
		items:   make(map[string]*KnowledgeItem),
	}
}

// Load scans the base directory, parsing each subdirectory's metadata.json.
// Directories without valid metadata.json are silently skipped.
func (ks *KnowledgeStore) Load() error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.items = make(map[string]*KnowledgeItem)

	entries, err := os.ReadDir(ks.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No knowledge dir yet — empty store
		}
		return fmt.Errorf("knowledge store: read dir %s: %w", ks.baseDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		kiDir := filepath.Join(ks.baseDir, entry.Name())
		metaPath := filepath.Join(kiDir, "metadata.json")

		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue // Skip dirs without metadata.json
		}

		var ki KnowledgeItem
		if err := json.Unmarshal(data, &ki); err != nil {
			continue // Skip malformed metadata
		}

		ki.BasePath = kiDir
		ki.Artifacts = scanArtifacts(kiDir)
		ks.items[ki.Name] = &ki
	}

	return nil
}

// List returns all KIs sorted by name (for deterministic per-message injection).
func (ks *KnowledgeStore) List() []KnowledgeItem {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	result := make([]KnowledgeItem, 0, len(ks.items))
	for _, ki := range ks.items {
		result = append(result, *ki) // Copy to avoid mutation
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Get returns a single KI by name.
func (ks *KnowledgeStore) Get(name string) (*KnowledgeItem, bool) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	ki, ok := ks.items[name]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *ki
	return &copy, true
}

// WriteArtifact creates or updates a KI and writes an artifact file.
// If the KI doesn't exist, a new directory and metadata.json are created.
// If it exists, metadata (summary, references, updatedAt) is updated.
func (ks *KnowledgeStore) WriteArtifact(kiName, summary, artifactPath, content string, refs []string) error {
	kiName = normalizeKIName(kiName)
	if !kiNameRegex.MatchString(kiName) {
		return fmt.Errorf("knowledge: invalid ki_name %q (use lowercase letters, digits, hyphens)", kiName)
	}
	if artifactPath == "" {
		return fmt.Errorf("knowledge: artifact_path is required")
	}
	// Prevent path traversal
	if strings.Contains(artifactPath, "..") || filepath.IsAbs(artifactPath) {
		return fmt.Errorf("knowledge: artifact_path must be a relative path without '..'")
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	kiDir := filepath.Join(ks.baseDir, kiName)
	artifactsDir := filepath.Join(kiDir, "artifacts")

	// Create directories
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return fmt.Errorf("knowledge: mkdir %s: %w", artifactsDir, err)
	}

	now := time.Now().UTC()

	// Load or create KI metadata
	ki, exists := ks.items[kiName]
	if !exists {
		ki = &KnowledgeItem{
			Name:      kiName,
			CreatedAt: now,
			BasePath:  kiDir,
		}
		ks.items[kiName] = ki
	}

	// Update metadata
	if summary != "" {
		ki.Summary = summary
	}
	if len(refs) > 0 {
		ki.References = refs
	}
	ki.UpdatedAt = now
	ki.Stale = false // Writing clears staleness

	// Write artifact file
	artifactFullPath := filepath.Join(artifactsDir, artifactPath)
	artifactDir := filepath.Dir(artifactFullPath)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return fmt.Errorf("knowledge: mkdir artifact dir %s: %w", artifactDir, err)
	}
	if err := os.WriteFile(artifactFullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("knowledge: write artifact %s: %w", artifactFullPath, err)
	}

	// Refresh artifact list
	ki.Artifacts = scanArtifacts(kiDir)

	// Persist metadata
	return ks.writeMetadata(ki)
}

// ReplaceInArtifact performs search-and-replace within an existing artifact file.
func (ks *KnowledgeStore) ReplaceInArtifact(kiName, artifactPath, target, replacement string) error {
	if target == "" {
		return fmt.Errorf("knowledge: target string is required")
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	ki, ok := ks.items[kiName]
	if !ok {
		return fmt.Errorf("knowledge: KI %q not found", kiName)
	}

	fullPath := filepath.Join(ki.BasePath, "artifacts", artifactPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("knowledge: read artifact %s: %w", fullPath, err)
	}

	content := string(data)
	if !strings.Contains(content, target) {
		return fmt.Errorf("knowledge: target string not found in %s", artifactPath)
	}

	newContent := strings.Replace(content, target, replacement, 1)
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("knowledge: write artifact %s: %w", fullPath, err)
	}

	// Update timestamp
	ki.UpdatedAt = time.Now().UTC()
	ki.Stale = false

	return ks.writeMetadata(ki)
}

// Delete removes an entire KI directory and its metadata.
func (ks *KnowledgeStore) Delete(kiName string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ki, ok := ks.items[kiName]
	if !ok {
		return fmt.Errorf("knowledge: KI %q not found", kiName)
	}

	if err := os.RemoveAll(ki.BasePath); err != nil {
		return fmt.Errorf("knowledge: remove %s: %w", ki.BasePath, err)
	}

	delete(ks.items, kiName)
	return nil
}

// DeleteArtifact removes a single artifact file from a KI.
// If no artifacts remain after deletion, the entire KI is removed.
func (ks *KnowledgeStore) DeleteArtifact(kiName, artifactPath string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ki, ok := ks.items[kiName]
	if !ok {
		return fmt.Errorf("knowledge: KI %q not found", kiName)
	}

	fullPath := filepath.Join(ki.BasePath, "artifacts", artifactPath)
	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("knowledge: remove artifact %s: %w", fullPath, err)
	}

	// Refresh artifact list
	ki.Artifacts = scanArtifacts(ki.BasePath)

	// If no artifacts remain, delete the entire KI
	if len(ki.Artifacts) == 0 {
		if err := os.RemoveAll(ki.BasePath); err != nil {
			return fmt.Errorf("knowledge: remove empty KI %s: %w", ki.BasePath, err)
		}
		delete(ks.items, kiName)
		return nil
	}

	return nil
}

// CheckStaleness compares each KI's references against file mtimes.
// If any referenced file has been modified since the KI's UpdatedAt,
// the KI is marked Stale=true. Missing references are not considered stale.
func (ks *KnowledgeStore) CheckStaleness(workspaceDirs []string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	for _, ki := range ks.items {
		ki.Stale = false
		for _, ref := range ki.References {
			for _, ws := range workspaceDirs {
				absPath := filepath.Join(ws, ref)
				info, err := os.Stat(absPath)
				if err != nil {
					continue // File deleted or inaccessible — not stale, just gone
				}
				if info.ModTime().After(ki.UpdatedAt) {
					ki.Stale = true
					break
				}
			}
			if ki.Stale {
				break
			}
		}
	}
}

// writeMetadata persists a KI's metadata.json (caller must hold write lock).
func (ks *KnowledgeStore) writeMetadata(ki *KnowledgeItem) error {
	metaPath := filepath.Join(ki.BasePath, "metadata.json")
	data, err := json.MarshalIndent(ki, "", "  ")
	if err != nil {
		return fmt.Errorf("knowledge: marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("knowledge: write metadata %s: %w", metaPath, err)
	}
	return nil
}

// scanArtifacts returns a sorted list of filenames in the KI's artifacts/ directory.
func scanArtifacts(kiDir string) []string {
	artifactsDir := filepath.Join(kiDir, "artifacts")
	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// normalizeKIName converts a name to lowercase kebab-case.
func normalizeKIName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	return name
}
