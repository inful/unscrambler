package realtime

import (
	"context"
	"sync"
	"time"
)

// Room holds state and a broadcaster for one room.
type Room[T any] struct {
	ID    string
	State T
	hub   *Broadcaster
}

// RoomStore manages rooms and their broadcasters.
type RoomStore[T any] struct {
	mu    sync.RWMutex
	rooms map[string]*Room[T]
	loops map[string]context.CancelFunc
	wakes map[string]chan struct{}
}

// NewRoomStore creates an empty room store.
func NewRoomStore[T any]() *RoomStore[T] {
	return &RoomStore[T]{
		rooms: make(map[string]*Room[T]),
		loops: make(map[string]context.CancelFunc),
		wakes: make(map[string]chan struct{}),
	}
}

// Create adds a room with the given id and state, and a new Broadcaster.
func (s *RoomStore[T]) Create(id string, state T) *Room[T] {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &Room[T]{ID: id, State: state, hub: NewBroadcaster()}
	s.rooms[id] = r
	return r
}

// Get returns the room by ID if it exists.
func (s *RoomStore[T]) Get(id string) (*Room[T], bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rooms[id]
	return r, ok
}

// Publish notifies subscribers of the room's broadcaster.
func (s *RoomStore[T]) Publish(id string, event string) {
	hub := s.Broadcaster(id)
	hub.Publish(event)
}

// Broadcaster returns the broadcaster for the room, creating it if the room exists but had none.
func (s *RoomStore[T]) Broadcaster(id string) *Broadcaster {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[id]
	if !ok {
		hub := NewBroadcaster()
		s.rooms[id] = &Room[T]{ID: id, hub: hub}
		return hub
	}
	if r.hub == nil {
		r.hub = NewBroadcaster()
	}
	return r.hub
}

// TickFunc is called by RunLoop to determine the next wake time and events to publish.
// stop true means exit the loop.
type TickFunc[T any] func(state T, now time.Time) (next time.Time, events []string, stop bool)

// RunLoop starts a timing loop for the room. If a loop already exists for id, it is not started again.
func (s *RoomStore[T]) RunLoop(id string, getState func() T, tick TickFunc[T]) {
	s.mu.Lock()
	if _, ok := s.loops[id]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)
	s.loops[id] = cancel
	s.wakes[id] = wake
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.loops, id)
			delete(s.wakes, id)
			s.mu.Unlock()
		}()

		for {
			state := getState()
			now := time.Now().UTC()
			next, events, stop := tick(state, now)
			if stop {
				return
			}
			// Publish events immediately so UI updates as soon as state advances
			// (e.g. after cooldown when moving to next round), not when the next timer fires.
			for _, e := range events {
				s.Publish(id, e)
			}
			wait := time.Until(next)
			if wait < 0 {
				wait = 0
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				// Timer fired; loop will re-run tick and publish any new events
			case <-wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				continue
			}
		}
	}()
}

// Wake unblocks the room's loop so it recomputes immediately.
func (s *RoomStore[T]) Wake(id string) {
	s.mu.RLock()
	wake, ok := s.wakes[id]
	s.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}
