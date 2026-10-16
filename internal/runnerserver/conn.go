package runnerserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/coder/websocket"
)

type pending struct {
	method   string
	results  chan reply
	progress func(step string)
}

type reply struct {
	result runnerproto.Result
	stream *stream
}

type conn struct {
	hub    *Hub
	ws     *websocket.Conn
	id     string
	name   string
	hello  runnerproto.Hello
	log    *slog.Logger
	out    *outbox
	remote *RemoteRunner

	ctx      context.Context
	cancel   context.CancelFunc
	finished chan struct{}
	nextID   atomic.Uint64

	lastTouch time.Time

	mu            sync.Mutex
	done          bool
	pending       map[uint64]*pending
	streams       map[uint32]*stream
	closedStreams map[uint32]struct{}
}

func newConn(h *Hub, ws *websocket.Conn, id string, hello runnerproto.Hello, log *slog.Logger) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		hub:           h,
		ws:            ws,
		id:            id,
		name:          hello.Name,
		hello:         hello,
		log:           log,
		out:           newOutbox(h.maxOutbox),
		ctx:           ctx,
		cancel:        cancel,
		finished:      make(chan struct{}),
		pending:       map[uint64]*pending{},
		streams:       map[uint32]*stream{},
		closedStreams: map[uint32]struct{}{},
	}
	c.remote = &RemoteRunner{c: c}
	go func() {
		err := c.out.run(c.ctx, ws)
		if err == nil {
			return
		}
		if c.ctx.Err() == nil {
			c.log.Warn("write to runner", "error", err)
		}
		_ = ws.CloseNow()
	}()
	return c
}

func (c *conn) serve(ctx context.Context) {
	defer close(c.finished)
	defer c.teardown()

	watchdog := time.AfterFunc(c.hub.pingTimeout, func() {
		c.log.Warn("no ping from the runner, dropping the connection", "timeout", c.hub.pingTimeout)
		_ = c.ws.CloseNow()
	})
	defer watchdog.Stop()

	stop := context.AfterFunc(ctx, c.cancel)
	defer stop()

	for {
		kind, data, err := c.ws.Read(c.ctx)
		if err != nil {
			if c.ctx.Err() == nil {
				c.log.Info("runner connection closed", "status", websocket.CloseStatus(err), "error", err)
			}
			return
		}
		if kind == websocket.MessageBinary {
			c.handleFrame(data)
			continue
		}
		message, err := runnerproto.Decode(data)
		if err != nil {
			c.log.Warn("ignore a malformed runner message", "error", err)
			continue
		}
		switch m := message.(type) {
		case runnerproto.Ping:
			watchdog.Reset(c.hub.pingTimeout)
			c.hub.pinged(c, m.Active)
		case runnerproto.Result:
			c.handleResult(m)
		case runnerproto.Progress:
			c.handleProgress(m)
		case runnerproto.StreamClosed:
			c.handleStreamClosed(m.Stream)
		default:
			c.log.Warn("ignore an unexpected runner message", "type", fmt.Sprintf("%T", m))
		}
	}
}

func (c *conn) teardown() {
	c.mu.Lock()
	c.done = true
	streams := c.streams
	c.streams = map[uint32]*stream{}
	c.pending = map[uint64]*pending{}
	c.mu.Unlock()

	c.cancel()
	c.out.finish()
	_ = c.ws.CloseNow()
	for _, s := range streams {
		s.end()
	}
}

func (c *conn) shutdown(code websocket.StatusCode, reason string) {
	c.out.finish()
	go func() { _ = c.ws.Close(code, reason) }()
}

func (c *conn) send(m runnerproto.Message) error {
	data, err := runnerproto.Encode(m)
	if err != nil {
		return err
	}
	return c.sendFrame(websocket.MessageText, data)
}

func (c *conn) sendFrame(kind websocket.MessageType, data []byte) error {
	err := c.out.send(kind, data)
	if errors.Is(err, errOutboxFull) {
		c.log.Warn("the runner cannot keep up, dropping the connection")
		_ = c.ws.CloseNow()
	}
	if errors.Is(err, errOutboxClosed) {
		return ErrDisconnected
	}
	return err
}

func (c *conn) call(ctx context.Context, method string, params any, progress func(string)) (json.RawMessage, error) {
	r, err := c.roundTrip(ctx, method, params, progress)
	return r.result.Value, err
}

