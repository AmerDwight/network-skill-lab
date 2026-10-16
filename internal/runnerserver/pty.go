package runnerserver

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/coder/websocket"
)

const (
	maxStreamBuffer = 1 << 20
	resizeTimeout   = 10 * time.Second
)

type stream struct {
	c  *conn
	id uint32

	once   sync.Once
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	ended  bool
	closed bool
}

func newStream(c *conn, id uint32) *stream {
	s := &stream{c: c, id: id}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *stream) push(data []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ended {
		return true
	}
	if s.buf.Len()+len(data) > maxStreamBuffer {
		return false
	}
	s.buf.Write(data)
	s.cond.Broadcast()
	return true
}

func (s *stream) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
	s.cond.Broadcast()
}

func (s *stream) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.buf.Len() == 0 && !s.ended && !s.closed {
		s.cond.Wait()
	}
	if s.closed {
		return 0, io.EOF
	}
	if s.buf.Len() > 0 {
		return s.buf.Read(b)
	}
	return 0, io.EOF
}

func (s *stream) Write(b []byte) (int, error) {
	s.mu.Lock()
	gone := s.closed || s.ended
	s.mu.Unlock()
	if gone {
		return 0, io.ErrClosedPipe
	}
	if err := s.c.sendFrame(websocket.MessageBinary, runnerproto.EncodeStreamFrame(s.id, b)); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (s *stream) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.buf.Reset()
		s.cond.Broadcast()
		s.mu.Unlock()
		s.c.closeStream(s.id)
	})
	return nil
}

func (s *stream) Resize(cols, rows uint16) error {
	ctx, cancel := context.WithTimeout(context.Background(), resizeTimeout)
	defer cancel()
	_, err := s.c.call(ctx, runnerproto.MethodResize, runnerproto.ResizeParams{Stream: s.id, Cols: cols, Rows: rows}, nil)
	return err
}
