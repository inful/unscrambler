package realtime

import (
	"testing"
	"time"
)

func TestTimedRounds_NextWake_NotStarted(t *testing.T) {
	var tr TimedRounds
	tr.Rounds = 2
	tr.Duration = time.Minute
	tr.Cooldown = DefaultCooldown
	next, ok := tr.NextWake(time.Now().UTC())
	if ok {
		t.Error("NextWake should return false when not started")
	}
	if !next.IsZero() {
		t.Error("next should be zero")
	}
}

func TestTimedRounds_NextWake_ActiveRound(t *testing.T) {
	now := time.Now().UTC()
	tr := TimedRounds{
		Rounds:       2,
		Duration:     100 * time.Millisecond,
		Cooldown:     50 * time.Millisecond,
		CurrentRound: 1,
		RoundStarted: now,
	}
	next, ok := tr.NextWake(now)
	if !ok {
		t.Fatal("NextWake should return true when active")
	}
	want := now.Add(100 * time.Millisecond)
	if next.Before(want.Add(-time.Millisecond)) || next.After(want.Add(time.Millisecond)) {
		t.Errorf("next %v, want ~%v", next, want)
	}
}

func TestTimedRounds_Advance_EndRoundThenNext(t *testing.T) {
	now := time.Now().UTC()
	tr := TimedRounds{
		Rounds:       2,
		Duration:     50 * time.Millisecond,
		Cooldown:     20 * time.Millisecond,
		CurrentRound: 1,
		RoundStarted: now,
	}
	// Before round end: no advance
	advanced, finished := tr.Advance(now.Add(10 * time.Millisecond))
	if advanced || finished {
		t.Error("should not advance before round end")
	}
	// At round end: set RoundEndedAt
	advanced, finished = tr.Advance(now.Add(60 * time.Millisecond))
	if !advanced || finished {
		t.Errorf("advanced=%v finished=%v, want true false", advanced, finished)
	}
	if tr.RoundEndedAt.IsZero() {
		t.Error("RoundEndedAt should be set")
	}
	// Before cooldown: no advance to next round
	advanced, finished = tr.Advance(now.Add(65 * time.Millisecond))
	if advanced || finished {
		t.Error("should not advance before cooldown")
	}
	// After cooldown: advance to round 2
	advanced, finished = tr.Advance(now.Add(100 * time.Millisecond))
	if !advanced || finished {
		t.Errorf("advanced=%v finished=%v, want true false", advanced, finished)
	}
	if tr.CurrentRound != 2 {
		t.Errorf("CurrentRound %d, want 2", tr.CurrentRound)
	}
	if !tr.RoundEndedAt.IsZero() {
		t.Error("RoundEndedAt should be cleared for next round")
	}
}

func TestTimedRounds_Advance_Finish(t *testing.T) {
	now := time.Now().UTC()
	tr := TimedRounds{
		Rounds:       1,
		Duration:     50 * time.Millisecond,
		Cooldown:     20 * time.Millisecond,
		CurrentRound: 1,
		RoundStarted: now,
	}
	tr.Advance(now.Add(60 * time.Millisecond)) // end round
	advanced, finished := tr.Advance(now.Add(100 * time.Millisecond))
	if !advanced || !finished {
		t.Errorf("advanced=%v finished=%v, want true true", advanced, finished)
	}
}

func TestTimedRounds_Start(t *testing.T) {
	now := time.Now().UTC()
	tr := TimedRounds{
		Rounds:   3,
		Duration: time.Minute,
		Cooldown: DefaultCooldown,
	}
	tr.Start(now)
	if tr.CurrentRound != 1 {
		t.Errorf("CurrentRound %d, want 1", tr.CurrentRound)
	}
	if tr.RoundStarted != now {
		t.Error("RoundStarted not set")
	}
	if !tr.RoundEndedAt.IsZero() {
		t.Error("RoundEndedAt should be zero")
	}
}
