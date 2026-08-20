package util

import (
	"fmt"
	"strings"
)

// UnifiedDiff generates a standard unified diff between oldText and newText.
func UnifiedDiff(oldName, newName, oldText, newText string) string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)

	m := len(oldLines)
	n := len(newLines)

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				if dp[i+1][j] >= dp[i][j+1] {
					dp[i][j] = dp[i+1][j]
				} else {
					dp[i][j] = dp[i+1][j+1]
				}
			}
		}
	}

	type diffOp struct {
		op   byte // ' ', '-', '+'
		line string
	}

	var ops []diffOp
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			ops = append(ops, diffOp{' ', oldLines[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, diffOp{'-', oldLines[i]})
			i++
		} else {
			ops = append(ops, diffOp{'+', newLines[j]})
			j++
		}
	}
	for i < m {
		ops = append(ops, diffOp{'-', oldLines[i]})
		i++
	}
	for j < n {
		ops = append(ops, diffOp{'+', newLines[j]})
		j++
	}

	hasChanges := false
	for _, op := range ops {
		if op.op != ' ' {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s\n", oldName))
	sb.WriteString(fmt.Sprintf("+++ %s\n", newName))
	sb.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", m, n))
	for _, op := range ops {
		sb.WriteString(fmt.Sprintf("%c%s\n", op.op, op.line))
	}

	return sb.String()
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s == "" {
		return []string{}
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
