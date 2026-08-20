package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func TestClientCommunication(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	var receivedClientMsgs []*pb.ClientMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send InitResponse
		initResp := &pb.ServerMessage{
			Payload: &pb.ServerMessage_InitResponse{
				InitResponse: &pb.InitResponse{
					ConversationId: "conv-1234",
					HarnessVersion: "0.0.0-dev",
				},
			},
		}
		data, _ := proto.Marshal(initResp)
		_ = conn.WriteMessage(websocket.BinaryMessage, data)

		for {
			_, msgData, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var clientMsg pb.ClientMessage
			if err := proto.Unmarshal(msgData, &clientMsg); err == nil {
				receivedClientMsgs = append(receivedClientMsgs, &clientMsg)
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	c, err := New(Config{
		URL:    wsURL,
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer c.Close()

	// Wait for InitResponse
	select {
	case event := <-c.Events():
		if event.GetInitResponse() == nil || event.GetInitResponse().ConversationId != "conv-1234" {
			t.Errorf("unexpected init response: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for InitResponse")
	}

	// Send messages
	if err := c.SendUserMessage("Hello", nil, nil); err != nil {
		t.Errorf("SendUserMessage failed: %v", err)
	}
	if err := c.SendPermissionResponse("req-1", true, "", pb.PermissionResponse_SCOPE_ONCE); err != nil {
		t.Errorf("SendPermissionResponse failed: %v", err)
	}
	if err := c.SendSetYoloMode(true); err != nil {
		t.Errorf("SendSetYoloMode failed: %v", err)
	}
	if err := c.SendWorkspaceRequest("list", "", "", ""); err != nil {
		t.Errorf("SendWorkspaceRequest failed: %v", err)
	}
	if err := c.SendInterrupt(); err != nil {
		t.Errorf("SendInterrupt failed: %v", err)
	}
	if err := c.SendResume("proceed"); err != nil {
		t.Errorf("SendResume failed: %v", err)
	}
	if err := c.SendCancel(); err != nil {
		t.Errorf("SendCancel failed: %v", err)
	}

	// Allow server to process messages
	time.Sleep(100 * time.Millisecond)

	if len(receivedClientMsgs) < 7 {
		t.Errorf("expected 7 client messages received, got %d", len(receivedClientMsgs))
	}
}
