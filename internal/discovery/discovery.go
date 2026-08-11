// Package discovery provides auto-discovery of skills and plugins from the
// filesystem. It scans global (appDataDir) and workspace (.agents/) directories
// to find SKILL.md files and plugin.json definitions.
//
// Discovery sources (merged with deduplication, SDK > workspace > global):
//
//   - Global skills:  <appDataDir>/skills/<name>/SKILL.md
//   - Global plugins: <appDataDir>/plugins/<name>/plugin.json
//   - Workspace skills:  <workspace>/.agents/skills/<name>/SKILL.md
//   - Workspace plugins: <workspace>/.agents/plugins/<name>/plugin.json
//   - ADK-injected: passed via HarnessConfig.skills / HarnessConfig.plugins
package discovery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/divmora/localharness/internal/engine"
)

// DiscoverAll finds skills and plugins from all filesystem sources and merges
// them with ADK-injected definitions. Deduplication: SDK > workspace > global.
func DiscoverAll(appDataDir string, workspaceDirs []string, adkSkills []engine.SkillDef, adkPlugins []engine.PluginDef, logger *slog.Logger) ([]engine.SkillDef, []engine.PluginDef) {
	// Discover from global directories
	globalSkills := discoverSkills(filepath.Join(appDataDir, "skills"), logger)
	globalPlugins := discoverPlugins(filepath.Join(appDataDir, "plugins"), logger)

	// Discover from workspace directories
	var wsSkills []engine.SkillDef
	var wsPlugins []engine.PluginDef
	for _, ws := range workspaceDirs {
		agentsDir := filepath.Join(ws, ".agents")
		wsSkills = append(wsSkills, discoverSkills(filepath.Join(agentsDir, "skills"), logger)...)
		wsPlugins = append(wsPlugins, discoverPlugins(filepath.Join(agentsDir, "plugins"), logger)...)
	}

	// Merge: SDK > workspace > global (first occurrence of each name wins)
	allSkills := MergeSkills(adkSkills, wsSkills, globalSkills)
	allPlugins := MergePlugins(adkPlugins, wsPlugins, globalPlugins)

	if logger != nil {
		logger.Info("discovered skills and plugins",
			"sdk_skills", len(adkSkills),
			"workspace_skills", len(wsSkills),
			"global_skills", len(globalSkills),
			"total_skills", len(allSkills),
			"sdk_plugins", len(adkPlugins),
			"workspace_plugins", len(wsPlugins),
			"global_plugins", len(globalPlugins),
			"total_plugins", len(allPlugins),
		)
	}

	return allSkills, allPlugins
}

// discoverSkills scans a directory for skill subdirectories containing SKILL.md.
// Expected layout: <dir>/<skill-name>/SKILL.md
func discoverSkills(dir string, logger *slog.Logger) []engine.SkillDef {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist — not an error, just no skills here
		return nil
	}

	var skills []engine.SkillDef
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillMDPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		name, description, err := parseSkillMD(skillMDPath)
		if err != nil {
			if logger != nil {
				logger.Debug("skipping skill directory", "dir", entry.Name(), "error", err)
			}
			continue
		}

		skills = append(skills, engine.SkillDef{
			Name:        name,
			Description: description,
			SkillPath:   skillMDPath,
		})
	}

	return skills
}

// discoverPlugins scans a directory for plugin subdirectories containing plugin.json.
// Expected layout: <dir>/<plugin-name>/plugin.json
// Each plugin may also have a skills/ subdirectory with SKILL.md files.
func discoverPlugins(dir string, logger *slog.Logger) []engine.PluginDef {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist — not an error, just no plugins here
		return nil
	}

	var plugins []engine.PluginDef
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(dir, entry.Name())
		pluginJSONPath := filepath.Join(pluginDir, "plugin.json")

		name, description, disabled, err := parsePluginJSON(pluginJSONPath)
		if err != nil {
			if logger != nil {
				logger.Debug("skipping plugin directory", "dir", entry.Name(), "error", err)
			}
			continue
		}

		if disabled {
			if logger != nil {
				logger.Debug("skipping disabled plugin", "name", name)
			}
			continue
		}

		// Discover skills within this plugin
		pluginSkills := discoverSkills(filepath.Join(pluginDir, "skills"), logger)

		plugins = append(plugins, engine.PluginDef{
			Name:        name,
			Description: description,
			Path:        pluginDir,
			Skills:      pluginSkills,
		})
	}

	return plugins
}

// parseSkillMD parses YAML frontmatter from a SKILL.md file.
// Returns the name and description from the frontmatter.
//
// Expected format:
//
//	---
//	name: skill-name
//	description: >
//	  Multi-line description.
//	---
func parseSkillMD(path string) (name, description string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("open SKILL.md: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// First line must be "---"
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", "", fmt.Errorf("SKILL.md missing YAML frontmatter delimiter")
	}

	// Parse frontmatter lines until closing "---"
	var descLines []string
	inDescription := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "---" {
			// End of frontmatter
			break
		}

		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			inDescription = false
		} else if strings.HasPrefix(line, "description:") {
			// description can be inline or multi-line (using > or |)
			value := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			if value != "" && value != ">" && value != "|" {
				descLines = append(descLines, value)
			}
			inDescription = true
		} else if inDescription && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")) {
			// Continuation of multi-line description
			descLines = append(descLines, strings.TrimSpace(line))
		} else {
			// Some other key — stop collecting description
			inDescription = false
		}
	}

	if name == "" {
		return "", "", fmt.Errorf("SKILL.md missing 'name' in frontmatter")
	}

	description = strings.Join(descLines, " ")

	return name, description, nil
}

// pluginJSON represents the structure of a plugin.json file.
type pluginJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

// parsePluginJSON parses a plugin.json file.
// Returns (name, description, disabled, error).
func parsePluginJSON(path string) (name, description string, disabled bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false, fmt.Errorf("read plugin.json: %w", err)
	}

	var pj pluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return "", "", false, fmt.Errorf("parse plugin.json: %w", err)
	}

	if pj.Name == "" {
		return "", "", false, fmt.Errorf("plugin.json missing 'name'")
	}

	return pj.Name, pj.Description, pj.Disabled, nil
}

// MergeSkills merges skills from multiple sources with deduplication by name.
// First occurrence of each name wins (call order determines priority).
func MergeSkills(sources ...[]engine.SkillDef) []engine.SkillDef {
	seen := make(map[string]bool)
	var result []engine.SkillDef

	for _, src := range sources {
		for _, s := range src {
			if !seen[s.Name] {
				seen[s.Name] = true
				result = append(result, s)
			}
		}
	}

	return result
}

// MergePlugins merges plugins from multiple sources with deduplication by name.
// First occurrence of each name wins (call order determines priority).
func MergePlugins(sources ...[]engine.PluginDef) []engine.PluginDef {
	seen := make(map[string]bool)
	var result []engine.PluginDef

	for _, src := range sources {
		for _, p := range src {
			if !seen[p.Name] {
				seen[p.Name] = true
				result = append(result, p)
			}
		}
	}

	return result
}
