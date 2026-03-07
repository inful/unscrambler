// Package viewmodel defines the view-layer types for the draw game.
package viewmodel

// PlayerInfo describes a player as rendered in the UI.
type PlayerInfo struct {
	ID           string
	Name         string
	IsExplainer  bool
}

// Point is a 2D point for drawing.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// DrawStrokeView is a single stroke for the view (list of points and color).
type DrawStrokeView struct {
	Points []Point `json:"points"`
	Color  string  `json:"color,omitempty"`
}

// ScoreEntry holds one player's running score.
type ScoreEntry struct {
	Name   string
	Points int
}

// SnapData is a view-friendly representation of the current game snapshot.
type SnapData struct {
	Status           string
	CurrentRound     int
	Rounds           int
	RoundDurationSec int
	RoundStartedMs   int64
	NextRoundAtMs    int64
	ExplainerName    string
	RoundWinnerName  string
	WinnerName       string
	IsExplainer      bool
	IsGuesser        bool
	Word             string
	RevealedWord     string
	WordLength       int
	Strokes          []DrawStrokeView
	StrokesJSON      string // JSON-encoded strokes for data attribute in canvas fragment
	Players          []PlayerInfo
	Scores           []ScoreEntry

	ShowStart   bool
	PlayerCount int
	MinPlayers  int

	CurrentPlayerName string
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
