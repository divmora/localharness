package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/divmora/localharness/internal/util"
)

// Project represents a registered project with a stable UUID.
// Projects map workspace paths to a persistent identity so that
// Knowledge Items (KIs) stay associated even if paths change.
type Project struct {
	ID         string    `json:"id"`         // UUID v4
	Name       string    `json:"name"`       // Human-readable (derived from workspace basename)
	Workspaces []string  `json:"workspaces"` // Absolute workspace paths
	CreatedAt  time.Time `json:"created_at"`
}

// projectsFile is the on-disk JSON format for projects.json.
type projectsFile struct {
	Projects []*Project `json:"projects"`
}

// ProjectRegistry manages the projects.json file that maps workspace
// paths to stable project UUIDs. Thread-safe for concurrent access.
type ProjectRegistry struct {
	mu       sync.RWMutex
	filePath string              // <appDataDir>/projects.json
	projects map[string]*Project // keyed by project UUID
}

// NewProjectRegistry creates a project registry backed by projects.json
// in the given appDataDir. Call Load() to populate from disk.
func NewProjectRegistry(appDataDir string) *ProjectRegistry {
	return &ProjectRegistry{
		filePath: filepath.Join(appDataDir, "projects.json"),
		projects: make(map[string]*Project),
	}
}

// Load reads and parses projects.json from disk.
// If the file doesn't exist, the registry starts empty (not an error).
func (pr *ProjectRegistry) Load() error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	data, err := os.ReadFile(pr.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet — empty registry
		}
		return fmt.Errorf("project registry: read %s: %w", pr.filePath, err)
	}

	var pf projectsFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("project registry: parse %s: %w", pr.filePath, err)
	}

	pr.projects = make(map[string]*Project, len(pf.Projects))
	for _, p := range pf.Projects {
		pr.projects[p.ID] = p
	}

	return nil
}

// Save writes projects.json atomically (write to temp, rename).
func (pr *ProjectRegistry) Save() error {
	pr.mu.RLock()
	projects := make([]*Project, 0, len(pr.projects))
	for _, p := range pr.projects {
		projects = append(projects, p)
	}
	pr.mu.RUnlock()

	pf := projectsFile{Projects: projects}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("project registry: marshal: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(pr.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("project registry: mkdir %s: %w", dir, err)
	}

	// Atomic write: temp file + rename
	tmpPath := pr.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("project registry: write temp: %w", err)
	}
	if err := os.Rename(tmpPath, pr.filePath); err != nil {
		os.Remove(tmpPath) // Clean up on failure
		return fmt.Errorf("project registry: rename: %w", err)
	}

	return nil
}

// FindByWorkspace returns the project that contains the given workspace path.
// Returns nil if no project matches. Uses filepath.Clean for normalization.
func (pr *ProjectRegistry) FindByWorkspace(workspacePath string) *Project {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	ws := filepath.Clean(workspacePath)
	for _, p := range pr.projects {
		for _, pw := range p.Workspaces {
			if filepath.Clean(pw) == ws {
				return p
			}
		}
	}
	return nil
}

// FindOrCreate finds a project matching any of the given workspace paths,
// or creates a new one with a UUID. If a new project is created, it is
// auto-named from the first workspace's basename and saved to disk.
func (pr *ProjectRegistry) FindOrCreate(workspacePaths []string) (*Project, error) {
	if len(workspacePaths) == 0 {
		return nil, fmt.Errorf("project registry: no workspace paths provided")
	}

	// Check existing projects first
	for _, ws := range workspacePaths {
		if p := pr.FindByWorkspace(ws); p != nil {
			return p, nil
		}
	}

	// Create new project
	pr.mu.Lock()
	defer pr.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have created it)
	cleanPaths := make([]string, len(workspacePaths))
	for i, ws := range workspacePaths {
		cleanPaths[i] = filepath.Clean(ws)
	}
	for _, p := range pr.projects {
		for _, pw := range p.Workspaces {
			for _, cws := range cleanPaths {
				if filepath.Clean(pw) == cws {
					return p, nil
				}
			}
		}
	}

	project := &Project{
		ID:         util.NewUUID(),
		Name:       filepath.Base(cleanPaths[0]),
		Workspaces: cleanPaths,
		CreatedAt:  time.Now().UTC(),
	}
	pr.projects[project.ID] = project

	// Save to disk (best-effort — don't fail the operation if save fails)
	// Release lock temporarily for I/O by saving inline
	data, err := json.MarshalIndent(projectsFile{Projects: pr.projectList()}, "", "  ")
	if err == nil {
		dir := filepath.Dir(pr.filePath)
		os.MkdirAll(dir, 0755)
		tmpPath := pr.filePath + ".tmp"
		if writeErr := os.WriteFile(tmpPath, data, 0644); writeErr == nil {
			os.Rename(tmpPath, pr.filePath)
		}
	}

	return project, nil
}

// AddWorkspace adds a workspace path to an existing project.
// Returns an error if the project doesn't exist or the workspace is already registered.
func (pr *ProjectRegistry) AddWorkspace(projectID, workspacePath string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	p, ok := pr.projects[projectID]
	if !ok {
		return fmt.Errorf("project registry: project %s not found", projectID)
	}

	ws := filepath.Clean(workspacePath)

	// Check for duplicates
	for _, pw := range p.Workspaces {
		if filepath.Clean(pw) == ws {
			return nil // Already registered — idempotent
		}
	}

	p.Workspaces = append(p.Workspaces, ws)
	return pr.saveLocked()
}

// List returns all registered projects.
func (pr *ProjectRegistry) List() []*Project {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.projectList()
}

// projectList returns a slice of all projects (caller must hold lock).
func (pr *ProjectRegistry) projectList() []*Project {
	result := make([]*Project, 0, len(pr.projects))
	for _, p := range pr.projects {
		result = append(result, p)
	}
	return result
}

// saveLocked writes projects.json (caller must hold write lock).
func (pr *ProjectRegistry) saveLocked() error {
	data, err := json.MarshalIndent(projectsFile{Projects: pr.projectList()}, "", "  ")
	if err != nil {
		return fmt.Errorf("project registry: marshal: %w", err)
	}

	dir := filepath.Dir(pr.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("project registry: mkdir %s: %w", dir, err)
	}

	tmpPath := pr.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("project registry: write temp: %w", err)
	}
	if err := os.Rename(tmpPath, pr.filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("project registry: rename: %w", err)
	}

	return nil
}
