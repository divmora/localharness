package tools

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

var (
	// MockFetchFunc allows unit tests to mock url fetches
	MockFetchFunc func(url string) (string, string, error)

	webFetchClient = newSafeHTTPClient()

	reScript = regexp.MustCompile(`(?i)<script[^>]*>[\s\S]*?<\/script>`)
	reStyle  = regexp.MustCompile(`(?i)<style[^>]*>[\s\S]*?<\/style>`)
	reBlock  = regexp.MustCompile(`(?i)</?(p|div|h[1-6]|li|br|tr|td)[^>]*>`)
	reTags   = regexp.MustCompile(`<[^>]+>`)
)

func registerWebFetch(r *Registry) {
	r.Register("read_url_content", executeWebFetch, ToolSchema{
		Group:       ToolGroupRead,
		Name:        "read_url_content",
		Description: "Fetch content from a URL via HTTP request (invisible to USER). Use when: (1) extracting text from public pages, (2) reading static content/documentation, (3) batch processing multiple URLs, (4) speed is important, or (5) no visual interaction needed. Converts HTML to markdown. No JavaScript execution, no authentication. For pages requiring login, JavaScript, or USER visibility, use read_browser_page instead.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Url": map[string]interface{}{
					"type":        "string",
					"description": "URL to read content from",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{
				"Url",
				"ToolSummary",
				"ToolAction",
			},
		},
	})
}

func executeWebFetch(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	startTime := time.Now()
	wf := step.GetReadUrlContent()
	if wf == nil {
		return fmt.Errorf("read_url_content: missing action")
	}

	targetURL := wf.Url
	if targetURL == "" {
		return fmt.Errorf("read_url_content: Url is required")
	}

	// Validate URL scheme and protect against SSRF
	if _, err := validateURLForSSRF(targetURL); err != nil {
		return err
	}

	r.Logger().Info("executing web fetch", "url", targetURL)

	if MockFetchFunc != nil {
		content, contentType, err := MockFetchFunc(targetURL)
		if err != nil {
			return fmt.Errorf("web_fetch mock: %w", err)
		}
		wf.Content = content
		wf.ContentType = contentType

		completedTime := time.Now()
		timeFormat := "2006-01-02T15:04:05-07:00"
		wf.FormattedOutput = fmt.Sprintf("Created At: %s\nCompleted At: %s\n%s",
			startTime.Format(timeFormat), completedTime.Format(timeFormat), wf.Content)
		return nil
	}

	// Make HTTP GET request
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// Inject GitHub auth token if available to prevent 429 rate limit (60 req/hr unauthenticated)
	if strings.Contains(targetURL, "api.github.com") || strings.Contains(targetURL, "raw.githubusercontent.com") {
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			token = os.Getenv("GH_TOKEN")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/vnd.github.v3+json")
		}
	}

	resp, err := webFetchClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	lowerContentType := strings.ToLower(contentType)
	if !isTextContentType(lowerContentType) {
		return fmt.Errorf("unsupported content type: %s (only text, html, markdown, json, xml, javascript are supported)", contentType)
	}

	// Limit reader to 50KB to protect context window size
	limit := int64(51200)
	limitedReader := io.LimitReader(resp.Body, limit)

	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(bodyBytes)
	if int64(len(bodyBytes)) >= limit {
		content += "\n\n[Content truncated — response reached 50KB limit]"
	}
	if strings.Contains(lowerContentType, "html") {
		content = cleanHTMLContent(content)
	}

	wf.Content = content
	wf.ContentType = contentType

	completedTime := time.Now()
	timeFormat := "2006-01-02T15:04:05-07:00"
	wf.FormattedOutput = fmt.Sprintf("Created At: %s\nCompleted At: %s\n%s",
		startTime.Format(timeFormat), completedTime.Format(timeFormat), wf.Content)
	return nil
}

func isTextContentType(ct string) bool {
	return strings.Contains(ct, "text/") ||
		strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "application/xhtml+xml") ||
		strings.Contains(ct, "application/javascript")
}

func cleanHTMLContent(htmlStr string) string {
	// 1. Remove script blocks
	htmlStr = reScript.ReplaceAllString(htmlStr, " ")

	// 2. Remove style blocks
	htmlStr = reStyle.ReplaceAllString(htmlStr, " ")

	// 3. Replace structure tag closures with newlines to preserve paragraph/list flow
	htmlStr = reBlock.ReplaceAllString(htmlStr, "\n")

	// 4. Strip remaining HTML tags
	htmlStr = reTags.ReplaceAllString(htmlStr, " ")

	// 5. Unescape HTML entities
	htmlStr = html.UnescapeString(htmlStr)

	// 6. Remove excess spacing and collapse consecutive blank lines
	lines := strings.Split(htmlStr, "\n")
	var cleanedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanedLines = append(cleanedLines, trimmed)
		}
	}

	return strings.Join(cleanedLines, "\n")
}
