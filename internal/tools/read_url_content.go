package tools

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

var (
	// MockFetchFunc allows unit tests to mock url fetches
	MockFetchFunc func(url string) (string, string, error)

	webFetchClient = &http.Client{
		Timeout: 15 * time.Second,
	}
)

func registerWebFetch(r *Registry) {
	r.Register("read_url_content", executeWebFetch, ToolSchema{
		Group: ToolGroupRead,
		Name:        "read_url_content",
		Description: "Fetch content from a URL via HTTP request. " +
			"Use this instead of run_command with curl or wget. " +
			"Converts HTML to clean plain text. No JavaScript execution, no authentication. " +
			"Use for extracting text from public pages, reading documentation, or processing static content.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The HTTP/HTTPS URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	})
}

func executeWebFetch(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	wf := step.GetReadUrlContent()
	if wf == nil {
		return fmt.Errorf("read_url_content: missing action")
	}

	targetURL := wf.Url
	if targetURL == "" {
		return fmt.Errorf("read_url_content: url is required")
	}

	// Simple scheme validation
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return fmt.Errorf("read_url_content: invalid scheme, only http and https are supported")
	}

	r.Logger().Info("executing web fetch", "url", targetURL)

	if MockFetchFunc != nil {
		content, contentType, err := MockFetchFunc(targetURL)
		if err != nil {
			return fmt.Errorf("web_fetch mock: %w", err)
		}
		wf.Content = content
		wf.ContentType = contentType
		return nil
	}

	// Make HTTP GET request
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

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
	if strings.Contains(lowerContentType, "html") {
		content = cleanHTMLContent(content)
	}

	wf.Content = content
	wf.ContentType = contentType
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
	reScript := regexp.MustCompile(`(?i)<script[^>]*>[\s\S]*?<\/script>`)
	htmlStr = reScript.ReplaceAllString(htmlStr, " ")

	// 2. Remove style blocks
	reStyle := regexp.MustCompile(`(?i)<style[^>]*>[\s\S]*?<\/style>`)
	htmlStr = reStyle.ReplaceAllString(htmlStr, " ")

	// 3. Replace structure tag closures with newlines to preserve paragraph/list flow
	reBlock := regexp.MustCompile(`(?i)</?(p|div|h[1-6]|li|br|tr|td)[^>]*>`)
	htmlStr = reBlock.ReplaceAllString(htmlStr, "\n")

	// 4. Strip remaining HTML tags
	reTags := regexp.MustCompile(`<[^>]+>`)
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
