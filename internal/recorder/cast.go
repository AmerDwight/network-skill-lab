package recorder

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	castVersion   = 2
	castTerm      = "xterm-256color"
	castShell     = "/bin/bash"
	recordingsDir = "recordings"
	eventOutput   = "o"
	eventInput    = "i"
	eventResize   = "r"
	replacement   = "\uFFFD"
)

var errClosed = errors.New("recording is closed")

type castEnv struct {
	Term  string `json:"TERM"`
	Shell string `json:"SHELL"`
}

type castHeader struct {
	Version   int     `json:"version"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Timestamp int64   `json:"timestamp"`
	Env       castEnv `json:"env"`
}

type Cast struct {
	store *store.Store
	id    string
	path  string
	start time.Time

	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	closed bool
}

func (r *Recorder) OpenRecording(ctx context.Context, attemptID, node, tab string, cols, rows int) (*Cast, error) {
	for _, name := range []string{attemptID, node, tab} {
		if !isPathSafe(name) {
			return nil, fmt.Errorf("invalid recording name %q", name)
		}
	}

	dir := filepath.Join(r.dataDir, recordingsDir, attemptID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create recording directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, node+"-"+tab+".cast")
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create recording %s: %w", path, err)
	}

	start := time.Now()
	writer := bufio.NewWriter(file)
	header, err := json.Marshal(castHeader{
		Version:   castVersion,
		Width:     cols,
		Height:    rows,
		Timestamp: start.Unix(),
		Env:       castEnv{Term: castTerm, Shell: castShell},
	})
	if err == nil {
		_, err = fmt.Fprintf(writer, "%s\n", header)
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write recording header %s: %w", path, err)
	}

	rec := store.Recording{
		ID:        store.NewID(),
		AttemptID: attemptID,
		Node:      node,
		TabID:     tab,
		Path:      path,
		StartedAt: start,
	}
	if err := r.store.Recordings.Create(ctx, rec); err != nil {
		_ = file.Close()
		return nil, err
	}
	r.log.Info("recording opened", "attempt", attemptID, "node", node, "tab", tab, "path", path)

	return &Cast{store: r.store, id: rec.ID, path: path, start: start, file: file, writer: writer}, nil
}

func (c *Cast) Output(p []byte) error {
	return c.event(eventOutput, string(p))
}

func (c *Cast) Input(p []byte) error {
	return c.event(eventInput, string(p))
}

func (c *Cast) Resize(cols, rows int) error {
	return c.event(eventResize, fmt.Sprintf("%dx%d", cols, rows))
}

func (c *Cast) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	err := c.writer.Flush()
	if err != nil {
		err = fmt.Errorf("flush recording %s: %w", c.path, err)
	}
	if cerr := c.file.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("close recording %s: %w", c.path, cerr)
	}

	var size int64
	if info, serr := os.Stat(c.path); serr == nil {
		size = info.Size()
	}

	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if serr := c.store.Recordings.SetEnded(ctx, c.id, time.Now(), size); serr != nil && err == nil {
		err = serr
	}
	return err
}

func (c *Cast) event(kind, data string) error {
	payload, err := json.Marshal(strings.ToValidUTF8(data, replacement))
	if err != nil {
		return fmt.Errorf("encode recording event of %s: %w", c.path, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errClosed
	}
	at := strconv.FormatFloat(time.Since(c.start).Seconds(), 'f', 6, 64)
	if _, err := fmt.Fprintf(c.writer, "[%s, %q, %s]\n", at, kind, payload); err != nil {
		return fmt.Errorf("write recording %s: %w", c.path, err)
	}
	return nil
}

func isPathSafe(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}
