package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
)

// joinParts joins multi-part result into a single string for test assertions.
// This preserves existing test semantics (checking content in the joined output)
// while EnrichUserMessage now returns []string.
func joinParts(parts []string) string {
	return strings.Join(parts, "\n")
}

func TestEnrichUserMessage_Full(t *testing.T) {
	// Create temp brain dir with artifacts
	brainDir := t.TempDir()
	os.WriteFile(filepath.Join(brainDir, "plan.md"), []byte("plan"), 0644)
	os.WriteFile(filepath.Join(brainDir, "task.md"), []byte("task"), 0644)
	os.WriteFile(filepath.Join(brainDir, ".hidden"), []byte("skip"), 0644)

	parts := EnrichUserMessage("fix the bug", MessageContextConfig{
		ConversationID: "conv-123",
		AppDataDir:     "/home/user/.divmora/localharness",
		BrainDir:       brainDir,
		ProjectID:      "proj-uuid-456",
		Workspaces:     []WorkspaceInfo{{Directory: "/home/user/project"}},
		UserRules:      []config.UserRule{{Filename: "AGENTS.md", Content: "# AGENTS.md\nKeep docs in sync."}},
		HostContext: &pb.UserContext{
			ActiveFile: &pb.FileInfo{Path: "/home/user/project/main.go"},
			CursorLine: 42,
			OpenFiles: []*pb.FileInfo{
				{Path: "/home/user/project/main.go"},
				{Path: "/home/user/project/test.go"},
			},
		},
	})

	result := joinParts(parts)

	// Verify multi-part structure
	if len(parts) < 5 {
		t.Errorf("expected at least 5 parts (user_info, user_rules, artifacts, metadata, user_request), got %d", len(parts))
	}

	// First part should be user_information
	if !strings.Contains(parts[0], "<user_information>") {
		t.Error("first part should be <user_information>")
	}

	// Last part should be USER_REQUEST
	if !strings.Contains(parts[len(parts)-1], "<USER_REQUEST>") {
		t.Error("last part should be <USER_REQUEST>")
	}

	// Check user_information
	if !strings.Contains(result, "<user_information>") {
		t.Error("should contain <user_information>")
	}
	if !strings.Contains(result, "conv-123") {
		t.Error("should contain conversation ID")
	}
	if !strings.Contains(result, "/home/user/project") {
		t.Error("should contain workspace directory")
	}
	if !strings.Contains(result, "App Data Directory: /home/user/.divmora/localharness") {
		t.Error("should contain App Data Directory")
	}
	if !strings.Contains(result, "Project ID: proj-uuid-456") {
		t.Error("should contain Project ID")
	}

	// Check user_rules
	if !strings.Contains(result, "<user_rules>") {
		t.Error("should contain <user_rules>")
	}
	if !strings.Contains(result, "MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION") {
		t.Error("should contain preamble directive")
	}
	if !strings.Contains(result, "<RULE[AGENTS.md]>") {
		t.Error("should contain <RULE[AGENTS.md]> tag")
	}
	if !strings.Contains(result, "Keep docs in sync") {
		t.Error("should contain AGENTS.md content")
	}
	if !strings.Contains(result, "</RULE[AGENTS.md]>") {
		t.Error("should contain closing </RULE[AGENTS.md]> tag")
	}

	// Check artifacts
	if !strings.Contains(result, "<artifacts>") {
		t.Error("should contain <artifacts>")
	}
	if !strings.Contains(result, "plan.md") {
		t.Error("should list plan.md artifact")
	}
	if !strings.Contains(result, "task.md") {
		t.Error("should list task.md artifact")
	}
	if strings.Contains(result, ".hidden") {
		t.Error("should NOT list hidden files")
	}

	// Check ADDITIONAL_METADATA
	if !strings.Contains(result, "<ADDITIONAL_METADATA>") {
		t.Error("should contain <ADDITIONAL_METADATA>")
	}
	if !strings.Contains(result, "The current local time is:") {
		t.Error("should contain local time")
	}
	if !strings.Contains(result, "The user's current state is as follows:") {
		t.Error("should contain prose framing")
	}
	if !strings.Contains(result, "main.go") {
		t.Error("should contain active file")
	}
	if !strings.Contains(result, "Cursor is on line: 42") {
		t.Error("should contain cursor line with IDE field name")
	}
	if !strings.Contains(result, "Other open documents:") {
		t.Error("should use 'Other open documents' label")
	}

	// Check user request wrapping
	if !strings.Contains(result, "<USER_REQUEST>") {
		t.Error("should wrap prompt in <USER_REQUEST>")
	}
	if !strings.Contains(result, "fix the bug") {
		t.Error("should contain original prompt")
	}
}

