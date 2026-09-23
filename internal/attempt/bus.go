package attempt

import (
	"log/slog"
	"sync"
	"time"
)

const (
	EventStatus       = "status"
	EventProvisioning = "provisioning"
	EventCheckpoint   = "checkpoint"
	EventTick         = "tick"
	EventError        = "error"
)

const subscriberBuffer = 64

type CheckpointEvent struct {
	Id            string
	Status        string
	FirstPassedAt *time.Time
}

type Event struct {
	AttemptID    string
	Type         string
	Status       string
	ErrorMessage string
	Step         string
	ElapsedMS    int64
	ServerTime   time.Time
	Checkpoint   *CheckpointEvent
}

type subscription struct {
	attemptID string
	ch        chan Event
}

type bus struct {
	log *slog.Logger

	mu     sync.Mutex
	next   int64
	subs   map[int64]subscription
	closed bool
}

func newBus(log *slog.Logger) *bus {
	return &bus{log: log, subs: map[int64]subscription{}}
}

func (b *bus) subscribe(attemptID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, subscriberBuffer)
	if b.closed {
		close(ch)
		return ch, func() {}
	}

	id := b.next
	b.next++
	b.subs[id] = subscription{attemptID: attemptID, ch: ch}

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if sub, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(sub.ch)
		}
	}
}

func (b *bus) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, sub := range b.subs {
		if sub.attemptID != "" && sub.attemptID != ev.AttemptID {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			b.log.Warn("dropped event for a slow subscriber", "attempt", ev.AttemptID, "type", ev.Type)
		}
	}
}

func (b *bus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for id, sub := range b.subs {
		delete(b.subs, id)
		close(sub.ch)
	}
}
