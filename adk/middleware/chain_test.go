package middleware

import (
	"context"
	"fmt"
	"testing"
)

// --- Test helpers ---

type mockPreTurn struct {
	name      string
	transform func(req *TurnRequest) *TurnRequest
	err       error
}

func (m *mockPreTurn) Name() string { return m.name }
func (m *mockPreTurn) PreTurn(_ context.Context, req *TurnRequest) (*TurnRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.transform != nil {
		return m.transform(req), nil
	}
	return req, nil
}

type mockPostTurn struct {
	name      string
	transform func(resp *TurnResponse) *TurnResponse
	err       error
}

func (m *mockPostTurn) Name() string { return m.name }
func (m *mockPostTurn) PostTurn(_ context.Context, resp *TurnResponse) (*TurnResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.transform != nil {
		return m.transform(resp), nil
	}
	return resp, nil
}

type mockStepMW struct {
	name   string
	filter bool
	err    error
}

func (m *mockStepMW) Name() string { return m.name }
func (m *mockStepMW) ProcessStep(_ context.Context, event *StepEvent) (*StepEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.filter {
		event.ShouldFilter = true
	}
	return event, nil
}

// mockMultiPhase implements all three interfaces.
type mockMultiPhase struct {
	name string
}

func (m *mockMultiPhase) Name() string { return m.name }
func (m *mockMultiPhase) PreTurn(_ context.Context, req *TurnRequest) (*TurnRequest, error) {
	req.Metadata["preTurn"] = true
	return req, nil
}
func (m *mockMultiPhase) PostTurn(_ context.Context, resp *TurnResponse) (*TurnResponse, error) {
	resp.Metadata["postTurn"] = true
	return resp, nil
}
func (m *mockMultiPhase) ProcessStep(_ context.Context, event *StepEvent) (*StepEvent, error) {
	event.Metadata["step"] = true
	return event, nil
}

// --- Tests ---

func TestChain_Empty(t *testing.T) {
	c := NewChain(nil)

	if !c.IsEmpty() {
		t.Fatal("empty chain should report IsEmpty")
	}
	if c.HasPreTurn() || c.HasPostTurn() || c.HasStep() {
		t.Fatal("empty chain should not have any phases")
	}
}

func TestChain_PreTurn_Order(t *testing.T) {
	var order []string

	m1 := &mockPreTurn{
		name: "first",
		transform: func(req *TurnRequest) *TurnRequest {
			order = append(order, "first")
			req.Prompt = req.Prompt + " [first]"
			return req
		},
	}
	m2 := &mockPreTurn{
		name: "second",
		transform: func(req *TurnRequest) *TurnRequest {
			order = append(order, "second")
			req.Prompt = req.Prompt + " [second]"
			return req
		},
	}

	c := NewChain(nil, m1, m2)

	if !c.HasPreTurn() {
		t.Fatal("should have PreTurn")
	}

	req := &TurnRequest{Prompt: "hello"}
	result, err := c.RunPreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if result.Prompt != "hello [first] [second]" {
		t.Fatalf("expected prompt chaining, got: %s", result.Prompt)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("expected order [first, second], got: %v", order)
	}
}

func TestChain_PostTurn_ReverseOrder(t *testing.T) {
	var order []string

	m1 := &mockPostTurn{
		name: "first",
		transform: func(resp *TurnResponse) *TurnResponse {
			order = append(order, "first")
			return resp
		},
	}
	m2 := &mockPostTurn{
		name: "second",
		transform: func(resp *TurnResponse) *TurnResponse {
			order = append(order, "second")
			return resp
		},
	}

	c := NewChain(nil, m1, m2)

	resp := &TurnResponse{Text: "done"}
	_, err := c.RunPostTurn(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}

	// PostTurn runs in REVERSE order
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("expected reverse order [second, first], got: %v", order)
	}
}

func TestChain_PreTurn_Error(t *testing.T) {
	m := &mockPreTurn{name: "failing", err: fmt.Errorf("boom")}
	c := NewChain(nil, m)

	_, err := c.RunPreTurn(context.Background(), &TurnRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error from failing middleware")
	}
	if err.Error() != `middleware "failing" PreTurn: boom` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChain_PostTurn_Error(t *testing.T) {
	m := &mockPostTurn{name: "failing", err: fmt.Errorf("boom")}
	c := NewChain(nil, m)

	_, err := c.RunPostTurn(context.Background(), &TurnResponse{Text: "hi"})
	if err == nil {
		t.Fatal("expected error from failing middleware")
	}
}

func TestChain_Step_Filter(t *testing.T) {
	m := &mockStepMW{name: "filter", filter: true}
	c := NewChain(nil, m)

	event := &StepEvent{EventType: 1, TextDelta: "hello"}
	result, err := c.RunStep(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShouldFilter {
		t.Fatal("expected event to be filtered")
	}
}

func TestChain_Step_Error(t *testing.T) {
	m := &mockStepMW{name: "errorer", err: fmt.Errorf("step error")}
	c := NewChain(nil, m)

	_, err := c.RunStep(context.Background(), &StepEvent{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChain_MultiPhase(t *testing.T) {
	m := &mockMultiPhase{name: "multi"}
	c := NewChain(nil, m)

	if !c.HasPreTurn() || !c.HasPostTurn() || !c.HasStep() {
		t.Fatal("multi-phase middleware should register for all phases")
	}

	// PreTurn
	req := &TurnRequest{Prompt: "test"}
	reqResult, err := c.RunPreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := reqResult.Metadata["preTurn"]; !ok || v != true {
		t.Fatal("PreTurn should set metadata")
	}

	// PostTurn
	resp := &TurnResponse{Text: "done"}
	respResult, err := c.RunPostTurn(context.Background(), resp)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := respResult.Metadata["postTurn"]; !ok || v != true {
		t.Fatal("PostTurn should set metadata")
	}

	// Step
	event := &StepEvent{EventType: 1}
	stepResult, err := c.RunStep(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := stepResult.Metadata["step"]; !ok || v != true {
		t.Fatal("ProcessStep should set metadata")
	}
}

func TestChain_MetadataInitialized(t *testing.T) {
	m := &mockPreTurn{name: "meta-check",
		transform: func(req *TurnRequest) *TurnRequest {
			// Metadata should be initialized even if caller didn't set it
			req.Metadata["key"] = "value"
			return req
		},
	}
	c := NewChain(nil, m)

	req := &TurnRequest{Prompt: "test"} // No Metadata set
	result, err := c.RunPreTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["key"] != "value" {
		t.Fatal("metadata should be accessible after middleware sets it")
	}
}
