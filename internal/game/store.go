package game

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"dagame/pkg/gamecommon"
	"dagame/pkg/realtime"
)

// Store holds games and delegates to realtime.GameStore for persistence and broadcast.
type Store struct {
	*realtime.GameStore[*Game]
}

// NewStore creates an in-memory game store with SSE broadcasters.
func NewStore() *Store {
	return &Store{GameStore: realtime.NewGameStore[*Game]()}
}

// CreateGame initializes a game and registers its broadcaster.
func (s *Store) CreateGame(rounds int, duration time.Duration, lang string) *Game {
	g := NewGame(rounds, duration, lang)
	s.Create(g.ID, g)
	return g
}

// EnsureRoundLoop starts the timing loop for a game if not already running.
func (s *Store) EnsureRoundLoop(id string, _ *Game) {
	s.GameStore.EnsureRoundLoop(id, []string{"round", "scores", "players"})
}

// WakeRoundLoop unblocks the round loop so it recomputes (e.g. after early round end).
func (s *Store) WakeRoundLoop(id string) {
	s.Wake(id)
}

func NewGame(rounds int, duration time.Duration, lang string) *Game {
	if lang == "" {
		lang = "en"
	}
	roundData := BuildRounds(lang, rounds)
	return &Game{
		BaseGame: gamecommon.BaseGame{
			ID:        gamecommon.NewGameID(),
			CreatedAt: time.Now().UTC(),
			TimedRounds: realtime.TimedRounds{
				Rounds:   rounds,
				Duration: duration,
				Cooldown: realtime.DefaultCooldown,
			},
			Status:  gamecommon.StatusLobby,
			Lang:    lang,
			Players: make(map[string]*gamecommon.BasePlayer),
		},
		RoundData: roundData,
		Progress:  make(map[string]int),
	}
}

// Game holds the state for a single session. It embeds gamecommon.BaseGame; per-round
// progress (correct letters) is in Progress keyed by player ID.
type Game struct {
	gamecommon.BaseGame
	RoundData []Round
	Progress  map[string]int
}

// Round describes a single word and its scrambled version.
type Round struct {
	Word      string
	Scrambled string
}

// Player is the per-session player type for this game (alias for gamecommon.BasePlayer).
type Player = gamecommon.BasePlayer

// AddPlayer registers a player and assigns ownership if unset.
func (g *Game) AddPlayer(username string) *Player {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	return gamecommon.AddPlayer(g.Players, &g.OwnerID, username)
}

// Start begins round one if the game is in the lobby.
func (g *Game) Start(now time.Time) error {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	if g.Status != gamecommon.StatusLobby {
		return errors.New("game already started")
	}
	if len(g.Players) < gamecommon.MinPlayers {
		return errors.New("need at least " + strconv.Itoa(gamecommon.MinPlayers) + " players to start")
	}
	g.Status = gamecommon.StatusInProgress
	g.TimedRounds.Start(now)
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	for id := range g.Players {
		g.Progress[id] = 0
	}
	return nil
}

// Restart resets rounds and scores while keeping the same session ID.
func (g *Game) Restart(now time.Time) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	g.RoundData = BuildRounds(g.Lang, g.TimedRounds.Rounds)
	g.Status = gamecommon.StatusInProgress
	g.TimedRounds.Start(now)
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	for _, player := range g.Players {
		player.Points = 0
	}
	for id := range g.Players {
		g.Progress[id] = 0
	}
}

// AdvanceIfNeeded moves the game to the next round if timing conditions are met.
func (g *Game) AdvanceIfNeeded(now time.Time) bool {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	return g.advanceIfNeededLocked(now)
}

func (g *Game) advanceIfNeededLocked(now time.Time) bool {
	if g.Status != gamecommon.StatusInProgress || g.TimedRounds.RoundStarted.IsZero() {
		return false
	}
	advanced, finished := g.TimedRounds.Advance(now)
	if finished {
		g.Status = gamecommon.StatusFinished
		return true
	}
	if advanced {
		g.RoundWinnerID = ""
		g.RoundSolvedAt = time.Time{}
		for id := range g.Players {
			g.Progress[id] = 0
		}
	}
	return advanced
}

// CurrentRoundData returns the word data for the current round.
func (g *Game) CurrentRoundData() Round {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	return g.currentRoundDataLocked()
}

func (g *Game) currentRoundDataLocked() Round {
	if g.TimedRounds.CurrentRound == 0 || g.TimedRounds.CurrentRound > len(g.RoundData) {
		return Round{}
	}
	return g.RoundData[g.TimedRounds.CurrentRound-1]
}

// SubmitGuess validates a guess, awards points, and ends the round on success.
func (g *Game) SubmitGuess(playerID string, guess string, now time.Time) (bool, error) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	if g.Status != gamecommon.StatusInProgress {
		return false, errors.New("game not in progress")
	}
	if g.TimedRounds.RoundStarted.IsZero() {
		return false, errors.New("round not started")
	}
	_ = g.advanceIfNeededLocked(now)
	if g.Status != gamecommon.StatusInProgress {
		return false, nil
	}
	if !g.TimedRounds.RoundEndedAt.IsZero() {
		return false, nil
	}
	if g.RoundWinnerID != "" {
		return false, nil
	}
	player, ok := g.Players[playerID]
	if !ok {
		return false, errors.New("player not found")
	}
	normalized := strings.ToLower(strings.TrimSpace(guess))
	normalized = strings.ReplaceAll(normalized, " ", "")
	round := g.currentRoundDataLocked()
	if normalized == "" || round.Word == "" {
		return false, nil
	}
	if normalized != round.Word {
		return false, nil
	}
	points := 1
	halfTime := g.TimedRounds.RoundStarted.Add(g.TimedRounds.Duration / 2)
	if now.Before(halfTime) {
		points = 2
	}
	player.Points += points
	g.Progress[playerID] = len(round.Word)
	g.RoundWinnerID = playerID
	g.RoundSolvedAt = now
	g.TimedRounds.RoundEndedAt = now
	return true, nil
}

