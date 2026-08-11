package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
)

// MessageContextConfig holds dynamic per-message context that is prepended
// to user prompts before sending to the LLM.
type MessageContextConfig struct {
	// ConversationID is the active conversation identifier.
	ConversationID string

	// AppDataDir is the root data directory (e.g. ~/.divmora/localharness).
	// Included in <user_information> so the agent can resolve <appDataDir> placeholders.
	AppDataDir string

	// BrainDir is the path to the brain/artifacts directory for this conversation.
	BrainDir string

	// ProjectID is the stable UUID for the current project. Used by the agent
	// to resolve KI paths (<appDataDir>/knowledge/<project-id>/) and maintain
	// project-level context across conversations.
	ProjectID string

	// Workspaces are the active workspace definitions with optional corpus mappings.
	Workspaces []WorkspaceInfo

	// UserRules are the rules loaded from AGENTS.md files in workspace roots.
	// Nil if no AGENTS.md files were found.
	UserRules []config.UserRule

	// HostContext is the per-message metadata from the SDK/host (active file, cursor, etc.).
	// Nil if not provided.
	HostContext *pb.UserContext

	// PendingMessages are system-generated notifications (timer fires, task completions)
	// to inject as <SYSTEM_MESSAGE> blocks before the user's prompt.
	PendingMessages []string

	// EphemeralMessages are system control directives (harness/ADK-injected instructions)
	// to inject as <EPHEMERAL_MESSAGE> blocks. The agent must follow these strictly
	// but not acknowledge them to the user.
	EphemeralMessages []string

	// Skills are available skills to list in per-message context (reinforcement).
	Skills []SkillDef

	// Plugins are installed plugins to list in per-message context (reinforcement).
	Plugins []PluginDef

	// SlashCommands are available slash commands to list in per-message context.
	// Reinforcement after compaction. Empty if slash commands are disabled.
	SlashCommands []SlashCommandDef

	// SubagentTypes are available subagent types to list in per-message context.
	// Reinforcement after compaction. Empty if subagents are disabled.
	SubagentTypes []SubagentTypeDef

	// KnowledgeItems are available KI summaries for per-message injection.
	// Data-driven: when non-empty, a <knowledge_items> block is injected
	// with KI names, summaries, artifact paths, and staleness indicators.
	KnowledgeItems []KnowledgeItem

	// SettingsChanges are user-initiated settings changes since the last message.
	// Rendered as <USER_SETTINGS_CHANGE> blocks in the enriched prompt so the
	// model can adapt its behavior (e.g., after a model switch or mode toggle).
	// Single-use: consumed and cleared after each turn.
	SettingsChanges []SettingsChange
}

// WorkspaceInfo holds workspace metadata for per-message context.
type WorkspaceInfo struct {
	Directory  string // Absolute path (URI)
	CorpusName string // Semantic search corpus (e.g. "divmora/localharness"); empty = no corpus
}

// SettingsChange describes a user-initiated settings change.
// Go-side mirror of the proto SettingsChange message.
type SettingsChange struct {
	Setting  string // The setting that changed (e.g. "Model Selection")
	OldValue string // Previous value (empty if newly set)
	NewValue string // New value
	Hint     string // Optional model instruction (e.g. "No need to comment...")
}

