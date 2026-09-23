package attempt

import (
	"testing"
	"time"
)

func TestBusDeliversPerAttempt(t *testing.T) {
	b := newBus(discardLogger())
	mine, cancelMine := b.subscribe("a")
	defer cancelMine()
	all, cancelAll := b.subscribe("")
	defer cancelAll()

	b.publish(Event{AttemptID: "b", Type: EventTick})
	b.publish(Event{AttemptID: "a", Type: EventStatus})

	if ev := <-mine; ev.AttemptID != "a" || ev.Type != EventStatus {
		t.Fatalf("event = %+v, want the attempt's own status", ev)
	}
	if len(mine) != 0 {
		t.Fatalf("subscriber received %d events of other attempts", len(mine))
	}
	if len(all) != 2 {
		t.Fatalf("SubscribeAll received %d events, want 2", len(all))
	}
}

func TestBusDropsEventsForSlowSubscriber(t *testing.T) {
	b := newBus(discardLogger())
	slow, cancel := b.subscribe("a")
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range subscriberBuffer + 10 {
			b.publish(Event{AttemptID: "a", Type: EventTick})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("publishing blocked on a slow subscriber")
	}
	if len(slow) != subscriberBuffer {
		t.Fatalf("buffered %d events, want %d", len(slow), subscriberBuffer)
	}
}

func TestBusUnsubscribeClosesChannel(t *testing.T) {
	b := newBus(discardLogger())
	ch, cancel := b.subscribe("a")

	cancel()
	cancel()

	if _, ok := <-ch; ok {
		t.Fatal("channel was not closed by the cancel function")
	}
	b.publish(Event{AttemptID: "a", Type: EventTick})
}