// NextTimer returns the next time the round state should advance.
func (g *Game) NextTimer(now time.Time) (time.Time, bool) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	if g.Status != gamecommon.StatusInProgress {
		return time.Time{}, false
	}
	return g.TimedRounds.NextWake(now)
}

// UpdateProgress stores a player's correct letter count for the current round.
func (g *Game) UpdateProgress(playerID string, correct int, now time.Time) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	if g.Status != gamecommon.StatusInProgress {
		return
	}
	g.advanceIfNeededLocked(now)
	if g.Status != gamecommon.StatusInProgress || !g.TimedRounds.RoundEndedAt.IsZero() {
		return
	}
	round := g.currentRoundDataLocked()
	if round.Word == "" {
		return
	}
	if correct < 0 {
		correct = 0
	}
	if correct > len(round.Word) {
		correct = len(round.Word)
	}
	if _, ok := g.Players[playerID]; !ok {
		return
	}
	g.Progress[playerID] = correct
}

// PlayerName resolves a player's display name by ID.
func (g *Game) PlayerName(playerID string) (string, bool) {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	player, ok := g.Players[playerID]
	if !ok {
		return "", false
	}
	return player.Username, true
}

// IsOwner reports whether the given player ID owns the session.
func (g *Game) IsOwner(playerID string) bool {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	return gamecommon.IsOwner(g.OwnerID, playerID)
}

// PlayerNames returns a snapshot of all player names.
func (g *Game) PlayerNames() []string {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	players := make([]string, 0, len(g.Players))
	for _, player := range g.Players {
		players = append(players, player.Username)
	}
	return players
}

// Snapshot captures the state needed for rendering UI fragments.
type Snapshot struct {
	ID            string
	Status        string
	CurrentRound  int
	Rounds        int
	RoundDuration time.Duration
	RoundStarted  time.Time
	RoundData     Round
	RoundWinner   string
	RoundEndedAt  time.Time
	NextRoundAt   time.Time
	Players       []string
	Progress      []PlayerProgress
	WordLength    int
	Scores        []ScoreEntry
	WinnerName    string
}

// Snapshot returns a consistent view of the current game state.
func (g *Game) Snapshot(now time.Time) Snapshot {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	g.advanceIfNeededLocked(now)
	players := make([]string, 0, len(g.Players))
	scores := make([]ScoreEntry, 0, len(g.Players))
	progress := make([]PlayerProgress, 0, len(g.Players))
	for id, player := range g.Players {
		players = append(players, player.Username)
		scores = append(scores, ScoreEntry{
			Name:   player.Username,
			Points: player.Points,
		})
		progress = append(progress, PlayerProgress{
			Name:    player.Username,
			Correct: g.Progress[id],
		})
	}
	sortScores(scores)
	sortProgress(progress)
	roundWinner := ""
	if g.RoundWinnerID != "" {
		if winner, ok := g.Players[g.RoundWinnerID]; ok {
			roundWinner = winner.Username
		}
	}
	var nextRoundAt time.Time
	if !g.TimedRounds.RoundEndedAt.IsZero() {
		nextRoundAt = g.TimedRounds.RoundEndedAt.Add(g.TimedRounds.Cooldown)
	}
	winnerName := ""
	if g.Status == gamecommon.StatusFinished {
		winnerName = resolveWinner(scores)
	}
	wordLength := 0
	if round := g.currentRoundDataLocked(); round.Word != "" {
		wordLength = len(round.Word)
	}
	return Snapshot{
		ID:            g.ID,
		Status:        g.Status,
		CurrentRound:  g.TimedRounds.CurrentRound,
		Rounds:        g.TimedRounds.Rounds,
		RoundDuration: g.TimedRounds.Duration,
		RoundStarted:  g.TimedRounds.RoundStarted,
		RoundData:     g.currentRoundDataLocked(),
		RoundWinner:   roundWinner,
		RoundEndedAt:  g.TimedRounds.RoundEndedAt,
		NextRoundAt:   nextRoundAt,
		Players:       players,
		Progress:      progress,
		WordLength:    wordLength,
		Scores:        scores,
		WinnerName:    winnerName,
	}
}

// ScoreEntry represents a player's total points.
type ScoreEntry struct {
	Name   string
	Points int
}

// PlayerProgress represents a player's correct letter count.
type PlayerProgress struct {
	Name    string
	Correct int
}

func sortScores(scores []ScoreEntry) {
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Points == scores[j].Points {
			return scores[i].Name < scores[j].Name
		}
		return scores[i].Points > scores[j].Points
	})
}

func resolveWinner(scores []ScoreEntry) string {
	if len(scores) == 0 {
		return ""
	}
	top := scores[0].Points
	if top == 0 {
		return "No winner"
	}
	winners := make([]string, 0, len(scores))
	for _, entry := range scores {
		if entry.Points != top {
			break
		}
		winners = append(winners, entry.Name)
	}
	if len(winners) == 1 {
		return winners[0]
	}
	return "Tie: " + strings.Join(winners, ", ")
}

func sortProgress(entries []PlayerProgress) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Correct == entries[j].Correct {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Correct > entries[j].Correct
	})
}

