// Package gameview defines shared view types used by common UI components
// across scramble, explain, and draw games.
package gameview

// ScoreEntry holds a player's score for rendering in the shared ScoresCard.
type ScoreEntry struct {
	Name   string
	Points int
}

// PlayerInfo holds a player's display info for the shared PlayersCard.
// Optional fields: Correct/WordLength for scramble progress; Role for explain/draw (e.g. "explainer", "guesser", "drawer").
type PlayerInfo struct {
	ID          string
	Name        string
	Correct     int  // scramble: correct letters this round
	WordLength  int  // scramble: total letters (show Correct/WordLength when > 0)
	Role        string // explain/draw: "explainer", "guesser", "drawer", etc.
}
