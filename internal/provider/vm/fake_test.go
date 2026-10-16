package vm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeVirsh struct {
	mu     sync.Mutex
	calls  [][]string
	handle func(args []string) ([]byte, error)
}

func (f *fakeVirsh) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(args))
	handle := f.handle
	f.mu.Unlock()
	if handle == nil {
		return nil, nil
	}
	return handle(args)
}

func (f *fakeVirsh) called(verb string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, call := range f.calls {
		if len(call) > 0 && call[0] == verb {
			out = append(out, call)
		}
	}
	return out
}

func noCommand(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("unexpected command")
}

func newTestProvider(t *testing.T, v virsh) *Provider {
	t.Helper()
	return newProvider(Config{Image: "/var/tmp/nsl-node.qcow2", Dir: t.TempDir(), Instance: "inst1"}, slog.New(slog.DiscardHandler), v, noCommand)
}

func fastPolling(t *testing.T) {
	t.Helper()
	start, maxInterval := pollStart, pollMax
	pollStart, pollMax = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { pollStart, pollMax = start, maxInterval })
}

func domainNotFound(name string) error {
	return fmt.Errorf("virsh: exit status 1: error: failed to get domain '%s'", name)
}

func ownerXML(root string, o owner) []byte {
	if o.Instance == "" {
		return fmt.Appendf(nil, "<%s><name>x</name></%s>", root, root)
	}
	return fmt.Appendf(nil, "<%s><name>x</name><metadata><nsl:sandbox xmlns:nsl='%s' instance='%s' attempt='%s'/></metadata></%s>",
		root, metadataNamespace, o.Instance, o.Attempt, root)
}

type libvirtState struct {
	mu       sync.Mutex
	domains  map[string]owner
	networks map[string]owner
	running  map[string]bool
}

func (s *libvirtState) handle(args []string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch args[0] {
	case "list":
		return []byte(names(s.domains) + "win10\n"), nil
	case "net-list":
		return []byte(names(s.networks) + "default\n"), nil
	case "dumpxml":
		o, ok := s.domains[args[1]]
		if !ok {
			return nil, domainNotFound(args[1])
		}
		return ownerXML("domain", o), nil
	case "net-dumpxml":
		o, ok := s.networks[args[1]]
		if !ok {
			return nil, fmt.Errorf("error: failed to get network '%s'", args[1])
		}
		return ownerXML("network", o), nil
	case "destroy":
		if !s.running[args[1]] {
			return nil, errors.New("error: Requested operation is not valid: domain is not running")
		}
		s.running[args[1]] = false
		return nil, nil
	case "undefine":
		delete(s.domains, args[1])
		return nil, nil
	case "net-destroy":
		delete(s.networks, args[1])
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected virsh %s", strings.Join(args, " "))
}

func names(m map[string]owner) string {
	var b strings.Builder
	for name := range m {
		b.WriteString(name + "\n")
	}
	return b.String()
}
