package util

import (
	"strings"
	"testing"
)

func TestUnifiedDiff(t *testing.T) {
	oldText := "line 1\nline 2\nline 3\n"
	newText := "line 1\nline 2 modified\nline 3\nline 4\n"

	diff := UnifiedDiff("a/test.txt", "b/test.txt", oldText, newText)
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}

	if !strings.Contains(diff, "--- a/test.txt") {
		t.Errorf("diff missing old name header: %s", diff)
	}
	if !strings.Contains(diff, "+++ b/test.txt") {
		t.Errorf("diff missing new name header: %s", diff)
	}
	if !strings.Contains(diff, "-line 2") {
		t.Errorf("diff missing deleted line: %s", diff)
	}
	if !strings.Contains(diff, "+line 2 modified") {
		t.Errorf("diff missing added modified line: %s", diff)
	}
	if !strings.Contains(diff, "+line 4") {
		t.Errorf("diff missing added line 4: %s", diff)
	}

	// Identical texts should return empty diff
	noDiff := UnifiedDiff("a/test.txt", "b/test.txt", oldText, oldText)
	if noDiff != "" {
		t.Errorf("expected empty diff for identical text, got: %s", noDiff)
	}
}
