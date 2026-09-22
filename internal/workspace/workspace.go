// Package workspace provides path validation and restriction for the agent.
// It ensures file operations stay within configured workspace directories.
package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/divmora/localharness/internal/errors"
)

// Manager validates file paths against configured workspace directories.
type Manager struct {
	mu                   sync.RWMutex
	workspaces           []string // Absolute paths of allowed workspace directories
	resolvedWorkspaces   []string // Pre-resolved canonical symlink targets of workspaces
	allowedPaths         []string // Additional absolute paths allowed beyond workspaces (e.g., brain dir)
	resolvedAllowedPaths []string // Pre-resolved canonical symlink targets of allowed paths
}

// NewManager creates a workspace manager from a list of directories.
func NewManager(dirs []string) (*Manager, error) {
	m := &Manager{}
	for _, d := range dirs {
		if err := m.AddWorkspace(d); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// AddWorkspace dynamically adds a workspace directory to the manager.
func (m *Manager) AddWorkspace(d string) error {
	abs, err := filepath.Abs(d)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
			"invalid workspace path").
			WithContext("path", d).
			WithContext("component", "workspace")
	}
	// Verify directory exists
	info, err := os.Stat(abs)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeFileNotFound,
			"workspace directory not found").
			WithContext("path", abs).
			WithContext("component", "workspace")
	}
	if !info.IsDir() {
		return errors.New(errors.ErrCodeWorkspaceValidation,
			"workspace path is not a directory").
			WithContext("path", abs).
			WithContext("component", "workspace")
	}

	resolvedWS, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolvedWS = abs
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ws := range m.workspaces {
		if ws == abs {
			return nil // already registered
		}
	}
	m.workspaces = append(m.workspaces, abs)
	m.resolvedWorkspaces = append(m.resolvedWorkspaces, resolvedWS)
	return nil
}

// AddAllowedPath registers an additional directory that ValidatePath will accept.
// This does not appear in Workspaces() — it is for internal paths like the
// brain/artifacts directory that need write access but are not user workspaces.
// The directory does not need to exist yet (it will be created on first artifact write).
func (m *Manager) AddAllowedPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
			"invalid allowed path").
			WithContext("path", path).
			WithContext("component", "workspace")
	}

	resolvedAP, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolvedAP = abs
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ap := range m.allowedPaths {
		if ap == abs {
			return nil
		}
	}
	m.allowedPaths = append(m.allowedPaths, abs)
	m.resolvedAllowedPaths = append(m.resolvedAllowedPaths, resolvedAP)
	return nil
}

// ValidatePath checks if a path is within any configured workspace or allowed path.
// Relative paths are resolved against the first configured workspace.
// Returns the cleaned absolute path if valid, or an error if not.
func (m *Manager) ValidatePath(path string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		// Resolve relative paths against first workspace
		if len(m.workspaces) > 0 {
			abs = filepath.Join(m.workspaces[0], path)
		} else {
			var err error
			abs, err = filepath.Abs(path)
			if err != nil {
				return "", errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
					"invalid path resolution").
					WithContext("path", path).
					WithContext("component", "workspace")
			}
		}
	}

	// Resolve symlinks to prevent escapes
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File might not exist yet (e.g., create_file). Check parent.
		dir := filepath.Dir(abs)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			// Parent doesn't exist either — check the raw path
			resolved = abs
		} else {
			resolved = filepath.Join(resolvedDir, filepath.Base(abs))
		}
	}

	for i, ws := range m.workspaces {
		resolvedWS := m.resolvedWorkspaces[i]
		if isSubPath(ws, abs) || isSubPath(ws, resolved) || isSubPath(resolvedWS, resolved) || isSubPath(resolvedWS, abs) {
			return abs, nil
		}
	}

	for i, ap := range m.allowedPaths {
		resolvedAP := m.resolvedAllowedPaths[i]
		if isSubPath(ap, abs) || isSubPath(ap, resolved) || isSubPath(resolvedAP, resolved) || isSubPath(resolvedAP, abs) {
			return abs, nil
		}
	}

	return "", errors.New(errors.ErrCodePathTraversal,
		"path is outside all configured workspaces").
		WithContext("path", path).
		WithContext("resolved_path", resolved).
		WithContext("component", "workspace")
}

// isSubPath checks if child is within (or equal to) parent directory without string allocations.
func isSubPath(parent, child string) bool {
	if len(parent) == 0 || len(child) == 0 {
		return false
	}
	if child == parent {
		return true
	}
	if strings.HasPrefix(child, parent) {
		sep := byte(filepath.Separator)
		if parent[len(parent)-1] == sep || parent[len(parent)-1] == '/' {
			return true
		}
		if len(child) > len(parent) && (child[len(parent)] == sep || child[len(parent)] == '/') {
			return true
		}
	}
	return false
}

// Workspaces returns the list of configured workspace directories.
func (m *Manager) Workspaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.workspaces))
	copy(out, m.workspaces)
	return out
}
