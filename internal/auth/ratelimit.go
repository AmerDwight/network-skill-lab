package auth

import (
	"errors"
	"sync"
	"time"
)

const (
	MaxLoginFailures = 10
	LoginBlockFor    = time.Minute
)

var ErrTooManyAttempts = errors.New("too many login attempts")

type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	max     int
	block   time.Duration
	now     func() time.Time
}

type limiterEntry struct {
	failures     int
	lastFailure  time.Time
	blockedUntil time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		entries: make(map[string]*limiterEntry),
		max:     MaxLoginFailures,
		block:   LoginBlockFor,
		now:     time.Now,
	}
}

func (l *RateLimiter) Allow(key string) error {
	if key == "" {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok {
		return nil
	}
	if l.now().Before(entry.blockedUntil) {
		return ErrTooManyAttempts
	}
	if !entry.blockedUntil.IsZero() {
		delete(l.entries, key)
	}
	return nil
}

func (l *RateLimiter) Fail(key string) {
	if key == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, ok := l.entries[key]
	if !ok {
		entry = &limiterEntry{}
		l.entries[key] = entry
	}
	if now.Sub(entry.lastFailure) > l.block {
		entry.failures = 0
	}
	entry.failures++
	entry.lastFailure = now
	if entry.failures >= l.max {
		entry.failures = 0
		entry.blockedUntil = now.Add(l.block)
	}
	l.prune(now)
}

func (l *RateLimiter) Reset(key string) {
	if key == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *RateLimiter) prune(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastFailure) > l.block && now.After(entry.blockedUntil) {
			delete(l.entries, key)
		}
	}
}
