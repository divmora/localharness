package engine

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestKnowledgeStore_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)
	if err := ks.Load(); err != nil {
		t.Fatalf("Load should succeed on empty dir: %v", err)
	}
	if len(ks.List()) != 0 {
		t.Error("expected no items")
	}
}

func TestKnowledgeStore_LoadMissing(t *testing.T) {
	ks := NewKnowledgeStore("/nonexistent/path/knowledge")
	if err := ks.Load(); err != nil {
		t.Fatalf("Load should succeed on missing dir: %v", err)
	}
	if len(ks.List()) != 0 {
		t.Error("expected no items")
	}
}

func TestKnowledgeStore_WriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	err := ks.WriteArtifact("error-patterns", "Error handling patterns", "overview.md", "# Error Patterns\nUse fmt.Errorf with %w.", []string{"internal/engine/engine.go"})
	if err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}

	// Verify in-memory
	items := ks.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "error-patterns" {
		t.Errorf("expected name 'error-patterns', got %q", items[0].Name)
	}
	if items[0].Summary != "Error handling patterns" {
		t.Errorf("expected summary 'Error handling patterns', got %q", items[0].Summary)
	}
	if len(items[0].Artifacts) != 1 || items[0].Artifacts[0] != "overview.md" {
		t.Errorf("expected artifacts [overview.md], got %v", items[0].Artifacts)
	}

	// Verify on-disk
	content, err := os.ReadFile(filepath.Join(dir, "error-patterns", "artifacts", "overview.md"))
	if err != nil {
		t.Fatalf("artifact file should exist: %v", err)
	}
	if string(content) != "# Error Patterns\nUse fmt.Errorf with %w." {
		t.Errorf("unexpected artifact content: %q", content)
	}

	// Reload from disk
	ks2 := NewKnowledgeStore(dir)
	if err := ks2.Load(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	items2 := ks2.List()
	if len(items2) != 1 {
		t.Fatalf("expected 1 item after reload, got %d", len(items2))
	}
	if items2[0].Name != "error-patterns" {
		t.Errorf("expected name 'error-patterns' after reload, got %q", items2[0].Name)
	}
}

func TestKnowledgeStore_WriteUpdatesMetadata(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	// First write
	ks.WriteArtifact("test-ki", "Initial summary", "file1.md", "content1", nil)
	ki1, _ := ks.Get("test-ki")
	time1 := ki1.UpdatedAt

	// Brief pause for time difference
	time.Sleep(10 * time.Millisecond)

	// Second write — updates summary and timestamp
	ks.WriteArtifact("test-ki", "Updated summary", "file2.md", "content2", []string{"ref.go"})
	ki2, _ := ks.Get("test-ki")

	if ki2.Summary != "Updated summary" {
		t.Errorf("expected summary 'Updated summary', got %q", ki2.Summary)
	}
	if !ki2.UpdatedAt.After(time1) {
		t.Error("UpdatedAt should be later after second write")
	}
	if len(ki2.Artifacts) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(ki2.Artifacts))
	}
}