// EnrichUserMessage prepends dynamic context sections to the user's prompt.
// The original prompt is wrapped in <USER_REQUEST> tags.
// Returns a slice of strings, one per XML section, for multi-part API calls.
//
// Part order (preserved as-is — highly optimized):
//
//  1. <user_information> — always present
//  2. <user_rules> — if user rules exist
//  3. <skills> — if skills exist
//  4. <plugins> — if plugins exist
//  5. <artifacts> — if brain dir set
//  6. <knowledge_items> — if KIs exist
//  7. <slash_commands> — if slash commands exist
//  8. <subagents> — if subagent types exist
//  9. <ADDITIONAL_METADATA> — if host context provided
//  10. <USER_SETTINGS_CHANGE> — per settings change
//  11. <EPHEMERAL_MESSAGE> — per ephemeral message
//  12. <SYSTEM_MESSAGE> — per pending notification
//  13. <USER_REQUEST> — always last
func EnrichUserMessage(prompt string, cfg MessageContextConfig) []string {
	var parts []string

	// Part 1: <user_information> — always present
	{
		var b strings.Builder
		b.WriteString("<user_information>\n")
		fmt.Fprintf(&b, "The USER's OS version is %s.\n", runtime.GOOS)
		if len(cfg.Workspaces) > 0 {
			// Check if any workspace has a corpus name → use URI→CorpusName mapping format.
			hasCorpus := false
			for _, ws := range cfg.Workspaces {
				if ws.CorpusName != "" {
					hasCorpus = true
					break
				}
			}
			if hasCorpus {
				fmt.Fprintf(&b, "The user has %d active workspaces, each defined by a URI and a CorpusName. Multiple URIs potentially map to the same CorpusName. The mapping is shown as follows in the format [URI] -> [CorpusName]:\n", len(cfg.Workspaces))
				for _, ws := range cfg.Workspaces {
					if ws.CorpusName != "" {
						fmt.Fprintf(&b, "%s -> %s\n", ws.Directory, ws.CorpusName)
					} else {
						fmt.Fprintf(&b, "%s\n", ws.Directory)
					}
				}
			} else {
				fmt.Fprintf(&b, "The user has %d active workspace(s):\n", len(cfg.Workspaces))
				for _, ws := range cfg.Workspaces {
					fmt.Fprintf(&b, "- %s\n", ws.Directory)
				}
			}
			b.WriteString("Code relating to the user's requests should be written in the locations listed above. Avoid writing project code files to tmp, in the .divmora dir, or directly to the Desktop and similar folders unless explicitly asked.\n")
		}
		if cfg.ConversationID != "" {
			fmt.Fprintf(&b, "Conversation ID: %s\n", cfg.ConversationID)
		}
		if cfg.AppDataDir != "" {
			fmt.Fprintf(&b, "App Data Directory: %s\n", cfg.AppDataDir)
		}
		if cfg.BrainDir != "" {
			fmt.Fprintf(&b, "Brain Directory: %s\n", cfg.BrainDir)
		}
		if cfg.ProjectID != "" {
			fmt.Fprintf(&b, "Project ID: %s\n", cfg.ProjectID)
		}
		b.WriteString("</user_information>")
		parts = append(parts, b.String())
	}

	// Part 2: <user_rules> — only if user rules exist (auto-discovered + ADK-injected)
	if len(cfg.UserRules) > 0 {
		// Pre-compute tag labels with multi-workspace disambiguation.
		tagLabels := userRuleTagLabels(cfg.UserRules)

		var b strings.Builder
		b.WriteString("<user_rules>\n")
		b.WriteString("The following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any following instructions.\n")
		b.WriteString("Review them carefully and always take them into account when you generate responses and code:\n")
		for i, rule := range cfg.UserRules {
			label := tagLabels[i]
			fmt.Fprintf(&b, "<RULE[%s]>\n", label)
			b.WriteString(rule.Content)
			if !strings.HasSuffix(rule.Content, "\n") {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "</RULE[%s]>\n", label)
		}
		b.WriteString("</user_rules>")
		parts = append(parts, b.String())
	}

	// Part 3: <skills> — list-only reinforcement (guidance is in system prompt)
	if len(cfg.Skills) > 0 {
		var b strings.Builder
		b.WriteString("<skills>\nAvailable skills:\n")
		for _, s := range cfg.Skills {
			if s.SkillPath != "" {
				fmt.Fprintf(&b, "- %s (%s): %s\n", s.Name, s.SkillPath, s.Description)
			} else {
				fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
			}
		}
		b.WriteString("</skills>")
		parts = append(parts, b.String())
	}

	// Part 4: <plugins> — list-only reinforcement (guidance is in system prompt)
	if len(cfg.Plugins) > 0 {
		var b strings.Builder
		b.WriteString("<plugins>\nAvailable plugins:\n")
		for _, p := range cfg.Plugins {
			if p.Path != "" {
				fmt.Fprintf(&b, "# %s (file://%s)\n", p.Name, p.Path)
			} else {
				fmt.Fprintf(&b, "# %s\n", p.Name)
			}
			if len(p.Skills) > 0 {
				b.WriteString("Skills:\n")
				for _, s := range p.Skills {
					if s.SkillPath != "" {
						fmt.Fprintf(&b, "- %s (%s): %s\n", s.Name, s.SkillPath, s.Description)
					} else {
						fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
					}
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("</plugins>")
		parts = append(parts, b.String())
	}

	// Part 5: <artifacts> — list artifacts in brain dir
	if cfg.BrainDir != "" {
		artifactList := listArtifacts(cfg.BrainDir)
		var b strings.Builder
		b.WriteString("<artifacts>\n")
		fmt.Fprintf(&b, "Artifact Directory Path: %s\n", cfg.BrainDir)
		if len(artifactList) > 0 {
			b.WriteString("Existing artifacts:\n")
			for _, a := range artifactList {
				fmt.Fprintf(&b, "- %s\n", a)
			}
		}
		b.WriteString("</artifacts>")
		parts = append(parts, b.String())
	}

	// Part 6: <knowledge_items> — KI summaries with artifact paths and staleness indicators
	if len(cfg.KnowledgeItems) > 0 {
		var b strings.Builder
		b.WriteString("<knowledge_items>\n")
		b.WriteString("# Knowledge Items (KI) Summaries\n\n")
		b.WriteString("The following KIs are available for this repository. **Check relevant KIs before starting any research.** Read artifacts with view_file.\n\n")
		for _, ki := range cfg.KnowledgeItems {
			if ki.Stale {
				fmt.Fprintf(&b, "### %s ⚠️ Potentially outdated (references changed since last update)\n", ki.Name)
			} else {
				fmt.Fprintf(&b, "### %s\n", ki.Name)
			}
			fmt.Fprintf(&b, "Summary: %s\n", ki.Summary)
			if len(ki.Artifacts) > 0 {
				b.WriteString("Artifacts:\n")
				for _, a := range ki.Artifacts {
					fmt.Fprintf(&b, "- %s\n", filepath.Join(ki.BasePath, "artifacts", a))
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("</knowledge_items>")
		parts = append(parts, b.String())
	}

	// Part 7: <slash_commands> — per-message reinforcement (after compaction)
	if len(cfg.SlashCommands) > 0 {
		var b strings.Builder
		b.WriteString("<slash_commands>\n")
		b.WriteString("Available slash commands you can recommend to the user:\n")
		for _, cmd := range cfg.SlashCommands {
			fmt.Fprintf(&b, "- %s: %s\n", cmd.Name, cmd.Description)
		}
		b.WriteString("</slash_commands>")
		parts = append(parts, b.String())
	}

	// Part 8: <subagents> — per-message reinforcement (after compaction)
	if len(cfg.SubagentTypes) > 0 {
		var b strings.Builder
		b.WriteString("<subagents>\n")
		b.WriteString("Available subagents:\n")
		for _, t := range cfg.SubagentTypes {
			fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
		}
		b.WriteString("\nAfter launching a subagent, you do NOT need to poll or check your inbox in a loop. The system will automatically notify you when the subagent sends a message. Simply proceed with other work or stop calling tools, and you will be notified when there is a message to process.\n")
		b.WriteString("</subagents>")
		parts = append(parts, b.String())
	}

	// Part 9: <ADDITIONAL_METADATA> — only if host context is provided
	if cfg.HostContext != nil {
		var b strings.Builder
		b.WriteString("<ADDITIONAL_METADATA>\n")
		fmt.Fprintf(&b, "The current local time is: %s.\n", time.Now().Format(time.RFC3339))

		// Only add prose framing when there's actual file/cursor state
		hasFileState := (cfg.HostContext.ActiveFile != nil && cfg.HostContext.ActiveFile.Path != "") ||
			cfg.HostContext.CursorLine > 0 ||
			len(cfg.HostContext.OpenFiles) > 0
		if hasFileState {
			b.WriteString("\nThe user's current state is as follows:\n")
		}

		if cfg.HostContext.ActiveFile != nil && cfg.HostContext.ActiveFile.Path != "" {
			if cfg.HostContext.ActiveFile.Language != "" {
				fmt.Fprintf(&b, "Active Document: %s (%s)\n", cfg.HostContext.ActiveFile.Path, cfg.HostContext.ActiveFile.Language)
			} else {
				fmt.Fprintf(&b, "Active Document: %s\n", cfg.HostContext.ActiveFile.Path)
			}
		}
		if cfg.HostContext.CursorLine > 0 {
			fmt.Fprintf(&b, "Cursor is on line: %d\n", cfg.HostContext.CursorLine)
		}
		if len(cfg.HostContext.OpenFiles) > 0 {
			b.WriteString("Other open documents:\n")
			for _, f := range cfg.HostContext.OpenFiles {
				if f.Language != "" {
					fmt.Fprintf(&b, "- %s (%s)\n", f.Path, f.Language)
				} else {
					fmt.Fprintf(&b, "- %s\n", f.Path)
				}
			}
		}
		for k, v := range cfg.HostContext.Extra {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
		b.WriteString("</ADDITIONAL_METADATA>")
		parts = append(parts, b.String())
	}

	// Part 10: <USER_SETTINGS_CHANGE> — user-initiated settings changes (one part per change)
	for _, sc := range cfg.SettingsChanges {
		var b strings.Builder
		b.WriteString("<USER_SETTINGS_CHANGE>\n")
		if sc.OldValue != "" {
			fmt.Fprintf(&b, "The user changed setting `%s` from %s to %s.\n", sc.Setting, sc.OldValue, sc.NewValue)
		} else {
			fmt.Fprintf(&b, "The user set `%s` to %s.\n", sc.Setting, sc.NewValue)
		}
		if sc.Hint != "" {
			b.WriteString(sc.Hint)
			if !strings.HasSuffix(sc.Hint, "\n") {
				b.WriteString("\n")
			}
		}
		b.WriteString("</USER_SETTINGS_CHANGE>")
		parts = append(parts, b.String())
	}

	// Part 11: <EPHEMERAL_MESSAGE> — system control directives (one part per message)
	for _, msg := range cfg.EphemeralMessages {
		var b strings.Builder
		b.WriteString("<EPHEMERAL_MESSAGE>\n")
		b.WriteString(msg)
		if !strings.HasSuffix(msg, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("</EPHEMERAL_MESSAGE>")
		parts = append(parts, b.String())
	}

	// Part 12: <SYSTEM_MESSAGE> — async notifications (one part per message)
	for _, msg := range cfg.PendingMessages {
		var b strings.Builder
		b.WriteString("<SYSTEM_MESSAGE>\n")
		b.WriteString("The following is a system notification, not a user message.\n\n")
		b.WriteString(msg)
		if !strings.HasSuffix(msg, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("</SYSTEM_MESSAGE>")
		parts = append(parts, b.String())
	}

	// Part 13: <USER_REQUEST> — always last
	parts = append(parts, fmt.Sprintf("<USER_REQUEST>\n%s\n</USER_REQUEST>", prompt))

	return parts
}


// userRuleTagLabels computes display labels for each UserRule.
// When all Filenames are unique, labels are just the filename (e.g. "AGENTS.md").
// When multiple rules share the same Filename from different workspaces,
// they are disambiguated with the workspace basename (e.g. "project-a/AGENTS.md").
// ADK-injected rules (empty WorkspaceDir) always use Filename directly.
func userRuleTagLabels(rules []config.UserRule) []string {
	labels := make([]string, len(rules))

	// Count how many times each filename appears (only for workspace-sourced rules).
	filenameCounts := make(map[string]int)
	for _, r := range rules {
		if r.WorkspaceDir != "" {
			filenameCounts[r.Filename]++
		}
	}

	for i, r := range rules {
		if r.WorkspaceDir != "" && filenameCounts[r.Filename] > 1 {
			// Disambiguate: "project-name/AGENTS.md"
			labels[i] = filepath.Base(r.WorkspaceDir) + "/" + r.Filename
		} else {
			labels[i] = r.Filename
		}
	}

	return labels
}

// listArtifacts returns the names of files in the brain directory (non-recursive).
func listArtifacts(brainDir string) []string {
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Skip hidden/system files
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, filepath.Base(e.Name()))
	}
	return names
}
