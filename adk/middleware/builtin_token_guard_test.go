package middleware

import (
	"context"
	"testing"
)

func TestTokenGuard_Unlimited(t *testing.T) {
	guard := NewTokenGuard(0, 0, nil) // No limit

	req := &TurnRequest{Prompt: "hello"}
	_, err := guard.PreTurn(context.Background(), req)
	if err != nil {
		t.Fatalf("unlimited guard should not block PreTurn: %v", err)
	}

	resp := &TurnResponse{TotalTokens: 50000, Metadata: make(map[string]any)}
	_, err = guard.PostTurn(context.Background(), resp)
	if err != nil {
		t.Fatalf("unlimited guard should not error on PostTurn: %v", err)
	}

	if guard.TotalTokens() != 50000 {
		t.Fatalf("expected 50000 tokens, got %d", guard.TotalTokens())
	}
	if guard.BudgetExhausted() {
		t.Fatal("should not be exhausted with no limit")
	}
}

func TestTokenGuard_WithLimit(t *testing.T) {
	guard := NewTokenGuard(100, 0.8, nil)

	// First turn: 70 tokens (under warning)
	resp := &TurnResponse{TotalTokens: 70, Metadata: make(map[string]any)}
	_, err := guard.PostTurn(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if guard.TotalTokens() != 70 {
		t.Fatalf("expected 70 tokens, got %d", guard.TotalTokens())
	}

	// Second turn: 20 tokens (total 90 — above warning but below limit)
	resp = &TurnResponse{TotalTokens: 20, Metadata: make(map[string]any)}
	_, err = guard.PostTurn(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}

	// Third turn: 20 tokens (total 110 — over limit)
	resp = &TurnResponse{TotalTokens: 20, Metadata: make(map[string]any)}
	_, err = guard.PostTurn(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if !guard.BudgetExhausted() {
		t.Fatal("should be exhausted at 110/100")
	}

	// Exhausted metadata flag
	if v, ok := resp.Metadata["token_budget_exhausted"]; !ok || v != true {
		t.Fatal("expected token_budget_exhausted metadata")
	}

	// Next PreTurn should be blocked
	_, err = guard.PreTurn(context.Background(), &TurnRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("should block PreTurn when exhausted")
	}
}

func TestTokenGuard_Reset(t *testing.T) {
	guard := NewTokenGuard(100, 0.8, nil)

	resp := &TurnResponse{TotalTokens: 110, Metadata: make(map[string]any)}
	guard.PostTurn(context.Background(), resp)

	if !guard.BudgetExhausted() {
		t.Fatal("should be exhausted")
	}

	guard.Reset()

	if guard.BudgetExhausted() {
		t.Fatal("should not be exhausted after reset")
	}
	if guard.TotalTokens() != 0 {
		t.Fatalf("expected 0 tokens after reset, got %d", guard.TotalTokens())
	}

	// Should allow PreTurn again
	_, err := guard.PreTurn(context.Background(), &TurnRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("should allow PreTurn after reset: %v", err)
	}
}

func TestTokenGuard_ZeroTokens(t *testing.T) {
	guard := NewTokenGuard(100, 0.8, nil)

	// Turn with zero tokens should be a no-op
	resp := &TurnResponse{TotalTokens: 0, Metadata: make(map[string]any)}
	_, err := guard.PostTurn(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if guard.TotalTokens() != 0 {
		t.Fatalf("expected 0 tokens, got %d", guard.TotalTokens())
	}
}

func TestTokenGuard_Name(t *testing.T) {
	guard := NewTokenGuard(100, 0.8, nil)
	if guard.Name() != "token_guard" {
		t.Fatalf("expected 'token_guard', got %q", guard.Name())
	}
}
