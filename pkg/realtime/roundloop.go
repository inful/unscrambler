package realtime

import "time"

// RoundLoopState is implemented by game state that uses timed rounds and
// advances on a schedule. Games that implement it can use TickFromRoundLoopState
// to drive RunLoop without writing custom tick logic.
type RoundLoopState interface {
	NextTimer(now time.Time) (time.Time, bool)
	AdvanceIfNeeded(now time.Time) bool
}

// TickFromRoundLoopState returns a TickFunc that calls NextTimer, then
// AdvanceIfNeeded; when the state advances it publishes eventsOnAdvance and
// recomputes the next wake time. The returned tick does not handle nil state—
// the caller should wrap it (e.g. if state == nil { return zero, nil, true }).
func TickFromRoundLoopState[G RoundLoopState](eventsOnAdvance []string) TickFunc[G] {
	return func(state G, now time.Time) (time.Time, []string, bool) {
		next, ok := state.NextTimer(now)
		if !ok {
			return time.Time{}, nil, true
		}
		advanced := state.AdvanceIfNeeded(now)
		if advanced {
			next2, ok2 := state.NextTimer(now)
			if !ok2 {
				return time.Time{}, nil, true
			}
			return next2, eventsOnAdvance, false
		}
		return next, nil, false
	}
}
