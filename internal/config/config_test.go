package config

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"NSL_LISTEN", "NSL_DATA_DIR", "NSL_CONTENT_DIR", "NSL_NODE_IMAGE",
		"NSL_INSTANCE", "NSL_IDLE_TIMEOUT", "NSL_CHECK_INTERVAL", "NSL_SYSTEMD_TIMEOUT",
		"NSL_MAX_SANDBOXES", "NSL_LOG_LEVEL",
	} {
		t.Setenv(key, "")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		Listen:         ":8080",
		DataDir:        "./data",
		ContentDir:     "./content",
		NodeImage:      "nsl/node",
		Instance:       instanceID("./data"),
		IdleTimeout:    15 * time.Minute,
		CheckInterval:  5 * time.Second,
		SystemdTimeout: 60 * time.Second,
		MaxSandboxes:   3,
		LogLevel:       slog.LevelInfo,
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("NSL_LISTEN", "127.0.0.1:9000")
	t.Setenv("NSL_DATA_DIR", "/var/lib/nsl")
	t.Setenv("NSL_CONTENT_DIR", "/srv/content")
	t.Setenv("NSL_NODE_IMAGE", "example/node:v1")
	t.Setenv("NSL_INSTANCE", "acceptance")
	t.Setenv("NSL_IDLE_TIMEOUT", "30m")
	t.Setenv("NSL_CHECK_INTERVAL", "2s")
	t.Setenv("NSL_SYSTEMD_TIMEOUT", "90s")
	t.Setenv("NSL_MAX_SANDBOXES", "5")
	t.Setenv("NSL_LOG_LEVEL", "debug")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		Listen:         "127.0.0.1:9000",
		DataDir:        "/var/lib/nsl",
		ContentDir:     "/srv/content",
		NodeImage:      "example/node:v1",
		Instance:       "acceptance",
		IdleTimeout:    30 * time.Minute,
		CheckInterval:  2 * time.Second,
		SystemdTimeout: 90 * time.Second,
		MaxSandboxes:   5,
		LogLevel:       slog.LevelDebug,
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"idle timeout not a duration", "NSL_IDLE_TIMEOUT", "fifteen"},
		{"idle timeout not positive", "NSL_IDLE_TIMEOUT", "0s"},
		{"check interval not a duration", "NSL_CHECK_INTERVAL", "5"},
		{"check interval negative", "NSL_CHECK_INTERVAL", "-1s"},
		{"systemd timeout not a duration", "NSL_SYSTEMD_TIMEOUT", "a minute"},
		{"max sandboxes not a number", "NSL_MAX_SANDBOXES", "many"},
		{"max sandboxes not positive", "NSL_MAX_SANDBOXES", "0"},
		{"log level unknown", "NSL_LOG_LEVEL", "verbose"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() with %s=%q: expected error", tt.key, tt.value)
			}
		})
	}
}

func TestInstanceIDFollowsTheDataDir(t *testing.T) {
	dir := t.TempDir()
	if instanceID(dir) != instanceID(filepath.Join(dir, "sub", "..")) {
		t.Errorf("instanceID is not stable across equivalent paths for %s", dir)
	}
	if instanceID(dir) == instanceID(filepath.Join(dir, "other")) {
		t.Errorf("instanceID collides for two different data dirs under %s", dir)
	}
	if got := instanceID(dir); len(got) != 8 {
		t.Errorf("instanceID = %q, want 8 hex characters", got)
	}
}
