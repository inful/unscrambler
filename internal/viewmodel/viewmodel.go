package viewmodel

// LanguageOption is a language choice for the create-game form.
type LanguageOption struct {
	Code  string
	Label string
}

// GamePage holds data for the main game page template.
type GamePage struct {
	Title          string
	GameID         string
	InviteURL      string
	Players        []PlayerProgress
	HasPlayer      bool
	PlayerName     string
	IsOwner        bool
	Rounds         int
	RoundDuration  int
	Status         string
	ShowStart      bool
	Scores         []ScoreEntry
	WinnerName     string
	CurrentRound   int
	TotalRounds    int
	RoundStartedMs int64
	Scrambled      string
	TargetWord     string
	WordLength     int
}

// RoundFragment holds data for the round UI fragment.
type RoundFragment struct {
	GameID         string
	Status         string
	CurrentRound   int
	TotalRounds    int
	RoundStartedMs int64
	DurationSec    int
	Scrambled      string
	TargetWord     string
	Expired        bool
	RoundWinner    string
	RoundEndedMs   int64
	NextRoundMs    int64
	RoundLocked    bool
	RoundKey       string
}

// ScoreEntry holds a player's score for rendering.
type ScoreEntry struct {
	Name   string
	Points int
}

// ScoresFragment holds data for the scores panel.
type ScoresFragment struct {
	GameID     string
	Scores     []ScoreEntry
	WinnerName string
	Status     string
	IsOwner    bool
}

// PlayerProgress holds a player's correct-letter progress.
type PlayerProgress struct {
	Name    string
	Correct int
}

// PlayersFragment holds data for the players panel.
type PlayersFragment struct {
	Players    []PlayerProgress
	WordLength int
	PlayerName string
}
