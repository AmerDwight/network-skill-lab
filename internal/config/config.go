package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen         string
	DataDir        string
	ContentDir     string
	NodeImage      string
	Instance       string
	IdleTimeout    time.Duration
	CheckInterval  time.Duration
	SystemdTimeout time.Duration
	MaxSandboxes   int
	PruneAfter     time.Duration
	LogLevel       slog.Level
	VM             VM
}

type VM struct {
	Image       string
	Dir         string
	MgmtNetwork string
	MemoryMiB   int
	VCPUs       int
	BootTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Listen:     lookupString("NSL_LISTEN", ":8080"),
		DataDir:    lookupString("NSL_DATA_DIR", "./data"),
		ContentDir: lookupString("NSL_CONTENT_DIR", "./content"),
		NodeImage:  lookupString("NSL_NODE_IMAGE", "nsl/node"),
	}
	cfg.Instance = lookupString("NSL_INSTANCE", instanceID(cfg.DataDir))

	var err error
	if cfg.IdleTimeout, err = lookupDuration("NSL_IDLE_TIMEOUT", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.CheckInterval, err = lookupDuration("NSL_CHECK_INTERVAL", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.SystemdTimeout, err = lookupDuration("NSL_SYSTEMD_TIMEOUT", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.MaxSandboxes, err = lookupInt("NSL_MAX_SANDBOXES", 3); err != nil {
		return Config{}, err
	}
	if cfg.PruneAfter, err = lookupDuration("NSL_PRUNE_AFTER", 0); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = lookupLevel("NSL_LOG_LEVEL", slog.LevelInfo); err != nil {
		return Config{}, err
	}
	if cfg.VM, err = loadVM(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadVM() (VM, error) {
	vm := VM{
		Image:       lookupString("NSL_VM_IMAGE", ""),
		Dir:         lookupString("NSL_VM_DIR", "/var/lib/nsl/vm"),
		MgmtNetwork: lookupString("NSL_VM_MGMT_NETWORK", "default"),
	}
	var err error
	if vm.MemoryMiB, err = lookupInt("NSL_VM_MEMORY_MIB", 1024); err != nil {
		return VM{}, err
	}
	if vm.VCPUs, err = lookupInt("NSL_VM_VCPUS", 1); err != nil {
		return VM{}, err
	}
	if vm.BootTimeout, err = lookupDuration("NSL_VM_BOOT_TIMEOUT", 120*time.Second); err != nil {
		return VM{}, err
	}
	return vm, nil
}

// instanceID keys the Docker labels that scope garbage collection, so two nsl
// processes only ever collect their own sandboxes when their data dirs differ.
func instanceID(dataDir string) string {
	path, err := filepath.Abs(dataDir)
	if err != nil {
		path = dataDir
	}
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:4])
}

func lookupString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func lookupDuration(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	d, err := ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("parse %s: must be positive, got %q", key, v)
	}
	return d, nil
}

// ParseDuration extends time.ParseDuration with a leading whole-day component,
// as in "30d" or "1d12h", since retention periods are naturally given in days.
func ParseDuration(s string) (time.Duration, error) {
	days, rest, ok := strings.Cut(s, "d")
	if !ok {
		return time.ParseDuration(s)
	}
	n, err := strconv.ParseUint(days, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("time: invalid duration %q", s)
	}
	d := time.Duration(n) * 24 * time.Hour
	if rest == "" {
		return d, nil
	}
	extra, err := time.ParseDuration(rest)
	if err != nil || extra < 0 {
		return 0, fmt.Errorf("time: invalid duration %q", s)
	}
	return d + extra, nil
}

func lookupInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("parse %s: must be positive, got %q", key, v)
	}
	return n, nil
}

func lookupLevel(key string, fallback slog.Level) (slog.Level, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(v)); err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return level, nil
}
