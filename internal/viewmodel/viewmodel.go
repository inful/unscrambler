package viewmodel

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

type ScoreEntry struct {
	Name   string
	Points int
}

type ScoresFragment struct {
	GameID     string
	Scores     []ScoreEntry
	WinnerName string
	Status     string
	IsOwner    bool
}

type PlayerProgress struct {
	Name    string
	Correct int
}

type PlayersFragment struct {
	Players    []PlayerProgress
	WordLength int
	PlayerName string
}
