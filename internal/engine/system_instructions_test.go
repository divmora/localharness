package engine

import (
	"strings"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestBuildSystemPrompt_Default(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{})

	if !strings.Contains(result, "<identity>") {
		t.Error("default prompt should contain <identity> section")
	}
	if !strings.Contains(result, defaultIdentity) {
		t.Error("default prompt should contain default identity")
	}
	if !strings.Contains(result, "</identity>") {
		t.Error("default prompt should close identity tag")
	}
	// Default guidelines should always be present
	if !strings.Contains(result, "<guidelines>") {
		t.Error("default prompt should contain <guidelines> section")
	}
	if !strings.Contains(result, "Maintain documentation integrity") {
		t.Error("default prompt should contain default guidelines content")
	}
	// Web dev should be OFF by default
	if strings.Contains(result, "<web_application_development>") {
		t.Error("default prompt should NOT contain <web_application_development> (off by default)")
	}
	// Ephemeral message should always be present
	if !strings.Contains(result, "<ephemeral_message>") {
		t.Error("default prompt should contain <ephemeral_message> section")
	}
	if !strings.Contains(result, "EPHEMERAL_MESSAGE") {
		t.Error("ephemeral message should reference EPHEMERAL_MESSAGE tag")
	}
	// Messaging should always be present
	if !strings.Contains(result, "<messaging>") {
		t.Error("default prompt should contain <messaging> section")
	}
	if !strings.Contains(result, "Reactive Wakeup") {
		t.Error("messaging section should teach reactive wakeup pattern")
	}
	// Artifacts should always be present
	if !strings.Contains(result, "<artifacts>") {
		t.Error("default prompt should contain <artifacts> (always present)")
	}
	if !strings.Contains(result, "artifact directory") {
		t.Error("artifacts should mention artifact directory")
	}
	// Conversation transcript should NOT be present without BrainDir
	if strings.Contains(result, "<conversation_transcript>") {
		t.Error("default prompt should NOT contain <conversation_transcript> without BrainDir")
	}
	// With BrainDir set, it should appear
	withBrain := BuildSystemPrompt(SystemPromptConfig{BrainDir: "/tmp/test-brain"})
	if !strings.Contains(withBrain, "<conversation_transcript>") {
		t.Error("prompt with BrainDir should contain <conversation_transcript>")
	}
	if !strings.Contains(withBrain, "transcript.jsonl") {
		t.Error("conversation transcript should reference transcript.jsonl")
	}
	if !strings.Contains(withBrain, "not available in KI summaries") {
		t.Error("conversation transcript should reinforce KI → transcript hierarchy")
	}
	// Communication style defaults should always be present
	if !strings.Contains(result, "<communication_style>") {
		t.Error("default prompt should contain <communication_style> (always present)")
	}
	if !strings.Contains(result, "Keep your responses concise") {
		t.Error("communication style defaults should contain conciseness rule")
	}
	// Opt-in modules should be OFF by default
	if strings.Contains(result, "<slash_commands>") {
		t.Error("default prompt should NOT contain <slash_commands> (opt-in)")
	}
	if strings.Contains(result, "<knowledge_items>") {
		t.Error("default prompt should NOT contain <knowledge_items> (opt-in)")
	}
	// Data-driven modules should be OFF when no data
	if strings.Contains(result, "<skills>") {
		t.Error("default prompt should NOT contain <skills> (data-driven, no data)")
	}
	if strings.Contains(result, "<plugins>") {
		t.Error("default prompt should NOT contain <plugins> (data-driven, no data)")
	}
}

func TestBuildSystemPrompt_WebDevDisabled(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		EnableWebDev: false,
	})

	if strings.Contains(result, "<web_application_development>") {
		t.Error("web dev section should be absent when EnableWebDev is false (default)")
	}
	// Other sections should still be present
	if !strings.Contains(result, "<identity>") {
		t.Error("identity should still be present")
	}
	if !strings.Contains(result, "<guidelines>") {
		t.Error("guidelines should still be present")
	}
}

