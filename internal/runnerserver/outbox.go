package runnerserver

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	writeTimeout    = 10 * time.Second
	maxOutboxBytes  = 1 << 20
	maxOutboxFrames = 4096
)

var (
	errOutboxFull   = errors.New("outbound buffer to the runner exceeded")
	errOutboxClosed = errors.New("outbound buffer to the runner is closed")
)

type frame struct {
	kind websocket.MessageType
	data []byte
}

type outbox struct {
	frames   chan frame
	maxBytes int

	mu     sync.Mutex
	bytes  int
	count  int
	closed bool
}

func newOutbox(maxBytes int) *outbox {
	return &outbox{frames: make(chan frame, maxOutboxFrames), maxBytes: maxBytes}
}

func (o *outbox) run(ctx context.Context, ws *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case f, ok := <-o.frames:
			if !ok {
				return nil
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := ws.Write(writeCtx, f.kind, f.data)
			cancel()
			o.written(len(f.data))
			if err != nil {
				return err
			}
		}
	}
}

func (o *outbox) send(kind websocket.MessageType, data []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case o.closed:
		return errOutboxClosed
	case o.count+1 > maxOutboxFrames || o.bytes+len(data) > o.maxBytes:
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
