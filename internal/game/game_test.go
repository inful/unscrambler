package game

import (
	"testing"
	"time"
)

func TestNewGame(t *testing.T) {
	g := NewGame(2, time.Minute, "en")
	if g == nil {
		t.Fatal("NewGame returned nil")
	}
	if g.ID == "" {
		t.Error("ID is empty")
	}
	if g.Status != StatusLobby {
		t.Errorf("Status %q, want %q", g.Status, StatusLobby)
	}
	if g.TimedRounds.Rounds != 2 {
		t.Errorf("Rounds %d, want 2", g.TimedRounds.Rounds)
	}
	if g.TimedRounds.Duration != time.Minute {
		t.Errorf("RoundDuration %v, want 1m", g.TimedRounds.Duration)
	}
	if len(g.RoundData) != 2 {
		t.Errorf("len(RoundData) %d, want 2", len(g.RoundData))
	}
	if g.Lang != "en" {
		t.Errorf("Lang %q, want en", g.Lang)
	}
}

func TestGame_AddPlayer(t *testing.T) {
	g := NewGame(1, time.Minute, "en")
	p1 := g.AddPlayer("alice")
	if p1 == nil {
		t.Fatal("AddPlayer returned nil")
	}
	if p1.Username != "alice" {
		t.Errorf("Username %q, want alice", p1.Username)
	}
	if len(g.Players) != 1 {
		t.Errorf("len(Players) %d, want 1", len(g.Players))
	}
	if g.OwnerID != p1.ID {
		t.Errorf("OwnerID %q, want first player %q", g.OwnerID, p1.ID)
	}

	p2 := g.AddPlayer("bob")
	if p2.ID == p1.ID {
		t.Error("second player should have different ID")
	}
	if len(g.Players) != 2 {
		t.Errorf("len(Players) %d, want 2", len(g.Players))
	}
	if g.OwnerID != p1.ID {
		t.Errorf("OwnerID should stay first player, got %q", g.OwnerID)
	}
}

func TestGame_Start(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(1, time.Minute, "en")
	g.AddPlayer("alice")

	err := g.Start(now)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if g.Status != StatusInProgress {
		t.Errorf("Status %q, want %q", g.Status, StatusInProgress)
	}
	if g.TimedRounds.CurrentRound != 1 {
		t.Errorf("CurrentRound %d, want 1", g.TimedRounds.CurrentRound)
	}
	if g.TimedRounds.RoundStarted.IsZero() {
		t.Error("RoundStarted should be set")
	}

	err = g.Start(now)
	if err == nil {
		t.Error("Start again should return error")
	}
	if g.Status != StatusInProgress {
		t.Errorf("Status should still be in progress, got %q", g.Status)
	}
}

func TestGame_SubmitGuess(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(1, time.Minute, "en")
	p := g.AddPlayer("alice")
	_ = g.Start(now)
	round := g.CurrentRoundData()
	if round.Word == "" {
		t.Fatal("no round word (empty word list?)")
	}

	// Correct guess
	ok, err := g.SubmitGuess(p.ID, round.Word, now)
	if err != nil {
		t.Fatalf("SubmitGuess: %v", err)
	}
	if !ok {
		t.Error("correct guess should return true")
	}
	if p.Points < 1 {
		t.Errorf("player should have points, got %d", p.Points)
	}
	if g.RoundWinnerID != p.ID {
		t.Errorf("RoundWinnerID %q, want %q", g.RoundWinnerID, p.ID)
	}

	// Wrong guess (same round already won)
	ok2, _ := g.SubmitGuess(p.ID, "wrong", now)
	if ok2 {
		t.Error("wrong guess or after round end should return false")
	}
}

func TestGame_SubmitGuess_WrongWord(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(1, time.Minute, "en")
	p := g.AddPlayer("alice")
	_ = g.Start(now)
	round := g.CurrentRoundData()
	if round.Word == "" {
		t.Skip("no word list")
	}

	ok, err := g.SubmitGuess(p.ID, "wrongword", now)
	if err != nil {
		t.Fatalf("SubmitGuess: %v", err)
	}
	if ok {
		t.Error("wrong guess should return false")
	}
	if p.Points != 0 {
		t.Errorf("points should be 0, got %d", p.Points)
	}
}

