package connection

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/divmora/localharness/internal/config"
)

// BinaryResolver implements the universal binary resolution chain shared by
// all LocalHarness SDKs (Go, Python, JS).
//
// Resolution order:
//  1. Explicit path (config.BinaryPath)
//  2. $LOCALHARNESS_BIN environment variable
//  3. "localharness" in system PATH
//  3.5. Local dev paths: ./bin/localharness, ./localharness (dev builds only)
//  4. Cached version at ~/.divmora/localharness/bin/v<VERSION>/localharness
//  5. Auto-download from GitHub releases → cache
//
// The cache directory is shared across all SDKs so a binary downloaded by
// the Python SDK is immediately available to the Go ADK and vice versa.
type BinaryResolver struct {
	Logger     *slog.Logger
	Version    string // Required binary version (e.g., "0.3.0"); empty = use HarnessVersion
	CacheDir   string // Default: ~/.divmora/localharness/bin/
	GitHubRepo string // Default: "divmora/localharness"
}

const (
	defaultGitHubRepo = "divmora/localharness"
	defaultCacheDir   = ".divmora/localharness/bin"
	binaryName        = "localharness"
	modulePath        = "github.com/divmora/localharness"
)

// platformSuffix returns the platform identifier for GitHub release assets.
// e.g., "linux-amd64", "darwin-arm64"
func platformSuffix() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch {
	case goos == "linux" && goarch == "amd64":
		return "linux-amd64", nil
	case goos == "linux" && goarch == "arm64":
		return "linux-arm64", nil
	case goos == "darwin" && goarch == "amd64":
		return "darwin-amd64", nil
	case goos == "darwin" && goarch == "arm64":
		return "darwin-arm64", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s (supported: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)", goos, goarch)
	}
}

// Resolve returns the absolute path to a usable localharness binary.
// It tries each resolution step in order and returns the first success.
func (r *BinaryResolver) Resolve(explicitPath string) (string, error) {
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Step 1: Explicit path
	if explicitPath != "" {
		resolved, err := r.resolveExplicit(explicitPath)
		if err != nil {
			return "", fmt.Errorf("explicit binary path %q: %w", explicitPath, err)
		}
		logger.Debug("binary resolved via explicit path", "path", resolved)
		return resolved, nil
	}

	// Step 2: $LOCALHARNESS_BIN environment variable
	if envPath := os.Getenv("LOCALHARNESS_BIN"); envPath != "" {
		resolved, err := r.resolveExplicit(envPath)
		if err != nil {
			return "", fmt.Errorf("$LOCALHARNESS_BIN=%q: %w", envPath, err)
		}
		logger.Debug("binary resolved via $LOCALHARNESS_BIN", "path", resolved)
		return resolved, nil
	}

	// Step 3: System PATH
	if pathBin, err := exec.LookPath(binaryName); err == nil {
		abs, _ := filepath.Abs(pathBin)
		logger.Debug("binary resolved via PATH", "path", abs)
		return abs, nil
	}

	// Step 3.5: Local development paths (./bin/localharness, ./localharness)
	// This enables the `make build && go run ./cmd/testclient` workflow.
	for _, localPath := range []string{
		"./bin/localharness",
		"./localharness",
		"../bin/localharness", // When running from a subdirectory (e.g., examples/)
	} {
		if abs, err := filepath.Abs(localPath); err == nil && isExecutable(abs) {
			logger.Debug("binary resolved via local dev path", "path", abs)
			return abs, nil
		}
	}

	// Step 4: Cached version
	version := r.resolveVersion()
	if version == "" || version == "0.0.0-dev" {
		// Development build — can't resolve version for cache/download.
		// Fall back with a helpful error.
		return "", fmt.Errorf(
			"localharness binary not found.\n\n" +
				"During development, run:\n" +
				"  make build    # builds to ./bin/localharness\n\n" +
				"For production, install via:\n" +
				"  1. go install github.com/divmora/localharness/cmd/localharness@latest\n" +
				"  2. Download from https://github.com/divmora/localharness/releases\n" +
				"  3. Set $LOCALHARNESS_BIN to the binary path\n" +
				"  4. Pass BinaryPath in the SDK config",
		)
	}

	cacheDir := r.resolveCacheDir()
	cachedPath := filepath.Join(cacheDir, "v"+version, binaryName)
	if isExecutable(cachedPath) {
		logger.Debug("binary resolved via cache", "path", cachedPath, "version", version)
		return cachedPath, nil
	}

	// Step 5: Auto-download from GitHub releases
	logger.Info("localharness binary not found — downloading from GitHub releases",
		"version", version,
		"cache_dir", cacheDir,
	)

	downloaded, err := r.downloadAndCache(version, cacheDir, logger)
	if err != nil {
		return "", fmt.Errorf(
			"localharness binary not found and auto-download failed: %w\n\n"+
				"Install it manually:\n"+
				"  1. go install github.com/divmora/localharness/cmd/localharness@v%s\n"+
				"  2. Download from https://github.com/divmora/localharness/releases/tag/v%s\n"+
				"  3. Set $LOCALHARNESS_BIN to the binary path",
			err, version, version,
		)
	}

	logger.Info("binary downloaded and cached", "path", downloaded, "version", version)
	return downloaded, nil
}

