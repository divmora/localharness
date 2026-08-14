// Package workspace provides path validation and restriction for the agent.
// It ensures file operations stay within configured workspace directories.
package workspace

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/divmora/localharness/internal/errors"
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
			return nil, errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
				"invalid workspace path").
				WithContext("path", d).
				WithContext("component", "workspace")
		}
		// Verify directory exists
		info, err := os.Stat(abs)
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrCodeFileNotFound,
				"workspace directory not found").
				WithContext("path", abs).
				WithContext("component", "workspace")
		}
		if !info.IsDir() {
			return nil, errors.New(errors.ErrCodeWorkspaceValidation,
				"workspace path is not a directory").
				WithContext("path", abs).
				WithContext("component", "workspace")
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
		return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
			"invalid allowed path").
			WithContext("path", path).
			WithContext("component", "workspace")
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

	return "", errors.New(errors.ErrCodePathTraversal,
		"path is outside all configured workspaces").
		WithContext("path", path).
		WithContext("resolved_path", resolved).
		WithContext("component", "workspace")
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