func TestBuildSystemPrompt_WebDevEnabled(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		EnableWebDev: true,
	})

	if !strings.Contains(result, "<web_application_development>") {
		t.Error("web dev should be present when DisableWebDev is false (default)")
	}
	if !strings.Contains(result, "Technology Stack") {
		t.Error("web dev should contain Technology Stack section")
	}
	if !strings.Contains(result, "SEO Best Practices") {
		t.Error("web dev should contain SEO Best Practices section")
	}
}

func TestBuildSystemPrompt_UserInstructions(t *testing.T) {
	raw := "You are a custom bot. Do whatever."
	result := BuildSystemPrompt(SystemPromptConfig{
		UserInstructions: raw,
	})

	// UserInstructions should appear as a <user_instructions> section
	if !strings.Contains(result, "<user_instructions>") {
		t.Error("user instructions should be included as <user_instructions> section")
	}
	if !strings.Contains(result, raw) {
		t.Error("user instructions content should appear in prompt")
	}
	// Identity should STILL be present — UserInstructions is purely additive
	if !strings.Contains(result, "<identity>") {
		t.Error("identity should always be present, even with UserInstructions")
	}
	if !strings.Contains(result, "Zenith") {
		t.Error("default Zenith identity should be present when no Structured.Identity")
	}
	// All base sections should be present
	if !strings.Contains(result, "<guidelines>") {
		t.Error("guidelines section should be present")
	}
}

func TestBuildSystemPrompt_CustomIdentity(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		Structured: &pb.StructuredSystemInstructions{
			Identity: "You are DevBot, a DevOps assistant.",
		},
	})

	if !strings.Contains(result, "You are DevBot, a DevOps assistant.") {
		t.Error("custom identity should appear in prompt")
	}
	if strings.Contains(result, defaultIdentity) {
		t.Error("default identity should NOT appear when custom is set")
	}
}

func TestBuildSystemPrompt_WorkspacesRemovedFromSystemPrompt(t *testing.T) {
	// Workspaces should NOT appear in system prompt (moved to per-message user_information)
	result := BuildSystemPrompt(SystemPromptConfig{})
	if strings.Contains(result, "<workspaces>") {
		t.Error("system prompt should not contain <workspaces> section (moved to per-message)")
	}
}

func TestBuildSystemPrompt_Guidelines(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		Structured: &pb.StructuredSystemInstructions{
			Guidelines: "Always write tests. Keep functions small.",
		},
	})

	// Default guidelines should always be present
	if !strings.Contains(result, "<guidelines>") {
		t.Error("should contain default <guidelines> section")
	}
	if !strings.Contains(result, "Maintain documentation integrity") {
		t.Error("should contain default guidelines content")
	}
	// User-provided guidelines should appear in <user_guidelines> section
	if !strings.Contains(result, "<user_guidelines>") {
		t.Error("should contain <user_guidelines> section for structured guidelines")
	}
	if !strings.Contains(result, "Always write tests") {
		t.Error("should contain user guidelines content")
	}
}

func TestBuildSystemPrompt_CommunicationStyle(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		Structured: &pb.StructuredSystemInstructions{
			CommunicationStyle: "Be concise. Use bullet points.",
		},
	})

	if !strings.Contains(result, "<communication_style>") {
		t.Error("should contain <communication_style> section")
	}
}

func TestBuildSystemPrompt_CustomSections(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		Structured: &pb.StructuredSystemInstructions{
			Sections: []*pb.SystemSection{
				{Tag: "project_context", Content: "This is a Go microservice.", Priority: 50},
				{Tag: "security_rules", Content: "Never expose secrets.", Priority: 30},
			},
		},
	})

	if !strings.Contains(result, "<security_rules>") {
		t.Error("should contain security_rules section")
	}
	if !strings.Contains(result, "<project_context>") {
		t.Error("should contain project_context section")
	}

	// security_rules (priority 30) should come before project_context (priority 50)
	secIdx := strings.Index(result, "<security_rules>")
	projIdx := strings.Index(result, "<project_context>")
	if secIdx > projIdx {
		t.Error("security_rules (priority 30) should come before project_context (priority 50)")
	}
}