// resolveExplicit validates that the given path exists and is executable.
func (r *BinaryResolver) resolveExplicit(path string) (string, error) {
	// Try as-is first, then look up in PATH
	if isExecutable(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path, nil
		}
		return abs, nil
	}

	// Maybe it's a command name resolvable via PATH
	if looked, err := exec.LookPath(path); err == nil {
		return filepath.Abs(looked)
	}

	return "", fmt.Errorf("binary not found or not executable: %s", path)
}

func (r *BinaryResolver) resolveVersion() string {
	// 1. Explicit version set by SDK user
	if r.Version != "" {
		return r.Version
	}
	// 2. Build-time ldflags version (make build / release builds)
	if config.HarnessVersion != "" && config.HarnessVersion != "0.0.0-dev" {
		return config.HarnessVersion
	}
	// 3. Go module version — for third-party SDK users who import the SDK
	//    as a dependency (e.g., go get @v0.8.0). The Go toolchain records the
	//    module version in the binary's build metadata automatically.
	if v := moduleVersion(); v != "" {
		return v
	}
	// 4. Development build — no version available
	return config.HarnessVersion
}

// moduleVersion extracts this module's version from Go build info.
// When a third-party program imports the SDK as a dependency, Go records
// the dependency version (e.g., "v0.8.0") in the binary's build metadata.
// This allows the resolver to auto-download the matching binary version
// without requiring -ldflags or manual configuration.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	// If we ARE the main module (local dev build), the version is "(devel)"
	// which is not useful for downloading a release.
	if info.Main.Path == modulePath {
		return ""
	}
	// Search dependencies for our module version
	for _, dep := range info.Deps {
		if dep.Path == modulePath {
			v := strings.TrimPrefix(dep.Version, "v")
			// Ignore pseudo-versions (e.g., v0.0.0-20260528...) — they don't
			// correspond to GitHub release tags.
			if strings.Contains(v, "-0.") {
				return ""
			}
			return v
		}
	}
	return ""
}

func (r *BinaryResolver) resolveCacheDir() string {
	if r.CacheDir != "" {
		return r.CacheDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), defaultCacheDir)
	}
	return filepath.Join(home, defaultCacheDir)
}

func (r *BinaryResolver) resolveGitHubRepo() string {
	if r.GitHubRepo != "" {
		return r.GitHubRepo
	}
	return defaultGitHubRepo
}

