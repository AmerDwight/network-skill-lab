package runnerproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const (
	CodeSandboxNotFound = "sandbox_not_found"
	CodeNodeNotFound    = "node_not_found"
)

type NodeSpec struct {
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	K3sDisable []string `json:"k3s_disable"`
}

type EndpointSpec struct {
	Node    string `json:"node"`
	Iface   string `json:"iface"`
	Address string `json:"address"`
}

type LinkSpec struct {
	Name      string         `json:"name"`
	Subnet    string         `json:"subnet"`
	Endpoints []EndpointSpec `json:"endpoints"`
}

type Script struct {
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

type ProvisionParams struct {
	AttemptID string            `json:"attempt_id"`
	Image     string            `json:"image"`
	Nodes     []NodeSpec        `json:"nodes"`
	Links     []LinkSpec        `json:"links"`
	Setup     Script            `json:"setup"`
	Env       map[string]string `json:"env"`
	Internet  bool              `json:"internet"`
}

type SandboxResult struct {
	Sandbox string `json:"sandbox"`
}

type SandboxParams struct {
	Sandbox string `json:"sandbox"`
}

type ExecParams struct {
	Sandbox   string            `json:"sandbox"`
	Node      string            `json:"node"`
	Cmd       []string          `json:"cmd"`
	Env       map[string]string `json:"env,omitempty"`
	Stdin     []byte            `json:"stdin,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
	User      string            `json:"user,omitempty"`
}

type ExecResult struct {
	Stdout   []byte `json:"stdout"`
	Stderr   []byte `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

type Health struct {
	OK             bool   `json:"ok"`
	Docker         bool   `json:"docker"`
	Image          bool   `json:"image"`
	ImageName      string `json:"image_name"`
	Instance       string `json:"instance"`
	MemAvailableMB int    `json:"mem_available_mb"`
	Error          string `json:"error,omitempty"`
}

type OpenTerminalParams struct {
	Sandbox string `json:"sandbox"`
	Node    string `json:"node"`
}

type StreamResult struct {
	Stream uint32 `json:"stream"`
}

type ResizeParams struct {
	Stream uint32 `json:"stream"`
	Cols   uint16 `json:"cols"`
	Rows   uint16 `json:"rows"`
}

type CloseTerminalParams struct {
	Stream uint32 `json:"stream"`
}

func FromSpec(spec runner.SandboxSpec) ProvisionParams {
	params := ProvisionParams{
		AttemptID: spec.AttemptID,
		Image:     spec.Image,
		Nodes:     make([]NodeSpec, len(spec.Nodes)),
		Links:     make([]LinkSpec, len(spec.Links)),
		Setup:     Script(spec.Setup),
		Env:       spec.Env,
		Internet:  spec.Internet,
	}
	for i, node := range spec.Nodes {
		params.Nodes[i] = NodeSpec(node)
	}
	for i, link := range spec.Links {
		endpoints := make([]EndpointSpec, len(link.Endpoints))
		for j, endpoint := range link.Endpoints {
			endpoints[j] = EndpointSpec(endpoint)
		}
		params.Links[i] = LinkSpec{Name: link.Name, Subnet: link.Subnet, Endpoints: endpoints}
	}
	return params
}

func (p ProvisionParams) Spec() runner.SandboxSpec {
	spec := runner.SandboxSpec{
		AttemptID: p.AttemptID,
		Image:     p.Image,
		Nodes:     make([]runner.NodeSpec, len(p.Nodes)),
		Links:     make([]runner.LinkSpec, len(p.Links)),
		Setup:     runner.Script(p.Setup),
		Env:       p.Env,
		Internet:  p.Internet,
	}
	for i, node := range p.Nodes {
		spec.Nodes[i] = runner.NodeSpec(node)
	}
	for i, link := range p.Links {
		endpoints := make([]runner.EndpointSpec, len(link.Endpoints))
		for j, endpoint := range link.Endpoints {
			endpoints[j] = runner.EndpointSpec(endpoint)
		}
		spec.Links[i] = runner.LinkSpec{Name: link.Name, Subnet: link.Subnet, Endpoints: endpoints}
	}
	return spec
}

func FromExec(sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (ExecParams, error) {
	params := ExecParams{
		Sandbox:   string(sb),
		Node:      node,
		Cmd:       cmd,
		Env:       opts.Env,
		TimeoutMS: opts.Timeout.Milliseconds(),
		User:      opts.User,
	}
	if opts.Stdin != nil {
		stdin, err := io.ReadAll(opts.Stdin)
		if err != nil {
			return ExecParams{}, fmt.Errorf("read exec stdin: %w", err)
		}
		params.Stdin = stdin
	}
	return params, nil
}

func (p ExecParams) Options() runner.ExecOptions {
	opts := runner.ExecOptions{
		Env:     p.Env,
		Timeout: time.Duration(p.TimeoutMS) * time.Millisecond,
		User:    p.User,
	}
	if len(p.Stdin) > 0 {
		opts.Stdin = bytes.NewReader(p.Stdin)
	}
	return opts
}

func FromExecResult(r runner.ExecResult) ExecResult {
	return ExecResult(r)
}

func (r ExecResult) Runner() runner.ExecResult {
	return runner.ExecResult(r)
}

func FromHealth(h runner.Health) Health {
	return Health(h)
}

func (h Health) Runner() runner.Health {
	return runner.Health(h)
}

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, runner.ErrSandboxNotFound):
		return CodeSandboxNotFound
	case errors.Is(err, runner.ErrNodeNotFound):
		return CodeNodeNotFound
	default:
		return ""
	}
}

type RemoteError struct {
	Message string
	Code    string
}

func (e *RemoteError) Error() string { return e.Message }

func (e *RemoteError) Unwrap() error {
	switch e.Code {
	case CodeSandboxNotFound:
		return runner.ErrSandboxNotFound
	case CodeNodeNotFound:
		return runner.ErrNodeNotFound
	default:
		return nil
	}
}

func (r Result) Err() error {
	if r.OK {
		return nil
	}
	return &RemoteError{Message: r.Error, Code: r.Code}
}

func NewResult(id uint64, value any, err error) (Result, error) {
	if err != nil {
		return Result{ID: id, Error: err.Error(), Code: ErrorCode(err)}, nil
	}
	if value == nil {
		return Result{ID: id, OK: true}, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return Result{}, fmt.Errorf("encode result %d: %w", id, err)
	}
	return Result{ID: id, OK: true, Value: data}, nil
}

func CapabilityNames(envs []runner.Env) []string {
	names := make([]string, len(envs))
	for i, env := range envs {
		names[i] = string(env)
	}
	return names
}

func Capabilities(names []string) []runner.Env {
	envs := make([]runner.Env, len(names))
	for i, name := range names {
		envs[i] = runner.Env(name)
	}
	return envs
}
