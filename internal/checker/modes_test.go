package checker

import (
	"context"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func TestRealModeRecordsWithoutPublishingOrFinishing(t *testing.T) {
	h := newHarnessInMode(t, "real")
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", pass())

	h.sweep(t)

	runs := h.runs(t)
	if got := runs["link-up"]; got.LastStatus != store.CheckpointPass || got.FirstPassedAt == nil {
		t.Errorf("link-up run = %+v, want a recorded pass", got)
	}
	if got := runs["ping-peer"]; got.LastStatus != store.CheckpointPass || got.FirstPassedAt == nil {
		t.Errorf("ping-peer run = %+v, want a recorded pass", got)
	}
	if got := h.status(t); got != store.StatusRunning {
		t.Errorf("attempt status = %s, want it to stay running", got)
	}

	select {
	case ev := <-h.events:
		if ev.Type == attempt.EventCheckpoint {
			t.Fatalf("a checkpoint event was published in real mode: %+v", ev.Checkpoint)
		}
	case <-time.After(100 * time.Millisecond):
	}

	h.sweep(t)
	if h.loopCount() != 1 {
		t.Errorf("sweep loops = %d, want the loop to keep running", h.loopCount())
	}
}

func TestSweepNowReturnsEveryStatus(t *testing.T) {
	h := newHarnessInMode(t, "real")
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail())

	statuses, err := h.SweepNow(t.Context(), h.id)
	if err != nil {
		t.Fatalf("SweepNow: %v", err)
	}
	if statuses["link-up"] != store.CheckpointPass || statuses["ping-peer"] != store.CheckpointFail {
		t.Errorf("statuses = %v", statuses)
	}
	if got := h.runs(t)["ping-peer"]; got.LastStatus != store.CheckpointFail {
		t.Errorf("ping-peer run = %+v", got)
	}
	if got := h.status(t); got != store.StatusRunning {
		t.Errorf("attempt status = %s, want SweepNow to leave it running", got)
	}
}

func TestSweepNowWaitsForTheBackgroundSweep(t *testing.T) {
	h := newHarnessInMode(t, "real")
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail())

	release := h.runner.blockOn(h.key(t, "link-up"))
	h.sendTick(t)

	deadline := time.Now().Add(waitTimeout)
	for h.runner.waitingOn(h.key(t, "link-up")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the background sweep never reached the blocked checkpoint")
		}
		time.Sleep(time.Millisecond)
	}

	failed := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := h.SweepNow(context.Background(), h.id); err != nil {
			failed <- err
		}
	}()

	select {
	case <-done:
		t.Fatal("SweepNow ran while the background sweep held the attempt")
	case <-time.After(100 * time.Millisecond):
	}
	if want := 2; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d while the sweeps are serialized", h.runner.execs(), want)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("SweepNow never finished after the background sweep was released")
	}
	select {
	case err := <-failed:
		t.Fatalf("SweepNow: %v", err)
	default:
	}
	if want := 4; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d after both sweeps", h.runner.execs(), want)
	}
}
