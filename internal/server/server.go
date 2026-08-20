// Package server implements the WebSocket server that accepts SDK connections.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/divmora/localharness/internal/config"
)

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
	mux.HandleFunc("/", AuthMiddleware(s.apiKey, s.handleWebSocket))
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
