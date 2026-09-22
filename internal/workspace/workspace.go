// Package workspace provides path validation and restriction for the agent.
// It ensures file operations stay within configured workspace directories.
package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
)

// AccessMode aliases the proto AccessMode enum for internal workspace usage.
type AccessMode = pb.AccessMode

const (
	AccessModeWorkspace    = pb.AccessMode_ACCESS_MODE_WORKSPACE
	AccessModeSystem       = pb.AccessMode_ACCESS_MODE_SYSTEM
	AccessModeUnrestricted = pb.AccessMode_ACCESS_MODE_UNRESTRICTED
)

// PathPolicy contains metadata about a validated path.
type PathPolicy struct {
	CleanPath   string
	IsSensitive bool
	InWorkspace bool
}

// Manager validates file paths against configured workspace directories and access modes.
type Manager struct {
	mu                   sync.RWMutex
	accessMode           pb.AccessMode
	workspaces           []string // Absolute paths of allowed workspace directories
	resolvedWorkspaces   []string // Pre-resolved canonical symlink targets of workspaces
	allowedPaths         []string // Additional absolute paths allowed beyond workspaces (e.g., brain dir)
	resolvedAllowedPaths []string // Pre-resolved canonical symlink targets of allowed paths
}

// NewManager creates a workspace manager from a list of directories with default AccessModeWorkspace.
func NewManager(dirs []string) (*Manager, error) {
	return NewManagerWithAccessMode(dirs, pb.AccessMode_ACCESS_MODE_WORKSPACE)
}

// NewManagerWithAccessMode creates a workspace manager with an explicit access mode.
func NewManagerWithAccessMode(dirs []string, mode pb.AccessMode) (*Manager, error) {
	m := &Manager{accessMode: mode}
	for _, d := range dirs {
		if err := m.AddWorkspace(d); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// AccessMode returns the current access mode.
func (m *Manager) AccessMode() pb.AccessMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accessMode
}

// SetAccessMode updates the access mode for this workspace manager.
func (m *Manager) SetAccessMode(mode pb.AccessMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accessMode = mode
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

// IsSensitivePath checks if a path targets sensitive host files, credentials,
// authentication keys, or system directories that warrant supervision.
func IsSensitivePath(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	realPath, err := filepath.EvalSymlinks(clean)
	if err != nil {
		realPath = clean
	}

	// 1. Home-relative sensitive directories and files
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		homeClean := filepath.Clean(home)
		realHome, _ := filepath.EvalSymlinks(homeClean)

		sensitiveHomeItems := []string{
			filepath.Join(homeClean, ".ssh"),
			filepath.Join(homeClean, ".aws"),
			filepath.Join(homeClean, ".gnupg"),
			filepath.Join(homeClean, ".gpg"),
			filepath.Join(homeClean, ".kube"),
			filepath.Join(homeClean, ".azure"),
			filepath.Join(homeClean, ".config", "gcloud"),
			filepath.Join(homeClean, ".docker"),
			filepath.Join(homeClean, ".netrc"),
			filepath.Join(homeClean, ".git-credentials"),
			filepath.Join(homeClean, ".bash_history"),
			filepath.Join(homeClean, ".zsh_history"),
			filepath.Join(homeClean, ".sh_history"),
			filepath.Join(homeClean, ".bashrc"),
			filepath.Join(homeClean, ".zshrc"),
			filepath.Join(homeClean, ".bash_profile"),
			filepath.Join(homeClean, ".profile"),
			filepath.Join(homeClean, ".zprofile"),
		}

		for _, item := range sensitiveHomeItems {
			if isSubPath(item, clean) || isSubPath(item, realPath) {
				return true
			}
			if realHome != "" {
				realItem, _ := filepath.EvalSymlinks(item)
				if realItem != "" && (isSubPath(realItem, clean) || isSubPath(realItem, realPath)) {
					return true
				}
			}
		}
	}

	// 2. System directories
	systemPrefixes := []string{
		"/etc",
		"/private/etc",
		"/boot",
		"/sys",
		"/proc",
		"/dev",
		"/var/root",
	}
	if runtime.GOOS == "windows" {
		systemPrefixes = []string{
			`C:\Windows`,
			`C:\ProgramData`,
		}
	}
	for _, sp := range systemPrefixes {
		if isSubPath(sp, clean) || isSubPath(sp, realPath) {
			return true
		}
	}

	// 3. Sensitive file names and extensions
	base := strings.ToLower(filepath.Base(clean))
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") ||
		strings.HasPrefix(base, "id_ecdsa") || strings.HasPrefix(base, "id_dsa") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(clean))
	if ext == ".key" || ext == ".pem" || ext == ".pkcs12" || ext == ".pfx" {
		return true
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "credentials.json" || base == "service-account.json" {
		return true
	}

	return false
}

// IsSensitivePath checks if a path targets sensitive host files or system directories.
func (m *Manager) IsSensitivePath(path string) bool {
	return IsSensitivePath(path)
}

// ValidatePathWithPolicy evaluates a path against the current access mode, returning
// detailed policy metadata including cleanliness, sensitivity, and workspace membership.
func (m *Manager) ValidatePathWithPolicy(path string) (PathPolicy, error) {
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
				return PathPolicy{}, errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
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

	inWorkspace := false
	for i, ws := range m.workspaces {
		resolvedWS := m.resolvedWorkspaces[i]
		if isSubPath(ws, abs) || isSubPath(ws, resolved) || isSubPath(resolvedWS, resolved) || isSubPath(resolvedWS, abs) {
			inWorkspace = true
			break
		}
	}

	if !inWorkspace {
		for i, ap := range m.allowedPaths {
			resolvedAP := m.resolvedAllowedPaths[i]
			if isSubPath(ap, abs) || isSubPath(ap, resolved) || isSubPath(resolvedAP, resolved) || isSubPath(resolvedAP, abs) {
				inWorkspace = true
				break
			}
		}
	}

	isSensitive := IsSensitivePath(abs) || IsSensitivePath(resolved)

	switch m.accessMode {
	case pb.AccessMode_ACCESS_MODE_UNRESTRICTED:
		// Full autonomous host access
		return PathPolicy{
			CleanPath:   abs,
			IsSensitive: false,
			InWorkspace: inWorkspace,
		}, nil

	case pb.AccessMode_ACCESS_MODE_SYSTEM:
		// Supervised host-wide access
		return PathPolicy{
			CleanPath:   abs,
			IsSensitive: isSensitive,
			InWorkspace: inWorkspace,
		}, nil

	default: // ACCESS_MODE_WORKSPACE
		if !inWorkspace {
			return PathPolicy{}, errors.New(errors.ErrCodePathTraversal,
				"path is outside all configured workspaces").
				WithContext("path", path).
				WithContext("resolved_path", resolved).
				WithContext("component", "workspace")
		}
		return PathPolicy{
			CleanPath:   abs,
			IsSensitive: isSensitive,
			InWorkspace: true,
		}, nil
	}
}

// ValidatePath checks if a path is within any configured workspace or allowed path.
// Under ACCESS_MODE_WORKSPACE, paths outside workspaces are rejected.
// Under ACCESS_MODE_SYSTEM and ACCESS_MODE_UNRESTRICTED, valid host paths are permitted.
// Returns the cleaned absolute path if valid, or an error if not.
func (m *Manager) ValidatePath(path string) (string, error) {
	policy, err := m.ValidatePathWithPolicy(path)
	if err != nil {
		return "", err
	}
	return policy.CleanPath, nil
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
