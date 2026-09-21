package codegraph

import (
	"math"
	"sort"
	"strings"
	"sync"
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

	var tokens []string
	seen := make(map[string]bool)

	addToken := func(t string) {
		if len(t) < 2 {
			return
		}
		lower := strings.ToLower(t)
		if !seen[lower] {
			seen[lower] = true
			tokens = append(tokens, lower)
		}
	}

	start := -1
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start == -1 {
				start = i
			}
		} else {
			if start != -1 {
				word := text[start:i]
				addToken(word)
				splitCamelCaseFast(word, addToken)
				start = -1
			}
		}
	}
	if start != -1 {
		word := text[start:]
		addToken(word)
		splitCamelCaseFast(word, addToken)
	}

	return tokens
}

func splitCamelCaseFast(s string, emit func(string)) {
	if len(s) == 0 {
		return
	}
	start := 0
	for i := 1; i < len(s); i++ {
		prev := s[i-1]
		curr := s[i]
		// Transition from lowercase letter or digit to uppercase letter
		if (prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9') && (curr >= 'A' && curr <= 'Z') {
			if i > start {
				emit(s[start:i])
				start = i
			}
		}
	}
	if start < len(s) && start > 0 {
		emit(s[start:])
	}
}

// FTSIndex provides in-memory BM25-style inverted indexing for symbols and docstrings.
type FTSIndex struct {
	mu sync.RWMutex

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
	fts.mu.Lock()
	defer fts.mu.Unlock()

	// If node already exists, replace it cleanly
	if _, exists := fts.nodes[n.SymbolID]; exists {
		fts.removeNodeLocked(n.SymbolID)
	}

	docID := n.SymbolID
	fts.nodes[docID] = n

	// Name (3x), Signature (2x), Docstring (2x), FilePath (1x), SymbolID (1x)
	nameTokens := TokenizeText(n.Name)
	sigTokens := TokenizeText(n.Signature)
	docTokens := TokenizeText(n.Docstring)
	pathTokens := TokenizeText(n.FilePath)
	idTokens := TokenizeText(n.SymbolID)

	docLen := 3*len(nameTokens) + 2*len(sigTokens) + 2*len(docTokens) + len(pathTokens) + len(idTokens)
	fts.docLengths[docID] = docLen
	fts.totalTokens += docLen
	fts.docCount++

	addWeightedTerms := func(terms []string, weight int) {
		for _, term := range terms {
			postings, ok := fts.index[term]
			if !ok {
				postings = make(map[string]int)
				fts.index[term] = postings
			}
			postings[docID] += weight
		}
	}

	addWeightedTerms(nameTokens, 3)
	addWeightedTerms(sigTokens, 2)
	addWeightedTerms(docTokens, 2)
	addWeightedTerms(pathTokens, 1)
	addWeightedTerms(idTokens, 1)
}

// RemoveNode removes a Node by SymbolID from the FTS index, updating doc count and postings.
func (fts *FTSIndex) RemoveNode(symbolID string) {
	fts.mu.Lock()
	defer fts.mu.Unlock()
	fts.removeNodeLocked(symbolID)
}

func (fts *FTSIndex) removeNodeLocked(symbolID string) {
	n, exists := fts.nodes[symbolID]
	if !exists {
		return
	}

	docLen := fts.docLengths[symbolID]
	fts.totalTokens -= docLen
	if fts.totalTokens < 0 {
		fts.totalTokens = 0
	}
	fts.docCount--
	if fts.docCount < 0 {
		fts.docCount = 0
	}
	delete(fts.docLengths, symbolID)
	delete(fts.nodes, symbolID)

	removeTerms := func(terms []string) {
		for _, term := range terms {
			if postings, ok := fts.index[term]; ok {
				delete(postings, symbolID)
				if len(postings) == 0 {
					delete(fts.index, term)
				}
			}
		}
	}

	removeTerms(TokenizeText(n.Name))
	removeTerms(TokenizeText(n.Signature))
	removeTerms(TokenizeText(n.Docstring))
	removeTerms(TokenizeText(n.FilePath))
	removeTerms(TokenizeText(n.SymbolID))
}

// HasNode checks if symbolID is in index.
func (fts *FTSIndex) HasNode(symbolID string) bool {
	fts.mu.RLock()
	defer fts.mu.RUnlock()
	_, exists := fts.nodes[symbolID]
	return exists
}

// Len returns the number of indexed documents.
func (fts *FTSIndex) Len() int {
	fts.mu.RLock()
	defer fts.mu.RUnlock()
	return fts.docCount
}

// Search ranks nodes matching query terms using BM25 relevance scoring.
func (fts *FTSIndex) Search(query string, kindFilter string, limit int) []ScoredNode {
	fts.mu.RLock()
	defer fts.mu.RUnlock()

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