// TestBuildSystemPrompt_SectionOrder verifies the cache-optimized ordering:
// identity(0) < web_dev(3) < ephemeral(5) < skills(6) < plugins(7) < subagents(8) < messaging(9) < knowledge_items(10) < ...
func TestBuildSystemPrompt_SectionOrder(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		UserInstructions: "Custom user prompt",
		EnableWebDev:     true,
		EnablePlanningMode: true,
		EnableSlashCommands: true,
		SlashCommands: []SlashCommandDef{
			{Name: "/goal", Description: "test"},
		},
		EnableKnowledgeItems: true,
		Skills: []SkillDef{
			{Name: "test-skill", Description: "test", SkillPath: "/path/SKILL.md"},
		},
		Plugins: []PluginDef{
			{Name: "test-plugin", Path: "/path/plugin", Skills: []SkillDef{
				{Name: "plugin-skill", Description: "test"},
			}},
		},
		SubagentsEnabled: true,
		SubagentTypes: []SubagentTypeDef{
			{Name: "research", Description: "Research agent"},
		},
		BrainDir: "/tmp/test-brain",
		Structured: &pb.StructuredSystemInstructions{
			Guidelines: "Rules",
		},
	})

	identityIdx := strings.Index(result, "<identity>")
	webDevIdx := strings.Index(result, "<web_application_development>")
	ephemeralIdx := strings.Index(result, "<ephemeral_message>")
	skillsIdx := strings.Index(result, "<skills>")
	pluginsIdx := strings.Index(result, "<plugins>")
	subagentsIdx := strings.Index(result, "<subagents>")
	messagingIdx := strings.Index(result, "<messaging>")
	artifactsIdx := strings.Index(result, "<artifacts>")
	planningIdx := strings.Index(result, "<planning_mode>")
	planningArtifactsIdx := strings.Index(result, "<planning_mode_artifacts>")
	guidelinesIdx := strings.Index(result, "<guidelines>")
	commStyleIdx := strings.Index(result, "<communication_style>")
	slashCmdsIdx := strings.Index(result, "<slash_commands>")
	kiIdx := strings.Index(result, "<knowledge_items>")
	transcriptIdx := strings.Index(result, "<conversation_transcript>")
	userInstrIdx := strings.Index(result, "<user_instructions>")
	userGuidelinesIdx := strings.Index(result, "<user_guidelines>")

	// Presence checks
	for tag, idx := range map[string]int{
		"identity": identityIdx, "web_application_development": webDevIdx,
		"ephemeral_message": ephemeralIdx, "skills": skillsIdx, "plugins": pluginsIdx,
		"subagents": subagentsIdx,
		"messaging": messagingIdx, "artifacts": artifactsIdx,
		"planning_mode": planningIdx, "planning_mode_artifacts": planningArtifactsIdx,
		"guidelines": guidelinesIdx, "communication_style": commStyleIdx,
		"slash_commands": slashCmdsIdx, "knowledge_items": kiIdx,
		"conversation_transcript": transcriptIdx,
		"user_instructions": userInstrIdx,
		"user_guidelines": userGuidelinesIdx,
	} {
		if idx == -1 {
			t.Errorf("%s should be present", tag)
		}
	}

	// Order checks (renumbered priorities with subagents at 8)
	if identityIdx > webDevIdx {
		t.Error("identity (0) should come before web_application_development (3)")
	}
	if webDevIdx > ephemeralIdx {
		t.Error("web_application_development (3) should come before ephemeral_message (5)")
	}
	if ephemeralIdx > skillsIdx {
		t.Error("ephemeral_message (5) should come before skills (6)")
	}
	if skillsIdx > pluginsIdx {
		t.Error("skills (6) should come before plugins (7)")
	}
	if pluginsIdx > subagentsIdx {
		t.Error("plugins (7) should come before subagents (8)")
	}
	if subagentsIdx > messagingIdx {
		t.Error("subagents (8) should come before messaging (9)")
	}
	if messagingIdx > kiIdx {
		t.Error("messaging (9) should come before knowledge_items (10)")
	}
	if kiIdx > transcriptIdx {
		t.Error("knowledge_items (10) should come before conversation_transcript (11)")
	}
	if transcriptIdx > artifactsIdx {
		t.Error("conversation_transcript (11) should come before artifacts (12)")
	}
	if artifactsIdx > slashCmdsIdx {
		t.Error("artifacts (12) should come before slash_commands (13)")
	}
	if slashCmdsIdx > planningIdx {
		t.Error("slash_commands (13) should come before planning_mode (14)")
	}
	if planningIdx > planningArtifactsIdx {
		t.Error("planning_mode (14) should come before planning_mode_artifacts (15)")
	}
	if planningArtifactsIdx > guidelinesIdx {
		t.Error("planning_mode_artifacts (15) should come before guidelines (16)")
	}
	if guidelinesIdx > commStyleIdx {
		t.Error("guidelines (16) should come before communication_style (17)")
	}
	if commStyleIdx > userInstrIdx {
		t.Error("communication_style (17) should come before user_instructions (60)")
	}
	if userInstrIdx > userGuidelinesIdx {
		t.Error("user_instructions (60) should come before user_guidelines (70)")
	}
}