func TestEnrichUserMessage_ProjectID(t *testing.T) {
	// With ProjectID — should appear in user_information
	parts := EnrichUserMessage("test", MessageContextConfig{
		ProjectID: "abc-123-def",
	})
	result := joinParts(parts)

	if !strings.Contains(result, "Project ID: abc-123-def") {
		t.Error("should contain Project ID in user_information")
	}

	// Without ProjectID — should NOT appear
	parts = EnrichUserMessage("test", MessageContextConfig{})
	result = joinParts(parts)
	if strings.Contains(result, "Project ID:") {
		t.Error("should NOT contain Project ID when empty")
	}
}

func TestEnrichUserMessage_Minimal(t *testing.T) {
	parts := EnrichUserMessage("hello", MessageContextConfig{})
	result := joinParts(parts)

	// Should always have user_information
	if !strings.Contains(result, "<user_information>") {
		t.Error("should always have <user_information>")
	}

	// Should NOT have optional sections
	if strings.Contains(result, "<user_rules>") {
		t.Error("should NOT have <user_rules> when empty")
	}
	if strings.Contains(result, "<artifacts>") {
		t.Error("should NOT have <artifacts> when no brain dir")
	}
	if strings.Contains(result, "<ADDITIONAL_METADATA>") {
		t.Error("should NOT have <ADDITIONAL_METADATA> when nil")
	}

	// Should still wrap prompt
	if !strings.Contains(result, "<USER_REQUEST>") {
		t.Error("should wrap prompt in <USER_REQUEST>")
	}
	if !strings.Contains(result, "hello") {
		t.Error("should contain original prompt")
	}

	// Minimal should have exactly 2 parts: user_information and USER_REQUEST
	if len(parts) != 2 {
		t.Errorf("minimal config should produce exactly 2 parts, got %d", len(parts))
	}
}

func TestEnrichUserMessage_NoHostContext(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		ConversationID: "conv-1",
		UserRules:      []config.UserRule{{Filename: "AGENTS.md", Content: "Rule 1"}},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "<user_rules>") {
		t.Error("should include user_rules")
	}
	if strings.Contains(result, "<ADDITIONAL_METADATA>") {
		t.Error("should NOT include ADDITIONAL_METADATA when nil")
	}
}

func TestEnrichUserMessage_MultiWorkspaceDisambiguation(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		UserRules: []config.UserRule{
			{Filename: "AGENTS.md", Content: "rules from project-a", WorkspaceDir: "/home/user/project-a"},
			{Filename: "AGENTS.md", Content: "rules from project-b", WorkspaceDir: "/home/user/project-b"},
		},
	})
	result := joinParts(parts)

	// When two rules share the same Filename, they should be disambiguated
	if !strings.Contains(result, "<RULE[project-a/AGENTS.md]>") {
		t.Error("should disambiguate with workspace basename: project-a/AGENTS.md")
	}
	if !strings.Contains(result, "<RULE[project-b/AGENTS.md]>") {
		t.Error("should disambiguate with workspace basename: project-b/AGENTS.md")
	}
	if !strings.Contains(result, "rules from project-a") {
		t.Error("should contain project-a rules")
	}
	if !strings.Contains(result, "rules from project-b") {
		t.Error("should contain project-b rules")
	}
}

func TestEnrichUserMessage_SingleWorkspace_NoDisambiguation(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		UserRules: []config.UserRule{
			{Filename: "AGENTS.md", Content: "single workspace rules", WorkspaceDir: "/home/user/project"},
		},
	})
	result := joinParts(parts)

	// Single workspace should use plain filename
	if !strings.Contains(result, "<RULE[AGENTS.md]>") {
		t.Error("single workspace should use plain filename")
	}
	if strings.Contains(result, "project/AGENTS.md") {
		t.Error("should NOT prefix with workspace when only one rule")
	}
}

