package codegraph

import "github.com/divmora/localharness/internal/util"

// GitIgnoreMatcher matches file and directory paths against .gitignore rules.
type GitIgnoreMatcher = util.GitIgnoreMatcher

// LoadGitIgnore reads and parses .gitignore from the given directory (if present).
var LoadGitIgnore = util.LoadGitIgnore
