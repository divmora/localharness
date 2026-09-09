package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/server"
	"github.com/divmora/localharness/internal/util"
)

// RunDaemonServer runs the LocalHarness daemon listener and WebSocket server.
// Used by both localharness daemon run and lhctl daemon run.
func RunDaemonServer(logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Error("failed to bind to 127.0.0.1:0", "error", err)
		return fmt.Errorf("bind 127.0.0.1:0: %w", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	apiKeyBytes := make([]byte, 32)
	if _, err := rand.Read(apiKeyBytes); err != nil {
		logger.Error("failed to generate API key", "error", err)
		return fmt.Errorf("generate API key: %w", err)
	}
	apiKey := hex.EncodeToString(apiKeyBytes)

	info := &Info{
		PID:       os.Getpid(),
		Port:      port,
		APIKey:    apiKey,
		StartedAt: time.Now(),
		Version:   config.HarnessVersion,
	}

	srv := server.NewServer(apiKey, logger)

	daemonDir, err := GetDaemonDir()
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

	if err := SaveDaemonInfo(info); err != nil {
		logger.Error("failed to save daemon info", "error", err)
		return fmt.Errorf("save daemon info: %w", err)
	}
	defer func() { _ = RemoveDaemonInfo() }()

	logger.Info("LocalHarness daemon running", "pid", info.PID, "port", port, "socket", info.Socket, "version", config.HarnessVersion)

	// Multi-session registry for daemon connections
	var sessionMu sync.Mutex
	sessions := make(map[string]*server.Session)

	srv.SessionHandlerWithReq = func(conn *websocket.Conn, r *http.Request) {
		reqSessionID := r.Header.Get("x-localharness-session-id")
		if reqSessionID == "" {
			reqSessionID = r.URL.Query().Get("session_id")
		}

		sessionMu.Lock()
		if reqSessionID != "" {
			// Explicit attach/resume request by session ID
			if existingSess, ok := sessions[reqSessionID]; ok {
				sessionMu.Unlock()
				logger.Info("attaching client to existing daemon session", "session_id", reqSessionID)
				existingSess.Attach(conn)
				return
			}
		}

		// New session for this client/workspace connection
		sessCfg := config.DefaultServerConfig()
		if reqSessionID != "" {
			sessCfg.SessionID = reqSessionID
			sessCfg.IsNewSession = false
		} else {
			sessCfg.SessionID = util.NewUUID()
			sessCfg.IsNewSession = true
		}

		session := server.NewSession(conn, sessCfg, logger)
		session.SetDaemon(true)
		sessions[sessCfg.SessionID] = session
		srv.SetActiveSession(session)
		sessionMu.Unlock()

		defer func() {
			sessionMu.Lock()
			delete(sessions, sessCfg.SessionID)
			sessionMu.Unlock()
		}()

		session.Run()
	}

	if err := srv.StartWithListener(context.Background(), ln); err != nil {
		logger.Error("daemon server error", "error", err)
		return fmt.Errorf("daemon server error: %w", err)
	}

	return nil
}