// downloadAndCache downloads the binary for the given version from GitHub
// releases, verifies its checksum, and caches it.
func (r *BinaryResolver) downloadAndCache(version, cacheDir string, logger *slog.Logger) (string, error) {
	platSuffix, err := platformSuffix()
	if err != nil {
		return "", err
	}

	repo := r.resolveGitHubRepo()
	tag := "v" + version

	// Fetch release info from GitHub API
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	release, err := githubAPIGet(apiURL)
	if err != nil {
		return "", fmt.Errorf("fetch release %s: %w", tag, err)
	}

	// Find matching asset
	expectedAsset := fmt.Sprintf("localharness-%s-%s.tar.gz", version, platSuffix)
	var assetURL, checksumsURL string

	assets, _ := release["assets"].([]any)
	for _, a := range assets {
		asset, ok := a.(map[string]any)
		if !ok {
			continue
		}
		name, _ := asset["name"].(string)
		dlURL, _ := asset["browser_download_url"].(string)

		switch name {
		case expectedAsset:
			assetURL = dlURL
		case "checksums.txt":
			checksumsURL = dlURL
		}
	}

	if assetURL == "" {
		available := make([]string, 0)
		for _, a := range assets {
			if asset, ok := a.(map[string]any); ok {
				if name, ok := asset["name"].(string); ok {
					available = append(available, name)
				}
			}
		}
		return "", fmt.Errorf(
			"no release asset for platform %s in %s (expected: %s, available: %v)",
			platSuffix, tag, expectedAsset, available,
		)
	}

	// Download tarball to temp file
	logger.Info("downloading", "url", assetURL)
	tmpDir, err := os.MkdirTemp("", "localharness-download-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, expectedAsset)
	if err := httpDownload(assetURL, tarballPath); err != nil {
		return "", fmt.Errorf("download tarball: %w", err)
	}

	// Verify checksum if available
	if checksumsURL != "" {
		if err := verifyChecksum(tarballPath, expectedAsset, checksumsURL, tmpDir, logger); err != nil {
			return "", err
		}
	} else {
		logger.Warn("no checksums.txt found — skipping checksum verification")
	}

	// Extract binary from tarball
	extractedBin, err := extractBinaryFromTarGz(tarballPath, tmpDir)
	if err != nil {
		return "", fmt.Errorf("extract tarball: %w", err)
	}

	// Cache it
	versionDir := filepath.Join(cacheDir, "v"+version)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	cachedBin := filepath.Join(versionDir, binaryName)
	if err := copyFile(extractedBin, cachedBin); err != nil {
		return "", fmt.Errorf("cache binary: %w", err)
	}

	// Ensure executable
	if err := os.Chmod(cachedBin, 0755); err != nil {
		return "", fmt.Errorf("chmod cached binary: %w", err)
	}

	return cachedBin, nil
}

// --- Helper functions ---

// isExecutable checks if a file exists and has the executable bit set.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	// On Unix, check executable bit
	return info.Mode()&0111 != 0
}

// githubAPIGet makes a GET request to the GitHub API and returns the JSON response.
func githubAPIGet(url string) (map[string]any, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "localharness-sdk-go/"+config.HarnessVersion)

	// Support authenticated requests via $GITHUB_TOKEN for higher rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}

	return result, nil
}

// httpDownload downloads a file from a URL to a local path.
func httpDownload(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// verifyChecksum downloads checksums.txt and verifies the tarball's SHA256.
func verifyChecksum(tarballPath, expectedName, checksumsURL, tmpDir string, logger *slog.Logger) error {
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := httpDownload(checksumsURL, checksumsPath); err != nil {
		logger.Warn("could not download checksums — skipping verification", "error", err)
		return nil
	}

	// Parse checksums.txt: "<sha256>  <filename>"
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return nil
	}

	var expectedHash string
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == expectedName {
			expectedHash = parts[0]
			break
		}
	}

	if expectedHash == "" {
		logger.Warn("no checksum entry for asset — skipping verification", "asset", expectedName)
		return nil
	}

	// Compute actual hash
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf(
			"checksum mismatch for %s: expected %s, got %s — possible tampering or incomplete download",
			expectedName, expectedHash, actualHash,
		)
	}

	logger.Debug("checksum verified", "asset", expectedName)
	return nil
}

// extractBinaryFromTarGz extracts the "localharness" binary from a .tar.gz archive.
func extractBinaryFromTarGz(tarballPath, destDir string) (string, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}

		// Security: skip path traversal attempts
		cleanName := filepath.Clean(header.Name)
		if strings.Contains(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
			continue
		}

		// Look for the binary
		baseName := filepath.Base(cleanName)
		if baseName == binaryName && header.Typeflag == tar.TypeReg {
			destPath := filepath.Join(destDir, binaryName)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return "", err
			}

			// Limit copy to 500MB to prevent decompression bombs
			_, err = io.Copy(out, io.LimitReader(tr, 500*1024*1024))
			out.Close()
			if err != nil {
				return "", err
			}

			return destPath, nil
		}
	}

	return "", fmt.Errorf("binary %q not found in archive", binaryName)
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	// Preserve source permissions
	info, err := os.Stat(src)
	if err == nil {
		os.Chmod(dst, info.Mode())
	}

	return nil
}
