package tools

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

var (
	// MockSearchFunc allows unit tests to mock search queries
	MockSearchFunc func(query string) ([]*pb.WebSearchResult, error)

	webSearchClient = &http.Client{
		Timeout: 15 * time.Second,
	}
)

func registerWebSearch(r *Registry) {
	r.Register("search_web", executeWebSearch, ToolSchema{
		Group: ToolGroupRead,
		Name:        "search_web",
		Description: "Perform a web search query and return a list of search results (title, url, snippet).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query string",
				},
			},
			"required": []string{"query"},
		},
	})
}

func executeWebSearch(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	ws := step.GetSearchWeb()
	if ws == nil {
		return fmt.Errorf("search_web: missing action")
	}

	query := ws.Query
	if query == "" {
		return fmt.Errorf("search_web: query is required")
	}

	r.Logger().Info("executing web search", "query", query)

	if MockSearchFunc != nil {
		results, err := MockSearchFunc(query)
		if err != nil {
			return fmt.Errorf("web_search mock: %w", err)
		}
		ws.Results = results
		return nil
	}

	// Perform real query using DuckDuckGo
	results, err := performDuckDuckGoSearch(ctx, query)
	if err != nil {
		return fmt.Errorf("search_web: %w", err)
	}

	ws.Results = results
	return nil
}

func performDuckDuckGoSearch(ctx context.Context, query string) ([]*pb.WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	// Set standard browser user agent to prevent 403 robot detection
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := webSearchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	htmlContent := string(bodyBytes)
	return parseDuckDuckGoHTML(htmlContent), nil
}

// parseDuckDuckGoHTML extracts search results from the DuckDuckGo HTML page.
func parseDuckDuckGoHTML(htmlContent string) []*pb.WebSearchResult {
	var results []*pb.WebSearchResult

	// DDG HTML search results are wrapped in result__body blocks
	parts := strings.Split(htmlContent, "class=\"result__body\"")
	if len(parts) <= 1 {
		// Fallback to class="result "
		parts = strings.Split(htmlContent, "class=\"result ")
	}

	// Skip first part as it precedes search results
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if len(results) >= 5 {
			break
		}

		// 1. Extract URL and Title
		// Expected pattern: class="result__a" href="[URL]">[TITLE]</a>
		urlReg := regexp.MustCompile(`class="result__a"[^>]*href="([^"]+)"[^>]*>([^<]+)`)
		urlMatches := urlReg.FindStringSubmatch(part)
		if len(urlMatches) < 3 {
			continue
		}

		rawURL := urlMatches[1]
		title := html.UnescapeString(strings.TrimSpace(urlMatches[2]))

		// Decode redirect URLs (e.g. /l/?kh=-1&uddg=https%3A%2F%2F...)
		finalURL := rawURL
		if strings.Contains(rawURL, "uddg=") {
			if u, err := url.Parse(rawURL); err == nil {
				if uddg := u.Query().Get("uddg"); uddg != "" {
					finalURL = uddg
				}
			}
		}

		// 2. Extract Snippet
		// Expected pattern: class="result__snippet"[^>]*>[SNIPPET]</a>
		snippetReg := regexp.MustCompile(`class="result__snippet"[^>]*>([^<]+)`)
		snippetMatches := snippetReg.FindStringSubmatch(part)
		snippet := ""
		if len(snippetMatches) >= 2 {
			snippet = html.UnescapeString(strings.TrimSpace(snippetMatches[1]))
		}

		if title != "" && finalURL != "" {
			results = append(results, &pb.WebSearchResult{
				Title:   title,
				Url:     finalURL,
				Snippet: snippet,
			})
		}
	}

	return results
}
