package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/divmora/localharness/internal/errors"
)

// Info represents the metadata stored in ~/.divmora/localharness/daemon.json.
type Info struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port,omitempty"`
	Socket    string    `json:"socket,omitempty"`
	APIKey    string    `json:"apiKey"`
	StartedAt time.Time `json:"startedAt"`
	Version   string    `json:"version"`
}

// GetDaemonDir returns ~/.divmora/localharness/.
func GetDaemonDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, errors.ErrCodeConfiguration, "cannot determine home directory")
	}
	dir := filepath.Join(home, ".divmora", "localharness")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", errors.Wrap(err, errors.ErrCodeConfiguration, "cannot create daemon directory")
	}
	return dir, nil
}

// GetDaemonInfoPath returns ~/.divmora/localharness/daemon.json.
func GetDaemonInfoPath() (string, error) {
	dir, err := GetDaemonDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.json"), nil
}

// SaveDaemonInfo writes daemon metadata to disk.
func SaveDaemonInfo(info *Info) error {
	path, err := GetDaemonInfoPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeConfiguration, "marshal daemon info failed")
	}
	return os.WriteFile(path, data, 0600)
}

// LoadDaemonInfo reads daemon metadata from disk.
func LoadDaemonInfo() (*Info, error) {
	path, err := GetDaemonInfoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeConfiguration, "unmarshal daemon info failed")
	}
	return &info, nil
}

// RemoveDaemonInfo deletes daemon metadata file.
func RemoveDaemonInfo() error {
	path, err := GetDaemonInfoPath()
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}

// IsDaemonRunning checks if the daemon process is alive.
func IsDaemonRunning() (bool, *Info, error) {
	info, err := LoadDaemonInfo()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil
		}
		return false, nil, err
	}

	if isProcessAlive(info.PID) {
		return true, info, nil
	}

	// Stale daemon file
	_ = RemoveDaemonInfo()
	return false, nil, nil
}

// StopDaemon stops the currently running daemon.
func StopDaemon(logger *slog.Logger) error {
	running, info, err := IsDaemonRunning()
	if err != nil {
		return err
	}
	if !running {
		if logger != nil {
			logger.Info("no daemon running")
		}
		return nil
	}

	// Send termination signal
	if err := terminateProcess(info.PID); err != nil {
		return errors.Wrap(err, errors.ErrCodeEngineError, "failed to terminate daemon")
	}

	// Wait up to 5 seconds for process to exit
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if !isProcessAlive(info.PID) {
			_ = RemoveDaemonInfo()
			if logger != nil {
				logger.Info("daemon stopped successfully", "pid", info.PID)
			}
			return nil
		}
	}

	// Force kill if still running
	if p, err := os.FindProcess(info.PID); err == nil {
		_ = p.Kill()
	}
	_ = RemoveDaemonInfo()
	if logger != nil {
		logger.Info("daemon killed", "pid", info.PID)
	}
	return nil
}
