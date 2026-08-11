// Package personas defines the Zenith agent persona system.
//
// Each persona (bolt, sentinel, palette) follows a scan → pick → fix → verify
// pattern but targets a different domain:
//
//   - bolt:     ⚡ Performance optimization
//   - sentinel: 🛡️ Security hardening
//   - palette:  🎨 UX/accessibility improvement
//
// Personas inject into the system prompt's Tier 2 (user content) via
// StructuredPrompt — they augment the agent's default identity rather than
// replacing it.
package personas

import "github.com/divmora/localharness/adk"

// Persona defines the contract for an agent persona.
type Persona interface {
	// Name returns the unique identifier (e.g., "bolt", "sentinel").
	Name() string

	// Description returns a short human-readable summary.
	Description() string

	// Prompt returns the StructuredPrompt for this persona.
	// Identity MUST be empty — personas augment the default identity.
	Prompt() *adk.StructuredPrompt

	// DefaultMessage returns the default chat message to kick off the agent.
	DefaultMessage() string

	// JournalFile returns the filename for this persona's journal
	// (e.g., "bolt.md"). Stored under .zenith/ in the workspace root.
	JournalFile() string
}

// registry holds all registered personas, keyed by Name().
var registry = map[string]Persona{}

// Register adds a persona to the global registry.
func Register(p Persona) {
	registry[p.Name()] = p
}

// Get returns a persona by name and whether it was found.
func Get(name string) (Persona, bool) {
	p, ok := registry[name]
	return p, ok
}

// All returns a copy of the full registry map.
func All() map[string]Persona {
	out := make(map[string]Persona, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}
