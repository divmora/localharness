package tools

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/workspace"
)

func TestWebSearchTool(t *testing.T) {
	// Setup registry
	logger := slog.Default()
	wsMgr, _ := workspace.NewManager([]string{"/tmp"})
	r := NewRegistry(wsMgr, logger)
	registerWebSearch(r)

	// Mock search implementation
	MockSearchFunc = func(query string) ([]*pb.WebSearchResult, error) {
		if query == "fizz" {
			return []*pb.WebSearchResult{
				{Title: "FizzTitle", Url: "http://fizz.com", Snippet: "FizzSnippet"},
			}, nil
		}
		return nil, errors.New("search error")
	}
	defer func() { MockSearchFunc = nil }()

	// Test successful search
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_SearchWeb{
			SearchWeb: &pb.ActionSearchWeb{
				Query: "fizz",
			},
		},
	}
	err := r.Execute(context.Background(), "search_web", step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ws := step.GetSearchWeb()
	if len(ws.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(ws.Results))
	}
	if ws.Results[0].Title != "FizzTitle" || ws.Results[0].Url != "http://fizz.com" {
		t.Errorf("unexpected search result content: %v", ws.Results[0])
	}

	// Test search error
	stepErr := &pb.StepUpdate{
		Action: &pb.StepUpdate_SearchWeb{
			SearchWeb: &pb.ActionSearchWeb{
				Query: "buzz",
			},
		},
	}
	err = r.Execute(context.Background(), "search_web", stepErr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWebFetchTool(t *testing.T) {
	// Setup registry
	logger := slog.Default()
	wsMgr, _ := workspace.NewManager([]string{"/tmp"})
	r := NewRegistry(wsMgr, logger)
	registerWebFetch(r)

	// Mock fetch implementation
	MockFetchFunc = func(url string) (string, string, error) {
		if url == "https://example.com" {
			return "Clean Content", "text/plain", nil
		}
		return "", "", errors.New("fetch error")
	}
	defer func() { MockFetchFunc = nil }()

	// Test successful fetch
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReadUrlContent{
			ReadUrlContent: &pb.ActionReadUrlContent{
				Url: "https://example.com",
			},
		},
	}
	err := r.Execute(context.Background(), "read_url_content", step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wf := step.GetReadUrlContent()
	if wf.Content != "Clean Content" || wf.ContentType != "text/plain" {
		t.Errorf("unexpected fetch content: %v", wf)
	}

	// Test fetch error
	stepErr := &pb.StepUpdate{
		Action: &pb.StepUpdate_ReadUrlContent{
			ReadUrlContent: &pb.ActionReadUrlContent{
				Url: "https://unknown.com",
			},
		},
	}
	err = r.Execute(context.Background(), "read_url_content", stepErr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
