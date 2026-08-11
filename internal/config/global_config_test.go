package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestLoadGlobalSettings_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{
		"enableTelemetry": true,
		"trustedWorkspaces": ["/home/user/projects", "/tmp/safe"]
	}`), 0644)

	settings := LoadGlobalSettingsFrom(path, slog.Default())

	if !settings.EnableTelemetry {
		t.Fatal("expected telemetry enabled")
	}
	if len(settings.TrustedWorkspaces) != 2 {
		t.Fatalf("expected 2 trusted workspaces, got %d", len(settings.TrustedWorkspaces))
	}
}

func TestLoadGlobalSettings_MissingFile(t *testing.T) {
	settings := LoadGlobalSettingsFrom("/nonexistent/settings.json", slog.Default())

	if settings.EnableTelemetry {
		t.Fatal("expected telemetry disabled by default")
	}
	if len(settings.TrustedWorkspaces) != 0 {
		t.Fatal("expected empty trusted workspaces")
	}
}

func TestLoadGlobalSettings_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{not valid json}`), 0644)

	settings := LoadGlobalSettingsFrom(path, slog.Default())

	// Should return defaults, not panic
	if settings.EnableTelemetry {
		t.Fatal("expected default false for invalid json")
	}
}

func TestIsWorkspaceTrusted(t *testing.T) {
	settings := &GlobalSettings{
		TrustedWorkspaces: []string{"/home/user/projects"},
	}

	if !settings.IsWorkspaceTrusted("/home/user/projects") {
		t.Fatal("exact match should be trusted")
	}
	if !settings.IsWorkspaceTrusted("/home/user/projects/myrepo") {
		t.Fatal("subdirectory should be trusted")
	}
	if settings.IsWorkspaceTrusted("/home/user/other") {
		t.Fatal("non-child should NOT be trusted")
	}
	if settings.IsWorkspaceTrusted("/home/user/projects-fake") {
		t.Fatal("prefix match without separator should NOT be trusted")
	}
}

func TestLoadGlobalMcpConfig_StdioServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"filesystem": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
			}
		}
	}`), 0644)

	servers := LoadGlobalMcpConfigFrom(path, slog.Default())

	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Name != "filesystem" {
		t.Fatalf("expected name 'filesystem', got %q", servers[0].Name)
	}
	stdio := servers[0].GetStdio()
	if stdio == nil {
		t.Fatal("expected stdio transport")
	}
	if stdio.Command != "npx" {
		t.Fatalf("expected command 'npx', got %q", stdio.Command)
	}
	if len(stdio.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(stdio.Args))
	}
}

func TestLoadGlobalMcpConfig_HttpServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"postgres": {
				"url": "http://localhost:3001/sse",
				"headers": {"Authorization": "Bearer secret"}
			}
		}
	}`), 0644)

	servers := LoadGlobalMcpConfigFrom(path, slog.Default())

	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	http := servers[0].GetHttp()
	if http == nil {
		t.Fatal("expected http transport")
	}
	if http.Url != "http://localhost:3001/sse" {
		t.Fatalf("expected sse url, got %q", http.Url)
	}
	if http.Headers["Authorization"] != "Bearer secret" {
		t.Fatal("expected auth header")
	}
}

func TestLoadGlobalMcpConfig_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_MCP_TOKEN", "my-secret-token")
	defer os.Unsetenv("TEST_MCP_TOKEN")

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"github": {
				"command": "npx",
				"args": ["-y", "@mcp/github"],
				"env": {"GITHUB_TOKEN": "${TEST_MCP_TOKEN}"}
			}
		}
	}`), 0644)

	servers := LoadGlobalMcpConfigFrom(path, slog.Default())

	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Env["GITHUB_TOKEN"] != "my-secret-token" {
		t.Fatalf("expected env var expansion, got %q", servers[0].Env["GITHUB_TOKEN"])
	}
}

func TestLoadGlobalMcpConfig_MissingFile(t *testing.T) {
	servers := LoadGlobalMcpConfigFrom("/nonexistent/mcp_config.json", slog.Default())
	if len(servers) != 0 {
		t.Fatal("expected empty for missing file")
	}
}

func TestLoadGlobalMcpConfig_EmptyServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	os.WriteFile(path, []byte(`{"mcpServers": {}}`), 0644)

	servers := LoadGlobalMcpConfigFrom(path, slog.Default())
	if len(servers) != 0 {
		t.Fatal("expected empty for empty servers")
	}
}

func TestLoadGlobalMcpConfig_NoTransport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"broken": {"env": {"KEY": "VAL"}}
		}
	}`), 0644)

	servers := LoadGlobalMcpConfigFrom(path, slog.Default())
	if len(servers) != 0 {
		t.Fatal("expected server without transport to be skipped")
	}
}

