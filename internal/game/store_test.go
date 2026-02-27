package game

import (
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	s := NewStore()
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
}

func TestStore_CreateGame_GetGame(t *testing.T) {
	s := NewStore()
	g := s.CreateGame(2, time.Minute, "en")
	if g == nil {
		t.Fatal("CreateGame returned nil")
	}
	if g.ID == "" {
		t.Error("game ID is empty")
	}
	if g.Status != StatusLobby {
		t.Errorf("game status %q, want %q", g.Status, StatusLobby)
	}
	if g.TimedRounds.Rounds != 2 {
		t.Errorf("game Rounds %d, want 2", g.TimedRounds.Rounds)
	}
	if g.TimedRounds.Duration != time.Minute {
		t.Errorf("game RoundDuration %v, want 1m", g.TimedRounds.Duration)
	}

	got, ok := s.GetGame(g.ID)
	if !ok {
		t.Fatal("GetGame returned false for existing game")
	}
	if got != g {
		t.Error("GetGame returned different pointer")
	}

	_, ok = s.GetGame("nonexistent")
	if ok {
		t.Error("GetGame should return false for missing ID")
	}
}

func TestStore_Publish(t *testing.T) {
	s := NewStore()
	g := s.CreateGame(1, time.Minute, "en")
	hub := s.Broadcaster(g.ID)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	s.Publish(g.ID, "round")
	got := <-ch
	if got != "round" {
		t.Errorf("got event %q, want round", got)
	}
}

func TestStore_Broadcaster(t *testing.T) {
	s := NewStore()
	g := s.CreateGame(1, time.Minute, "en")
	hub := s.Broadcaster(g.ID)
	if hub == nil {
		t.Fatal("Broadcaster returned nil for existing game")
	}
	// Current behavior: Broadcaster for unknown ID creates and returns a hub
	unknownHub := s.Broadcaster("unknown-id")
	if unknownHub == nil {
		t.Fatal("Broadcaster returned nil for unknown ID")
	}
}

func TestStore_EnsureRoundLoop_DoesNotPanic(t *testing.T) {
	s := NewStore()
	g := s.CreateGame(1, 100*time.Millisecond, "en")
	g.AddPlayer("p1")
	_ = g.Start(time.Now().UTC())

	// Idempotent: calling twice should not panic
	s.EnsureRoundLoop(g.ID, g)
	s.EnsureRoundLoop(g.ID, g)
}

func TestStore_WakeRoundLoop_NoPanicWhenNoLoop(t *testing.T) {
	s := NewStore()
	// No EnsureRoundLoop called; Wake should not panic
	s.WakeRoundLoop("nonexistent")
}
