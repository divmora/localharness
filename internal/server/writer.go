package server

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

const (
	// wsWriteQueueSize is the capacity of the outbound message queue per connection.
	wsWriteQueueSize = 512

	// wsWriteTimeout is the deadline for individual WebSocket write operations.
	wsWriteTimeout = 10 * time.Second

	// wsCriticalWriteTimeout is the max wait time when enqueueing critical messages
	// into a saturated outbound queue before assuming the client is dead.
	wsCriticalWriteTimeout = 1 * time.Second

	// maxPooledBufferSize defines the maximum capacity of a buffer recycled into the pool.
	// Larger buffers (e.g. huge diffs or file dumps) are discarded to avoid pool bloat.
	maxPooledBufferSize = 64 * 1024
)

var protoBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 2048)
		return &b
	},
}

// getProtoBuf retrieves a clean byte buffer from the pool.
func getProtoBuf() *[]byte {
	buf := protoBufPool.Get().(*[]byte)
	*buf = (*buf)[:0]
	return buf
}

// putProtoBuf returns a byte buffer to the pool if within acceptable size limits.
func putProtoBuf(buf *[]byte) {
	if buf == nil {
		return
	}
	if cap(*buf) > maxPooledBufferSize {
		return
	}
	*buf = (*buf)[:0]
	protoBufPool.Put(buf)
}

// isStreamingMessage checks if a ServerMessage contains a partial streaming delta.
func isStreamingMessage(msg *pb.ServerMessage) bool {
	if su := msg.GetStepUpdate(); su != nil {
		return su.State == pb.StepUpdate_STATE_STREAMING
	}
	return false
}

// wsWriter manages asynchronous outbound WebSocket writes for a single client connection.
// It ensures that only a single goroutine (writePump) ever writes to the websocket.Conn,
// eliminating write contention, mutex stalls, and protocol race conditions.
type wsWriter struct {
	conn      *websocket.Conn
	outCh     chan *[]byte
	done      chan struct{}
	closeOnce sync.Once
	logger    *slog.Logger
}

// newWSWriter creates a new wsWriter for the given connection.
func newWSWriter(conn *websocket.Conn, logger *slog.Logger) *wsWriter {
	if logger == nil {
		logger = slog.Default()
	}
	return &wsWriter{
		conn:   conn,
		outCh:  make(chan *[]byte, wsWriteQueueSize),
		done:   make(chan struct{}),
		logger: logger,
	}
}

// writePump is the dedicated write loop goroutine for the WebSocket connection.
// It serializes writes, enforces write deadlines, emits periodic keepalive pings,
// and recycles serialized protobuf buffers back to protoBufPool.
func (w *wsWriter) writePump() {
	ticker := time.NewTicker(wsPingInterval)
	defer func() {
		ticker.Stop()
		if w.conn != nil {
			_ = w.conn.Close()
		}
		w.Close()
		w.drain()
	}()

	for {
		select {
		case <-w.done:
			return

		case <-ticker.C:
			if w.conn == nil {
				return
			}
			_ = w.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := w.conn.WriteMessage(websocket.PingMessage, []byte("keepalive")); err != nil {
				w.logger.Debug("ping write failed, connection likely closed", "error", err)
				return
			}

		case bufPtr, ok := <-w.outCh:
			if !ok {
				return
			}
			if w.conn == nil {
				putProtoBuf(bufPtr)
				return
			}
			_ = w.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			err := w.conn.WriteMessage(websocket.BinaryMessage, *bufPtr)
			putProtoBuf(bufPtr)
			if err != nil {
				w.logger.Error("WebSocket write error", "error", err)
				return
			}
		}
	}
}

// Send queues a serialized buffer to be written to the WebSocket.
// If droppable is true and the queue is full, the frame is dropped to prevent backpressure.
// If droppable is false (critical state), it attempts to enqueue with a timeout.
func (w *wsWriter) Send(bufPtr *[]byte, droppable bool) bool {
	if bufPtr == nil {
		return false
	}

	select {
	case <-w.done:
		putProtoBuf(bufPtr)
		return false
	default:
	}

	if droppable {
		select {
		case <-w.done:
			putProtoBuf(bufPtr)
			return false
		case w.outCh <- bufPtr:
			return true
		default:
			putProtoBuf(bufPtr)
			w.logger.Warn("dropping streaming message due to saturated WebSocket write queue")
			return false
		}
	}

	select {
	case <-w.done:
		putProtoBuf(bufPtr)
		return false
	case w.outCh <- bufPtr:
		return true
	case <-time.After(wsCriticalWriteTimeout):
		putProtoBuf(bufPtr)
		w.logger.Error("WebSocket write queue blocked on critical message; closing stalled connection")
		w.Close()
		return false
	}
}

// Close gracefully stops the writer and closes the connection.
func (w *wsWriter) Close() {
	w.closeOnce.Do(func() {
		close(w.done)
		if w.conn != nil {
			_ = w.conn.Close()
		}
	})
	w.drain()
}

// drain empties any pending buffers in outCh and returns them to protoBufPool.
func (w *wsWriter) drain() {
	for {
		select {
		case bufPtr := <-w.outCh:
			putProtoBuf(bufPtr)
		default:
			return
		}
	}
}
