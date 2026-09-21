package util

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultDiffContextLines is the standard number of context lines included in unified diff hunks.
const DefaultDiffContextLines = 3

type diffOp struct {
	op   byte // ' ', '-', '+'
	line string
}

type diffLine struct {
	op      byte
	text    string
	oldLine int
	newLine int
}

// UnifiedDiff generates a standard unified diff between oldText and newText with default 3-line context.
func UnifiedDiff(oldName, newName, oldText, newText string) string {
	return UnifiedDiffWithContext(oldName, newName, oldText, newText, DefaultDiffContextLines)
}

// UnifiedDiffWithContext generates a standard unified diff between oldText and newText
// using common prefix/suffix trimming, patience anchor partitioning, O(ND) Myers diff,
// and hunk-scoped context collapsing.
func UnifiedDiffWithContext(oldName, newName, oldText, newText string, contextLines int) string {
	if oldText == newText {
		return ""
	}
	if contextLines < 0 {
		contextLines = DefaultDiffContextLines
	}

	oldLines := splitLines(oldText)
	newLines := splitLines(newText)

	m := len(oldLines)
	n := len(newLines)

	// Quick check: both empty
	if m == 0 && n == 0 {
		return ""
	}

	// 1. Common prefix trimming
	prefix := 0
	for prefix < m && prefix < n && oldLines[prefix] == newLines[prefix] {
		prefix++
	}

	// 2. Common suffix trimming
	suffix := 0
	for suffix < m-prefix && suffix < n-prefix && oldLines[m-1-suffix] == newLines[n-1-suffix] {
		suffix++
	}

	// If prefix + suffix covers both completely, no changes
	if prefix+suffix == m && prefix+suffix == n {
		return ""
	}

	// Middle segments to diff
	midA := oldLines[prefix : m-suffix]
	midB := newLines[prefix : n-suffix]

	midOps := patienceDiff(midA, midB)

	// Check if midOps contains any changes
	hasChanges := false
	for _, op := range midOps {
		if op.op != ' ' {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	// Build scoped line stream (lead context + midOps + trail context)
	leadCount := min(prefix, contextLines)
	leadStart := prefix - leadCount
	trailCount := min(suffix, contextLines)

	totalItems := leadCount + len(midOps) + trailCount
	items := make([]diffLine, 0, totalItems)

	currOld := leadStart + 1
	currNew := leadStart + 1

	for i := leadStart; i < prefix; i++ {
		items = append(items, diffLine{
			op:      ' ',
			text:    oldLines[i],
			oldLine: currOld,
			newLine: currNew,
		})
		currOld++
		currNew++
	}

	for _, op := range midOps {
		dl := diffLine{
			op:   op.op,
			text: op.line,
		}
		switch op.op {
		case ' ':
			dl.oldLine = currOld
			dl.newLine = currNew
			currOld++
			currNew++
		case '-':
			dl.oldLine = currOld
			currOld++
		case '+':
			dl.newLine = currNew
			currNew++
		}
		items = append(items, dl)
	}

	for i := 0; i < trailCount; i++ {
		items = append(items, diffLine{
			op:      ' ',
			text:    oldLines[m-suffix+i],
			oldLine: currOld,
			newLine: currNew,
		})
		currOld++
		currNew++
	}

	// Find changes and group into hunks
	var changeIndices []int
	for i, item := range items {
		if item.op != ' ' {
			changeIndices = append(changeIndices, i)
		}
	}
	if len(changeIndices) == 0 {
		return ""
	}

	type hunkRange struct {
		start int
		end   int
	}
	var hunks []hunkRange

	curStart := max(0, changeIndices[0]-contextLines)
	curEnd := min(len(items), changeIndices[0]+contextLines+1)

	for i := 1; i < len(changeIndices); i++ {
		ci := changeIndices[i]
		if ci-contextLines <= curEnd {
			// Overlapping or adjacent: extend current hunk
			curEnd = min(len(items), ci+contextLines+1)
		} else {
			// Gap exceeds context: finalize hunk and begin next
			hunks = append(hunks, hunkRange{start: curStart, end: curEnd})
			curStart = max(0, ci-contextLines)
			curEnd = min(len(items), ci+contextLines+1)
		}
	}
	hunks = append(hunks, hunkRange{start: curStart, end: curEnd})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s\n", oldName))
	sb.WriteString(fmt.Sprintf("+++ %s\n", newName))

	for _, h := range hunks {
		hunkItems := items[h.start:h.end]
		var origCount, newCount int
		origStart := -1
		newStart := -1

		for _, item := range hunkItems {
			if item.op == ' ' || item.op == '-' {
				origCount++
				if origStart == -1 && item.oldLine > 0 {
					origStart = item.oldLine
				}
			}
			if item.op == ' ' || item.op == '+' {
				newCount++
				if newStart == -1 && item.newLine > 0 {
					newStart = item.newLine
				}
			}
		}

		if origCount == 0 {
			if h.start > 0 && items[h.start-1].oldLine > 0 {
				origStart = items[h.start-1].oldLine
			} else if len(hunkItems) > 0 && hunkItems[0].newLine > 1 {
				origStart = hunkItems[0].newLine - 1
			} else {
				origStart = 0
			}
		}

		if newCount == 0 {
			if h.start > 0 && items[h.start-1].newLine > 0 {
				newStart = items[h.start-1].newLine
			} else if len(hunkItems) > 0 && hunkItems[0].oldLine > 1 {
				newStart = hunkItems[0].oldLine - 1
			} else {
				newStart = 0
			}
		}

		sb.WriteString(formatHunkHeader(origStart, origCount, newStart, newCount))
		for _, item := range hunkItems {
			sb.WriteString(fmt.Sprintf("%c%s\n", item.op, item.text))
		}
	}

	return sb.String()
}

func formatHunkHeader(origStart, origCount, newStart, newCount int) string {
	var origPart, newPart string
	if origCount == 1 {
		origPart = fmt.Sprintf("-%d", origStart)
	} else {
		origPart = fmt.Sprintf("-%d,%d", origStart, origCount)
	}
	if newCount == 1 {
		newPart = fmt.Sprintf("+%d", newStart)
	} else {
		newPart = fmt.Sprintf("+%d,%d", newStart, newCount)
	}
	return fmt.Sprintf("@@ %s %s @@\n", origPart, newPart)
}

type matchItem struct {
	idxA int
	idxB int
}

// patienceDiff partitions large diffs by unique common anchor lines,
// then applies Myers diff to smaller chunks.
func patienceDiff(a, b []string) []diffOp {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	if len(a) == 0 {
		ops := make([]diffOp, len(b))
		for i, line := range b {
			ops[i] = diffOp{op: '+', line: line}
		}
		return ops
	}
	if len(b) == 0 {
		ops := make([]diffOp, len(a))
		for i, line := range a {
			ops[i] = diffOp{op: '-', line: line}
		}
		return ops
	}

	// If chunk is small, use Myers diff directly
	if len(a) <= 256 && len(b) <= 256 {
		return myersDiff(a, b)
	}

	// Find unique lines in a and b
	countA := make(map[string]int)
	posA := make(map[string]int)
	for i, line := range a {
		countA[line]++
		posA[line] = i
	}

	countB := make(map[string]int)
	posB := make(map[string]int)
	for i, line := range b {
		countB[line]++
		posB[line] = i
	}

	var candidates []matchItem
	for line, cA := range countA {
		if cA == 1 && countB[line] == 1 {
			candidates = append(candidates, matchItem{idxA: posA[line], idxB: posB[line]})
		}
	}

	// If no unique matches found, fall back to Myers diff
	if len(candidates) == 0 {
		return myersDiff(a, b)
	}

	// Sort candidates by idxA
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].idxA < candidates[j].idxA
	})

	// Find Longest Increasing Subsequence (LIS) on idxB
	anchors := longestIncreasingSubsequence(candidates)
	if len(anchors) == 0 {
		return myersDiff(a, b)
	}

	var ops []diffOp
	lastA := 0
	lastB := 0

	for _, anc := range anchors {
		if anc.idxA > lastA || anc.idxB > lastB {
			subOps := patienceDiff(a[lastA:anc.idxA], b[lastB:anc.idxB])
			ops = append(ops, subOps...)
		}
		ops = append(ops, diffOp{op: ' ', line: a[anc.idxA]})
		lastA = anc.idxA + 1
		lastB = anc.idxB + 1
	}

	if lastA < len(a) || lastB < len(b) {
		subOps := patienceDiff(a[lastA:], b[lastB:])
		ops = append(ops, subOps...)
	}

	return ops
}

