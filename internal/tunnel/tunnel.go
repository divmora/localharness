package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var (
	tunnelURLRegex = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)
)

func getDaemonDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	dir := filepath.Join(home, ".divmora", "localharness")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir daemon dir: %w", err)
	}
	return dir, nil
}

// Info represents metadata for an active Cloudflare Argo Quick Tunnel.
type Info struct {
	URL              string    `json:"url"`                        // Raw public tunnel URL (https://xxx.trycloudflare.com)
	ControlURL       string    `json:"controlUrl"`                 // Full Web Remote Control URL with ?key=...#<sessionID>
	PID              int       `json:"pid"`                        // Process ID of cloudflared
	StartedAt        time.Time `json:"startedAt"`                  // Startup timestamp
	LocalPort        int       `json:"localPort"`                  // Local forwarded port
	InitialSessionID string    `json:"initialSessionId,omitempty"` // Default session ID for deep link
}

// GetTunnelInfoPath returns ~/.divmora/localharness/tunnel.json.
func GetTunnelInfoPath() (string, error) {
	dir, err := getDaemonDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tunnel.json"), nil
}

// SaveTunnelInfo writes tunnel metadata to disk.
func SaveTunnelInfo(info *Info) error {
	path, err := GetTunnelInfoPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tunnel info: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// LoadTunnelInfo reads tunnel metadata from disk.
func LoadTunnelInfo() (*Info, error) {
	path, err := GetTunnelInfoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("unmarshal tunnel info: %w", err)
	}
	return &info, nil
}

// RemoveTunnelInfo deletes the tunnel metadata file.
func RemoveTunnelInfo() error {
	path, err := GetTunnelInfoPath()
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}

// IsTunnelRunning checks if the recorded tunnel process is alive.
func IsTunnelRunning() (bool, *Info, error) {
	info, err := LoadTunnelInfo()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil
		}
		return false, nil, err
	}

	if isPIDAlive(info.PID) {
		return true, info, nil
	}

	_ = RemoveTunnelInfo()
	return false, nil, nil
}

// Manager orchestrates the lifecycle of the cloudflared Quick Tunnel.
type Manager struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	info   *Info
	logger *slog.Logger
}

// NewManager creates a new TunnelManager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger: logger,
	}
}

// Start launches a Cloudflare Quick Tunnel forwarding to localPort without login.
func (m *Manager) Start(ctx context.Context, localPort int, initialSessionID string, apiKey string) (*Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if running, info, _ := IsTunnelRunning(); running && info != nil {
		m.info = info
		m.logger.Info("cloudflared tunnel is already running", "url", info.ControlURL, "pid", info.PID)
		return info, nil
	}

	binPath, err := ResolveBinary(m.logger)
	if err != nil {
		return nil, fmt.Errorf("resolve cloudflared binary: %w", err)
	}

	daemonDir, err := getDaemonDir()
	if err != nil {
		return nil, fmt.Errorf("resolve daemon directory: %w", err)
	}

	logFile, err := os.OpenFile(filepath.Join(daemonDir, "tunnel.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		m.logger.Warn("cannot open tunnel log file, proceeding without file log", "error", err)
	}

	args := []string{
		"tunnel",
		"--url", fmt.Sprintf("http://127.0.0.1:%d", localPort),
		"--no-autoupdate",
	}

	cmd := exec.CommandContext(ctx, binPath, args...)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stderrPipe.Close()
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stderrPipe.Close()
		stdoutPipe.Close()
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}

	m.cmd = cmd

	// Channel to receive announced trycloudflare.com URL
	urlChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Multiplex stderr & stdout to find URL and write to tunnel.log
	combinedReader := io.MultiReader(stderrPipe, stdoutPipe)
	scanner := bufio.NewScanner(combinedReader)

	go func() {
		var foundURL string
		for scanner.Scan() {
			line := scanner.Text()
			if logFile != nil {
				_, _ = fmt.Fprintln(logFile, line)
			}
			if foundURL == "" {
				if match := tunnelURLRegex.FindString(line); match != "" {
					foundURL = match
					urlChan <- match
				}
			}
		}
		if err := scanner.Err(); err != nil && foundURL == "" {
			errChan <- err
		}
		if logFile != nil {
			_ = logFile.Close()
		}
	}()

	// Wait up to 30 seconds for Cloudflare to assign the quick tunnel URL
	var rawURL string
	select {
	case rawURL = <-urlChan:
		m.logger.Info("cloudflared tunnel established", "url", rawURL)
	case err := <-errChan:
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("cloudflared exited before assigning url: %w", err)
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("timed out waiting for cloudflared to establish quick tunnel")
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return nil, ctx.Err()
	}

	// Format Web Remote Control URL
	controlURL := rawURL
	if apiKey != "" {
		controlURL = fmt.Sprintf("%s/?key=%s", rawURL, apiKey)
	}
	if initialSessionID != "" {
		controlURL = fmt.Sprintf("%s#%s", controlURL, initialSessionID)
	}

	info := &Info{
		URL:              rawURL,
		ControlURL:       controlURL,
		PID:              cmd.Process.Pid,
		StartedAt:        time.Now(),
		LocalPort:        localPort,
		InitialSessionID: initialSessionID,
	}

	if err := SaveTunnelInfo(info); err != nil {
		m.logger.Warn("failed to save tunnel.json", "error", err)
	}

	m.info = info

	// Launch background monitor to clean up when cloudflared process exits
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		_ = RemoveTunnelInfo()
		m.cmd = nil
		m.info = nil
		m.mu.Unlock()
		m.logger.Info("cloudflared tunnel process exited")
	}()

	return info, nil
}

// Stop terminates the running cloudflared tunnel process.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Terminate owned command if active
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}

	// Also check if any external tunnel PID is running
	if running, info, _ := IsTunnelRunning(); running && info != nil {
		if p, err := os.FindProcess(info.PID); err == nil {
			_ = p.Kill()
		}
	}

	_ = RemoveTunnelInfo()
	m.info = nil
	m.logger.Info("cloudflared tunnel stopped")
	return nil
}

// Status returns current active tunnel info if running.
func (m *Manager) Status() (*Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.info != nil && isPIDAlive(m.info.PID) {
		return m.info, true
	}

	running, info, _ := IsTunnelRunning()
	if running && info != nil {
		m.info = info
		return info, true
	}

	return nil, false
}

// isPIDAlive checks if process is alive.
func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, signal 0 tests process existence without killing it
	return p.Signal(os.Signal(nil)) == nil
}