func TestLoadGlobalMcpConfig_WithToolWhitelist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	os.WriteFile(path, []byte(`{
		"mcpServers": {
			"limited": {
				"command": "npx",
				"args": ["-y", "@mcp/test"],
				"enabledTools": ["read_file", "list_dir"]
			}
		}
	}`), 0644)

	servers := LoadGlobalMcpConfigFrom(path, slog.Default())
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if len(servers[0].EnabledTools) != 2 {
		t.Fatalf("expected 2 enabled tools, got %d", len(servers[0].EnabledTools))
	}
}

func TestMergeMcpConfigs_AgentOverridesGlobal(t *testing.T) {
	global := []*pb.McpServerConfig{
		{Name: "filesystem", Transport: &pb.McpServerConfig_Stdio{Stdio: &pb.McpStdioTransport{Command: "global-cmd"}}},
		{Name: "github", Transport: &pb.McpServerConfig_Stdio{Stdio: &pb.McpStdioTransport{Command: "global-github"}}},
	}
	agent := []*pb.McpServerConfig{
		{Name: "filesystem", Transport: &pb.McpServerConfig_Stdio{Stdio: &pb.McpStdioTransport{Command: "agent-cmd"}}},
		{Name: "postgres", Transport: &pb.McpServerConfig_Http{Http: &pb.McpHttpTransport{Url: "http://localhost:5432"}}},
	}

	merged := MergeMcpConfigs(global, agent)

	if len(merged) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(merged))
	}

	// Check that agent's "filesystem" overrides global's
	names := make(map[string]string)
	for _, s := range merged {
		if stdio := s.GetStdio(); stdio != nil {
			names[s.Name] = stdio.Command
		} else {
			names[s.Name] = s.GetHttp().Url
		}
	}

	if names["filesystem"] != "agent-cmd" {
		t.Fatalf("expected agent override for filesystem, got %q", names["filesystem"])
	}
	if names["github"] != "global-github" {
		t.Fatalf("expected global github preserved, got %q", names["github"])
	}
	if names["postgres"] != "http://localhost:5432" {
		t.Fatalf("expected agent postgres, got %q", names["postgres"])
	}
}

func TestMergeMcpConfigs_EmptyGlobal(t *testing.T) {
	agent := []*pb.McpServerConfig{{Name: "test"}}
	merged := MergeMcpConfigs(nil, agent)
	if len(merged) != 1 || merged[0].Name != "test" {
		t.Fatal("expected agent-only result")
	}
}

func TestMergeMcpConfigs_EmptyAgent(t *testing.T) {
	global := []*pb.McpServerConfig{{Name: "test"}}
	merged := MergeMcpConfigs(global, nil)
	if len(merged) != 1 || merged[0].Name != "test" {
		t.Fatal("expected global-only result")
	}
}

func TestExpandEnvString(t *testing.T) {
	os.Setenv("TEST_EXPAND_VAR", "hello")
	defer os.Unsetenv("TEST_EXPAND_VAR")

	result := expandEnvString("prefix-${TEST_EXPAND_VAR}-suffix")
	if result != "prefix-hello-suffix" {
		t.Fatalf("expected expansion, got %q", result)
	}

	// Unresolved vars preserved
	result = expandEnvString("${NONEXISTENT_VAR_12345}")
	if result != "${NONEXISTENT_VAR_12345}" {
		t.Fatalf("expected unresolved var preserved, got %q", result)
	}
}
