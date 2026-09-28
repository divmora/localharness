// Package server implements the WebSocket server that accepts SDK connections.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/divmora/localharness/internal/config"
	"github.com/gorilla/websocket"
)

//go:embed web/remote_control.html
var remoteControlHTML []byte

// SessionSummary represents concise metadata for a running or saved session.
type SessionSummary struct {
	ID              string `json:"id"`
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	Workspace       string `json:"workspace,omitempty"`
	Model           string `json:"model,omitempty"`
	Status          string `json:"status"` // RUNNING, WAITING, IDLE, SAVED
	WaitingApproval bool   `json:"waitingApproval"`
	TokenCount      int64  `json:"tokenCount"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// Server is the WebSocket server for LocalHarness.
type Server struct {
	apiKey    string
	upgrader  websocket.Upgrader
	logger    *slog.Logger
	agentCard *AgentCard
	// SessionHandler is called for each new WebSocket connection.
	SessionHandler func(conn *websocket.Conn)
	// SessionHandlerWithReq is called for each new WebSocket connection with HTTP request metadata.
	SessionHandlerWithReq func(conn *websocket.Conn, r *http.Request)
	// ActiveSessionsProvider returns list of active sessions for the Web Remote Control dashboard.
	ActiveSessionsProvider func() []SessionSummary
	// StopTunnelHandler is invoked when the Web Remote Control requests stopping the public tunnel.
	StopTunnelHandler func() error

	activeSession *Session
	sessionMu     sync.RWMutex
}

// NewServer creates a new WebSocket server.
// The apiKey is used to authenticate incoming WebSocket connections
// via the x-localharness-api-key header.
func NewServer(apiKey string, logger *slog.Logger) *Server {
	return &Server{
		apiKey: apiKey,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024 * 1024, // 1MB
			WriteBufferSize: 1024 * 1024,
			// Allow all origins for local connections
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		logger: logger,
	}
}

// SetAgentCard sets a custom agent card to serve at /.well-known/agent.json.
// If not set, a default card is generated from the binary's capabilities.
func (s *Server) SetAgentCard(card *AgentCard) {
	s.agentCard = card
}

// SetActiveSession stores the current session for status queries.
func (s *Server) SetActiveSession(sess *Session) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.activeSession = sess
}

// StartWithListener begins serving on a pre-bound listener.
// The listener is typically created by main.go binding to localhost:0 atomically.
// The ctx parameter is currently unused but reserved for future graceful shutdown.
// Session lifecycle handles cleanup — the HTTP server exits when the listener closes.
func (s *Server) StartWithListener(ctx context.Context, ln net.Listener) error {
	mux := http.NewServeMux()

	// Secure endpoints
	mux.HandleFunc("/", AuthMiddleware(s.apiKey, s.handleRoot))
	mux.HandleFunc("/control", AuthMiddleware(s.apiKey, s.handleRemoteControlWeb))
	mux.HandleFunc("/ui", AuthMiddleware(s.apiKey, s.handleRemoteControlWeb))
	mux.HandleFunc("/api/sessions", AuthMiddleware(s.apiKey, s.handleListSessions))
	mux.HandleFunc("/api/tunnel/stop", AuthMiddleware(s.apiKey, s.handleStopTunnel))
	mux.HandleFunc("/status", AuthMiddleware(s.apiKey, s.handleStatus))

	// Public endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/.well-known/agent.json", s.handleAgentCard)

	s.logger.Info("LocalHarness serving",
		"address", ln.Addr().String(),
		"version", config.HarnessVersion,
	)

	return http.Serve(ln, mux)
}

// handleRoot dispatches to WebSocket upgrade if requested, or serves the Web Remote Control UI.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") == "websocket" {
		s.handleWebSocket(w, r)
		return
	}
	s.handleRemoteControlWeb(w, r)
}

// handleRemoteControlWeb serves the embedded Web Remote Control single-page application.
func (s *Server) handleRemoteControlWeb(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(remoteControlHTML)
}

// handleListSessions returns active sessions for the remote control multi-session switcher.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var sessions []SessionSummary
	if s.ActiveSessionsProvider != nil {
		sessions = s.ActiveSessionsProvider()
	} else {
		s.sessionMu.RLock()
		sess := s.activeSession
		s.sessionMu.RUnlock()
		if sess != nil {
			sessions = append(sessions, sess.Summary())
		}
	}
	_ = json.NewEncoder(w).Encode(sessions)
}

// handleStopTunnel stops the Cloudflare tunnel upon remote request.
func (s *Server) handleStopTunnel(w http.ResponseWriter, r *http.Request) {
	if s.StopTunnelHandler != nil {
		if err := s.StopTunnelHandler(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"stopped"}`)
}

// handleWebSocket upgrades HTTP to WebSocket and creates a session.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", "error", err)
		return
	}

	s.logger.Info("new WebSocket connection", "remote", r.RemoteAddr)

	if s.SessionHandlerWithReq != nil {
		go s.SessionHandlerWithReq(conn, r)
	} else if s.SessionHandler != nil {
		// Handle session in a goroutine
		go s.SessionHandler(conn)
	} else {
		conn.Close()
	}
}

// handleHealth returns a simple health check response.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","version":"%s"}`, config.HarnessVersion)
}

// handleStatus returns the current agent session state.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.sessionMu.RLock()
	sess := s.activeSession
	s.sessionMu.RUnlock()

	status := "WAITING"
	if sess != nil {
		status = sess.Status()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"%s"}`, status)
}
