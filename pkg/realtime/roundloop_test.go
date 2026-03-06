package realtime

import (
	"testing"
	"time"
)

// mockRoundLoopState implements RoundLoopState for tests.
type mockRoundLoopState struct {
	nextTime    time.Time
	hasNext     bool
	advance     bool
	advanceCall int
}

func (m *mockRoundLoopState) NextTimer(now time.Time) (time.Time, bool) {
	return m.nextTime, m.hasNext
}

func (m *mockRoundLoopState) AdvanceIfNeeded(now time.Time) bool {
	m.advanceCall++
	return m.advance
}

func TestTickFromRoundLoopState_NoNextTimer_Stops(t *testing.T) {
	m := &mockRoundLoopState{hasNext: false}
	tick := TickFromRoundLoopState[*mockRoundLoopState]([]string{"a"})
	next, events, stop := tick(m, time.Now().UTC())
	if !stop {
		t.Error("expected stop when NextTimer returns false")
	}
	if len(events) != 0 {
		t.Errorf("expected no events, got %v", events)
	}
	if !next.IsZero() {
		t.Errorf("expected zero next time, got %v", next)
	}
}

func TestTickFromRoundLoopState_NoAdvance_ReturnsNextOnly(t *testing.T) {
	now := time.Now().UTC()
	wantNext := now.Add(time.Minute)
	m := &mockRoundLoopState{nextTime: wantNext, hasNext: true, advance: false}
	tick := TickFromRoundLoopState[*mockRoundLoopState]([]string{"round", "scores"})
	next, events, stop := tick(m, now)
	if stop {
		t.Error("expected !stop when NextTimer returns true and no advance")
	}
	if next != wantNext {
		t.Errorf("next = %v, want %v", next, wantNext)
	}
	if len(events) != 0 {
		t.Errorf("expected no events when no advance, got %v", events)
	}
}

func TestTickFromRoundLoopState_Advance_PublishesEventsAndRecomputesNext(t *testing.T) {
	now := time.Now().UTC()
	m := &mockRoundLoopState{
		nextTime: now.Add(time.Minute),
		hasNext:  true,
		advance:  true,
	}
	// Second call to NextTimer (after advance) - we need to return something.
	// Our mock always returns the same nextTime/hasNext. So after AdvanceIfNeeded
	// we'll call NextTimer again and get (nextTime, true). So next should be nextTime.
	tick := TickFromRoundLoopState[*mockRoundLoopState]([]string{"round", "scores", "players"})
	next, events, stop := tick(m, now)
	if stop {
		t.Error("expected !stop")
	}
	if next != m.nextTime {
		t.Errorf("next = %v, want %v", next, m.nextTime)
	}
	if len(events) != 3 || events[0] != "round" || events[1] != "scores" || events[2] != "players" {
		t.Errorf("events = %v, want [round scores players]", events)
	}
	// AdvanceIfNeeded was called once
	if m.advanceCall != 1 {
		t.Errorf("advanceCall = %d, want 1", m.advanceCall)
	}
}

// countingMock returns hasNext false on the second NextTimer call (e.g. game finished).
type countingMock struct {
	nextCalls  int
	nextTime   time.Time
	advance    bool
	advanceCall int
}

func (c *countingMock) NextTimer(now time.Time) (time.Time, bool) {
	c.nextCalls++
	if c.nextCalls >= 2 {
		return time.Time{}, false // game finished
	}
	return c.nextTime, true
}

func (c *countingMock) AdvanceIfNeeded(now time.Time) bool {
	c.advanceCall++
	return c.advance
}

func TestTickFromRoundLoopState_AdvanceThenNoNext_Stops(t *testing.T) {
	now := time.Now().UTC()
	m := &countingMock{nextTime: now.Add(time.Minute), advance: true}
	tick := TickFromRoundLoopState[*countingMock]([]string{"done"})
	next, events, stop := tick(m, now)
	if !stop {
		t.Error("expected stop when NextTimer returns false after advance (game finished)")
	}
	if len(events) != 0 {
		t.Errorf("expected no events when stopping, got %v", events)
	}
	if !next.IsZero() {
		t.Errorf("next should be zero when stop, got %v", next)
	}
	if m.advanceCall != 1 || m.nextCalls != 2 {
		t.Errorf("expected AdvanceIfNeeded once and NextTimer twice, got advanceCall=%d nextCalls=%d", m.advanceCall, m.nextCalls)
	}
}
