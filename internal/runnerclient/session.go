package runnerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/coder/websocket"
)

const (
	readLimit         = 16 << 20
	writeTimeout      = 10 * time.Second
	ptyBufferSize     = 32 << 10
	streamInputFrames = 256
)

type stream struct {
	pty   runner.PTY
	input chan []byte
	done  chan struct{}
}

type session struct {
	c  *client
	ws *websocket.Conn

	wg         sync.WaitGroup
	mu         sync.Mutex
	calls      map[uint64]context.CancelFunc
	streams    map[uint32]*stream
	nextStream uint32
	closed     bool
}

func newSession(c *client, ws *websocket.Conn) *session {
	return &session{
		c:       c,
		ws:      ws,
		calls:   map[uint64]context.CancelFunc{},
		streams: map[uint32]*stream{},
	}
}

func (s *session) serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		s.closeStreams()
		s.wg.Wait()
	}()

	s.wg.Go(func() { s.ping(ctx) })

	for {
		kind, data, err := s.ws.Read(ctx)
		if err != nil {
			return err
		}
		if kind == websocket.MessageBinary {
			s.input(data)
			continue
		}
		message, err := runnerproto.Decode(data)
		if err != nil {
			s.c.log.Warn("ignore a malformed control plane message", "error", err)
			continue
		}
		switch m := message.(type) {
		case runnerproto.Call:
			s.start(ctx, m)
		case runnerproto.Cancel:
			s.cancel(m.ID)
		default:
			s.c.log.Warn("ignore an unexpected control plane message", "type", fmt.Sprintf("%T", m))
		}
	}
}

func (s *session) send(ctx context.Context, m runnerproto.Message) error {
	data, err := runnerproto.Encode(m)
	if err != nil {
		return err
	}
	return s.write(ctx, websocket.MessageText, data)
}

func (s *session) write(ctx context.Context, kind websocket.MessageType, data []byte) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	if err := s.ws.Write(ctx, kind, data); err != nil {
		return fmt.Errorf("write to the control plane: %w", err)
	}
	return nil
}

func (s *session) ping(ctx context.Context) {
	ticker := time.NewTicker(s.c.pingInterval)
	defer ticker.Stop()
	for {
		if err := s.send(ctx, runnerproto.Ping{Active: s.c.active()}); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *session) start(ctx context.Context, call runnerproto.Call) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.calls[call.ID] = cancel
	s.mu.Unlock()

	s.wg.Go(func() {
		defer func() {
			s.mu.Lock()
			delete(s.calls, call.ID)
			s.mu.Unlock()
			cancel()
		}()

		value, after, err := s.dispatch(ctx, call)
		result, encodeErr := runnerproto.NewResult(call.ID, value, err)
		if encodeErr != nil {
			result, _ = runnerproto.NewResult(call.ID, nil, encodeErr)
		}
		if err := s.send(ctx, result); err != nil {
			s.c.log.Warn("send result", "method", call.Method, "error", err)
		}
		if after != nil {
			after()
		}
	})
}

func (s *session) cancel(id uint64) {
	s.mu.Lock()
	cancel, ok := s.calls[id]
	s.mu.Unlock()
	if ok {
		cancel()
	}
}