func TestBuildSystemPrompt_PlanningMode(t *testing.T) {
	// When enabled — planning_mode section should appear
	result := BuildSystemPrompt(SystemPromptConfig{
		EnablePlanningMode: true,
	})

	if !strings.Contains(result, "<planning_mode>") {
		t.Error("planning_mode section should be present when enabled")
	}
	if !strings.Contains(result, "When to Plan") {
		t.Error("should contain planning criteria")
	}
	if !strings.Contains(result, "implementation_plan.md") {
		t.Error("should reference implementation_plan.md artifact")
	}
	if !strings.Contains(result, "task.md") {
		t.Error("should reference task.md artifact")
	}
	if !strings.Contains(result, "walkthrough.md") {
		t.Error("should reference walkthrough.md artifact")
	}
	if !strings.Contains(result, "When NOT to plan") {
		t.Error("should contain skip criteria")
	}
	// planning_mode_artifacts should also be present
	if !strings.Contains(result, "<planning_mode_artifacts>") {
		t.Error("planning_mode_artifacts should be present when planning is enabled")
	}
	if !strings.Contains(result, "[MODIFY]") {
		t.Error("should contain [MODIFY] file marker in plan template")
	}
	if !strings.Contains(result, "[NEW]") {
		t.Error("should contain [NEW] file marker in plan template")
	}
	if !strings.Contains(result, "Verification Plan") {
		t.Error("should contain Verification Plan section")
	}
	if !strings.Contains(result, "User Review Required") {
		t.Error("should contain User Review Required section")
	}

	// When disabled — both sections should be absent
	result = BuildSystemPrompt(SystemPromptConfig{})
	if strings.Contains(result, "<planning_mode>") {
		t.Error("planning_mode should be absent when disabled (default)")
	}
	if strings.Contains(result, "<planning_mode_artifacts>") {
		t.Error("planning_mode_artifacts should be absent when disabled (default)")
	}
}

func TestBuildSystemPrompt_Artifacts(t *testing.T) {
	// Artifacts section is always present with full formatting guidance
	result := BuildSystemPrompt(SystemPromptConfig{})

	if !strings.Contains(result, "<artifacts>") {
		t.Error("artifacts section should always be present")
	}
	// Key content checks
	if !strings.Contains(result, "artifact directory") {
		t.Error("should mention artifact directory")
	}
	if !strings.Contains(result, "Naming Artifacts") {
		t.Error("should contain naming guidance")
	}
	if !strings.Contains(result, "[!NOTE]") {
		t.Error("should contain GitHub alert examples")
	}
	if !strings.Contains(result, "Mermaid Diagrams") {
		t.Error("should contain mermaid diagram guidance")
	}
	if !strings.Contains(result, "Carousels") {
		t.Error("should contain carousel guidance")
	}
	if !strings.Contains(result, "File Links and Media") {
		t.Error("should contain file link guidance")
	}
	if !strings.Contains(result, "#L123-L145") {
		t.Error("should show line range linking syntax")
	}
	if !strings.Contains(result, "Critical Rules") {
		t.Error("should contain critical rules section")
	}
	if !strings.Contains(result, "scratch") {
		t.Error("should mention scratch directory")
	}
}

