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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/conversation"
	"github.com/divmora/localharness/internal/server"
	"github.com/divmora/localharness/internal/tunnel"
	"github.com/divmora/localharness/internal/util"
)

// RunDaemonServer runs the LocalHarness daemon listener and WebSocket server.
// Used by both localharness daemon run and lhctl daemon run.
func RunDaemonServer(logger *slog.Logger) error {
	return RunDaemonServerWithTunnel(logger, false)
}

// RunDaemonServerWithTunnel runs the daemon listener and optionally exposes it via a Cloudflare Quick Tunnel.
func RunDaemonServerWithTunnel(logger *slog.Logger, enableTunnel bool) error {
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

	if enableTunnel || os.Getenv("LOCALHARNESS_TUNNEL") == "true" {
		tunMgr := tunnel.NewManager(logger)
		tunInfo, err := tunMgr.Start(context.Background(), port, "", apiKey)
		if err == nil && tunInfo != nil {
			info.TunnelURL = tunInfo.ControlURL
			info.TunnelPID = tunInfo.PID
			srv.StopTunnelHandler = func() error {
				return tunMgr.Stop()
			}
			defer func() {
				_ = tunMgr.Stop()
			}()
			logger.Info("Cloudflare Quick Tunnel active for daemon", "url", tunInfo.ControlURL)
		} else if err != nil {
			logger.Warn("could not start cloudflared quick tunnel for daemon", "error", err)
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

	srv.ActiveSessionsProvider = func() []server.SessionSummary {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		var list []server.SessionSummary
		seen := make(map[string]bool)

		// 1. In-memory active daemon sessions
		for id, sess := range sessions {
			seen[id] = true
			list = append(list, sess.Summary())
		}

		// 2. Discover recent saved conversations on disk (~/.divmora/localharness/conversations/*.pb)
		home, _ := os.UserHomeDir()
		if home != "" {
			convDir := filepath.Join(home, ".divmora", "localharness", "conversations")
			if entries, err := os.ReadDir(convDir); err == nil {
				type diskEntry struct {
					id      string
					path    string
					modTime time.Time
				}
				var diskList []diskEntry
				for _, e := range entries {
					if !e.IsDir() && filepath.Ext(e.Name()) == ".pb" {
						id := strings.TrimSuffix(e.Name(), ".pb")
						if seen[id] {
							continue
						}
						if fi, err := e.Info(); err == nil {
							diskList = append(diskList, diskEntry{
								id:      id,
								path:    filepath.Join(convDir, e.Name()),
								modTime: fi.ModTime(),
							})
						}
					}
				}
				// Sort by modTime desc
				sort.Slice(diskList, func(i, j int) bool {
					return diskList[i].modTime.After(diskList[j].modTime)
				})

				limit := 15
				if len(diskList) < limit {
					limit = len(diskList)
				}
				for _, de := range diskList[:limit] {
					seen[de.id] = true
					data, err := os.ReadFile(de.path)
					if err != nil {
						continue
					}
					var st pb.ConversationState
					if err := proto.Unmarshal(data, &st); err != nil {
						continue
					}
					desc := conversation.ExtractDescription(st.Messages)
					if desc == "" {
						desc = "Session " + de.id[:8]
					}
					ws := ""
					if st.Config != nil && len(st.Config.Workspaces) > 0 {
						if st.Config.Workspaces[0].Name != "" {
							ws = st.Config.Workspaces[0].Name
						} else if st.Config.Workspaces[0].Directory != "" {
							ws = filepath.Base(st.Config.Workspaces[0].Directory)
						}
					}
					var tokens int64 = 0
					if st.TotalUsage != nil {
						tokens = int64(st.TotalUsage.TotalTokens)
					}
					list = append(list, server.SessionSummary{
						ID:          de.id,
						Title:       desc,
						Description: desc,
						Workspace:   ws,
						Status:      "SAVED",
						TokenCount:  tokens,
						UpdatedAt:   st.UpdatedAt,
					})
				}
			}
		}

		return list
	}

	srv.SessionHandlerWithReq = func(conn *websocket.Conn, r *http.Request) {
		reqSessionID := r.Header.Get("x-localharness-session-id")
		if reqSessionID == "" {
			reqSessionID = r.URL.Query().Get("session_id")
		}
		isJSON := r.URL.Query().Get("format") == "json"

		sessionMu.Lock()
		if reqSessionID != "" {
			// Explicit attach/resume request by session ID
			if existingSess, ok := sessions[reqSessionID]; ok {
				sessionMu.Unlock()
				logger.Info("attaching client to existing daemon session", "session_id", reqSessionID, "json", isJSON)
				existingSess.AttachWithMode(conn, isJSON)
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

		session := server.NewSessionWithMode(conn, sessCfg, logger, isJSON)
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
