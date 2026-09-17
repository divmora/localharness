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