func TestEnrichUserMessage_SDKAndWorkspaceRulesMerged(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		UserRules: []config.UserRule{
			{Filename: "team-standards", Content: "Always write tests."},          // SDK rule
			{Filename: "AGENTS.md", Content: "Keep docs in sync.", WorkspaceDir: "/home/user/project"}, // Workspace rule
		},
	})
	result := joinParts(parts)

	// Both rules should appear
	if !strings.Contains(result, "<RULE[team-standards]>") {
		t.Error("should contain SDK rule with label")
	}
	if !strings.Contains(result, "Always write tests.") {
		t.Error("should contain SDK rule content")
	}
	if !strings.Contains(result, "<RULE[AGENTS.md]>") {
		t.Error("should contain workspace AGENTS.md rule")
	}
	if !strings.Contains(result, "Keep docs in sync.") {
		t.Error("should contain workspace rule content")
	}
	// Preamble should be present
	if !strings.Contains(result, "MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION") {
		t.Error("should contain preamble")
	}
}

func TestEnrichUserMessage_HostContextExtra(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		HostContext: &pb.UserContext{
			Extra: map[string]string{
				"Language": "Go",
				"Theme":    "Dark",
			},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "<ADDITIONAL_METADATA>") {
		t.Error("should include ADDITIONAL_METADATA")
	}
	if !strings.Contains(result, "Language: Go") {
		t.Error("should include extra metadata")
	}
	// No file/cursor state — prose framing should NOT appear
	if strings.Contains(result, "The user's current state is as follows:") {
		t.Error("should NOT have prose framing when no file/cursor state")
	}
}

func TestEnrichUserMessage_LanguageAnnotations(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		HostContext: &pb.UserContext{
			ActiveFile: &pb.FileInfo{Path: "/project/main.go", Language: "LANGUAGE_GO"},
			OpenFiles: []*pb.FileInfo{
				{Path: "/project/utils.py", Language: "LANGUAGE_PYTHON"},
				{Path: "/project/readme.md"},
			},
		},
	})
	result := joinParts(parts)

	// Active file should have language annotation
	if !strings.Contains(result, "/project/main.go (LANGUAGE_GO)") {
		t.Error("active file should have language annotation")
	}
	// Open file with language should have annotation
	if !strings.Contains(result, "/project/utils.py (LANGUAGE_PYTHON)") {
		t.Error("open file with language should have annotation")
	}
	// Open file without language should NOT have annotation
	if strings.Contains(result, "readme.md (") {
		t.Error("open file without language should not have annotation")
	}
	if !strings.Contains(result, "- /project/readme.md") {
		t.Error("open file without language should still be listed")
	}
}

