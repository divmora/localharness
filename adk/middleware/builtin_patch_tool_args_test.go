package middleware

import (
	"testing"
)

func TestDetectJSONIssues_Clean(t *testing.T) {
	issues := detectJSONIssues(`{"key": "value", "num": 42}`)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got: %v", issues)
	}
}

func TestDetectJSONIssues_TrailingComma(t *testing.T) {
	issues := detectJSONIssues(`{"key": "value",}`)
	assertContains(t, issues, "trailing_comma")
}

func TestDetectJSONIssues_TrailingCommaArray(t *testing.T) {
	issues := detectJSONIssues(`["a", "b",]`)
	assertContains(t, issues, "trailing_comma")
}

func TestDetectJSONIssues_UnescapedNewline(t *testing.T) {
	issues := detectJSONIssues("{\"key\": \"value with\nnewline\"}")
	assertContains(t, issues, "unescaped_newline_in_string")
}

func TestDetectJSONIssues_EscapedNewline_OK(t *testing.T) {
	// Escaped newlines should not trigger
	issues := detectJSONIssues(`{"key": "value with\\nnewline"}`)
	for _, issue := range issues {
		if issue == "unescaped_newline_in_string" {
			t.Fatal("escaped newlines should not be flagged")
		}
	}
}

func TestDetectJSONIssues_UnbalancedBraces(t *testing.T) {
	issues := detectJSONIssues(`{"key": "value"`)
	assertContains(t, issues, "unbalanced_braces")
}

func TestDetectJSONIssues_UnbalancedBrackets(t *testing.T) {
	issues := detectJSONIssues(`["a", "b"`)
	assertContains(t, issues, "unbalanced_braces")
}

func TestDetectJSONIssues_Multiple(t *testing.T) {
	issues := detectJSONIssues("{\"key\": \"value\nnewline\",}")
	if len(issues) < 2 {
		t.Fatalf("expected at least 2 issues, got: %v", issues)
	}
}

func TestPatchToolArgs_Counts(t *testing.T) {
	p := NewPatchToolArgs(nil)

	counts := p.Counts()
	if len(counts) != 0 {
		t.Fatal("initial counts should be empty")
	}
}

func assertContains(t *testing.T, issues []string, expected string) {
	t.Helper()
	for _, issue := range issues {
		if issue == expected {
			return
		}
	}
	t.Fatalf("expected issue %q not found in: %v", expected, issues)
}