func longestIncreasingSubsequence(candidates []matchItem) []matchItem {
	if len(candidates) == 0 {
		return nil
	}

	var piles []int
	parent := make([]int, len(candidates))

	for i, c := range candidates {
		left, right := 0, len(piles)
		for left < right {
			mid := (left + right) / 2
			if candidates[piles[mid]].idxB >= c.idxB {
				right = mid
			} else {
				left = mid + 1
			}
		}

		if left > 0 {
			parent[i] = piles[left-1]
		} else {
			parent[i] = -1
		}

		if left == len(piles) {
			piles = append(piles, i)
		} else {
			piles[left] = i
		}
	}

	lis := make([]matchItem, len(piles))
	curr := piles[len(piles)-1]
	for i := len(piles) - 1; i >= 0; i-- {
		lis[i] = candidates[curr]
		curr = parent[curr]
	}

	return lis
}

// myersDiff implements the O(ND) Myers difference algorithm with diagonal vectors.
func myersDiff(a, b []string) []diffOp {
	n := len(a)
	m := len(b)

	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		ops := make([]diffOp, m)
		for i, line := range b {
			ops[i] = diffOp{op: '+', line: line}
		}
		return ops
	}
	if m == 0 {
		ops := make([]diffOp, n)
		for i, line := range a {
			ops[i] = diffOp{op: '-', line: line}
		}
		return ops
	}

	maxD := n + m
	if maxD > 2000 {
		maxD = 2000
	}

	offset := maxD
	v := make([]int, 2*maxD+1)
	trace := make([][]int, 0, maxD+1)

	v[1+offset] = 0

	reached := false
	finalD := 0

	for d := 0; d <= maxD; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+offset] < v[k+1+offset]) {
				x = v[k+1+offset]
			} else {
				x = v[k-1+offset] + 1
			}
			y := x - k

			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k+offset] = x

			if x >= n && y >= m {
				vCopy := make([]int, 2*d+1)
				for ik := -d; ik <= d; ik += 2 {
					vCopy[ik+d] = v[ik+offset]
				}
				trace = append(trace, vCopy)

				reached = true
				finalD = d
				break
			}
		}

		if reached {
			break
		}

		vCopy := make([]int, 2*d+1)
		for ik := -d; ik <= d; ik += 2 {
			vCopy[ik+d] = v[ik+offset]
		}
		trace = append(trace, vCopy)
	}

	// If maxD exceeded without reaching (e.g. completely disjoint large inputs), fallback
	if !reached {
		ops := make([]diffOp, 0, n+m)
		for _, line := range a {
			ops = append(ops, diffOp{op: '-', line: line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{op: '+', line: line})
		}
		return ops
	}

	// Backtrack edit script from trace
	var ops []diffOp
	x := n
	y := m

	for d := finalD; d > 0; d-- {
		k := x - y
		prevV := trace[d-1]
		prevOffset := d - 1

		var prevK int
		if k == -d || (k != d && getV(prevV, prevOffset, k-1) < getV(prevV, prevOffset, k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := getV(prevV, prevOffset, prevK)
		prevY := prevX - prevK

		var xMid, yMid int
		if prevK == k+1 {
			xMid = prevX
			yMid = prevY + 1
		} else {
			xMid = prevX + 1
			yMid = prevY
		}

		// Diagonal snake matches
		for x > xMid && y > yMid {
			ops = append(ops, diffOp{op: ' ', line: a[x-1]})
			x--
			y--
		}

		// Edit move
		if prevK == k+1 {
			ops = append(ops, diffOp{op: '+', line: b[prevY]})
		} else {
			ops = append(ops, diffOp{op: '-', line: a[prevX]})
		}

		x = prevX
		y = prevY
	}

	// Matches before step 1
	for x > 0 && y > 0 {
		ops = append(ops, diffOp{op: ' ', line: a[x-1]})
		x--
		y--
	}

	// Reverse ops to chronological order
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	return ops
}

func getV(v []int, offset, k int) int {
	idx := k + offset
	if idx < 0 || idx >= len(v) {
		return 0
	}
	return v[idx]
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	var lines []string
	start := 0
	n := len(s)
	for i := 0; i < n; i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		} else if s[i] == '\r' {
			if i+1 < n && s[i+1] == '\n' {
				continue
			}
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < n {
		lines = append(lines, s[start:])
	}
	return lines
}