func TestEnrichUserMessage_NoLanguageAnnotations(t *testing.T) {
	// When no language fields are set, paths should be bare (backward compat)
	parts := EnrichUserMessage("test", MessageContextConfig{
		HostContext: &pb.UserContext{
			ActiveFile: &pb.FileInfo{Path: "/project/main.go"},
			CursorLine: 10,
			OpenFiles:  []*pb.FileInfo{{Path: "/project/test.go"}},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "Active Document: /project/main.go\n") {
		t.Error("active file should be bare path without language")
	}
	if !strings.Contains(result, "- /project/test.go\n") {
		t.Error("open file should be bare path without language")
	}
}

func TestEnrichUserMessage_PendingMessages(t *testing.T) {
	parts := EnrichUserMessage("hello", MessageContextConfig{
		PendingMessages: []string{
			"[timer] Task sched-abc:\nCheck build status",
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "<SYSTEM_MESSAGE>") {
		t.Error("should contain <SYSTEM_MESSAGE> block")
	}
	if !strings.Contains(result, "system notification, not a user message") {
		t.Error("should contain system notification preamble")
	}
	if !strings.Contains(result, "Check build status") {
		t.Error("should contain notification content")
	}
	if !strings.Contains(result, "</SYSTEM_MESSAGE>") {
		t.Error("should close SYSTEM_MESSAGE tag")
	}

	// SYSTEM_MESSAGE should come before USER_REQUEST
	sysIdx := strings.Index(result, "<SYSTEM_MESSAGE>")
	userIdx := strings.Index(result, "<USER_REQUEST>")
	if sysIdx > userIdx {
		t.Error("SYSTEM_MESSAGE should come before USER_REQUEST")
	}
}

func TestEnrichUserMessage_MultiplePendingMessages(t *testing.T) {
	parts := EnrichUserMessage("do stuff", MessageContextConfig{
		PendingMessages: []string{
			"[timer] Task sched-1:\nFirst notification",
			"[completed] Task task-2:\nBuild finished",
		},
	})
	result := joinParts(parts)

	// Should have two separate SYSTEM_MESSAGE blocks
	count := strings.Count(result, "<SYSTEM_MESSAGE>")
	if count != 2 {
		t.Errorf("expected 2 SYSTEM_MESSAGE blocks, got %d", count)
	}
	if !strings.Contains(result, "First notification") {
		t.Error("should contain first notification")
	}
	if !strings.Contains(result, "Build finished") {
		t.Error("should contain second notification")
	}
}

func TestEnrichUserMessage_NoPendingMessages(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		PendingMessages: nil,
	})
	result := joinParts(parts)

	if strings.Contains(result, "<SYSTEM_MESSAGE>") {
		t.Error("should NOT contain SYSTEM_MESSAGE when no pending messages")
	}
}

func TestEnrichUserMessage_EphemeralMessages(t *testing.T) {
	parts := EnrichUserMessage("test prompt", MessageContextConfig{
		EphemeralMessages: []string{"Focus on security. Do not use eval()."},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "<EPHEMERAL_MESSAGE>") {
		t.Error("should contain <EPHEMERAL_MESSAGE> block")
	}
	if !strings.Contains(result, "Focus on security") {
		t.Error("should contain ephemeral message content")
	}
	if !strings.Contains(result, "</EPHEMERAL_MESSAGE>") {
		t.Error("should close EPHEMERAL_MESSAGE tag")
	}

	// EPHEMERAL_MESSAGE should come before USER_REQUEST
	ephIdx := strings.Index(result, "<EPHEMERAL_MESSAGE>")
	userIdx := strings.Index(result, "<USER_REQUEST>")
	if ephIdx > userIdx {
		t.Error("EPHEMERAL_MESSAGE should come before USER_REQUEST")
	}
}

func TestEnrichUserMessage_EphemeralBeforeSystem(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		EphemeralMessages: []string{"Be concise."},
		PendingMessages:   []string{"Build completed."},
	})
	result := joinParts(parts)

	ephIdx := strings.Index(result, "<EPHEMERAL_MESSAGE>")
	sysIdx := strings.Index(result, "<SYSTEM_MESSAGE>")

	if ephIdx == -1 {
		t.Fatal("should have EPHEMERAL_MESSAGE")
	}
	if sysIdx == -1 {
		t.Fatal("should have SYSTEM_MESSAGE")
	}
	if ephIdx > sysIdx {
		t.Error("EPHEMERAL_MESSAGE (directives) should come before SYSTEM_MESSAGE (notifications)")
	}
}

func TestEnrichUserMessage_NoEphemeralMessages(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		EphemeralMessages: nil,
	})
	result := joinParts(parts)

	if strings.Contains(result, "<EPHEMERAL_MESSAGE>") {
		t.Error("should NOT contain EPHEMERAL_MESSAGE when none provided")
	}
}

