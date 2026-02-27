package realtime

import "sync"

// Broadcaster publishes lightweight events to SSE subscribers.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

// NewBroadcaster creates an empty broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[chan string]struct{}),
	}
}

// Subscribe registers a new subscriber and returns its event channel.
func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 10)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Publish delivers an event to all subscribers.
func (b *Broadcaster) Publish(event string) {
	b.mu.Lock()
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
			// Drop if the subscriber is lagging; next event will catch it up.
		}
	}
	b.mu.Unlock()
}