func TestBuildSystemPrompt_EmptySectionsSkipped(t *testing.T) {
	result := BuildSystemPrompt(SystemPromptConfig{
		Structured: &pb.StructuredSystemInstructions{
			Sections: []*pb.SystemSection{
				{Tag: "", Content: "no tag"},          // skip: empty tag
				{Tag: "valid", Content: ""},            // skip: empty content
				{Tag: "keeper", Content: "kept"},       // keep
			},
		},
	})

	if strings.Contains(result, "no tag") {
		t.Error("section with empty tag should be skipped")
	}
	if strings.Contains(result, "<valid>") {
		t.Error("section with empty content should be skipped")
	}
	if !strings.Contains(result, "<keeper>") {
		t.Error("valid section should be included")
	}
}

func TestBuildSystemPrompt_SlashCommands(t *testing.T) {
	// When enabled with no SDK commands — built-in defaults should appear
	result := BuildSystemPrompt(SystemPromptConfig{
		EnableSlashCommands: true,
	})

	if !strings.Contains(result, "<slash_commands>") {
		t.Error("slash_commands section should be present when enabled (built-in defaults)")
	}
	if !strings.Contains(result, "/goal") {
		t.Error("built-in /goal should be present")
	}
	if !strings.Contains(result, "/schedule") {
		t.Error("built-in /schedule should be present")
	}
	if !strings.Contains(result, "/grill-me") {
		t.Error("built-in /grill-me should be present")
	}
	if !strings.Contains(result, "interactive interview") {
		t.Error("should contain /grill-me description")
	}
	if !strings.Contains(result, "cannot execute these commands yourself") {
		t.Error("should explain agent cannot execute slash commands")
	}

	// SDK commands override built-ins by name
	result = BuildSystemPrompt(SystemPromptConfig{
		EnableSlashCommands: true,
		SlashCommands: []SlashCommandDef{
			{Name: "/goal", Description: "Custom goal description"},
		},
	})
	if !strings.Contains(result, "Custom goal description") {
		t.Error("SDK should override built-in /goal description")
	}
	if !strings.Contains(result, "/schedule") {
		t.Error("non-overridden built-in /schedule should still be present")
	}
	if !strings.Contains(result, "/grill-me") {
		t.Error("non-overridden built-in /grill-me should still be present")
	}

	// SDK can add new commands alongside built-ins
	result = BuildSystemPrompt(SystemPromptConfig{
		EnableSlashCommands: true,
		SlashCommands: []SlashCommandDef{
			{Name: "/custom", Description: "A custom command"},
		},
	})
	if !strings.Contains(result, "/custom") {
		t.Error("SDK-provided /custom should be present")
	}
	if !strings.Contains(result, "/goal") {
		t.Error("built-in /goal should still be present alongside SDK commands")
	}

	// When disabled — should not appear
	result = BuildSystemPrompt(SystemPromptConfig{})
	if strings.Contains(result, "<slash_commands>") {
		t.Error("slash_commands should be absent when disabled (default)")
	}
}

