package game

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"dagame/pkg/realtime"
)

const (
	StatusLobby      = "lobby"
	StatusInProgress = "in_progress"
	StatusFinished   = "finished"
)

// Store holds games and delegates to realtime.RoomStore for persistence and broadcast.
type Store struct {
	r *realtime.RoomStore[*Game]
}

// NewStore creates an in-memory game store with SSE broadcasters.
func NewStore() *Store {
	return &Store{r: realtime.NewRoomStore[*Game]()}
}

// CreateGame initializes a game and registers its broadcaster.
func (s *Store) CreateGame(rounds int, duration time.Duration, lang string) *Game {
	g := NewGame(rounds, duration, lang)
	s.r.Create(g.ID, g)
	return g
}

// GetGame returns a game by ID if it exists.
func (s *Store) GetGame(id string) (*Game, bool) {
	room, ok := s.r.Get(id)
	if !ok {
		return nil, false
	}
	return room.State, ok
}

// Broadcaster returns the SSE broadcaster for a game, creating it if missing.
func (s *Store) Broadcaster(id string) *realtime.Broadcaster {
	return s.r.Broadcaster(id)
}

// Publish notifies subscribers of a game update with a typed event.
func (s *Store) Publish(id string, event string) {
	s.r.Publish(id, event)
}

// EnsureRoundLoop starts the timing loop for a game if not already running.
func (s *Store) EnsureRoundLoop(id string, _ *Game) {
	getState := func() *Game {
		room, ok := s.r.Get(id)
		if !ok {
			return nil
		}
		return room.State
	}
	tick := func(state *Game, now time.Time) (time.Time, []string, bool) {
		if state == nil {
			return time.Time{}, nil, true
		}
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
			return next2, []string{"round", "scores", "players"}, false
		}
		return next, nil, false
	}
	s.r.RunLoop(id, getState, tick)
}

// WakeRoundLoop unblocks the round loop so it recomputes (e.g. after early round end).
func (s *Store) WakeRoundLoop(id string) {
	s.r.Wake(id)
}

func NewGame(rounds int, duration time.Duration, lang string) *Game {
	if lang == "" {
		lang = "en"
	}
	roundData := BuildRounds(lang, rounds)
	return &Game{
		ID:        newID(),
		CreatedAt: time.Now().UTC(),
		TimedRounds: realtime.TimedRounds{
			Rounds:   rounds,
			Duration: duration,
			Cooldown: realtime.DefaultCooldown,
		},
		RoundData: roundData,
		Status:    StatusLobby,
		Lang:      lang,
		Players:   make(map[string]*Player),
	}
}

// Game holds the state for a single session.
type Game struct {
	mu            sync.Mutex
	ID            string
	CreatedAt     time.Time
	TimedRounds   realtime.TimedRounds // Rounds, Duration, Cooldown, CurrentRound, RoundStarted, RoundEndedAt
	RoundData     []Round
	Status        string
	Lang          string
	RoundWinnerID string
	RoundSolvedAt time.Time
	OwnerID       string
	Players       map[string]*Player
}

// Round describes a single word and its scrambled version.
type Round struct {
	Word      string
	Scrambled string
}

// Player tracks per-session state for a participant.
type Player struct {
	ID       string
	Username string
	JoinedAt time.Time
	Points   int
	Progress int
}

// AddPlayer registers a player and assigns ownership if unset.
func (g *Game) AddPlayer(username string) *Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	player := &Player{
		ID:       newID(),
		Username: username,
		JoinedAt: time.Now().UTC(),
	}
	g.Players[player.ID] = player
	if g.OwnerID == "" {
		g.OwnerID = player.ID
	}
	return player
}

// Start begins round one if the game is in the lobby.
func (g *Game) Start(now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusLobby {
		return errors.New("game already started")
	}
	g.Status = StatusInProgress
	g.TimedRounds.Start(now)
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	for _, player := range g.Players {
		player.Progress = 0
	}
	return nil
}

// Restart resets rounds and scores while keeping the same session ID.
func (g *Game) Restart(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RoundData = BuildRounds(g.Lang, g.TimedRounds.Rounds)
	g.Status = StatusInProgress
	g.TimedRounds.Start(now)
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	for _, player := range g.Players {
		player.Points = 0
		player.Progress = 0
	}
}

// AdvanceIfNeeded moves the game to the next round if timing conditions are met.
func (g *Game) AdvanceIfNeeded(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.advanceIfNeededLocked(now)
}

func (g *Game) advanceIfNeededLocked(now time.Time) bool {
	if g.Status != StatusInProgress || g.TimedRounds.RoundStarted.IsZero() {
		return false
	}
	advanced, finished := g.TimedRounds.Advance(now)
	if finished {
		g.Status = StatusFinished
		return true
	}
	if advanced {
		g.RoundWinnerID = ""
		g.RoundSolvedAt = time.Time{}
		for _, player := range g.Players {
			player.Progress = 0
		}
	}
	return advanced
}

// CurrentRoundData returns the word data for the current round.
func (g *Game) CurrentRoundData() Round {
	g.mu.Lock()
	defer g.mu.Unlock()
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
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return false, errors.New("game not in progress")
	}
	if g.TimedRounds.RoundStarted.IsZero() {
		return false, errors.New("round not started")
	}
	_ = g.advanceIfNeededLocked(now)
	if g.Status != StatusInProgress {
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
	player.Progress = len(round.Word)
	g.RoundWinnerID = playerID
	g.RoundSolvedAt = now
	g.TimedRounds.RoundEndedAt = now
	return true, nil
}

// NextTimer returns the next time the round state should advance.
func (g *Game) NextTimer(now time.Time) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return time.Time{}, false
	}
	return g.TimedRounds.NextWake(now)
}

// UpdateProgress stores a player's correct letter count for the current round.
func (g *Game) UpdateProgress(playerID string, correct int, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return
	}
	g.advanceIfNeededLocked(now)
	if g.Status != StatusInProgress || !g.TimedRounds.RoundEndedAt.IsZero() {
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
	player, ok := g.Players[playerID]
	if !ok {
		return
	}
	player.Progress = correct
}

// PlayerName resolves a player's display name by ID.
func (g *Game) PlayerName(playerID string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	player, ok := g.Players[playerID]
	if !ok {
		return "", false
	}
	return player.Username, true
}

// IsOwner reports whether the given player ID owns the session.
func (g *Game) IsOwner(playerID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return playerID != "" && playerID == g.OwnerID
}

// PlayerNames returns a snapshot of all player names.
func (g *Game) PlayerNames() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
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
	g.mu.Lock()
	defer g.mu.Unlock()
	g.advanceIfNeededLocked(now)
	players := make([]string, 0, len(g.Players))
	scores := make([]ScoreEntry, 0, len(g.Players))
	progress := make([]PlayerProgress, 0, len(g.Players))
	for _, player := range g.Players {
		players = append(players, player.Username)
		scores = append(scores, ScoreEntry{
			Name:   player.Username,
			Points: player.Points,
		})
		progress = append(progress, PlayerProgress{
			Name:    player.Username,
			Correct: player.Progress,
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
	if g.Status == StatusFinished {
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

func newID() string {
	// 10 bytes -> 16 chars of base32, short and url-safe.
	buf := make([]byte, 10)
	_, _ = rand.Read(buf)
	encoder := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(encoder.EncodeToString(buf))
}
