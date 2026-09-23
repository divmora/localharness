package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// webIndicatorFiles are root-level files that typically indicate a web application project.
var webIndicatorFiles = []string{
	"package.json",
	"tsconfig.json",
	"jsconfig.json",
	"index.html",
	"vite.config.js",
	"vite.config.ts",
	"vite.config.mjs",
	"vite.config.cjs",
	"next.config.js",
	"next.config.mjs",
	"next.config.ts",
	"nuxt.config.js",
	"nuxt.config.ts",
	"svelte.config.js",
	"astro.config.mjs",
	"angular.json",
	"webpack.config.js",
}

// webIndicatorSubpaths are relative paths to files that indicate web projects.
var webIndicatorSubpaths = []string{
	filepath.Join("public", "index.html"),
	filepath.Join("src", "index.html"),
	filepath.Join("src", "App.tsx"),
	filepath.Join("src", "App.jsx"),
	filepath.Join("src", "App.vue"),
	filepath.Join("src", "App.svelte"),
}

// HasWebIndicators checks whether any of the specified workspace directories contain
// files characteristic of a web application or frontend development environment.
// Returns false if dirs is empty or no web indicators are found.
func HasWebIndicators(dirs []string) bool {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		// Check primary root-level indicator files
		for _, name := range webIndicatorFiles {
			target := filepath.Join(dir, name)
			if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
				return true
			}
		}
		// Check common subpath indicators
		for _, sub := range webIndicatorSubpaths {
			target := filepath.Join(dir, sub)
			if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
				return true
			}
		}
		// Check for any .html files in the workspace root
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".html") {
					return true
				}
			}
		}
	}
	return false
}
