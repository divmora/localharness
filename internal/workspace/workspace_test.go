package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	// Create temp workspace directories
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	tests := []struct {
		name    string
		dirs    []string
		wantErr bool
	}{
		{
			name:    "single valid directory",
			dirs:    []string{dir1},
			wantErr: false,
		},
		{
			name:    "multiple valid directories",
			dirs:    []string{dir1, dir2},
			wantErr: false,
		},
		{
			name:    "nonexistent directory",
			dirs:    []string{"/nonexistent/path/that/does/not/exist"},
			wantErr: true,
		},
		{
			name:    "empty dirs list",
			dirs:    []string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.dirs)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && mgr == nil {
				t.Error("NewManager() returned nil manager without error")
			}
		})
	}
}

func TestNewManagerFileNotDirectory(t *testing.T) {
	// Create a temp file (not a directory)
	tmpFile := filepath.Join(t.TempDir(), "notadir.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := NewManager([]string{tmpFile})
	if err == nil {
		t.Error("NewManager() should error for non-directory path")
	}
}

func TestValidatePath(t *testing.T) {
	wsDir := t.TempDir()

	// Create a file inside the workspace for tests that need it
	testFile := filepath.Join(wsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory
	subDir := filepath.Join(wsDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "absolute path inside workspace",
			path:    testFile,
			wantErr: false,
		},
		{
			name:    "subdirectory inside workspace",
			path:    subDir,
			wantErr: false,
		},
		{
			name:    "workspace root itself",
			path:    wsDir,
			wantErr: false,
		},
		{
			name:    "absolute path outside workspace",
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "relative path resolved against workspace",
			path:    "test.txt",
			wantErr: false,
		},
		{
			name:    "path traversal attempt",
			path:    filepath.Join(wsDir, "..", "escape"),
			wantErr: true,
		},
		{
			name:    "nonexistent file inside workspace (e.g. create_file target)",
			path:    filepath.Join(wsDir, "newfile.txt"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mgr.ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == "" {
				t.Errorf("ValidatePath(%q) returned empty path without error", tt.path)
			}
		})
	}
}

func TestValidatePathMultipleWorkspaces(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()

	// Create files in each workspace
	file1 := filepath.Join(ws1, "file1.txt")
	file2 := filepath.Join(ws2, "file2.txt")
	os.WriteFile(file1, []byte("1"), 0644)
	os.WriteFile(file2, []byte("2"), 0644)

	mgr, err := NewManager([]string{ws1, ws2})
	if err != nil {
		t.Fatal(err)
	}

	// Both files should be valid
	if _, err := mgr.ValidatePath(file1); err != nil {
		t.Errorf("file in ws1 should be valid: %v", err)
	}
	if _, err := mgr.ValidatePath(file2); err != nil {
		t.Errorf("file in ws2 should be valid: %v", err)
	}
}

func TestValidatePathNoWorkspaces(t *testing.T) {
	mgr, err := NewManager([]string{})
	if err != nil {
		t.Fatal(err)
	}

	// With no workspaces, any absolute path should fail
	_, err = mgr.ValidatePath("/some/random/path")
	if err == nil {
		t.Error("ValidatePath should error when no workspaces configured and path doesn't exist")
	}
}

func TestWorkspaces(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()

	mgr, err := NewManager([]string{ws1, ws2})
	if err != nil {
		t.Fatal(err)
	}

	workspaces := mgr.Workspaces()
	if len(workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(workspaces))
	}
}

func TestIsSubPath(t *testing.T) {
	tests := []struct {
		name   string
		parent string
		child  string
		want   bool
	}{
		{
			name:   "exact match",
			parent: "/home/user/project",
			child:  "/home/user/project",
			want:   true,
		},
		{
			name:   "child is inside parent",
			parent: "/home/user/project",
			child:  "/home/user/project/src/main.go",
			want:   true,
		},
		{
			name:   "child is outside parent",
			parent: "/home/user/project",
			child:  "/home/user/other/file.go",
			want:   false,
		},
		{
			name:   "parent prefix trick - similar dir name",
			parent: "/home/user/project",
			child:  "/home/user/project-evil/attack.go",
			want:   false,
		},
		{
			name:   "parent is root",
			parent: "/",
			child:  "/any/path",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSubPath(tt.parent, tt.child)
			if got != tt.want {
				t.Errorf("isSubPath(%q, %q) = %v, want %v", tt.parent, tt.child, got, tt.want)
			}
		})
	}
}

