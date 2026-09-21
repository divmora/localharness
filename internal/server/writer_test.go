package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func setupTestWSServer(t *testing.T) (*httptest.Server, <-chan *websocket.Conn) {
	connCh := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		connCh <- conn
	}))
	return s, connCh
}

func dialClientWS(t *testing.T, s *httptest.Server) *websocket.Conn {
	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial client error: %v", err)
	}
	return clientConn
}

func TestProtoBufPool(t *testing.T) {
	// 1. Get buffer from pool
	buf := getProtoBuf()
	if buf == nil {
		t.Fatal("expected non-nil buffer from pool")
	}
	if len(*buf) != 0 {
		t.Fatalf("expected 0 length buffer, got %d", len(*buf))
	}

	// 2. Append data and recycle
	*buf = append(*buf, []byte("hello-localharness")...)
	putProtoBuf(buf)

	// 3. Re-acquire and ensure length reset
	buf2 := getProtoBuf()
	if len(*buf2) != 0 {
		t.Fatalf("expected reset length on recycled buffer, got %d", len(*buf2))
	}
	putProtoBuf(buf2)

	// 4. Oversized buffers (> 64KB) should be discarded, not recycled
	hugeBuf := getProtoBuf()
	hugeData := make([]byte, maxPooledBufferSize+1024)
	*hugeBuf = append(*hugeBuf, hugeData...)
	putProtoBuf(hugeBuf) // should not crash or keep oversized in pool
}

func TestWSWriter_SendAndReceive(t *testing.T) {
	server, srvConnCh := setupTestWSServer(t)
	defer server.Close()

	clientConn := dialClientWS(t, server)
	defer clientConn.Close()

	srvConn := <-srvConnCh
	defer srvConn.Close()

	writer := newWSWriter(srvConn, slog.Default())
	go writer.writePump()
	defer writer.Close()

	// Send 10 messages through writer
	const numMessages = 10
	for i := 0; i < numMessages; i++ {
		msg := &pb.ServerMessage{
			Payload: &pb.ServerMessage_StepUpdate{
				StepUpdate: &pb.StepUpdate{
					ConversationId: "conv-test",
					State:          pb.StepUpdate_STATE_ACTIVE,
				},
			},
		}
		buf := getProtoBuf()
		var err error
		*buf, err = proto.MarshalOptions{}.MarshalAppend(*buf, msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		if !writer.Send(buf, false) {
			t.Fatalf("failed to send message %d", i)
		}
	}

	// Receive on client and verify
	for i := 0; i < numMessages; i++ {
		msgType, data, err := clientConn.ReadMessage()
		if err != nil {
			t.Fatalf("client read error at %d: %v", i, err)
		}
		if msgType != websocket.BinaryMessage {
			t.Fatalf("expected binary message, got %d", msgType)
		}
		var received pb.ServerMessage
		if err := proto.Unmarshal(data, &received); err != nil {
			t.Fatalf("unmarshal error at %d: %v", i, err)
		}
		if received.GetStepUpdate() == nil || received.GetStepUpdate().ConversationId != "conv-test" {
			t.Fatalf("unexpected message payload: %v", &received)
		}
	}
}

func TestWSWriter_DropStreamingUnderBackpressure(t *testing.T) {
	// Create a writer without writePump running so outCh fills up immediately
	writer := &wsWriter{
		conn:   nil,
		outCh:  make(chan *[]byte, 2), // small capacity for test
		done:   make(chan struct{}),
		logger: slog.Default(),
	}
	defer writer.Close()

	// Fill queue with 2 buffers
	b1 := getProtoBuf()
	*b1 = append(*b1, 1)
	b2 := getProtoBuf()
	*b2 = append(*b2, 2)

	if !writer.Send(b1, false) {
		t.Fatal("expected b1 to enqueue")
	}
	if !writer.Send(b2, false) {
		t.Fatal("expected b2 to enqueue")
	}

	// Now send a droppable streaming message - should be dropped immediately without blocking
	b3 := getProtoBuf()
	*b3 = append(*b3, 3)

	start := time.Now()
	sent := writer.Send(b3, true) // droppable = true
	duration := time.Since(start)

	if sent {
		t.Fatal("expected droppable message to be dropped when queue is full")
	}
	if duration > 50*time.Millisecond {
		t.Fatalf("droppable send took too long (%v), expected non-blocking drop", duration)
	}
}

func TestWSWriter_ConcurrentSendsAndClose(t *testing.T) {
	server, srvConnCh := setupTestWSServer(t)
	defer server.Close()

	clientConn := dialClientWS(t, server)
	defer clientConn.Close()

	srvConn := <-srvConnCh
	defer srvConn.Close()

	writer := newWSWriter(srvConn, slog.Default())
	go writer.writePump()

	// Read on client in background so TCP doesn't stall
	go func() {
		for {
			_, _, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const msgsPerGoroutine = 50

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < msgsPerGoroutine; i++ {
				msg := &pb.ServerMessage{
					Payload: &pb.ServerMessage_StepUpdate{
						StepUpdate: &pb.StepUpdate{
							ConversationId: "conv-concurrent",
							State:          pb.StepUpdate_STATE_STREAMING,
						},
					},
				}
				buf := getProtoBuf()
				var err error
				*buf, err = proto.MarshalOptions{}.MarshalAppend(*buf, msg)
				if err != nil {
					return
				}
				writer.Send(buf, true)
			}
		}()
	}

	// Allow some sends to proceed, then close concurrently
	time.Sleep(10 * time.Millisecond)
	writer.Close()

	wg.Wait()
}
