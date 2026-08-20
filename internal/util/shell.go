package util

import (
	"strings"
)

// SplitShellCommands splits a compound shell command by top-level operators
// (&&, ||, ;, |, &, \n) while respecting single quotes, double quotes, and backslash escaping.
// It also detects unquoted subshells / command substitutions ($(..) or `..`).
func SplitShellCommands(cmd string) (commands []string, hasSubshell bool) {
	var current strings.Builder
	var inSingleQuote bool
	var inDoubleQuote bool
	var escaped bool

	runes := []rune(cmd)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]

		// Backslash escaping outside single quotes
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && !inSingleQuote {
			escaped = true
			current.WriteRune(r)
			continue
		}

		// Single quote handling
		if r == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			current.WriteRune(r)
			continue
		}

		// Double quote handling
		if r == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			current.WriteRune(r)
			continue
		}

		// Inside single quotes: everything is literal
		if inSingleQuote {
			current.WriteRune(r)
			continue
		}

		// Unquoted subshell detection: backtick or $(
		if r == '`' {
			hasSubshell = true
			current.WriteRune(r)
			continue
		}
		if r == '$' && i+1 < n && runes[i+1] == '(' {
			hasSubshell = true
			current.WriteRune(r)
			continue
		}

		// Inside double quotes: operators (&&, ||, ;, |, &) are part of literal string
		if inDoubleQuote {
			current.WriteRune(r)
			continue
		}

		// Outside quotes: check for shell chaining operators:
		// 1. &&
		if r == '&' && i+1 < n && runes[i+1] == '&' {
			if s := strings.TrimSpace(current.String()); s != "" {
				commands = append(commands, s)
			}
			current.Reset()
			i++ // skip second '&'
			continue
		}

		// 2. ||
		if r == '|' && i+1 < n && runes[i+1] == '|' {
			if s := strings.TrimSpace(current.String()); s != "" {
				commands = append(commands, s)
			}
			current.Reset()
			i++ // skip second '|'
			continue
		}

		// 3. | (pipe) or |&
		if r == '|' {
			if i+1 < n && runes[i+1] == '&' {
				i++ // skip '&'
			}
			if s := strings.TrimSpace(current.String()); s != "" {
				commands = append(commands, s)
			}
			current.Reset()
			continue
		}

		// 4. ; (semicolon) or \n (newline)
		if r == ';' || r == '\n' {
			if s := strings.TrimSpace(current.String()); s != "" {
				commands = append(commands, s)
			}
			current.Reset()
			continue
		}

		// 5. & (background execution, single &)
		if r == '&' {
			if s := strings.TrimSpace(current.String()); s != "" {
				commands = append(commands, s)
			}
			current.Reset()
			continue
		}

		current.WriteRune(r)
	}

	if s := strings.TrimSpace(current.String()); s != "" {
		commands = append(commands, s)
	}

	return commands, hasSubshell
}

// StripLeadingEnvVars strips leading environment variable assignments (e.g., CGO_ENABLED=0 FOO=bar).
func StripLeadingEnvVars(cmd string) string {
	parts := strings.Fields(cmd)
	for i, part := range parts {
		if strings.Contains(part, "=") && !strings.HasPrefix(part, "-") {
			eqIdx := strings.Index(part, "=")
			varName := part[:eqIdx]
			if isValidEnvVarName(varName) {
				continue
			}
		}
		return strings.Join(parts[i:], " ")
	}
	return cmd
}

func isValidEnvVarName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for i, r := range name {
		if i == 0 && (r >= '0' && r <= '9') {
			return false
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// MatchSingleCommand checks if a single command matches an allowed pattern with word boundary.
func MatchSingleCommand(cmd string, allowedPattern string) bool {
	trimmedCmd := strings.TrimSpace(cmd)
	trimmedPattern := strings.TrimSpace(allowedPattern)

	if trimmedCmd == "" || trimmedPattern == "" {
		return false
	}

	if trimmedPattern == "*" || trimmedPattern == trimmedCmd {
		return true
	}

	// Prefix match with word boundary (e.g. "go test" matches "go test ./...")
	if strings.HasPrefix(trimmedCmd, trimmedPattern+" ") {
		return true
	}

	// Try stripping leading env vars e.g. "CGO_ENABLED=0 go test" against "go test"
	strippedCmd := StripLeadingEnvVars(trimmedCmd)
	if strippedCmd != trimmedCmd {
		if strippedCmd == trimmedPattern || strings.HasPrefix(strippedCmd, trimmedPattern+" ") {
			return true
		}
	}

	return false
}

// IsCommandAllowedAgainstRules checks if every constituent command in a potentially
// compound shell command (separated by &&, ||, ;, |, &, \n) matches at least one
// allowed pattern in the provided rules list.
func IsCommandAllowedAgainstRules(cmd string, allowedPatterns []string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" || len(allowedPatterns) == 0 {
		return false
	}

	// 1. Exact match check against rules
	for _, allowed := range allowedPatterns {
		a := strings.TrimSpace(allowed)
		if a == "*" || a == trimmed {
			return true
		}
	}

	// 2. Split compound commands by operators (&&, ||, ;, |, &, \n)
	subCommands, hasSubshell := SplitShellCommands(trimmed)

	// If unquoted subshells / command substitutions are detected, prompt user
	if hasSubshell {
		return false
	}

	if len(subCommands) == 0 {
		return false
	}

	// 3. Every individual sub-command MUST match at least one allowed rule
	for _, subCmd := range subCommands {
		subAllowed := false
		for _, allowed := range allowedPatterns {
			if MatchSingleCommand(subCmd, allowed) {
				subAllowed = true
				break
			}
		}
		if !subAllowed {
			return false
		}
	}

	return true
}