func TestKnowledgeStore_ReplaceInArtifact(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	ks.WriteArtifact("test-ki", "Test", "doc.md", "Hello world, this is a test.", nil)

	err := ks.ReplaceInArtifact("test-ki", "doc.md", "Hello world", "Goodbye world")
	if err != nil {
		t.Fatalf("ReplaceInArtifact failed: %v", err)
	}

	// Verify content changed
	content, _ := os.ReadFile(filepath.Join(dir, "test-ki", "artifacts", "doc.md"))
	if string(content) != "Goodbye world, this is a test." {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestKnowledgeStore_ReplaceNotFound(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	ks.WriteArtifact("test-ki", "Test", "doc.md", "Hello world", nil)

	err := ks.ReplaceInArtifact("test-ki", "doc.md", "NONEXISTENT", "replacement")
	if err == nil {
		t.Error("expected error when target not found")
	}
	if !strings.Contains(err.Error(), "target string not found") {
		t.Errorf("expected 'target string not found' error, got: %v", err)
	}
}

func TestKnowledgeStore_Delete(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	ks.WriteArtifact("doomed-ki", "Will be deleted", "doc.md", "content", nil)

	err := ks.Delete("doomed-ki")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if len(ks.List()) != 0 {
		t.Error("expected no items after delete")
	}

	// Directory should be gone
	if _, err := os.Stat(filepath.Join(dir, "doomed-ki")); !os.IsNotExist(err) {
		t.Error("KI directory should be removed")
	}
}

func TestKnowledgeStore_DeleteArtifact(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	ks.WriteArtifact("test-ki", "Test", "file1.md", "content1", nil)
	ks.WriteArtifact("test-ki", "", "file2.md", "content2", nil)

	err := ks.DeleteArtifact("test-ki", "file1.md")
	if err != nil {
		t.Fatalf("DeleteArtifact failed: %v", err)
	}

	ki, ok := ks.Get("test-ki")
	if !ok {
		t.Fatal("KI should still exist")
	}
	if len(ki.Artifacts) != 1 || ki.Artifacts[0] != "file2.md" {
		t.Errorf("expected [file2.md], got %v", ki.Artifacts)
	}
}

func TestKnowledgeStore_DeleteLastArtifact(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	ks.WriteArtifact("test-ki", "Test", "only-file.md", "content", nil)

	err := ks.DeleteArtifact("test-ki", "only-file.md")
	if err != nil {
		t.Fatalf("DeleteArtifact failed: %v", err)
	}

	// KI should be cleaned up since no artifacts remain
	if len(ks.List()) != 0 {
		t.Error("KI should be removed when last artifact is deleted")
	}
}

func TestKnowledgeStore_List_Sorted(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	ks.WriteArtifact("zebra-ki", "Z", "doc.md", "z", nil)
	ks.WriteArtifact("alpha-ki", "A", "doc.md", "a", nil)
	ks.WriteArtifact("middle-ki", "M", "doc.md", "m", nil)

	items := ks.List()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Name != "alpha-ki" || items[1].Name != "middle-ki" || items[2].Name != "zebra-ki" {
		t.Errorf("expected sorted by name, got %v, %v, %v", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestKnowledgeStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "ki-" + string(rune('a'+n))
			ks.WriteArtifact(name, "Summary", "doc.md", "content", nil)
			ks.List()
			ks.Get(name)
		}(i)
	}
	wg.Wait()

	// Should have 10 items
	items := ks.List()
	if len(items) != 10 {
		t.Errorf("expected 10 items, got %d", len(items))
	}
}

func TestKnowledgeStore_Staleness(t *testing.T) {
	dir := t.TempDir()
	wsDir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	// Create a reference file
	refPath := filepath.Join(wsDir, "internal", "engine", "engine.go")
	os.MkdirAll(filepath.Dir(refPath), 0755)
	os.WriteFile(refPath, []byte("package engine"), 0644)

	// Create KI referencing that file
	ks.WriteArtifact("test-ki", "Test", "doc.md", "content", []string{"internal/engine/engine.go"})

	// Initially not stale
	ks.CheckStaleness([]string{wsDir})
	ki, _ := ks.Get("test-ki")
	if ki.Stale {
		t.Error("should not be stale immediately after creation")
	}

	// Modify the reference file (with a future mtime)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(refPath, []byte("package engine // modified"), 0644)

	ks.CheckStaleness([]string{wsDir})
	ki, _ = ks.Get("test-ki")
	if !ki.Stale {
		t.Error("should be stale after reference file was modified")
	}
}

func TestKnowledgeStore_Staleness_AfterUpdate(t *testing.T) {
	dir := t.TempDir()
	wsDir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	refPath := filepath.Join(wsDir, "ref.go")
	os.WriteFile(refPath, []byte("package ref"), 0644)

	ks.WriteArtifact("test-ki", "Test", "doc.md", "old", []string{"ref.go"})

	// Make it stale
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(refPath, []byte("package ref // changed"), 0644)
	ks.CheckStaleness([]string{wsDir})

	ki, _ := ks.Get("test-ki")
	if !ki.Stale {
		t.Fatal("should be stale")
	}

	// Writing clears staleness
	ks.WriteArtifact("test-ki", "Updated", "doc.md", "new", []string{"ref.go"})
	ki, _ = ks.Get("test-ki")
	if ki.Stale {
		t.Error("writing should clear staleness")
	}
}

func TestKnowledgeStore_Staleness_MissingRef(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	// Reference a file that doesn't exist
	ks.WriteArtifact("test-ki", "Test", "doc.md", "content", []string{"nonexistent.go"})

	ks.CheckStaleness([]string{t.TempDir()})
	ki, _ := ks.Get("test-ki")
	if ki.Stale {
		t.Error("missing reference should NOT be stale (just gone)")
	}
}

func TestKnowledgeStore_NameNormalization(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	err := ks.WriteArtifact("Error Handling Patterns", "Test", "doc.md", "content", nil)
	if err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}

	items := ks.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "error-handling-patterns" {
		t.Errorf("expected normalized name 'error-handling-patterns', got %q", items[0].Name)
	}
}

func TestKnowledgeStore_PathTraversalPrevention(t *testing.T) {
	dir := t.TempDir()
	ks := NewKnowledgeStore(dir)

	err := ks.WriteArtifact("test-ki", "Test", "../../../etc/passwd", "hacked", nil)
	if err == nil {
		t.Error("should reject path traversal")
	}
	if !strings.Contains(err.Error(), "relative path without '..'") {
		t.Errorf("expected path traversal error, got: %v", err)
	}
}