func (s *session) dispatch(ctx context.Context, call runnerproto.Call) (any, func(), error) {
	provider := s.c.opts.Provider
	switch call.Method {
	case runnerproto.MethodProvision:
		var params runnerproto.ProvisionParams
		if err := decodeParams(call, &params); err != nil {
			return nil, nil, err
		}
		spec := params.Spec()
		spec.Progress = func(step string) {
			if err := s.send(ctx, runnerproto.Progress{ID: call.ID, Step: step}); err != nil {
				s.c.log.Debug("send progress", "error", err)
			}
		}
		sb, err := provider.Provision(ctx, spec)
		if err != nil {
			return nil, nil, err
		}
		s.c.track(sb)
		return runnerproto.SandboxResult{Sandbox: string(sb)}, nil, nil

	case runnerproto.MethodExec:
		var params runnerproto.ExecParams
		if err := decodeParams(call, &params); err != nil {
			return nil, nil, err
		}
		result, err := provider.Exec(ctx, runner.SandboxID(params.Sandbox), params.Node, params.Cmd, params.Options())
		if err != nil {
			return nil, nil, err
		}
		return runnerproto.FromExecResult(result), nil, nil

	case runnerproto.MethodDestroy:
		var params runnerproto.SandboxParams
		if err := decodeParams(call, &params); err != nil {
			return nil, nil, err
		}
		sb := runner.SandboxID(params.Sandbox)
		err := provider.Destroy(ctx, sb)
		if err == nil || errors.Is(err, runner.ErrSandboxNotFound) {
			s.c.untrack(sb)
		}
		return nil, nil, err

	case runnerproto.MethodGC:
		if err := provider.GC(ctx); err != nil {
			return nil, nil, err
		}
		s.c.untrackAll()
		return nil, nil, nil

	case runnerproto.MethodHealth:
		return runnerproto.FromHealth(provider.Health(ctx)), nil, nil

	case runnerproto.MethodOpenTerminal:
		var params runnerproto.OpenTerminalParams
		if err := decodeParams(call, &params); err != nil {
			return nil, nil, err
		}
		return s.openTerminal(ctx, params)

	case runnerproto.MethodResize:
		var params runnerproto.ResizeParams
		if err := decodeParams(call, &params); err != nil {
			return nil, nil, err
		}
		st, ok := s.stream(params.Stream)
		if !ok {
			return nil, nil, fmt.Errorf("no terminal stream %d", params.Stream)
		}
		return nil, nil, st.pty.Resize(params.Cols, params.Rows)

	case runnerproto.MethodCloseTerminal:
		var params runnerproto.CloseTerminalParams
		if err := decodeParams(call, &params); err != nil {
			return nil, nil, err
		}
		s.closeStream(params.Stream)
		return nil, nil, nil

	default:
		return nil, nil, fmt.Errorf("unknown method %q", call.Method)
	}
}

func decodeParams(call runnerproto.Call, into any) error {
	if err := json.Unmarshal(call.Params, into); err != nil {
		return fmt.Errorf("decode %s params: %w", call.Method, err)
	}
	return nil
}

func (s *session) openTerminal(ctx context.Context, params runnerproto.OpenTerminalParams) (any, func(), error) {
	pty, err := s.c.opts.Provider.OpenTerminal(ctx, runner.SandboxID(params.Sandbox), params.Node)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = pty.Close()
		return nil, nil, err
	}

	st := &stream{pty: pty, input: make(chan []byte, streamInputFrames), done: make(chan struct{})}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = pty.Close()
		return nil, nil, errors.New("the connection to the control plane closed")
	}
	s.nextStream++
	id := s.nextStream
	s.streams[id] = st
	s.mu.Unlock()

	// The pumps start only after the result is written, so the control plane knows
	// the stream before its first frame arrives.
	after := func() {
		s.wg.Go(func() { s.pumpOutput(ctx, id, st) })
		s.wg.Go(func() { s.pumpInput(st) })
	}
	return runnerproto.StreamResult{Stream: id}, after, nil
}

func (s *session) stream(id uint32) (*stream, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.streams[id]
	return st, ok
}

func (s *session) pumpOutput(ctx context.Context, id uint32, st *stream) {
	buf := make([]byte, ptyBufferSize)
	for {
		n, err := st.pty.Read(buf)
		if n > 0 {
			if werr := s.write(ctx, websocket.MessageBinary, runnerproto.EncodeStreamFrame(id, bytes.Clone(buf[:n]))); werr != nil {
				s.closeStream(id)
				return
			}
		}
		if err != nil {
			if s.closeStream(id) {
				_ = s.send(ctx, runnerproto.StreamClosed{Stream: id})
			}
			return
		}
	}
}

func (s *session) pumpInput(st *stream) {
	for {
		select {
		case <-st.done:
			return
		case data := <-st.input:
			if _, err := st.pty.Write(data); err != nil {
				return
			}
		}
	}
}

func (s *session) input(frame []byte) {
	id, data, err := runnerproto.DecodeStreamFrame(frame)
	if err != nil {
		s.c.log.Warn("ignore a malformed stream frame", "error", err)
		return
	}
	st, ok := s.stream(id)
	if !ok {
		return
	}
	select {
	case st.input <- data:
	case <-st.done:
	default:
		s.c.log.Warn("terminal input is not draining, closing the stream", "stream", id)
		if s.closeStream(id) {
			_ = s.send(context.Background(), runnerproto.StreamClosed{Stream: id})
		}
	}
}

func (s *session) closeStream(id uint32) bool {
	s.mu.Lock()
	st, ok := s.streams[id]
	delete(s.streams, id)
	s.mu.Unlock()
	if !ok {
		return false
	}
	close(st.done)
	_ = st.pty.Close()
	return true
}

func (s *session) closeStreams() {
	s.mu.Lock()
	s.closed = true
	ids := make([]uint32, 0, len(s.streams))
	for id := range s.streams {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.closeStream(id)
	}
}
