package realtime

import (
	"testing"
	"time"
)

func TestGameStore_Create_GetGame(t *testing.T) {
	s := NewGameStore[*mockRoundLoopState]()
	m := &mockRoundLoopState{nextTime: time.Now().UTC(), hasNext: true}
	id := "room1"
	s.Create(id, m)
	got, ok := s.GetGame(id)
	if !ok {
		t.Fatal("GetGame: expected ok")
	}
	if got != m {
		t.Error("GetGame: expected same state pointer")
	}
}

func TestGameStore_GetGame_Missing(t *testing.T) {
	s := NewGameStore[*mockRoundLoopState]()
	_, ok := s.GetGame("missing")
	if ok {
		t.Error("GetGame(missing): expected !ok")
	}
}

func TestGameStore_EnsureRoundLoop_DoesNotPanic(t *testing.T) {
	s := NewGameStore[*mockRoundLoopState]()
	m := &mockRoundLoopState{nextTime: time.Now().UTC().Add(time.Hour), hasNext: true}
	s.Create("r1", m)
	s.EnsureRoundLoop("r1", []string{"a", "b"})
	// Loop is running; no panic. Wake and leave.
	s.Wake("r1")
	time.Sleep(10 * time.Millisecond)
}
