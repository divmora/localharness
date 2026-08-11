// Package workspace provides path validation and restriction for the agent.
// It ensures file operations stay within configured workspace directories.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager validates file paths against configured workspace directories.
type Manager struct {
	workspaces   []string // Absolute paths of allowed workspace directories
	allowedPaths []string // Additional absolute paths allowed beyond workspaces (e.g., brain dir)
}

// NewManager creates a workspace manager from a list of directories.
func NewManager(dirs []string) (*Manager, error) {
	m := &Manager{}
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, fmt.Errorf("invalid workspace path %q: %w", d, err)
		}
		// Verify directory exists
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("workspace %q: %w", abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("workspace %q is not a directory", abs)
		}
		m.workspaces = append(m.workspaces, abs)
	}
	return m, nil
}

// AddAllowedPath registers an additional directory that ValidatePath will accept.
// This does not appear in Workspaces() — it is for internal paths like the
// brain/artifacts directory that need write access but are not user workspaces.
// The directory does not need to exist yet (it will be created on first artifact write).
func (m *Manager) AddAllowedPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid allowed path %q: %w", path, err)
	}
	m.allowedPaths = append(m.allowedPaths, abs)
	return nil
}

// ValidatePath checks if a path is within any configured workspace or allowed path.
// Relative paths are resolved against the first configured workspace.
// Returns the cleaned absolute path if valid, or an error if not.
func (m *Manager) ValidatePath(path string) (string, error) {
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
				return "", fmt.Errorf("invalid path %q: %w", path, err)
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

	for _, ws := range m.workspaces {
		if isSubPath(ws, resolved) {
			return abs, nil
		}
	}

	for _, ap := range m.allowedPaths {
		if isSubPath(ap, resolved) {
			return abs, nil
		}
	}

	return "", fmt.Errorf("path %q is outside all configured workspaces", path)
}

// isSubPath checks if child is within (or equal to) parent directory.
func isSubPath(parent, child string) bool {
	// Ensure parent ends with separator for prefix check
	parentWithSep := parent
	if !strings.HasSuffix(parentWithSep, string(filepath.Separator)) {
		parentWithSep += string(filepath.Separator)
	}
	return child == parent || strings.HasPrefix(child, parentWithSep)
}

// Workspaces returns the list of configured workspace directories.
func (m *Manager) Workspaces() []string {
	return m.workspaces
}
