package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveArtifactReview_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	planPath := filepath.Join(tmpDir, "implementation_plan.md")
	if err := os.WriteFile(planPath, []byte("# Implementation Plan\n\n1. Step one\n2. Step two\n"), 0644); err != nil {
		t.Fatalf("failed to write test artifact: %v", err)
	}

	rev := NewActiveArtifactReview(planPath, "implementation_plan", "Plan for system refactoring")
	if rev == nil {
		t.Fatal("expected non-nil review")
	}
	if rev.Filename != "implementation_plan.md" {
		t.Errorf("expected filename implementation_plan.md, got %s", rev.Filename)
	}
	if rev.ArtifactType != "implementation_plan" {
		t.Errorf("expected type implementation_plan, got %s", rev.ArtifactType)
	}

	// Test feedback mode toggle
	rev.StartFeedback()
	if !rev.IsWritingFeedback {
		t.Error("expected IsWritingFeedback to be true")
	}
	rev.CancelFeedback()
	if rev.IsWritingFeedback {
		t.Error("expected IsWritingFeedback to be false")
	}

	// Test full view toggle
	rev.ToggleView()
	if !rev.ViewingFull {
		t.Error("expected ViewingFull to be true")
	}
	if !strings.Contains(rev.FullContent, "Step one") {
		t.Errorf("expected FullContent to contain 'Step one', got %s", rev.FullContent)
	}

	// Test rendering
	rendered := RenderArtifactReviewInline(rev, 80)
	if !strings.Contains(rendered, "implementation_plan.md") {
		t.Errorf("rendered card missing filename: %s", rendered)
	}
	if !strings.Contains(rendered, "IMPLEMENTATION_PLAN") {
		t.Errorf("rendered card missing uppercase badge: %s", rendered)
	}
	if !strings.Contains(rendered, "Proceed") {
		t.Errorf("rendered card missing proceed action: %s", rendered)
	}
}
