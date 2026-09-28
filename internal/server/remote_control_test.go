package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestRemoteControlWebAndAuth(t *testing.T) {
	apiKey := "test-secret-key-12345"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(apiKey, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	srv.ActiveSessionsProvider = func() []SessionSummary {
		return []SessionSummary{
			{
				ID:        "session-1",
				Title:     "Refactor Code",
				Workspace: "/test/dir",
				Status:    "RUNNING",
			},
		}
	}

	go func() {
		_ = srv.StartWithListener(context.Background(), ln)
	}()

	baseURL := "http://" + ln.Addr().String()

	// 1. Unauthenticated request without key -> 401
	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 without auth, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Request with ?key= -> 200 OK HTML
	respKey, err := http.Get(baseURL + "/?key=" + apiKey)
	if err != nil {
		t.Fatalf("get with ?key failed: %v", err)
	}
	if respKey.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 with ?key, got %d", respKey.StatusCode)
	}
	body, _ := io.ReadAll(respKey.Body)
	respKey.Body.Close()
	if !strings.Contains(string(body), "LocalHarness Remote Control") {
		t.Errorf("expected HTML body to contain title, got: %s", string(body)[:min(len(body), 100)])
	}

	// 3. Request with ?api_key= -> 200 OK HTML
	respAPIKey, err := http.Get(baseURL + "/?api_key=" + apiKey)
	if err != nil {
		t.Fatalf("get with ?api_key failed: %v", err)
	}
	if respAPIKey.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 with ?api_key, got %d", respAPIKey.StatusCode)
	}
	respAPIKey.Body.Close()

	// 4. Request with header x-localharness-api-key -> 200 OK
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/", nil)
	req.Header.Set("x-localharness-api-key", apiKey)
	respHeader, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get with header failed: %v", err)
	}
	if respHeader.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 with header, got %d", respHeader.StatusCode)
	}
	respHeader.Body.Close()

	// 5. Query /api/sessions
	respSessions, err := http.Get(baseURL + "/api/sessions?key=" + apiKey)
	if err != nil {
		t.Fatalf("get sessions failed: %v", err)
	}
	if respSessions.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for /api/sessions, got %d", respSessions.StatusCode)
	}
	var summaries []SessionSummary
	if err := json.NewDecoder(respSessions.Body).Decode(&summaries); err != nil {
		t.Fatalf("decode summaries failed: %v", err)
	}
	respSessions.Body.Close()

	if len(summaries) != 1 || summaries[0].ID != "session-1" {
		t.Errorf("unexpected summaries: %+v", summaries)
	}
}

func TestJSONWebSocketConnection(t *testing.T) {
	apiKey := "test-json-ws-key"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(apiKey, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	receivedCh := make(chan *pb.ClientMessage, 1)
	srv.SessionHandlerWithReq = func(conn *websocket.Conn, r *http.Request) {
		isJSON := r.URL.Query().Get("format") == "json"
		writer := newWSWriterWithMode(conn, logger, isJSON)
		go writer.writePump()

		// Read loop for test
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var clientMsg pb.ClientMessage
				if err := protojson.Unmarshal(data, &clientMsg); err == nil {
					receivedCh <- &clientMsg
					// Reply with server message in JSON
					resp := &pb.ServerMessage{
						Payload: &pb.ServerMessage_InitResponse{
							InitResponse: &pb.InitResponse{
								ConversationId: "conv-123",
							},
						},
					}
					jsonData, _ := protojson.Marshal(resp)
					buf := getProtoBuf()
					*buf = append(*buf, jsonData...)
					writer.Send(buf, false)
				}
			}
		}
	}

	go func() {
		_ = srv.StartWithListener(context.Background(), ln)
	}()

	wsURL := "ws://" + ln.Addr().String() + "/?key=" + apiKey + "&format=json"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed (status: %v): %v", resp, err)
	}
	defer conn.Close()

	// Send JSON message
	req := &pb.ClientMessage{
		Payload: &pb.ClientMessage_UserMessage{
			UserMessage: &pb.UserMessage{
				Content: "Hello JSON WebSocket!",
			},
		},
	}
	data, _ := protojson.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write text message failed: %v", err)
	}

	// Wait for server to receive
	select {
	case received := <-receivedCh:
		if received.GetUserMessage() == nil || received.GetUserMessage().Content != "Hello JSON WebSocket!" {
			t.Errorf("unexpected message received: %v", received)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server to receive message")
	}

	// Read reply
	msgType, replyData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read reply failed: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Errorf("expected TextMessage, got %d", msgType)
	}
	var srvMsg pb.ServerMessage
	if err := protojson.Unmarshal(replyData, &srvMsg); err != nil {
		t.Fatalf("protojson unmarshal reply failed: %v", err)
	}
	if srvMsg.GetInitResponse() == nil || srvMsg.GetInitResponse().ConversationId != "conv-123" {
		t.Errorf("unexpected reply payload: %v", &srvMsg)
	}
}
