// LocalHarness — Agent Runtime Engine
//
// A Go binary that runs a complete agentic loop:
// receives user prompts via WebSocket, calls Gemini, dispatches built-in tools,
// and streams StepUpdate events back to the SDK.
//
// The binary uses a pipe-based handshake protocol:
//  1. SDK spawns this binary and captures stdin/stdout/stderr pipes
//  2. SDK writes InputConfig (4-byte LE length + protobuf) to stdin
//  3. Binary reads InputConfig, binds localhost:0, generates API key
//  4. Binary writes OutputConfig (4-byte LE length + protobuf) to stdout
//  5. Binary closes stdout (signals handshake complete)
//  6. SDK connects via ws://localhost:<port>/ with x-localharness-api-key header
//  7. When SDK closes stdin (EOF), binary exits gracefully
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/server"
)

func main() {
	cfg := config.ParseFlags()

	if cfg.Version {
		fmt.Fprintf(os.Stderr, "localharness v%s\n", config.HarnessVersion)
		os.Exit(0)
	}

	// Set up logger (always to stderr — stdout is used for handshake)
	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// ─── Phase 1: Pipe Handshake ────────────────────────────────────────

	// Read InputConfig from stdin
	inputCfg, err := readInputConfig(os.Stdin)
	if err != nil {
		logger.Error("failed to read InputConfig from stdin", "error", err)
		os.Exit(1)
	}

	// Apply InputConfig overrides
	if inputCfg.Workspace != "" {
		cfg.Workspace = inputCfg.Workspace
	}
	if inputCfg.Debug {
		cfg.Debug = true
		// Upgrade logger to debug level
		logLevel = slog.LevelDebug
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		}))
	}

	logger.Debug("received InputConfig",
		"workspace", inputCfg.Workspace,
		"debug", inputCfg.Debug,
	)

	// Bind to 127.0.0.1:0 atomically to explicitly use IPv4 loopback
	// This prevents issues where it binds to IPv6 [::1] but GUI tries to connect to 127.0.0.1
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Error("failed to bind to 127.0.0.1:0", "error", err)
		os.Exit(1)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	// Generate crypto-random API key (32 bytes → 64 hex chars)
	apiKeyBytes := make([]byte, 32)
	if _, err := rand.Read(apiKeyBytes); err != nil {
		logger.Error("failed to generate API key", "error", err)
		os.Exit(1)
	}
	apiKey := hex.EncodeToString(apiKeyBytes)
	cfg.APIKey = apiKey

	// Write OutputConfig to stdout
	outputCfg := &pb.OutputConfig{
		Port:           int32(port),
		ApiKey:         apiKey,
		HarnessVersion: config.HarnessVersion,
	}
	if err := writeOutputConfig(os.Stdout, outputCfg); err != nil {
		logger.Error("failed to write OutputConfig to stdout", "error", err)
		os.Exit(1)
	}

	// Close stdout — signals SDK that handshake is complete
	os.Stdout.Close()

	logger.Info("LocalHarness starting",
		"version", config.HarnessVersion,
		"port", port,
		"workspace", cfg.Workspace,
		"data_dir", cfg.AppDataDir,
		"debug", cfg.Debug,
	)

	// ─── Phase 2: Start WebSocket Server ────────────────────────────────

	srv := server.NewServer(apiKey, logger)

	srv.SessionHandler = func(conn *websocket.Conn) {
		session := server.NewSession(conn, cfg, logger)
		session.Run()
	}

	// Launch stdin EOF watcher — when the SDK closes stdin, close the listener
	// so http.Serve returns and the process can exit cleanly.
	// Note: The SDK's Close() sequence ensures the WebSocket is closed FIRST
	// (triggering session cleanup + turnWg.Wait + SaveAll), then stdin is closed.
	go watchStdinEOF(ln, logger)

	// Start server on the pre-bound listener (blocks until listener closes)
	if err := srv.StartWithListener(context.Background(), ln); err != nil {
		// "use of closed network connection" is expected on clean shutdown
		if !isClosedListenerError(err) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	logger.Info("localharness exited cleanly")
}

// readInputConfig reads a length-prefixed protobuf InputConfig from r.
// Wire format: 4-byte little-endian length + protobuf payload.
func readInputConfig(r io.Reader) (*pb.InputConfig, error) {
	// Read 4-byte length prefix
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeHandshakeError,
			"read length prefix failed").
			WithComponent("localharness")
	}

	if length > 1024*1024 { // 1MB sanity limit
		return nil, errors.New(errors.ErrCodeHandshakeError,
			"InputConfig too large").
			WithContext("length", length).
			WithContext("max_length", 1048576).
			WithComponent("localharness")
	}

	// Read protobuf payload
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeHandshakeError,
			"read payload failed").
			WithComponent("localharness")
	}

	var cfg pb.InputConfig
	if err := proto.Unmarshal(buf, &cfg); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeProtocolError,
			"unmarshal InputConfig failed").
			WithComponent("localharness")
	}

	return &cfg, nil
}

// writeOutputConfig writes a length-prefixed protobuf OutputConfig to w.
// Wire format: 4-byte little-endian length + protobuf payload.
func writeOutputConfig(w io.Writer, cfg *pb.OutputConfig) error {
	data, err := proto.Marshal(cfg)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeProtocolError,
			"marshal OutputConfig failed").
			WithComponent("localharness")
	}

	// Write 4-byte length prefix
	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return errors.Wrap(err, errors.ErrCodeHandshakeError,
			"write length prefix failed").
			WithComponent("localharness")
	}

	// Write protobuf payload
	if _, err := w.Write(data); err != nil {
		return errors.Wrap(err, errors.ErrCodeHandshakeError,
			"write payload failed").
			WithComponent("localharness")
	}

	return nil
}

// watchStdinEOF blocks reading stdin until EOF (SDK closed its stdin pipe).
// On EOF, closes the listener so http.Serve() returns and the process can exit.
// This is the final shutdown step — by the time stdin closes, the SDK has already
// closed the WebSocket and the session has flushed its state.
func watchStdinEOF(ln net.Listener, logger *slog.Logger) {
	buf := make([]byte, 1)
	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			logger.Info("stdin closed (EOF) — closing listener for clean shutdown")
			ln.Close()
			return
		}
	}
}

// isClosedListenerError checks if the error is the expected "use of closed
// network connection" that occurs when we close the listener for shutdown.
func isClosedListenerError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}