func TestGame_SubmitGuess_NotInProgress(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(1, time.Minute, "en")
	p := g.AddPlayer("alice")
	// Do not start

	ok, err := g.SubmitGuess(p.ID, "anything", now)
	if err == nil {
		t.Error("SubmitGuess when not in progress should return error")
	}
	if ok {
		t.Error("ok should be false")
	}
}

func TestGame_AdvanceIfNeeded(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(2, 50*time.Millisecond, "en")
	g.AddPlayer("alice")
	_ = g.Start(now)

	// Before round end: no change
	advanced := g.AdvanceIfNeeded(now.Add(10 * time.Millisecond))
	if advanced {
		t.Error("should not advance before round end")
	}
	if !g.TimedRounds.RoundEndedAt.IsZero() {
		t.Error("RoundEndedAt should still be zero")
	}

	// After round end: should set RoundEndedAt
	advanced = g.AdvanceIfNeeded(now.Add(100 * time.Millisecond))
	if !advanced {
		t.Error("should advance (set RoundEndedAt)")
	}
	if g.TimedRounds.RoundEndedAt.IsZero() {
		t.Error("RoundEndedAt should be set")
	}

	// After cooldown: should advance to round 2
	advanced = g.AdvanceIfNeeded(now.Add(6 * time.Second))
	if !advanced {
		t.Error("should advance to next round")
	}
	if g.TimedRounds.CurrentRound != 2 {
		t.Errorf("CurrentRound %d, want 2", g.TimedRounds.CurrentRound)
	}
}

func TestGame_NextTimer(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(1, time.Minute, "en")
	g.AddPlayer("alice")

	// Not started
	next, ok := g.NextTimer(now)
	if ok {
		t.Error("NextTimer should return false when not in progress")
	}
	if !next.IsZero() {
		t.Error("next should be zero")
	}

	_ = g.Start(now)
	next, ok = g.NextTimer(now)
	if !ok {
		t.Fatal("NextTimer should return true when in progress")
	}
	wantNext := now.Add(time.Minute)
	if next.Before(wantNext.Add(-time.Second)) || next.After(wantNext.Add(time.Second)) {
		t.Errorf("next %v, want ~%v", next, wantNext)
	}
}

func TestGame_Snapshot(t *testing.T) {
	now := time.Now().UTC()
	g := NewGame(1, time.Minute, "en")
	g.AddPlayer("alice")
	g.AddPlayer("bob")

	snap := g.Snapshot(now)
	if snap.Status != StatusLobby {
		t.Errorf("Snapshot Status %q, want lobby", snap.Status)
	}
	if len(snap.Players) != 2 {
		t.Errorf("Snapshot Players len %d, want 2", len(snap.Players))
	}
	if len(snap.Scores) != 2 {
		t.Errorf("Snapshot Scores len %d, want 2", len(snap.Scores))
	}
}

func TestGame_IsOwner(t *testing.T) {
	g := NewGame(1, time.Minute, "en")
	p1 := g.AddPlayer("alice")
	p2 := g.AddPlayer("bob")

	if !g.IsOwner(p1.ID) {
		t.Error("first player should be owner")
	}
	if g.IsOwner(p2.ID) {
		t.Error("second player should not be owner")
	}
	if g.IsOwner("") {
		t.Error("empty ID should not be owner")
	}
}

func TestGame_PlayerName(t *testing.T) {
	g := NewGame(1, time.Minute, "en")
	p := g.AddPlayer("alice")

	name, ok := g.PlayerName(p.ID)
	if !ok {
		t.Fatal("PlayerName should find player")
	}
	if name != "alice" {
		t.Errorf("PlayerName %q, want alice", name)
	}

	_, ok = g.PlayerName("nonexistent")
	if ok {
		t.Error("PlayerName should return false for unknown ID")
	}
}
