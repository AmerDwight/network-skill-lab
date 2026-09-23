package runner

import (
	"context"
	"errors"
	"io"
	"time"
)

type Env string

const (
	EnvContainer Env = "container"
	EnvVM        Env = "vm"
)

type SandboxID string

var (
	ErrNodeNotFound    = errors.New("node not found")
	ErrSandboxNotFound = errors.New("sandbox not found")
)

type NodeSpec struct {
	Name       string
	Role       string
	K3sDisable []string
}

type EndpointSpec struct {
	Node    string
	Iface   string
	Address string
}

type LinkSpec struct {
	Name      string
	Subnet    string
	Endpoints []EndpointSpec
}

type Script struct {
	Name    string
	Content []byte
}

type SandboxSpec struct {
	AttemptID string
	Image     string
	Nodes     []NodeSpec
	Links     []LinkSpec
	Setup     Script
	Env       map[string]string
	Internet  bool
	Progress  func(step string)
}

type ExecOptions struct {
	Env     map[string]string
	Stdin   io.Reader
	Timeout time.Duration
	User    string
}

type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	TimedOut bool
}

type PTY interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
}

type Health struct {
	OK             bool
	Docker         bool
	Image          bool
	ImageName      string
	MemAvailableMB int
	Error          string
}

type Runner interface {
	Capabilities() []Env
	Provision(ctx context.Context, spec SandboxSpec) (SandboxID, error)
	OpenTerminal(ctx context.Context, sb SandboxID, node string) (PTY, error)
	Exec(ctx context.Context, sb SandboxID, node string, cmd []string, opts ExecOptions) (ExecResult, error)
	Destroy(ctx context.Context, sb SandboxID) error
	GC(ctx context.Context) error
	Health(ctx context.Context) Health
}
