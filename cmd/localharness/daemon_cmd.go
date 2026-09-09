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

func runDaemonCommand(args []string, logger *slog.Logger) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: localharness daemon [start|stop|status|run]")
		os.Exit(1)
	}

	subcmd := args[0]
	switch subcmd {
	case "status":
		running, info, err := daemon.IsDaemonRunning()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking daemon status: %v\n", err)
			os.Exit(1)
		}
		if !running {
			fmt.Println("LocalHarness daemon: not running")
			return
		}
		fmt.Printf("LocalHarness daemon: running (PID %d)\n", info.PID)
		fmt.Printf("  Port:       %d\n", info.Port)
		fmt.Printf("  Started:    %s\n", info.StartedAt.Format(time.RFC3339))
		fmt.Printf("  Version:    %s\n", info.Version)

	case "stop":
		if err := daemon.StopDaemon(logger); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to stop daemon: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("LocalHarness daemon stopped.")

	case "start":
		running, info, _ := daemon.IsDaemonRunning()
		if running {
			fmt.Printf("LocalHarness daemon is already running (PID %d, Port %d)\n", info.PID, info.Port)
			return
		}

		daemonDir, err := daemon.GetDaemonDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot resolve daemon directory: %v\n", err)
			os.Exit(1)
		}

		logFile, err := os.OpenFile(filepath.Join(daemonDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot open daemon log file: %v\n", err)
			os.Exit(1)
		}

		selfPath, err := os.Executable()
		if err != nil {
			selfPath = "localharness"
		}

		cmd := exec.Command(selfPath, "daemon", "run")
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start daemon process: %v\n", err)
			os.Exit(1)
		}

		// Wait briefly for daemon to initialize daemon.json
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			if r, info, _ := daemon.IsDaemonRunning(); r && info != nil {
				fmt.Printf("LocalHarness daemon started successfully (PID %d, Port %d)\n", info.PID, info.Port)
				return
			}
		}
		fmt.Println("LocalHarness daemon started in background.")

	case "run":
		if err := daemon.RunDaemonServer(logger); err != nil {
			logger.Error("daemon server terminated with error", "error", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown daemon subcommand %q. Use start, stop, status, or run.\n", subcmd)
		os.Exit(1)
	}
}
