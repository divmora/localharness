package codegraph

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ScoredNode wraps a Node with an FTS relevance score.
type ScoredNode struct {
	Node  Node    `json:"node"`
	Score float64 `json:"score"`
}

// TokenizeText splits text into searchable terms, handling CamelCase, snake_case, and punctuation.
func TokenizeText(text string) []string {
	if text == "" {
		return nil
	}

	// Replace non-alphanumeric characters with spaces
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, text)

	rawTokens := strings.Fields(cleaned)
	var tokens []string
	seen := make(map[string]bool)

	for _, token := range rawTokens {
		lower := strings.ToLower(token)
		if len(lower) >= 2 && !seen[lower] {
			seen[lower] = true
			tokens = append(tokens, lower)
		}

		// Split CamelCase (e.g. "ValidateToken" -> "validate", "token")
		subTokens := splitCamelCase(token)
		for _, sub := range subTokens {
			subLower := strings.ToLower(sub)
			if len(subLower) >= 2 && !seen[subLower] {
				seen[subLower] = true
				tokens = append(tokens, subLower)
			}
		}
	}

	return tokens
}

var camelCaseRegex = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func splitCamelCase(s string) []string {
	split := camelCaseRegex.ReplaceAllString(s, `${1} ${2}`)
	return strings.Fields(split)
}

// FTSIndex provides in-memory BM25-style inverted indexing for symbols and docstrings.
type FTSIndex struct {
	// term -> docID -> term frequency
	index       map[string]map[string]int
	docLengths  map[string]int
	totalTokens int
	docCount    int
	nodes       map[string]Node
}

// NewFTSIndex creates an empty FTS index.
func NewFTSIndex() *FTSIndex {
	return &FTSIndex{
		index:      make(map[string]map[string]int),
		docLengths: make(map[string]int),
		nodes:      make(map[string]Node),
	}
}

// IndexNode adds a Node and its metadata to the FTS index with weighted fields.
func (fts *FTSIndex) IndexNode(n Node) {
	docID := n.SymbolID
	fts.nodes[docID] = n

	// Weighted text representation:
	// Name has 3x weight, Signature 2x, Docstring 1.5x, SymbolID 1x
	var weightedTerms []string

	// Name (3x)
	nameTokens := TokenizeText(n.Name)
	for i := 0; i < 3; i++ {
		weightedTerms = append(weightedTerms, nameTokens...)
	}

	// Signature (2x)
	sigTokens := TokenizeText(n.Signature)
	for i := 0; i < 2; i++ {
		weightedTerms = append(weightedTerms, sigTokens...)
	}

	// Docstring (1.5x -> 2x)
	docTokens := TokenizeText(n.Docstring)
	weightedTerms = append(weightedTerms, docTokens...)
	weightedTerms = append(weightedTerms, docTokens...)

	// FilePath & SymbolID (1x)
	pathTokens := TokenizeText(n.FilePath)
	idTokens := TokenizeText(n.SymbolID)
	weightedTerms = append(weightedTerms, pathTokens...)
	weightedTerms = append(weightedTerms, idTokens...)

	fts.docLengths[docID] = len(weightedTerms)
	fts.totalTokens += len(weightedTerms)
	fts.docCount++

	for _, term := range weightedTerms {
		if _, ok := fts.index[term]; !ok {
			fts.index[term] = make(map[string]int)
		}
		fts.index[term][docID]++
	}
}

// Search ranks nodes matching query terms using BM25 relevance scoring.
func (fts *FTSIndex) Search(query string, kindFilter string, limit int) []ScoredNode {
	if limit <= 0 {
		limit = 50
	}

	queryTerms := TokenizeText(query)
	if len(queryTerms) == 0 {
		return nil
	}

	kindFilter = strings.ToLower(strings.TrimSpace(kindFilter))

	// BM25 parameters
	k1 := 1.5
	b := 0.75
	avgDocLen := 1.0
	if fts.docCount > 0 {
		avgDocLen = float64(fts.totalTokens) / float64(fts.docCount)
	}

	scores := make(map[string]float64)

	for _, term := range queryTerms {
		postings, ok := fts.index[term]
		if !ok {
			// Prefix matching fallback
			for idxTerm, idxPostings := range fts.index {
				if strings.HasPrefix(idxTerm, term) {
					postings = idxPostings
					break
				}
			}
		}

		if len(postings) == 0 {
			continue
		}

		// IDF calculation
		docFreq := float64(len(postings))
		idf := math.Log((float64(fts.docCount)-docFreq+0.5)/(docFreq+0.5) + 1.0)

		for docID, tf := range postings {
			n := fts.nodes[docID]
			if kindFilter != "" && strings.ToLower(n.Kind) != kindFilter {
				continue
			}

			docLen := float64(fts.docLengths[docID])
			tfScore := (float64(tf) * (k1 + 1.0)) / (float64(tf) + k1*(1.0-b+b*(docLen/avgDocLen)))

			// Exact name match boost
			if strings.EqualFold(n.Name, query) {
				tfScore *= 3.0
			} else if strings.Contains(strings.ToLower(n.Name), strings.ToLower(query)) {
				tfScore *= 1.5
			}

			scores[docID] += idf * tfScore
		}
	}

	var results []ScoredNode
	for docID, score := range scores {
		if score > 0 {
			results = append(results, ScoredNode{
				Node:  fts.nodes[docID],
				Score: score,
			})
		}
	}

	// Sort descending by relevance score
	sort.Slice(results, func(i, j int) bool {
		if math.Abs(results[i].Score-results[j].Score) > 0.001 {
			return results[i].Score > results[j].Score
		}
		return results[i].Node.Name < results[j].Node.Name
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results
}
