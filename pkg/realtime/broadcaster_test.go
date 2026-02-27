package realtime

import (
	"testing"
)

func TestNewBroadcaster(t *testing.T) {
	b := NewBroadcaster()
	if b == nil {
		t.Fatal("NewBroadcaster returned nil")
	}
}

func TestBroadcaster_Subscribe(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	b.Unsubscribe(ch)
}

func TestBroadcaster_PublishDeliversToSubscriber(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.Publish("round")
	got := <-ch
	if got != "round" {
		t.Errorf("got event %q, want %q", got, "round")
	}
}

func TestBroadcaster_PublishDeliversToMultipleSubscribers(t *testing.T) {
	b := NewBroadcaster()
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	defer b.Unsubscribe(ch1)
	defer b.Unsubscribe(ch2)

	b.Publish("scores")
	if got := <-ch1; got != "scores" {
		t.Errorf("ch1 got %q, want scores", got)
	}
	if got := <-ch2; got != "scores" {
		t.Errorf("ch2 got %q, want scores", got)
	}
}

func TestBroadcaster_UnsubscribeClosesChannel(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()
	b.Unsubscribe(ch)
	_, open := <-ch
	if open {
		t.Error("channel should be closed after Unsubscribe")
	}
}

func TestBroadcaster_UnsubscribeRemovesFromDelivery(t *testing.T) {
	b := NewBroadcaster()
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	b.Unsubscribe(ch1) // ch1 is closed; only ch2 should receive subsequent events
	b.Publish("players")
	if got := <-ch2; got != "players" {
		t.Errorf("ch2 got %q, want players", got)
	}
	b.Unsubscribe(ch2)
}
