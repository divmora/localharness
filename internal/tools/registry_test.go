package tools

import (
	"context"
	"sync"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestRegistryCloneIsolation(t *testing.T) {
	parent := NewRegistry(nil, nil)
	parentCalled := false
	parent.Register("test_tool", func(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
		parentCalled = true
		return nil
	}, ToolSchema{Name: "test_tool"})

	var parentEmitted *pb.StepUpdate
	parent.SetStepEmitter(func(step *pb.StepUpdate) {
		parentEmitted = step
	})

	child := parent.Clone()
	if !child.HasTool("test_tool") {
		t.Fatal("expected child to have test_tool")
	}

	// Overwrite child emitter
	var childEmitted *pb.StepUpdate
	child.SetStepEmitter(func(step *pb.StepUpdate) {
		childEmitted = step
	})

	// Emit on parent, check child didn't receive it
	pStep := &pb.StepUpdate{Text: "parent"}
	parent.EmitStep(pStep)
	if parentEmitted != pStep {
		t.Errorf("expected parent to receive parent step")
	}
	if childEmitted != nil {
		t.Errorf("child should not have received parent step")
	}

	// Emit on child, check parent didn't receive it
	cStep := &pb.StepUpdate{Text: "child"}
	child.EmitStep(cStep)
	if childEmitted != cStep {
		t.Errorf("expected child to receive child step")
	}
	if parentEmitted == cStep {
		t.Errorf("parent should not have received child step")
	}

	// Execute tool on child
	if err := child.Execute(context.Background(), "test_tool", &pb.StepUpdate{}); err != nil {
		t.Fatalf("unexpected error executing tool: %v", err)
	}
	if !parentCalled {
		t.Errorf("expected parentCalled to be true")
	}

	// Child task manager must be isolated
	if child.TaskManager() == parent.TaskManager() {
		t.Error("child task manager must be a distinct instance from parent")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	parent := NewRegistry(nil, nil)
	parent.Register("tool", func(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
		return nil
	}, ToolSchema{Name: "tool"})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cloned := parent.Clone()
			cloned.SetStepEmitter(func(step *pb.StepUpdate) {})
			cloned.EmitStep(&pb.StepUpdate{Text: "test"})
			_ = cloned.HasTool("tool")
			_ = cloned.Schemas()
			_ = cloned.SchemasAsJSON()
			_ = cloned.Execute(context.Background(), "tool", &pb.StepUpdate{})
		}(i)
	}
	wg.Wait()
}
