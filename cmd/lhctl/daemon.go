package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/divmora/localharness/internal/daemon"
)

func statusDaemon() error {
	running, info, err := daemon.IsDaemonRunning()
	if err != nil {
		return fmt.Errorf("error checking daemon status: %w", err)
	}
	if !running {
		fmt.Println("LocalHarness daemon: not running")
		return nil
	}
	fmt.Printf("LocalHarness daemon: running (PID %d)\n", info.PID)
	fmt.Printf("  Port:       %d\n", info.Port)
	fmt.Printf("  Started:    %s\n", info.StartedAt.Format(time.RFC3339))
	fmt.Printf("  Version:    %s\n", info.Version)
	return nil
}

func stopDaemon() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := daemon.StopDaemon(logger); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}
	fmt.Println("LocalHarness daemon stopped.")
	return nil
}

func startDaemon() error {
	running, info, _ := daemon.IsDaemonRunning()
	if running {
		fmt.Printf("LocalHarness daemon is already running (PID %d, Port %d)\n", info.PID, info.Port)
		return nil
	}

	daemonDir, err := daemon.GetDaemonDir()
	if err != nil {
		return fmt.Errorf("cannot resolve daemon directory: %w", err)
	}

	selfPath, err := os.Executable()
	if err != nil {
		selfPath = "localharness"
	}
	binDir := filepath.Dir(selfPath)
	harnessBin := filepath.Join(binDir, "localharness")
	if _, err := os.Stat(harnessBin); err != nil {
		harnessBin = "localharness"
	}

	logFile, err := os.OpenFile(filepath.Join(daemonDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer logFile.Close()
	}

	cmd := exec.Command(harnessBin, "daemon", "run")
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if r, info, _ := daemon.IsDaemonRunning(); r && info != nil {
			fmt.Printf("LocalHarness daemon started successfully (PID %d, Port %d)\n", info.PID, info.Port)
			return nil
		}
	}
	fmt.Println("LocalHarness daemon started in background.")
	return nil
}

func runDaemonControl(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: lhctl daemon [start|stop|status]")
		os.Exit(1)
	}

	subcmd := args[0]
	var err error
	switch subcmd {
	case "status":
		err = statusDaemon()
	case "stop":
		err = stopDaemon()
	case "start":
		err = startDaemon()
	default:
		fmt.Fprintf(os.Stderr, "Unknown daemon command %q. Use start, stop, or status.\n", subcmd)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