func TestAddAllowedPath(t *testing.T) {
	wsDir := t.TempDir()
	brainDir := t.TempDir()

	mgr, err := NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	// Before adding allowed path, brain dir should be denied
	brainFile := filepath.Join(brainDir, "implementation_plan.md")
	os.WriteFile(brainFile, []byte("plan"), 0644)

	if _, err := mgr.ValidatePath(brainFile); err == nil {
		t.Error("ValidatePath should reject brain dir before AddAllowedPath")
	}

	// Add brain dir as allowed
	if err := mgr.AddAllowedPath(brainDir); err != nil {
		t.Fatalf("AddAllowedPath failed: %v", err)
	}

	// Now brain dir should be accepted
	if _, err := mgr.ValidatePath(brainFile); err != nil {
		t.Errorf("ValidatePath should accept brain dir after AddAllowedPath: %v", err)
	}

	// Subdirectory of brain dir should also be accepted
	scratchFile := filepath.Join(brainDir, "scratch", "debug.sh")
	if _, err := mgr.ValidatePath(scratchFile); err != nil {
		t.Errorf("ValidatePath should accept subdirectory of allowed path: %v", err)
	}

	// Workspace files should still work
	wsFile := filepath.Join(wsDir, "main.go")
	os.WriteFile(wsFile, []byte("package main"), 0644)
	if _, err := mgr.ValidatePath(wsFile); err != nil {
		t.Errorf("ValidatePath should still accept workspace files: %v", err)
	}

	// Paths outside both should still be denied
	if _, err := mgr.ValidatePath("/etc/passwd"); err == nil {
		t.Error("ValidatePath should reject paths outside both workspaces and allowed paths")
	}

	// Workspaces() should NOT include allowed paths
	workspaces := mgr.Workspaces()
	for _, ws := range workspaces {
		if ws == brainDir {
			t.Error("Workspaces() should not include allowed paths")
		}
	}
}

func TestAddAllowedPathNonExistentDir(t *testing.T) {
	wsDir := t.TempDir()
	mgr, err := NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	// AddAllowedPath should succeed even for non-existent directories
	// (brain dir may not exist yet — it's created on first artifact write)
	nonExistent := filepath.Join(t.TempDir(), "future", "brain", "conv-123")
	if err := mgr.AddAllowedPath(nonExistent); err != nil {
		t.Errorf("AddAllowedPath should not error for non-existent dir: %v", err)
	}

	// ValidatePath should accept files under the non-existent allowed path
	futureFile := filepath.Join(nonExistent, "plan.md")
	if _, err := mgr.ValidatePath(futureFile); err != nil {
		t.Errorf("ValidatePath should accept files under non-existent allowed path: %v", err)
	}
}

// TestScopedAllowedPaths_OnlyBrainAndKnowledge verifies that when only brain/
// and knowledge/ are added as allowed paths (not the entire appDataDir), other
// subdirectories like conversations/, plugins/, skills/ are correctly denied.
func TestScopedAllowedPaths_OnlyBrainAndKnowledge(t *testing.T) {
	wsDir := t.TempDir()
	appDataDir := t.TempDir()

	// Create subdirectories to simulate real layout
	brainDir := filepath.Join(appDataDir, "brain")
	knowledgeDir := filepath.Join(appDataDir, "knowledge")
	convDir := filepath.Join(appDataDir, "conversations")
	pluginsDir := filepath.Join(appDataDir, "plugins")
	skillsDir := filepath.Join(appDataDir, "skills")

	for _, d := range []string{brainDir, knowledgeDir, convDir, pluginsDir, skillsDir} {
		os.MkdirAll(d, 0755)
	}

	mgr, err := NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	// Add only brain/ and knowledge/ (matching session.go behavior)
	mgr.AddAllowedPath(brainDir)
	mgr.AddAllowedPath(knowledgeDir)

	// ✅ Should accept brain paths
	brainFile := filepath.Join(brainDir, "conv-123", "plan.md")
	if _, err := mgr.ValidatePath(brainFile); err != nil {
		t.Errorf("brain path should be accepted: %v", err)
	}

	// ✅ Should accept knowledge paths
	kiFile := filepath.Join(knowledgeDir, "proj-456", "metadata.json")
	if _, err := mgr.ValidatePath(kiFile); err != nil {
		t.Errorf("knowledge path should be accepted: %v", err)
	}

	// ❌ Should reject conversations
	convFile := filepath.Join(convDir, "abc-123.pb")
	os.WriteFile(convFile, []byte("state"), 0644)
	if _, err := mgr.ValidatePath(convFile); err == nil {
		t.Error("conversations/ path should be REJECTED — agent must not modify conversation state")
	}

	// ❌ Should reject plugins
	pluginFile := filepath.Join(pluginsDir, "evil-plugin", "SKILL.md")
	if _, err := mgr.ValidatePath(pluginFile); err == nil {
		t.Error("plugins/ path should be REJECTED — agent must not modify user plugins")
	}

	// ❌ Should reject skills
	skillFile := filepath.Join(skillsDir, "evil-skill", "SKILL.md")
	if _, err := mgr.ValidatePath(skillFile); err == nil {
		t.Error("skills/ path should be REJECTED — agent must not modify user skills")
	}

	// ❌ Should reject projects.json (direct child of appDataDir)
	projectsFile := filepath.Join(appDataDir, "projects.json")
	os.WriteFile(projectsFile, []byte("{}"), 0644)
	if _, err := mgr.ValidatePath(projectsFile); err == nil {
		t.Error("projects.json should be REJECTED — agent must not modify project registry")
	}

	// ✅ Workspace files should still work
	wsFile := filepath.Join(wsDir, "main.go")
	os.WriteFile(wsFile, []byte("package main"), 0644)
	if _, err := mgr.ValidatePath(wsFile); err != nil {
		t.Errorf("workspace file should still be accepted: %v", err)
	}
}

