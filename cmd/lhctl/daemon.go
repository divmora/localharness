package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/divmora/localharness/cmd/lhctl/client"
	"github.com/divmora/localharness/internal/daemon"
	"github.com/divmora/localharness/internal/tunnel"
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
	if info.TunnelURL != "" {
		fmt.Printf("  Tunnel:     %s\n", info.TunnelURL)
	}
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

	harnessBin, err := client.ResolveDaemonBinary(slog.Default())
	if err != nil {
		return fmt.Errorf("resolve daemon binary: %w", err)
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

func startDaemonWithTunnel(enableTunnel bool) error {
	if enableTunnel {
		_ = os.Setenv("LOCALHARNESS_TUNNEL", "true")
	}
	return startDaemon()
}

func runDaemon() error {
	return runDaemonWithTunnel(false)
}

func runDaemonWithTunnel(enableTunnel bool) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return daemon.RunDaemonServerWithTunnel(logger, enableTunnel)
}

func statusTunnel() error {
	running, info, err := tunnel.IsTunnelRunning()
	if err != nil {
		return fmt.Errorf("check tunnel: %w", err)
	}
	if !running || info == nil {
		fmt.Println("Cloudflare Quick Tunnel: not running")
		return nil
	}
	fmt.Printf("Cloudflare Quick Tunnel: running (PID %d)\n", info.PID)
	fmt.Printf("  URL:        %s\n", info.URL)
	fmt.Printf("  Control:    %s\n", info.ControlURL)
	fmt.Printf("  Local Port: %d\n", info.LocalPort)
	fmt.Printf("  Started:    %s\n", info.StartedAt.Format(time.RFC3339))
	if qr, err := tunnel.GenerateTerminalQRCode(info.ControlURL); err == nil {
		fmt.Println("\nScan with phone:")
		fmt.Println(qr)
	}
	return nil
}

func stopTunnel() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mgr := tunnel.NewManager(logger)
	if err := mgr.Stop(); err != nil {
		return fmt.Errorf("stop tunnel: %w", err)
	}
	fmt.Println("Cloudflare Quick Tunnel stopped.")
	return nil
}

func startTunnel(customPort int) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mgr := tunnel.NewManager(logger)

	port := customPort
	apiKey := ""
	if port == 0 {
		running, dInfo, _ := daemon.IsDaemonRunning()
		if running && dInfo != nil {
			port = dInfo.Port
			apiKey = dInfo.APIKey
		} else {
			return fmt.Errorf("no daemon running. Start daemon first or specify --port")
		}
	}

	info, err := mgr.Start(context.Background(), port, "", apiKey)
	if err != nil {
		return fmt.Errorf("start tunnel: %w", err)
	}

	fmt.Printf("🌐 Cloudflare Quick Tunnel active (PID %d)\n", info.PID)
	fmt.Printf("Remote Control URL: %s\n\n", info.ControlURL)
	if qr, err := tunnel.GenerateTerminalQRCode(info.ControlURL); err == nil {
		fmt.Println("Scan with phone:")
		fmt.Println(qr)
	}
	return nil
}
