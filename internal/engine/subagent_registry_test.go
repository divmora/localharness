package engine

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewSubagentRegistry_BuiltinsIncluded(t *testing.T) {
	builtins := BuiltinSubagentTypes()
	r := NewSubagentRegistry(builtins, nil, nil, false)

	if r.Len() != len(builtins) {
		t.Errorf("expected %d types, got %d", len(builtins), r.Len())
	}

	research, ok := r.Get("research")
	if !ok {
		t.Fatal("expected 'research' type to be registered")
	}
	if !research.IsBuiltin {
		t.Error("research should be marked as built-in")
	}
	if research.EnableWriteTools {
		t.Error("research should NOT have write tools")
	}

	self, ok := r.Get("self")
	if !ok {
		t.Fatal("expected 'self' type to be registered")
	}
	if !self.EnableWriteTools {
		t.Error("self should have write tools")
	}
	if !self.EnableSubagentTools {
		t.Error("self should have subagent tools")
	}
}

func TestNewSubagentRegistry_ExcludeBuiltin(t *testing.T) {
	builtins := BuiltinSubagentTypes()
	r := NewSubagentRegistry(builtins, nil, []string{"self"}, false)

	if _, ok := r.Get("self"); ok {
		t.Error("'self' should be excluded")
	}
	if _, ok := r.Get("research"); !ok {
		t.Error("'research' should still be present")
	}
}

func TestNewSubagentRegistry_DisableAllBuiltins(t *testing.T) {
	builtins := BuiltinSubagentTypes()
	sdkTypes := []SubagentTypeDef{
		{Name: "custom", Description: "Custom type"},
	}

	r := NewSubagentRegistry(builtins, sdkTypes, nil, true)

	if _, ok := r.Get("research"); ok {
		t.Error("'research' should be excluded when all builtins disabled")
	}
	if _, ok := r.Get("self"); ok {
		t.Error("'self' should be excluded when all builtins disabled")
	}
	if _, ok := r.Get("custom"); !ok {
		t.Error("SDK type 'custom' should still be present")
	}
	if r.Len() != 1 {
		t.Errorf("expected 1 type, got %d", r.Len())
	}
}

func TestNewSubagentRegistry_SDKOverridesBuiltin(t *testing.T) {
	builtins := BuiltinSubagentTypes()
	sdkTypes := []SubagentTypeDef{
		{
			Name:             "research",
			Description:      "Custom research agent with write access",
			EnableWriteTools: true,
		},
	}

	r := NewSubagentRegistry(builtins, sdkTypes, nil, false)

	research, ok := r.Get("research")
	if !ok {
		t.Fatal("'research' should be present")
	}
	if research.IsBuiltin {
		t.Error("SDK-overridden type should not be marked as built-in")
	}
	if !research.EnableWriteTools {
		t.Error("SDK override should have write tools enabled")
	}
	if research.Description != "Custom research agent with write access" {
		t.Errorf("expected SDK description, got %q", research.Description)
	}
}

func TestDefine_AgentOverridesSDK(t *testing.T) {
	sdkTypes := []SubagentTypeDef{
		{Name: "reviewer", Description: "SDK reviewer"},
	}
	r := NewSubagentRegistry(nil, sdkTypes, nil, true)

	// Agent defines same name
	err := r.Define(SubagentTypeDef{
		Name:        "reviewer",
		Description: "Agent-defined reviewer with different capabilities",
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	reviewer, ok := r.Get("reviewer")
	if !ok {
		t.Fatal("'reviewer' should exist")
	}
	if reviewer.Description != "Agent-defined reviewer with different capabilities" {
		t.Errorf("agent-defined should override SDK, got %q", reviewer.Description)
	}
}

func TestDefine_EmptyName_Error(t *testing.T) {
	r := NewSubagentRegistry(nil, nil, nil, true)
	err := r.Define(SubagentTypeDef{Description: "no name"})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestDefine_NewType(t *testing.T) {
	r := NewSubagentRegistry(nil, nil, nil, true)

	err := r.Define(SubagentTypeDef{
		Name:             "test-writer",
		Description:      "Writes unit tests",
		SystemPrompt:     "You are a test writing specialist.",
		EnableWriteTools: true,
	})
	if err != nil {
		t.Fatalf("Define failed: %v", err)
	}

	tw, ok := r.Get("test-writer")
	if !ok {
		t.Fatal("'test-writer' should exist")
	}
	if !tw.EnableWriteTools {
		t.Error("should have write tools")
	}
	if tw.SystemPrompt != "You are a test writing specialist." {
		t.Error("system prompt mismatch")
	}
}

func TestGet_NotFound(t *testing.T) {
	r := NewSubagentRegistry(nil, nil, nil, true)
	if _, ok := r.Get("nonexistent"); ok {
		t.Error("should not find nonexistent type")
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	r := NewSubagentRegistry(nil, nil, nil, true)
	_ = r.Define(SubagentTypeDef{Name: "test", Description: "original"})

	t1, _ := r.Get("test")
	t1.Description = "mutated"

	t2, _ := r.Get("test")
	if t2.Description != "original" {
		t.Error("Get should return a copy — mutation should not affect registry")
	}
}

func TestList_Empty(t *testing.T) {
	r := NewSubagentRegistry(nil, nil, nil, true)
	if len(r.List()) != 0 {
		t.Error("empty registry should return empty list")
	}
}

func TestList_AllSources(t *testing.T) {
	builtins := BuiltinSubagentTypes()
	sdkTypes := []SubagentTypeDef{
		{Name: "custom-sdk", Description: "SDK type"},
	}

	r := NewSubagentRegistry(builtins, sdkTypes, nil, false)
	_ = r.Define(SubagentTypeDef{Name: "agent-defined", Description: "Runtime type"})

	list := r.List()
	// builtins (research, self) + SDK (custom-sdk) + agent (agent-defined) = 4
	if len(list) != 4 {
		t.Errorf("expected 4 types, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, t := range list {
		names[t.Name] = true
	}
	for _, expected := range []string{"research", "self", "custom-sdk", "agent-defined"} {
		if !names[expected] {
			t.Errorf("expected %q in list", expected)
		}
	}
}

func TestNames(t *testing.T) {
	r := NewSubagentRegistry(BuiltinSubagentTypes(), nil, nil, false)
	names := r.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestConcurrentDefine(t *testing.T) {
	r := NewSubagentRegistry(nil, nil, nil, true)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = r.Define(SubagentTypeDef{
				Name:        fmt.Sprintf("type-%d", n),
				Description: fmt.Sprintf("Type %d", n),
			})
		}(i)
	}
	wg.Wait()

	if r.Len() != 100 {
		t.Errorf("expected 100 types after concurrent define, got %d", r.Len())
	}
}