func TestBuildSystemPrompt_KnowledgeItems(t *testing.T) {
	// When enabled — knowledge_items section should appear
	result := BuildSystemPrompt(SystemPromptConfig{
		EnableKnowledgeItems: true,
	})

	if !strings.Contains(result, "<knowledge_items>") {
		t.Error("knowledge_items section should be present when enabled")
	}
	if !strings.Contains(result, "# Knowledge Items (KI) System") {
		t.Error("should contain KI System title")
	}
	if !strings.Contains(result, "Check KI Summaries Before Any Research") {
		t.Error("should contain mandatory check instruction with 'Any Research'")
	}
	if !strings.Contains(result, "KIs are Starting Points") {
		t.Error("should contain ground truth caveat")
	}
	if !strings.Contains(result, "metadata.json") {
		t.Error("should reference metadata.json")
	}
	if !strings.Contains(result, "### KI Structure") {
		t.Error("should contain KI Structure sub-section")
	}
	if !strings.Contains(result, "<appDataDir>/knowledge/<project-id>") {
		t.Error("should reference <appDataDir>/knowledge/<project-id> path")
	}
	if !strings.Contains(result, "artifacts/") {
		t.Error("KI Structure should reference artifacts/ directory")
	}

	// When disabled — should not appear
	result = BuildSystemPrompt(SystemPromptConfig{})
	if strings.Contains(result, "<knowledge_items>") {
		t.Error("knowledge_items should be absent when disabled (default)")
	}
}

func TestBuildSystemPrompt_Skills(t *testing.T) {
	// When skills are provided — section should appear with guidance and list
	result := BuildSystemPrompt(SystemPromptConfig{
		Skills: []SkillDef{
			{Name: "run-scanner", Description: "Scan files for vulnerabilities", SkillPath: "/skills/scanner/SKILL.md"},
			{Name: "no-path", Description: "Skill without path"},
		},
	})

	if !strings.Contains(result, "<skills>") {
		t.Error("skills section should be present when skills are provided")
	}
	if !strings.Contains(result, "SKILL.md") {
		t.Error("skills guidance should mention SKILL.md")
	}
	if !strings.Contains(result, "`view_file` tool") {
		t.Error("skills guidance should instruct using view_file on SKILL.md")
	}
	if !strings.Contains(result, "Available skills:") {
		t.Error("should contain available skills list")
	}
	if !strings.Contains(result, "run-scanner (/skills/scanner/SKILL.md): Scan files for vulnerabilities") {
		t.Error("should list skill with path")
	}
	if !strings.Contains(result, "no-path: Skill without path") {
		t.Error("should list skill without path")
	}

	// When no skills — section should not appear
	result = BuildSystemPrompt(SystemPromptConfig{})
	if strings.Contains(result, "<skills>") {
		t.Error("skills should be absent when no skills provided")
	}
}

func TestBuildSystemPrompt_Plugins(t *testing.T) {
	// When plugins are provided — section should appear with guidance and list
	result := BuildSystemPrompt(SystemPromptConfig{
		Plugins: []PluginDef{
			{
				Name: "securecoder",
				Path: "/plugins/securecoder",
				Skills: []SkillDef{
					{Name: "scan", Description: "Run scanner", SkillPath: "/plugins/securecoder/skills/scan/SKILL.md"},
					{Name: "audit", Description: "Generate audit report"},
				},
			},
			{
				Name: "minimal-plugin",
			},
		},
	})

	if !strings.Contains(result, "<plugins>") {
		t.Error("plugins section should be present when plugins are provided")
	}
	if !strings.Contains(result, "plugin.json") {
		t.Error("plugins guidance should mention plugin.json")
	}
	if !strings.Contains(result, "Available plugins:") {
		t.Error("should contain available plugins list")
	}
	if !strings.Contains(result, "# securecoder (file:///plugins/securecoder)") {
		t.Error("should list plugin with path")
	}
	if !strings.Contains(result, "# minimal-plugin") {
		t.Error("should list plugin without path")
	}
	if !strings.Contains(result, "scan (/plugins/securecoder/skills/scan/SKILL.md): Run scanner") {
		t.Error("should list plugin skill with path")
	}
	if !strings.Contains(result, "audit: Generate audit report") {
		t.Error("should list plugin skill without path")
	}

	// When no plugins — section should not appear
	result = BuildSystemPrompt(SystemPromptConfig{})
	if strings.Contains(result, "<plugins>") {
		t.Error("plugins should be absent when no plugins provided")
	}
}
