package vm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const (
	worldRX      = 0o005
	worldX       = 0o001
	checkTimeout = 10 * time.Second
)

var requiredTools = []string{"virsh", "qemu-img", "genisoimage"}

type Config struct {
	Image       string
	Dir         string
	MgmtNetwork string
	MemoryMiB   int
	VCPUs       int
	BootTimeout time.Duration
	Instance    string
}

type Provider struct {
	cfg     Config
	virsh   virsh
	command commandFunc
	log     *slog.Logger
}

var _ runner.Runner = (*Provider)(nil)

func New(cfg Config, logger *slog.Logger) (*Provider, error) {
	if cfg.Image == "" {
		return nil, errors.New("NSL_VM_IMAGE is required for the vm provider: point it at nsl-node.qcow2 built by images/vm/build.sh")
	}
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("vm provider needs %s on PATH: %w", tool, err)
		}
	}
	p := newProvider(cfg, logger, virshCLI{command: runCommand}, runCommand)

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	if _, err := p.virsh.run(ctx, "version"); err != nil {
		return nil, fmt.Errorf("connect to libvirt at %s (is this user in the libvirt group?): %w", libvirtURI, err)
	}
	if err := checkImage(p.cfg.Image); err != nil {
		return nil, err
	}
	if err := checkDir(p.cfg.Dir); err != nil {
		return nil, err
	}
	return p, nil
}

func newProvider(cfg Config, logger *slog.Logger, v virsh, command commandFunc) *Provider {
	if cfg.Instance == "" {
		cfg.Instance = defaultInst
	}
	if cfg.MgmtNetwork == "" {
		cfg.MgmtNetwork = "default"
	}
	if cfg.MemoryMiB <= 0 {
		cfg.MemoryMiB = 1024
	}
	if cfg.VCPUs <= 0 {
		cfg.VCPUs = 1
	}
	if cfg.BootTimeout <= 0 {
		cfg.BootTimeout = 120 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{cfg: cfg, virsh: v, command: command, log: logger}
}

func checkImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("NSL_VM_IMAGE %s is not readable: %w", path, err)
	}
	info, err := f.Stat()
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("NSL_VM_IMAGE %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("NSL_VM_IMAGE %s is not a regular file", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("NSL_VM_IMAGE %s: %w", path, err)
	}
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("NSL_VM_IMAGE directory %s: %w", dir, err)
		}
		if info.Mode().Perm()&worldX == 0 {
			return fmt.Errorf("NSL_VM_IMAGE %s: directory %s is not searchable by others, so qemu cannot open the image; move it under a directory such as /var/tmp or chmod o+x %s", path, dir, dir)
		}
		if dir == filepath.Dir(dir) {
			return nil
		}
	}
}

func checkDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("NSL_VM_DIR %s: %w (create it and chmod o+rx)", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("NSL_VM_DIR %s is not a directory", dir)
	}
	if info.Mode().Perm()&worldRX != worldRX {
		return fmt.Errorf("NSL_VM_DIR %s must be readable and searchable by others (chmod o+rx %s) so qemu can open overlays and seed ISOs", dir, dir)
	}
	return nil
}

func (p *Provider) Capabilities() []runner.Env {
	return []runner.Env{runner.EnvVM}
}

func (p *Provider) logFor(attempt string) *slog.Logger {
	return p.log.With("attempt", attempt)
}

func (p *Provider) Health(ctx context.Context) runner.Health {
	health := runner.Health{ImageName: p.cfg.Image, Instance: p.cfg.Instance, MemAvailableMB: memAvailableMB()}
	if _, err := p.virsh.run(ctx, "version"); err != nil {
		health.Error = fmt.Sprintf("libvirt: %v", err)
		return health
	}
	f, err := os.Open(p.cfg.Image)
	if err != nil {
		health.Error = fmt.Sprintf("open image %s: %v", p.cfg.Image, err)
		return health
	}
	_ = f.Close()
	health.Image = true
	health.OK = true
	return health
}

func memAvailableMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		value, ok := strings.CutPrefix(scanner.Text(), "MemAvailable:")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0
		}
		kb, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}