func (c *conn) roundTrip(ctx context.Context, method string, params any, progress func(string)) (reply, error) {
	var raw json.RawMessage
	if params != nil {
		var err error
		if raw, err = json.Marshal(params); err != nil {
			return reply{}, fmt.Errorf("encode %s params: %w", method, err)
		}
	}

	id := c.nextID.Add(1)
	p := &pending{method: method, results: make(chan reply, 1), progress: progress}
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return reply{}, fmt.Errorf("%s on runner %s: %w", method, c.name, ErrDisconnected)
	}
	c.pending[id] = p
	c.mu.Unlock()

	if err := c.send(runnerproto.Call{ID: id, Method: method, Params: raw}); err != nil {
		c.forget(id)
		return reply{}, fmt.Errorf("%s on runner %s: %w", method, c.name, err)
	}

	select {
	case r := <-p.results:
		return r, r.result.Err()
	case <-ctx.Done():
		c.forget(id)
		c.abandon(p)
		_ = c.send(runnerproto.Cancel{ID: id})
		return reply{}, ctx.Err()
	case <-c.ctx.Done():
		select {
		case r := <-p.results:
			return r, r.result.Err()
		default:
			return reply{}, fmt.Errorf("%s on runner %s: %w", method, c.name, ErrDisconnected)
		}
	}
}

func (c *conn) notify(method string, params any) {
	raw, err := json.Marshal(params)
	if err != nil {
		c.log.Error("encode notification", "method", method, "error", err)
		return
	}
	_ = c.send(runnerproto.Call{ID: c.nextID.Add(1), Method: method, Params: raw})
}

func (c *conn) forget(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

// abandon releases a terminal the runner opened for a call whose caller gave up.
func (c *conn) abandon(p *pending) {
	select {
	case r := <-p.results:
		if r.stream != nil {
			_ = r.stream.Close()
		}
	default:
	}
}

func (c *conn) handleResult(result runnerproto.Result) {
	var r reply
	c.mu.Lock()
	p, ok := c.pending[result.ID]
	if ok {
		delete(c.pending, result.ID)
		if p.method == runnerproto.MethodOpenTerminal && result.OK {
			r.stream = c.openStreamLocked(result)
		}
	}
	c.mu.Unlock()
	if ok {
		r.result = result
		p.results <- r
	}
}

// openStreamLocked registers the stream before the read loop moves on, because the
// runner starts sending its frames right after the result.
func (c *conn) openStreamLocked(result runnerproto.Result) *stream {
	var opened runnerproto.StreamResult
	if err := json.Unmarshal(result.Value, &opened); err != nil {
		c.log.Warn("malformed open_terminal result", "error", err)
		return nil
	}
	s := newStream(c, opened.Stream)
	c.streams[opened.Stream] = s
	return s
}

func (c *conn) handleProgress(progress runnerproto.Progress) {
	c.mu.Lock()
	p, ok := c.pending[progress.ID]
	c.mu.Unlock()
	if ok && p.progress != nil {
		p.progress(progress.Step)
	}
}

func (c *conn) handleFrame(data []byte) {
	id, payload, err := runnerproto.DecodeStreamFrame(data)
	if err != nil {
		c.log.Warn("ignore a malformed stream frame", "error", err)
		return
	}
	c.mu.Lock()
	s, ok := c.streams[id]
	_, closed := c.closedStreams[id]
	if !ok && !closed {
		c.closedStreams[id] = struct{}{}
	}
	c.mu.Unlock()

	switch {
	case ok:
		if !s.push(payload) {
			c.log.Warn("terminal reader cannot keep up, closing the stream", "stream", id)
			_ = s.Close()
		}
	case !closed:
		c.notify(runnerproto.MethodCloseTerminal, runnerproto.CloseTerminalParams{Stream: id})
	}
}

func (c *conn) handleStreamClosed(id uint32) {
	c.mu.Lock()
	s, ok := c.streams[id]
	delete(c.streams, id)
	c.closedStreams[id] = struct{}{}
	c.mu.Unlock()
	if ok {
		s.end()
	}
}

func (c *conn) closeStream(id uint32) {
	c.mu.Lock()
	_, ok := c.streams[id]
	delete(c.streams, id)
	c.closedStreams[id] = struct{}{}
	done := c.done
	c.mu.Unlock()
	if ok && !done {
		c.notify(runnerproto.MethodCloseTerminal, runnerproto.CloseTerminalParams{Stream: id})
	}
}
