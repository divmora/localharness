package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistry_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)
	if err := pr.Load(); err != nil {
		t.Fatalf("Load should succeed on missing file: %v", err)
	}
	if len(pr.List()) != 0 {
		t.Error("expected no projects")
	}
}

func TestProjectRegistry_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	// Create a project manually
	p, err := pr.FindOrCreate([]string{"/home/test/project-a"})
	if err != nil {
		t.Fatalf("FindOrCreate failed: %v", err)
	}
	if p.Name != "project-a" {
		t.Errorf("expected name 'project-a', got %q", p.Name)
	}

	// Reload from disk
	pr2 := NewProjectRegistry(dir)
	if err := pr2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	projects := pr2.List()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].ID != p.ID {
		t.Errorf("expected ID %q, got %q", p.ID, projects[0].ID)
	}
	if projects[0].Name != "project-a" {
		t.Errorf("expected name 'project-a', got %q", projects[0].Name)
	}
}

func TestProjectRegistry_FindByWorkspace(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	p, _ := pr.FindOrCreate([]string{"/home/test/my-project"})

	found := pr.FindByWorkspace("/home/test/my-project")
	if found == nil {
		t.Fatal("expected to find project by workspace")
	}
	if found.ID != p.ID {
		t.Errorf("wrong project: expected %q, got %q", p.ID, found.ID)
	}
}

func TestProjectRegistry_FindByWorkspace_NotFound(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	pr.FindOrCreate([]string{"/home/test/my-project"})

	found := pr.FindByWorkspace("/home/test/other-project")
	if found != nil {
		t.Error("should not find unregistered workspace")
	}
}

func TestProjectRegistry_FindOrCreate(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	// First call creates
	p1, err := pr.FindOrCreate([]string{"/home/test/project"})
	if err != nil {
		t.Fatalf("first FindOrCreate failed: %v", err)
	}
	if p1.ID == "" {
		t.Error("expected non-empty ID")
	}

	// Second call returns existing
	p2, err := pr.FindOrCreate([]string{"/home/test/project"})
	if err != nil {
		t.Fatalf("second FindOrCreate failed: %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("expected same ID, got %q vs %q", p1.ID, p2.ID)
	}
}

func TestProjectRegistry_FindOrCreate_MultipleWorkspaces(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	// Create with two workspaces
	p1, _ := pr.FindOrCreate([]string{"/home/test/ws1", "/home/test/ws2"})

	// Find by second workspace
	p2, _ := pr.FindOrCreate([]string{"/home/test/ws2"})
	if p2.ID != p1.ID {
		t.Error("should find same project by any workspace")
	}
}

func TestProjectRegistry_AddWorkspace(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	p, _ := pr.FindOrCreate([]string{"/home/test/ws1"})

	// Add another workspace
	if err := pr.AddWorkspace(p.ID, "/home/test/ws2"); err != nil {
		t.Fatalf("AddWorkspace failed: %v", err)
	}

	// Should find by new workspace
	found := pr.FindByWorkspace("/home/test/ws2")
	if found == nil {
		t.Fatal("expected to find project by new workspace")
	}
	if found.ID != p.ID {
		t.Error("wrong project ID")
	}

	// Idempotent — adding same workspace again should not error
	if err := pr.AddWorkspace(p.ID, "/home/test/ws2"); err != nil {
		t.Fatalf("duplicate AddWorkspace should not error: %v", err)
	}
}

func TestProjectRegistry_PathNormalization(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	pr.FindOrCreate([]string{"/home/test/project/"})

	// Should find with different path formatting
	if pr.FindByWorkspace("/home/test/project") == nil {
		t.Error("should find with trailing slash removed")
	}
	if pr.FindByWorkspace("/home/test/project/../project") == nil {
		t.Error("should find with .. normalized")
	}
}

func TestProjectRegistry_Save_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	pr := NewProjectRegistry(dir)

	pr.FindOrCreate([]string{"/home/test/project"})

	// Verify the file exists and is valid JSON
	data, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("projects.json should exist: %v", err)
	}

	var pf struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatalf("projects.json should be valid JSON: %v", err)
	}
	if len(pf.Projects) != 1 {
		t.Errorf("expected 1 project in file, got %d", len(pf.Projects))
	}

	// Temp file should not exist
	if _, err := os.Stat(filepath.Join(dir, "projects.json.tmp")); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up after atomic write")
	}
}