func TestEnrichUserMessage_KnowledgeItems(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		KnowledgeItems: []KnowledgeItem{
			{
				Name:      "error-patterns",
				Summary:   "Error handling patterns for Go services.",
				Artifacts: []string{"overview.md", "patterns.md"},
				BasePath:  "/data/knowledge/uuid1/error-patterns",
			},
			{
				Name:      "api-conventions",
				Summary:   "API naming conventions.",
				Artifacts: []string{"api_surface.md"},
				BasePath:  "/data/knowledge/uuid1/api-conventions",
				Stale:     true,
			},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "<knowledge_items>") {
		t.Error("should contain <knowledge_items> block")
	}
	if !strings.Contains(result, "### error-patterns") {
		t.Error("should list error-patterns KI")
	}
	if !strings.Contains(result, "Error handling patterns for Go services.") {
		t.Error("should include KI summary")
	}
	if !strings.Contains(result, "/data/knowledge/uuid1/error-patterns/artifacts/overview.md") {
		t.Error("should include artifact path")
	}

	// Stale KI should have warning marker
	if !strings.Contains(result, "### api-conventions ⚠️ Potentially outdated") {
		t.Error("stale KI should have warning marker with ### heading")
	}

	// Non-stale KI should NOT have warning
	if strings.Contains(result, "### error-patterns ⚠️") {
		t.Error("non-stale KI should not have warning marker")
	}
}

func TestEnrichUserMessage_KnowledgeItems_Empty(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		KnowledgeItems: nil,
	})
	result := joinParts(parts)

	if strings.Contains(result, "<knowledge_items>") {
		t.Error("should NOT contain <knowledge_items> when empty")
	}
}

func TestEnrichUserMessage_WorkspaceCorpusName(t *testing.T) {
	// With corpus name — should use URI→CorpusName mapping format
	parts := EnrichUserMessage("test", MessageContextConfig{
		Workspaces: []WorkspaceInfo{
			{Directory: "/home/user/project", CorpusName: "org/project"},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "each defined by a URI and a CorpusName") {
		t.Error("should contain corpus mapping preamble")
	}
	if !strings.Contains(result, "/home/user/project -> org/project") {
		t.Error("should contain URI -> CorpusName mapping")
	}
	if strings.Contains(result, "- /home/user/project") {
		t.Error("should NOT use simple list format when corpus is present")
	}
	// Should still have anti-tmp guidance
	if !strings.Contains(result, "Code relating to the user's requests") {
		t.Error("should contain anti-tmp guidance")
	}
}

func TestEnrichUserMessage_WorkspaceCorpusName_Multiple(t *testing.T) {
	// Multiple workspaces, some with corpus, some without
	parts := EnrichUserMessage("test", MessageContextConfig{
		Workspaces: []WorkspaceInfo{
			{Directory: "/home/user/project-a", CorpusName: "org/project-a"},
			{Directory: "/home/user/project-b"},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "2 active workspaces") {
		t.Error("should say 2 active workspaces")
	}
	if !strings.Contains(result, "/home/user/project-a -> org/project-a") {
		t.Error("should contain corpus mapping for project-a")
	}
	// project-b has no corpus — should still be listed (bare path)
	if !strings.Contains(result, "/home/user/project-b\n") {
		t.Error("should contain bare path for project-b")
	}
}

func TestEnrichUserMessage_WorkspaceNoCorpus(t *testing.T) {
	// No corpus name — should use simple list format
	parts := EnrichUserMessage("test", MessageContextConfig{
		Workspaces: []WorkspaceInfo{
			{Directory: "/home/user/project"},
		},
	})
	result := joinParts(parts)

	if strings.Contains(result, "CorpusName") {
		t.Error("should NOT mention CorpusName when none present")
	}
	if !strings.Contains(result, "- /home/user/project") {
		t.Error("should use simple list format")
	}
}

func TestEnrichUserMessage_SettingsChanges(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		SettingsChanges: []SettingsChange{
			{Setting: "Model Selection", OldValue: "None", NewValue: "Claude Opus 4.6 (Thinking)"},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "<USER_SETTINGS_CHANGE>") {
		t.Error("should contain <USER_SETTINGS_CHANGE> block")
	}
	if !strings.Contains(result, "The user changed setting `Model Selection` from None to Claude Opus 4.6 (Thinking).") {
		t.Error("should contain change description with old and new values")
	}
	if !strings.Contains(result, "</USER_SETTINGS_CHANGE>") {
		t.Error("should close USER_SETTINGS_CHANGE tag")
	}
}

func TestEnrichUserMessage_SettingsChanges_NoOldValue(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		SettingsChanges: []SettingsChange{
			{Setting: "Temperature", NewValue: "0.8"},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "The user set `Temperature` to 0.8.") {
		t.Error("should use 'set' phrasing when no old value")
	}
	if strings.Contains(result, "from") {
		t.Error("should NOT use 'from X to Y' when no old value")
	}
}

func TestEnrichUserMessage_SettingsChanges_Multiple(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		SettingsChanges: []SettingsChange{
			{Setting: "Model Selection", OldValue: "None", NewValue: "Claude Opus 4.6"},
			{Setting: "Planning Mode", OldValue: "auto", NewValue: "always"},
		},
	})
	result := joinParts(parts)

	count := strings.Count(result, "<USER_SETTINGS_CHANGE>")
	if count != 2 {
		t.Errorf("expected 2 USER_SETTINGS_CHANGE blocks, got %d", count)
	}
	if !strings.Contains(result, "Model Selection") {
		t.Error("should contain first change")
	}
	if !strings.Contains(result, "Planning Mode") {
		t.Error("should contain second change")
	}
}

func TestEnrichUserMessage_SettingsChanges_WithHint(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		SettingsChanges: []SettingsChange{
			{
				Setting:  "Model Selection",
				OldValue: "None",
				NewValue: "Claude Opus 4.6",
				Hint:     "No need to comment on this change if the user doesn't ask about it.",
			},
		},
	})
	result := joinParts(parts)

	if !strings.Contains(result, "No need to comment on this change") {
		t.Error("should contain hint text")
	}
	// Hint should be inside the USER_SETTINGS_CHANGE block
	startIdx := strings.Index(result, "<USER_SETTINGS_CHANGE>")
	endIdx := strings.Index(result, "</USER_SETTINGS_CHANGE>")
	hintIdx := strings.Index(result, "No need to comment")
	if hintIdx < startIdx || hintIdx > endIdx {
		t.Error("hint should be inside the USER_SETTINGS_CHANGE block")
	}
}

