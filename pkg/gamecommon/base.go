package gamecommon

import (
	"sync"
	"time"

	"dagame/pkg/id"
	"dagame/pkg/realtime"
)

// BasePlayer holds common per-player state shared across game types.
type BasePlayer struct {
	ID       string
	Username string
	JoinedAt time.Time
	Points   int
}

// BaseGame holds common game state: ID, timing, status, owner, players, and round winner.
// Games embed BaseGame and add their own fields (RoundData, Canvas, Progress map, etc.).
type BaseGame struct {
	Mu             sync.Mutex
	ID             string
	CreatedAt      time.Time
	TimedRounds    realtime.TimedRounds
	Status         string
	Lang           string
	OwnerID        string
	Players        map[string]*BasePlayer
	RoundWinnerID  string
	RoundSolvedAt  time.Time
}

// AddPlayer registers a new player in players and sets ownerID to the new player's ID if
// ownerID is empty. Caller must hold the game lock. Returns the new BasePlayer.
func AddPlayer(players map[string]*BasePlayer, ownerID *string, username string) *BasePlayer {
	p := &BasePlayer{
		ID:       id.NewID(),
		Username: username,
		JoinedAt: time.Now().UTC(),
	}
	players[p.ID] = p
	if *ownerID == "" {
		*ownerID = p.ID
	}
	return p
}

// IsOwner returns whether playerID is the game owner.
func IsOwner(ownerID, playerID string) bool {
	return playerID != "" && playerID == ownerID
}

// NewGameID returns a new unique game ID (e.g. for BaseGame.ID).
func NewGameID() string {
	return id.NewID()
}

// MinPlayers is the minimum number of players required to start a game.
const MinPlayers = 2
