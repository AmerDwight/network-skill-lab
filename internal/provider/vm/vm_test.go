package vm

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

func TestNewRequiresAnImage(t *testing.T) {
	_, err := New(Config{Dir: t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "NSL_VM_IMAGE is required") {
		t.Errorf("New() error = %v", err)
	}
}

func TestCheckImage(t *testing.T) {
	open := t.TempDir()
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkImage(filepath.Join(open, "missing.qcow2")); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Errorf("checkImage(missing) error = %v", err)
	}
	if err := checkImage(open); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("checkImage(dir) error = %v", err)
	}

	private := filepath.Join(open, "private")
	if err := os.Mkdir(private, 0o750); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(private, "nsl-node.qcow2")
	if err := os.WriteFile(hidden, []byte("qcow"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkImage(hidden); err == nil || !strings.Contains(err.Error(), private+" is not searchable") {
		t.Errorf("checkImage(private dir) error = %v", err)
	}
}

func TestCheckDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkDir(dir); err != nil {
		t.Errorf("checkDir(0755) error = %v", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := checkDir(dir); err == nil || !strings.Contains(err.Error(), "chmod o+rx") {
		t.Errorf("checkDir(0750) error = %v", err)
	}
	if err := checkDir(filepath.Join(dir, "missing")); err == nil {
		t.Error("checkDir(missing) succeeded")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkDir(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("checkDir(file) error = %v", err)
	}
}

func TestDefaultsAndCapabilities(t *testing.T) {
	p := newProvider(Config{Image: "/x"}, nil, &fakeVirsh{}, noCommand)
	if p.cfg.Instance != "default" || p.cfg.MgmtNetwork != "default" || p.cfg.MemoryMiB != 1024 || p.cfg.VCPUs != 1 || p.cfg.BootTimeout <= 0 {
		t.Errorf("defaults = %+v", p.cfg)
	}
	if got := p.Capabilities(); !slices.Equal(got, []runner.Env{runner.EnvVM}) {
		t.Errorf("Capabilities() = %v", got)
	}
}

func TestHealth(t *testing.T) {
	image := filepath.Join(t.TempDir(), "nsl-node.qcow2")
	if err := os.WriteFile(image, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	v := &fakeVirsh{}
	p := newProvider(Config{Image: image, Instance: "vm-01"}, nil, v, noCommand)
	if h := p.Health(t.Context()); !h.OK || !h.Image || h.Docker || h.ImageName != image || h.Instance != "vm-01" {
		t.Errorf("Health() = %+v", h)
	}

	v.handle = func([]string) ([]byte, error) { return nil, errors.New("error: failed to connect to the hypervisor") }
	if h := p.Health(t.Context()); h.OK || !strings.Contains(h.Error, "libvirt") {
		t.Errorf("Health() without libvirt = %+v", h)
	}

	v.handle = nil
	p.cfg.Image = image + ".missing"
	if h := p.Health(t.Context()); h.OK || h.Image {
		t.Errorf("Health() without an image = %+v", h)
	}
}

func TestCommandLabelAndErrors(t *testing.T) {
	if got := commandLabel("virsh", []string{"--connect=qemu:///system", "qemu-agent-command", "dom", "{}"}); got != "virsh qemu-agent-command" {
		t.Errorf("commandLabel = %q", got)
	}
	_, err := runCommand(t.Context(), "sh", "-c", "echo 'error: failed to get domain' >&2; exit 1")
	if !isDomainNotFound(err) || isNetworkNotFound(err) || isNotRunning(err) {
		t.Errorf("classification of %v is wrong", err)
	}
}
