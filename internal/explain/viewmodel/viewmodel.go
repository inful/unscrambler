// Package viewmodel defines the view-layer types for the explain game.
// These types are intentionally free of game-logic imports so that templ
// templates can use them without creating an import cycle.
package viewmodel

// PlayerInfo describes a player as rendered in the UI.
type PlayerInfo struct {
	ID          string
	Name        string
	IsExplainer bool
}

// CanvasItem is an emoji placed on the canvas.
type CanvasItem struct {
	ID    string
	Emoji string
	X     float64
	Y     float64
}

// ScoreEntry holds one player's running score.
type ScoreEntry struct {
	Name   string
	Points int
}

// SnapData is a view-friendly representation of the current game snapshot.
// It is populated by the handler from the domain Snapshot and then passed to
// templ components.
type SnapData struct {
	Status           string
	CurrentRound     int
	Rounds           int
	RoundDurationSec int
	RoundStartedMs   int64 // Unix milliseconds; drives the client-side countdown
	NextRoundAtMs    int64 // Unix milliseconds; drives the "next round in" countdown
	ExplainerName    string
	RoundWinnerName  string
	WinnerName       string
	IsExplainer      bool
	IsGuesser        bool
	Word             string // non-empty only for the explainer
	RevealedWord     string
	WordLength       int
	Canvas           []CanvasItem
	RoundEmojis      []string
	Players          []PlayerInfo
	Scores           []ScoreEntry

	// Lobby-only fields computed by the handler.
	ShowStart   bool
	PlayerCount int
	MinPlayers  int
}

// GamePageData carries everything the full game page template needs.
type GamePageData struct {
	GameID     string
	InviteURL  string
	HasPlayer  bool
	PlayerName string
	PlayerID   string
	Snap       SnapData
}
