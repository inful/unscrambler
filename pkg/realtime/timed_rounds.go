package realtime

import "time"

// TimedRounds holds the timing state for a sequence of rounds: duration per round,
// cooldown between rounds, and when the current round started/ended.
// It does not hold game-specific state (players, scores, etc.); the game composes
// it and reacts to Advance(now) by updating its own state.
type TimedRounds struct {
	Duration     time.Duration
	Cooldown     time.Duration
	Rounds       int
	CurrentRound int
	RoundStarted time.Time
	RoundEndedAt time.Time
}

// DefaultCooldown is the usual pause between rounds (e.g. 5s).
const DefaultCooldown = 5 * time.Second

// NextWake returns the next time the round state should advance, and whether
// the schedule is active (RoundStarted set). If not active, returns (zero, false).
func (t *TimedRounds) NextWake(now time.Time) (time.Time, bool) {
	if t.RoundStarted.IsZero() {
		return time.Time{}, false
	}
	if t.RoundEndedAt.IsZero() {
		return t.RoundStarted.Add(t.Duration), true
	}
	next := t.RoundEndedAt.Add(t.Cooldown)
	if now.After(next) {
		return now, true
	}
	return next, true
}

// Advance updates timing state based on now. It may set RoundEndedAt (round time
// expired), or advance to the next round (clear RoundEndedAt, increment
// CurrentRound, set RoundStarted = now). The caller should update game state
// when advanced is true (e.g. reset player progress, publish events); when
// finished is true, the game is done (e.g. set status to finished).
func (t *TimedRounds) Advance(now time.Time) (advanced bool, finished bool) {
	if t.RoundStarted.IsZero() {
		return false, false
	}
	roundEnd := t.RoundStarted.Add(t.Duration)
	if t.RoundEndedAt.IsZero() && now.After(roundEnd) {
		t.RoundEndedAt = roundEnd
		return true, false
	}
	if t.RoundEndedAt.IsZero() {
		return false, false
	}
	if now.Before(t.RoundEndedAt.Add(t.Cooldown)) {
		return false, false
	}
	if t.CurrentRound >= t.Rounds {
		return true, true
	}
	t.CurrentRound++
	t.RoundStarted = now
	t.RoundEndedAt = time.Time{}
	return true, false
}

// Start begins the first round at now. Call when the game leaves lobby.
func (t *TimedRounds) Start(now time.Time) {
	t.CurrentRound = 1
	t.RoundStarted = now
	t.RoundEndedAt = time.Time{}
}