func TestEnrichUserMessage_SettingsChanges_NoHint(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		SettingsChanges: []SettingsChange{
			{Setting: "Temperature", OldValue: "0.2", NewValue: "0.8"},
		},
	})

	// Find the settings change part
	var settingsPart string
	for _, p := range parts {
		if strings.Contains(p, "<USER_SETTINGS_CHANGE>") {
			settingsPart = p
			break
		}
	}
	if settingsPart == "" {
		t.Fatal("should have a USER_SETTINGS_CHANGE part")
	}
	// Opening tag + change description = 2 newlines before closing tag
	if strings.Count(settingsPart, "\n") != 2 {
		t.Errorf("block without hint should have exactly 2 newlines, got %d", strings.Count(settingsPart, "\n"))
	}
}

func TestEnrichUserMessage_SettingsChanges_Empty(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		SettingsChanges: nil,
	})
	result := joinParts(parts)

	if strings.Contains(result, "<USER_SETTINGS_CHANGE>") {
		t.Error("should NOT contain USER_SETTINGS_CHANGE when empty")
	}
}

func TestEnrichUserMessage_SettingsChanges_Ordering(t *testing.T) {
	parts := EnrichUserMessage("test", MessageContextConfig{
		HostContext: &pb.UserContext{
			Extra: map[string]string{"key": "val"},
		},
		SettingsChanges: []SettingsChange{
			{Setting: "Model", OldValue: "A", NewValue: "B"},
		},
		EphemeralMessages: []string{"Be concise."},
	})
	result := joinParts(parts)

	metaIdx := strings.Index(result, "</ADDITIONAL_METADATA>")
	settingsIdx := strings.Index(result, "<USER_SETTINGS_CHANGE>")
	ephIdx := strings.Index(result, "<EPHEMERAL_MESSAGE>")

	if metaIdx == -1 {
		t.Fatal("should have ADDITIONAL_METADATA")
	}
	if settingsIdx == -1 {
		t.Fatal("should have USER_SETTINGS_CHANGE")
	}
	if ephIdx == -1 {
		t.Fatal("should have EPHEMERAL_MESSAGE")
	}

	if settingsIdx < metaIdx {
		t.Error("USER_SETTINGS_CHANGE should come after ADDITIONAL_METADATA")
	}
	if settingsIdx > ephIdx {
		t.Error("USER_SETTINGS_CHANGE should come before EPHEMERAL_MESSAGE")
	}
}
