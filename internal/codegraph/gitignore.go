package codegraph

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// GitIgnoreMatcher matches file and directory paths against .gitignore rules.
type GitIgnoreMatcher struct {
	baseDir string
	rules   []ignoreRule
}

type ignoreRule struct {
	pattern   string
	isDirOnly bool
	negate    bool
	isGlobal  bool // does not contain slash (matches anywhere in hierarchy)
}

// LoadGitIgnore reads and parses .gitignore from the given directory (if present).
func LoadGitIgnore(baseDir string) *GitIgnoreMatcher {
	matcher := &GitIgnoreMatcher{
		baseDir: baseDir,
	}

	path := filepath.Join(baseDir, ".gitignore")
	file, err := os.Open(path)
	if err != nil {
		return matcher
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = strings.TrimPrefix(line, "!")
		}

		isDirOnly := false
		if strings.HasSuffix(line, "/") {
			isDirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		isGlobal := !strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")

		matcher.rules = append(matcher.rules, ignoreRule{
			pattern:   line,
			isDirOnly: isDirOnly,
			negate:    negate,
			isGlobal:  isGlobal,
		})
	}

	return matcher
}

// Matches checks if a relative path (and whether it is a directory) should be ignored.
func (m *GitIgnoreMatcher) Matches(relPath string, isDir bool) bool {
	if m == nil || len(m.rules) == 0 {
		return false
	}

	// Normalize separators to forward slash
	cleanPath := filepath.ToSlash(relPath)
	cleanPath = strings.TrimPrefix(cleanPath, "./")
	cleanPath = strings.TrimPrefix(cleanPath, "/")

	ignored := false
	baseName := filepath.Base(cleanPath)

	for _, r := range m.rules {
		matched := false

		if r.isDirOnly {
			if isDir {
				if r.isGlobal {
					matched = matchPattern(r.pattern, baseName) || matchPattern(r.pattern, cleanPath)
				} else {
					matched = matchPattern(r.pattern, cleanPath)
				}
			} else {
				// File inside an ignored directory
				if r.isGlobal {
					parts := strings.Split(cleanPath, "/")
					for _, p := range parts[:len(parts)-1] {
						if matchPattern(r.pattern, p) {
							matched = true
							break
						}
					}
				} else {
					matched = strings.HasPrefix(cleanPath, r.pattern+"/")
				}
			}
		} else {
			if r.isGlobal {
				if matchPattern(r.pattern, baseName) {
					matched = true
				} else {
					parts := strings.Split(cleanPath, "/")
					for _, part := range parts {
						if matchPattern(r.pattern, part) {
							matched = true
							break
						}
					}
				}
			} else {
				matched = matchPattern(r.pattern, cleanPath)
			}
		}

		if matched {
			if r.negate {
				ignored = false
			} else {
				ignored = true
			}
		}
	}

	return ignored
}

func matchPattern(pattern, name string) bool {
	// Handle wildcard **
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")
			if prefix != "" && !strings.HasPrefix(name, prefix) {
				return false
			}
			if suffix != "" && !strings.HasSuffix(name, suffix) {
				return false
			}
			return true
		}
	}

	ok, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	return ok
}
