package runnerproto

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

const Version = 1

const (
	CloseProtocolMismatch = 4001
	CloseUnauthorized     = 4003
)

const (
	TypeHello        = "hello"
	TypeWelcome      = "welcome"
	TypePing         = "ping"
	TypeCall         = "call"
	TypeResult       = "result"
	TypeProgress     = "progress"
	TypeStreamClosed = "stream_closed"
	TypeCancel       = "cancel"
)

const (
	MethodProvision     = "provision"
	MethodExec          = "exec"
	MethodDestroy       = "destroy"
	MethodGC            = "gc"
	MethodHealth        = "health"
	MethodOpenTerminal  = "open_terminal"
	MethodResize        = "resize"
	MethodCloseTerminal = "close_terminal"
)

type Message interface {
	messageType() string
}

type Hello struct {
	Proto        int      `json:"proto"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Capacity     int      `json:"capacity"`
	Sandboxes    []string `json:"sandboxes,omitempty"`
}

type Welcome struct {
	RunnerID string `json:"runner_id"`
}

type Ping struct {
	Active int `json:"active"`
}

type Call struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Result struct {
	ID    uint64          `json:"id"`
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error string          `json:"error,omitempty"`
	Code  string          `json:"code,omitempty"`
}

type Progress struct {
	ID   uint64 `json:"id"`
	Step string `json:"step"`
}

type StreamClosed struct {
	Stream uint32 `json:"stream"`
}

type Cancel struct {
	ID uint64 `json:"id"`
}

func (Hello) messageType() string        { return TypeHello }
func (Welcome) messageType() string      { return TypeWelcome }
func (Ping) messageType() string         { return TypePing }
func (Call) messageType() string         { return TypeCall }
func (Result) messageType() string       { return TypeResult }
func (Progress) messageType() string     { return TypeProgress }
func (StreamClosed) messageType() string { return TypeStreamClosed }
func (Cancel) messageType() string       { return TypeCancel }

func Encode(m Message) ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", m.messageType(), err)
	}
	head := fmt.Sprintf(`{"type":%q`, m.messageType())
	if bytes.Equal(body, []byte("{}")) {
		return []byte(head + "}"), nil
	}
	return append([]byte(head+","), body[1:]...), nil
}

func Decode(data []byte) (Message, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}

	switch kind := envelope.Type; kind {
	case TypeHello:
		return unmarshal[Hello](kind, data)
	case TypeWelcome:
		return unmarshal[Welcome](kind, data)
	case TypePing:
		return unmarshal[Ping](kind, data)
	case TypeCall:
		return unmarshal[Call](kind, data)
	case TypeResult:
		return unmarshal[Result](kind, data)
	case TypeProgress:
		return unmarshal[Progress](kind, data)
	case TypeStreamClosed:
		return unmarshal[StreamClosed](kind, data)
	case TypeCancel:
		return unmarshal[Cancel](kind, data)
	default:
		return nil, fmt.Errorf("decode message: unknown type %q", kind)
	}
}

func unmarshal[T Message](kind string, data []byte) (Message, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("decode %s: %w", kind, err)
	}
	return v, nil
}

const streamHeaderLen = 4

var ErrShortFrame = errors.New("stream frame shorter than its 4-byte header")

func EncodeStreamFrame(id uint32, data []byte) []byte {
	frame := make([]byte, streamHeaderLen+len(data))
	binary.BigEndian.PutUint32(frame, id)
	copy(frame[streamHeaderLen:], data)
	return frame
}

func DecodeStreamFrame(frame []byte) (uint32, []byte, error) {
	if len(frame) < streamHeaderLen {
		return 0, nil, ErrShortFrame
	}
	return binary.BigEndian.Uint32(frame), frame[streamHeaderLen:], nil
}
