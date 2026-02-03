package game

import "sync"

type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[chan string]struct{}),
	}
}

func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 10)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *Broadcaster) Publish(event string) {
	b.mu.Lock()
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
	b.mu.Unlock()
}
