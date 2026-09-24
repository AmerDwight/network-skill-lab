package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	LogLevel       slog.Level
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
	if cfg.LogLevel, err = lookupLevel("NSL_LOG_LEVEL", slog.LevelInfo); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("parse %s: must be positive, got %q", key, v)
	}
	return d, nil
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
