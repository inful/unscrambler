package realtime

import (
	"reflect"
	"time"
)

// GameStore wraps RoomStore[G] and provides game-session operations for state
// types that implement RoundLoopState. Games embed *GameStore[G] and add their
// own CreateGame with game-specific options.
type GameStore[G RoundLoopState] struct {
	R *RoomStore[G]
}

// NewGameStore creates a new game store.
func NewGameStore[G RoundLoopState]() *GameStore[G] {
	return &GameStore[G]{R: NewRoomStore[G]()}
}

// Create registers a room with the given id and state.
func (s *GameStore[G]) Create(id string, state G) {
	s.R.Create(id, state)
}

// GetGame returns the game state by ID if it exists.
func (s *GameStore[G]) GetGame(id string) (G, bool) {
	room, ok := s.R.Get(id)
	if !ok {
		var zero G
		return zero, false
	}
	return room.State, true
}

// Broadcaster returns the SSE broadcaster for the room.
func (s *GameStore[G]) Broadcaster(id string) *Broadcaster {
	return s.R.Broadcaster(id)
}

// Publish notifies subscribers of the room.
func (s *GameStore[G]) Publish(id string, event string) {
	s.R.Publish(id, event)
}

// Wake unblocks the room's round loop.
func (s *GameStore[G]) Wake(id string) {
	s.R.Wake(id)
}

// EnsureRoundLoop starts the timing loop for the room if not already running,
// using TickFromRoundLoopState with the given events when the state advances.
// For custom tick logic (e.g. extra steps when the round does not advance),
// use RunLoop on the underlying RoomStore instead.
func (s *GameStore[G]) EnsureRoundLoop(id string, eventsOnAdvance []string) {
	getState := func() G {
		room, ok := s.R.Get(id)
		if !ok {
			var zero G
			return zero
		}
		return room.State
	}
	tick := TickFromRoundLoopState[G](eventsOnAdvance)
	tickWithNilCheck := func(state G, now time.Time) (time.Time, []string, bool) {
		if isNil(state) {
			return time.Time{}, nil, true
		}
		return tick(state, now)
	}
	s.R.RunLoop(id, getState, tickWithNilCheck)
}

// RunLoop starts a timing loop with the given getState and tick. Use this when
// you need custom tick logic (e.g. publishing on steps other than advance).
func (s *GameStore[G]) RunLoop(id string, getState func() G, tick TickFunc[G]) {
	s.R.RunLoop(id, getState, tick)
}

// isNil reports whether v is nil (for pointer, slice, map, chan, func).
func isNil[G any](v G) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}
