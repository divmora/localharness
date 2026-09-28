package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BinaryName returns the platform-specific name for cloudflared executable.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

// ResolveBinary finds an executable cloudflared binary or downloads it if missing.
// Resolution order:
//  1. $CLOUDFLARED_BIN environment variable
//  2. System PATH (exec.LookPath)
//  3. Cached binary at ~/.divmora/localharness/bin/cloudflared
//  4. Auto-download from official Cloudflare GitHub releases
func ResolveBinary(logger *slog.Logger) (string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Environment variable override
	if envBin := os.Getenv("CLOUDFLARED_BIN"); envBin != "" {
		if path, err := exec.LookPath(envBin); err == nil {
			logger.Debug("cloudflared resolved via CLOUDFLARED_BIN", "path", path)
			return path, nil
		}
		if _, err := os.Stat(envBin); err == nil {
			return envBin, nil
		}
	}

	// 2. System PATH
	if path, err := exec.LookPath("cloudflared"); err == nil {
		logger.Debug("cloudflared resolved via PATH", "path", path)
		return path, nil
	}

	// 3. Cached binary in ~/.divmora/localharness/bin/
	daemonDir, err := getDaemonDir()
	if err == nil {
		cachedPath := filepath.Join(daemonDir, "bin", BinaryName())
		if info, err := os.Stat(cachedPath); err == nil && !info.IsDir() {
			logger.Debug("cloudflared resolved via cache", "path", cachedPath)
			return cachedPath, nil
		}
	}

	// 4. Auto-download from GitHub releases
	logger.Info("cloudflared not found locally; auto-downloading official release...")
	destPath, err := downloadCloudflared(logger)
	if err != nil {
		return "", fmt.Errorf("auto-download cloudflared: %w\nInstall manually via 'brew install cloudflared' or from https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/", err)
	}

	return destPath, nil
}

// releaseAssetInfo returns the asset filename and whether it's a tar.gz archive.
func releaseAssetInfo() (string, bool, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch {
	case goos == "darwin" && goarch == "arm64":
		return "cloudflared-darwin-arm64.tgz", true, nil
	case goos == "darwin" && goarch == "amd64":
		return "cloudflared-darwin-amd64.tgz", true, nil
	case goos == "linux" && goarch == "amd64":
		return "cloudflared-linux-amd64", false, nil
	case goos == "linux" && goarch == "arm64":
		return "cloudflared-linux-arm64", false, nil
	case goos == "windows" && goarch == "amd64":
		return "cloudflared-windows-amd64.exe", false, nil
	default:
		return "", false, fmt.Errorf("unsupported platform for auto-download: %s/%s", goos, goarch)
	}
}

// downloadCloudflared downloads the latest cloudflared release binary.
func downloadCloudflared(logger *slog.Logger) (string, error) {
	assetName, isTgz, err := releaseAssetInfo()
	if err != nil {
		return "", err
	}

	daemonDir, err := getDaemonDir()
	if err != nil {
		return "", fmt.Errorf("get daemon dir: %w", err)
	}

	binDir := filepath.Join(daemonDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}

	finalBinPath := filepath.Join(binDir, BinaryName())
	downloadURL := fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/latest/download/%s", assetName)

	logger.Info("downloading cloudflared", "url", downloadURL, "target", finalBinPath)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("http get %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with HTTP %d from %s", resp.StatusCode, downloadURL)
	}

	if isTgz {
		// Extract tar.gz archive to find binary
		gzr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("read gzip: %w", err)
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		found := false
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("read tar: %w", err)
			}
			if strings.HasSuffix(hdr.Name, "cloudflared") || hdr.Name == "cloudflared" {
				outFile, err := os.OpenFile(finalBinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
				if err != nil {
					return "", fmt.Errorf("create executable file: %w", err)
				}
				if _, err := io.Copy(outFile, tr); err != nil {
					outFile.Close()
					return "", fmt.Errorf("write binary: %w", err)
				}
				outFile.Close()
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("cloudflared binary not found inside archive %s", assetName)
		}
	} else {
		// Direct binary download
		outFile, err := os.OpenFile(finalBinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return "", fmt.Errorf("create executable file: %w", err)
		}
		if _, err := io.Copy(outFile, resp.Body); err != nil {
			outFile.Close()
			return "", fmt.Errorf("write binary: %w", err)
		}
		outFile.Close()
	}

	if err := os.Chmod(finalBinPath, 0755); err != nil {
		return "", fmt.Errorf("chmod binary: %w", err)
	}

	logger.Info("cloudflared installed successfully", "path", finalBinPath)
	return finalBinPath, nil
}
