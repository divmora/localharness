// Package engine — subagent type registry.
//
// SubagentRegistry merges subagent types from three sources:
//  1. Built-in (LH core): "research", "self"
//  2. SDK-registered (host app config)
//  3. Agent-defined (runtime, via define_subagent tool)
//
// Merge priority: Agent-defined > SDK-registered > Built-in.
// Thread-safe for concurrent access.
package engine

import (
	"fmt"
	"sync"
)

// SubagentRegistry stores and merges subagent type definitions.
type SubagentRegistry struct {
	mu    sync.RWMutex
	types map[string]*SubagentTypeDef
}

// NewSubagentRegistry creates a registry pre-populated with built-in and SDK types.
//
// Parameters:
//   - builtins: LH built-in types (e.g. "research", "self")
//   - sdkTypes: SDK-registered types from host app config
//   - excludeBuiltins: names of built-in types to exclude
//   - disableAllBuiltins: if true, all built-in types are excluded
func NewSubagentRegistry(builtins, sdkTypes []SubagentTypeDef, excludeBuiltins []string, disableAllBuiltins bool) *SubagentRegistry {
	r := &SubagentRegistry{
		types: make(map[string]*SubagentTypeDef),
	}

	// Layer 1: Built-in types (unless excluded)
	if !disableAllBuiltins {
		excludeSet := make(map[string]bool, len(excludeBuiltins))
		for _, name := range excludeBuiltins {
			excludeSet[name] = true
		}
		for i := range builtins {
			if !excludeSet[builtins[i].Name] {
				t := builtins[i] // copy
				t.IsBuiltin = true
				r.types[t.Name] = &t
			}
		}
	}

	// Layer 2: SDK-registered types (override built-in if same name)
	for i := range sdkTypes {
		t := sdkTypes[i] // copy
		t.IsBuiltin = false
		r.types[t.Name] = &t
	}

	return r
}

// Define registers an agent-defined subagent type at runtime.
// This is called by the define_subagent tool during a conversation.
// Overwrites any existing type with the same name (including built-in and SDK types).
func (r *SubagentRegistry) Define(t SubagentTypeDef) error {
	if t.Name == "" {
		return fmt.Errorf("subagent type name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	t.IsBuiltin = false
	r.types[t.Name] = &t
	return nil
}

// Get looks up a subagent type by name. Returns the type and whether it was found.
func (r *SubagentRegistry) Get(name string) (*SubagentTypeDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.types[name]
	if !ok {
		return nil, false
	}

	// Return a copy to prevent mutation
	copy := *t
	return &copy, true
}

// List returns all registered subagent types, sorted by name.
func (r *SubagentRegistry) List() []SubagentTypeDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SubagentTypeDef, 0, len(r.types))
	for _, t := range r.types {
		result = append(result, *t)
	}
	return result
}

// Names returns the names of all registered types.
func (r *SubagentRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.types))
	for name := range r.types {
		names = append(names, name)
	}
	return names
}

// Len returns the number of registered types.
func (r *SubagentRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.types)
}
