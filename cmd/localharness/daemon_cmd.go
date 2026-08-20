package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/daemon"
	"github.com/divmora/localharness/internal/server"
	"github.com/divmora/localharness/internal/util"
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
		runDaemonServer(logger)

	default:
		fmt.Fprintf(os.Stderr, "Unknown daemon subcommand %q. Use start, stop, status, or run.\n", subcmd)
		os.Exit(1)
	}
}

func runDaemonServer(logger *slog.Logger) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Error("failed to bind to 127.0.0.1:0", "error", err)
		os.Exit(1)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	apiKeyBytes := make([]byte, 32)
	if _, err := rand.Read(apiKeyBytes); err != nil {
		logger.Error("failed to generate API key", "error", err)
		os.Exit(1)
	}
	apiKey := hex.EncodeToString(apiKeyBytes)

	info := &daemon.Info{
		PID:       os.Getpid(),
		Port:      port,
		APIKey:    apiKey,
		StartedAt: time.Now(),
		Version:   config.HarnessVersion,
	}

	srv := server.NewServer(apiKey, logger)

	daemonDir, err := daemon.GetDaemonDir()
	if err == nil {
		sockPath := filepath.Join(daemonDir, "harness.sock")
		_ = os.Remove(sockPath)
		unixLn, err := net.Listen("unix", sockPath)
		if err == nil {
			info.Socket = sockPath
			defer func() {
				_ = unixLn.Close()
				_ = os.Remove(sockPath)
			}()
			go func() {
				if err := srv.StartWithListener(context.Background(), unixLn); err != nil {
					logger.Debug("unix socket server stopped", "error", err)
				}
			}()
		}
	}

	if err := daemon.SaveDaemonInfo(info); err != nil {
		logger.Error("failed to save daemon info", "error", err)
		os.Exit(1)
	}
	defer daemon.RemoveDaemonInfo()

	logger.Info("LocalHarness daemon running", "pid", info.PID, "port", port, "socket", info.Socket, "version", config.HarnessVersion)

	// Global session holder for daemon connections
	cfg := config.DefaultServerConfig()
	cfg.SessionID = util.NewUUID()
	cfg.IsNewSession = true

	var activeSession *server.Session
	var sessionMu sync.Mutex

	srv.SessionHandler = func(conn *websocket.Conn) {
		sessionMu.Lock()
		if activeSession != nil {
			sessionMu.Unlock()
			logger.Info("attaching new client to existing daemon session")
			activeSession.Attach(conn)
			return
		}
		session := server.NewSession(conn, cfg, logger)
		session.SetDaemon(true)
		activeSession = session
		srv.SetActiveSession(session)
		sessionMu.Unlock()

		session.Run()
	}

	if err := srv.StartWithListener(context.Background(), ln); err != nil {
		logger.Error("daemon server error", "error", err)
	}
}
