package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	writeTimeout      = 5 * time.Second
	maxOutboxBytes    = 1 << 20
	maxOutboxMessages = 256
)

var (
	errOutboxFull   = errors.New("outbound buffer exceeded")
	errOutboxClosed = errors.New("outbound buffer is closed")
)

func accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*", "[::1]:*"},
	})
}

type frame struct {
	kind websocket.MessageType
	data []byte
}

type outbox struct {
	conn   *websocket.Conn
	frames chan frame
	done   chan struct{}

	mu     sync.Mutex
	bytes  int
	count  int
	closed bool
}

func newOutbox(conn *websocket.Conn) *outbox {
	return &outbox{
		conn:   conn,
		frames: make(chan frame, maxOutboxMessages),
		done:   make(chan struct{}),
	}
}

func (o *outbox) start(ctx context.Context) {
	go func() {
		defer close(o.done)
		for {
			select {
			case <-ctx.Done():
				return
			case f, ok := <-o.frames:
				if !ok {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
				err := o.conn.Write(writeCtx, f.kind, f.data)
				cancel()
				o.written(len(f.data))
				if err != nil {
					return
				}
			}
		}
	}()
}

func (o *outbox) send(kind websocket.MessageType, data []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case o.closed:
		return errOutboxClosed
	case o.count+1 > maxOutboxMessages || o.bytes+len(data) > maxOutboxBytes:
		o.closed = true
		close(o.frames)
		return errOutboxFull
	}
	o.count++
	o.bytes += len(data)
	// count stays within the channel capacity, so this send cannot block while holding the lock.
	o.frames <- frame{kind: kind, data: data}
	return nil
}

func (o *outbox) finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	close(o.frames)
}

func (o *outbox) written(n int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.count--
	o.bytes -= n
}

type socket struct {
	conn      *websocket.Conn
	out       *outbox
	log       *slog.Logger
	attemptID string
}

func newSocket(conn *websocket.Conn, log *slog.Logger, attemptID string) *socket {
	return &socket{conn: conn, out: newOutbox(conn), log: log, attemptID: attemptID}
}

func (s *socket) start(ctx context.Context) {
	s.out.start(ctx)
}

func (s *socket) closed() <-chan struct{} {
	return s.out.done
}

func (s *socket) sendJSON(message any) bool {
	data, err := json.Marshal(message)
	if err != nil {
		s.log.Error("encode websocket message", "attempt", s.attemptID, "error", err)
		return false
	}
	return s.send(websocket.MessageText, data)
}

func (s *socket) sendBinary(data []byte) bool {
	return s.send(websocket.MessageBinary, data)
}

func (s *socket) send(kind websocket.MessageType, data []byte) bool {
	err := s.out.send(kind, data)
	if errors.Is(err, errOutboxFull) {
		s.log.Warn("closing a websocket client that cannot keep up", "attempt", s.attemptID)
		_ = s.conn.Close(websocket.StatusPolicyViolation, errOutboxFull.Error())
	}
	return err == nil
}

func (s *socket) shutdown(reason string) {
	s.out.finish()
	select {
	case <-s.out.done:
	case <-time.After(writeTimeout):
	}
	_ = s.conn.Close(websocket.StatusNormalClosure, reason)
}
