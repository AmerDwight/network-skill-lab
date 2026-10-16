package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const libvirtURI = "qemu:///system"

type virsh interface {
	run(ctx context.Context, args ...string) ([]byte, error)
}

type commandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

type virshCLI struct {
	command commandFunc
}

func (v virshCLI) run(ctx context.Context, args ...string) ([]byte, error) {
	return v.command(ctx, "virsh", append([]string{"--connect=" + libvirtURI}, args...)...)
}

type commandError struct {
	command string
	err     error
	stderr  string
}

func (e *commandError) Error() string {
	if e.stderr == "" {
		return fmt.Sprintf("%s: %v", e.command, e.err)
	}
	return fmt.Sprintf("%s: %v: %s", e.command, e.err, e.stderr)
}

func (e *commandError) Unwrap() error {
	return e.err
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &commandError{command: commandLabel(name, args), err: err, stderr: strings.TrimSpace(stderr.String())}
	}
	return stdout.Bytes(), nil
}

func commandLabel(name string, args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return name + " " + arg
		}
	}
	return name
}

func isDomainNotFound(err error) bool {
	return errorContains(err, "failed to get domain", "Domain not found")
}

func isNetworkNotFound(err error) bool {
	return errorContains(err, "failed to get network", "Network not found")
}

func isNotRunning(err error) bool {
	return errorContains(err, "domain is not running")
}

func errorContains(err error, needles ...string) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	var cmdErr *commandError
	if errors.As(err, &cmdErr) {
		text = cmdErr.stderr
	}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
