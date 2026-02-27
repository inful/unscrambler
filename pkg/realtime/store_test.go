package realtime

import "testing"

func TestNewRoomStore(t *testing.T) {
	s := NewRoomStore[string]()
	if s == nil {
		t.Fatal("NewRoomStore returned nil")
	}
}

func TestRoomStore_Create_Get(t *testing.T) {
	s := NewRoomStore[string]()
	s.Create("room1", "state1")
	room, ok := s.Get("room1")
	if !ok {
		t.Fatal("Get returned false for existing room")
	}
	if room.ID != "room1" {
		t.Errorf("room ID %q, want room1", room.ID)
	}
	if room.State != "state1" {
		t.Errorf("room State %q, want state1", room.State)
	}

	_, ok = s.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing ID")
	}
}

func TestRoomStore_Publish(t *testing.T) {
	s := NewRoomStore[string]()
	s.Create("r1", "x")
	hub := s.Broadcaster("r1")
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	s.Publish("r1", "event1")
	got := <-ch
	if got != "event1" {
		t.Errorf("got %q, want event1", got)
	}
}

func TestRoomStore_Wake_NoPanicWhenNoLoop(t *testing.T) {
	s := NewRoomStore[string]()
	s.Wake("nonexistent")
}
