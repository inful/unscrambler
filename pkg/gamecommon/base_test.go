package gamecommon

import (
	"testing"
	"time"

	"dagame/pkg/realtime"
)

func TestAddPlayer_SetsOwnerIfFirst(t *testing.T) {
	players := make(map[string]*BasePlayer)
	var ownerID string
	p := AddPlayer(players, &ownerID, "alice")
	if p.ID == "" {
		t.Error("expected non-empty ID")
	}
	if p.Username != "alice" {
		t.Errorf("Username = %q, want alice", p.Username)
	}
	if ownerID != p.ID {
		t.Errorf("ownerID = %q, want first player ID %q", ownerID, p.ID)
	}
	if len(players) != 1 {
		t.Errorf("len(players) = %d, want 1", len(players))
	}
}

func TestAddPlayer_KeepsOwnerIfAlreadySet(t *testing.T) {
	players := make(map[string]*BasePlayer)
	ownerID := "existing-owner"
	p := AddPlayer(players, &ownerID, "bob")
	if ownerID != "existing-owner" {
		t.Errorf("ownerID = %q, want existing-owner", ownerID)
	}
	if p.Username != "bob" {
		t.Errorf("Username = %q, want bob", p.Username)
	}
}

func TestIsOwner(t *testing.T) {
	if !IsOwner("owner1", "owner1") {
		t.Error("owner1 should be owner")
	}
	if IsOwner("owner1", "other") {
		t.Error("other should not be owner")
	}
	if IsOwner("owner1", "") {
		t.Error("empty string should not be owner")
	}
}

func TestBaseGame_ZeroValueUsable(t *testing.T) {
	var g BaseGame
	g.TimedRounds = realtime.TimedRounds{Rounds: 3, Duration: 10 * time.Second}
	g.Status = StatusLobby
	g.Players = make(map[string]*BasePlayer)
	g.Mu.Lock()
	p := AddPlayer(g.Players, &g.OwnerID, "test")
	g.Mu.Unlock()
	if p.Username != "test" {
		t.Errorf("Username = %q", p.Username)
	}
}
