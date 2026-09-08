package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHandleAgentCard(t *testing.T) {
	logger := slog.Default()
	s := NewServer("test-api-key", logger)

	// Bind to a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Start server in background
	go s.StartWithListener(context.Background(), ln)

	// Give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	addr := ln.Addr().String()

	t.Run("default agent card", func(t *testing.T) {
		resp, err := http.Get("http://" + addr + "/.well-known/agent.json")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		ct := resp.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %s", ct)
		}

		body, _ := io.ReadAll(resp.Body)
		var card AgentCard
		if err := json.Unmarshal(body, &card); err != nil {
			t.Fatalf("failed to decode agent card: %v", err)
		}

		if card.Name == "" {
			t.Error("expected non-empty agent card name")
		}
		if card.Version == "" {
			t.Error("expected non-empty agent card version")
		}
		if !card.Capabilities.Streaming {
			t.Error("expected streaming to be true")
		}
		if len(card.Skills) == 0 {
			t.Error("expected at least one skill")
		}
		if len(card.DefaultInputModes) == 0 {
			t.Error("expected at least one input mode")
		}
	})

	t.Run("custom agent card", func(t *testing.T) {
		custom := &AgentCard{
			Name:    "CustomBot",
			Version: "1.0.0",
			Capabilities: AgentCardCapabilities{
				Streaming: false,
			},
			DefaultInputModes:  []string{"application/json"},
			DefaultOutputModes: []string{"application/json"},
		}
		s.SetAgentCard(custom)

		resp, err := http.Get("http://" + addr + "/.well-known/agent.json")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var card AgentCard
		if err := json.Unmarshal(body, &card); err != nil {
			t.Fatalf("failed to decode agent card: %v", err)
		}

		if card.Name != "CustomBot" {
			t.Errorf("expected name CustomBot, got %s", card.Name)
		}
		if card.Version != "1.0.0" {
			t.Errorf("expected version 1.0.0, got %s", card.Version)
		}
		if card.Capabilities.Streaming {
			t.Error("expected streaming to be false for custom card")
		}
	})

	t.Run("CORS header", func(t *testing.T) {
		resp, err := http.Get("http://" + addr + "/.well-known/agent.json")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		cors := resp.Header.Get("Access-Control-Allow-Origin")
		if cors != "*" {
			t.Errorf("expected CORS header *, got %q", cors)
		}
	})
}
