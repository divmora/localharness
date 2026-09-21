package util

import (
	"fmt"
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

func TestUnifiedDiff_AllAdditionsAndDeletions(t *testing.T) {
	content := "line A\nline B\nline C\n"

	// All additions (new file)
	addDiff := UnifiedDiff("a/new.txt", "b/new.txt", "", content)
	if !strings.Contains(addDiff, "+line A") || !strings.Contains(addDiff, "+line B") || !strings.Contains(addDiff, "+line C") {
		t.Fatalf("expected all additions in new file diff: %s", addDiff)
	}
	if strings.Contains(addDiff, "\n-") {
		t.Fatalf("expected no deletions in new file diff: %s", addDiff)
	}

	// All deletions (deleted file)
	delDiff := UnifiedDiff("a/old.txt", "b/old.txt", content, "")
	if !strings.Contains(delDiff, "-line A") || !strings.Contains(delDiff, "-line B") || !strings.Contains(delDiff, "-line C") {
		t.Fatalf("expected all deletions in deleted file diff: %s", delDiff)
	}
	if strings.Contains(delDiff, "+line") {
		t.Fatalf("expected no additions in deleted file diff: %s", delDiff)
	}
}

func TestUnifiedDiff_LargePrefixSuffixAndPatience(t *testing.T) {
	var oldLines, newLines []string

	// 500 lines prefix
	for i := 0; i < 500; i++ {
		line := fmt.Sprintf("common prefix line %d", i)
		oldLines = append(oldLines, line)
		newLines = append(newLines, line)
	}

	// Modified middle segment
	oldLines = append(oldLines, "func OldImplementation() {", "    return false", "}")
	newLines = append(newLines, "func NewImplementation() {", "    return true", "}")

	// 500 lines suffix
	for i := 0; i < 500; i++ {
		line := fmt.Sprintf("common suffix line %d", i)
		oldLines = append(oldLines, line)
		newLines = append(newLines, line)
	}

	diff := UnifiedDiff("a/code.go", "b/code.go", strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"))
	if diff == "" {
		t.Fatal("expected non-empty diff for modified middle block")
	}

	if !strings.Contains(diff, "-func OldImplementation() {") {
		t.Errorf("missing old function removal: %s", diff)
	}
	if !strings.Contains(diff, "+func NewImplementation() {") {
		t.Errorf("missing new function addition: %s", diff)
	}
}

func TestUnifiedDiff_LineEndings(t *testing.T) {
	oldCRLF := "line 1\r\nline 2\r\nline 3\r\n"
	newLF := "line 1\nline 2 modified\nline 3\n"

	diff := UnifiedDiff("a/test.txt", "b/test.txt", oldCRLF, newLF)
	if !strings.Contains(diff, "-line 2") || !strings.Contains(diff, "+line 2 modified") {
		t.Errorf("unexpected diff with CRLF: %s", diff)
	}
}

func TestUnifiedDiff_HunkCollapsing(t *testing.T) {
	var oldLines, newLines []string
	for i := 1; i <= 200; i++ {
		line := fmt.Sprintf("line %03d content", i)
		oldLines = append(oldLines, line)
		newLines = append(newLines, line)
	}

	// Modify line 10 and line 190 (separated by 180 unchanged lines)
	oldLines[9] = "line 010 original"
	newLines[9] = "line 010 changed"

	oldLines[189] = "line 190 original"
	newLines[189] = "line 190 changed"

	diff := UnifiedDiff("a/file.txt", "b/file.txt", strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"))

	// Should contain exactly 2 hunk headers
	hunkHeaderCount := strings.Count(diff, "@@ ")
	if hunkHeaderCount != 2 {
		t.Fatalf("expected 2 hunk headers, got %d:\n%s", hunkHeaderCount, diff)
	}

	// Should NOT contain distant unchanged lines (e.g. line 100)
	if strings.Contains(diff, "line 100 content") {
		t.Fatalf("diff should have collapsed line 100, but found in diff:\n%s", diff)
	}

	// Should contain both changes
	if !strings.Contains(diff, "-line 010 original") || !strings.Contains(diff, "+line 010 changed") {
		t.Errorf("missing first change in diff: %s", diff)
	}
	if !strings.Contains(diff, "-line 190 original") || !strings.Contains(diff, "+line 190 changed") {
		t.Errorf("missing second change in diff: %s", diff)
	}
}

func TestUnifiedDiff_ConfigurableContext(t *testing.T) {
	oldText := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	newText := "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n10\n"

	// 1. With 1 line context
	diff1 := UnifiedDiffWithContext("a/t.txt", "b/t.txt", oldText, newText, 1)
	if !strings.Contains(diff1, " 4\n-5\n+FIVE\n 6") {
		t.Errorf("expected 1 line context around change, got:\n%s", diff1)
	}
	if strings.Contains(diff1, " 3\n") || strings.Contains(diff1, " 7\n") {
		t.Errorf("context 1 should not contain lines 3 or 7, got:\n%s", diff1)
	}

	// 2. With 0 line context
	diff0 := UnifiedDiffWithContext("a/t.txt", "b/t.txt", oldText, newText, 0)
	if strings.Contains(diff0, " 4\n") || strings.Contains(diff0, " 6\n") {
		t.Errorf("context 0 should contain no context lines, got:\n%s", diff0)
	}
	if !strings.Contains(diff0, "-5\n+FIVE") {
		t.Errorf("context 0 missing edit, got:\n%s", diff0)
	}
}

func BenchmarkUnifiedDiff(b *testing.B) {
	sizes := []int{500, 2000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("%d_lines", size), func(b *testing.B) {
			var oldBuilder, newBuilder strings.Builder
			// Generate file where 1% of lines in the middle are modified
			modStart := size / 2
			modEnd := modStart + (size / 100) + 1

			for i := 0; i < size; i++ {
				if i >= modStart && i < modEnd {
					oldBuilder.WriteString(fmt.Sprintf("line %d original content for benchmarking\n", i))
					newBuilder.WriteString(fmt.Sprintf("line %d updated and modified content for benchmarking\n", i))
				} else {
					line := fmt.Sprintf("line %d common content for benchmarking\n", i)
					oldBuilder.WriteString(line)
					newBuilder.WriteString(line)
				}
			}

			oldText := oldBuilder.String()
			newText := newBuilder.String()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				diff := UnifiedDiff("a/bench.txt", "b/bench.txt", oldText, newText)
				if diff == "" {
					b.Fatal("expected non-empty diff")
				}
			}
		})
	}
}
