package game

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	StatusLobby      = "lobby"
	StatusInProgress = "in_progress"
	StatusFinished   = "finished"
)

type Store struct {
	mu    sync.RWMutex
	games map[string]*Game
	loops map[string]context.CancelFunc
	hubs  map[string]*Broadcaster
	wakes map[string]chan struct{}
}

func NewStore() *Store {
	return &Store{
		games: make(map[string]*Game),
		loops: make(map[string]context.CancelFunc),
		hubs:  make(map[string]*Broadcaster),
		wakes: make(map[string]chan struct{}),
	}
}

func (s *Store) CreateGame(rounds int, duration time.Duration) *Game {
	game := NewGame(rounds, duration)
	s.mu.Lock()
	s.games[game.ID] = game
	s.hubs[game.ID] = NewBroadcaster()
	s.mu.Unlock()
	return game
}

func (s *Store) GetGame(id string) (*Game, bool) {
	s.mu.RLock()
	game, ok := s.games[id]
	s.mu.RUnlock()
	return game, ok
}

func (s *Store) Broadcaster(id string) *Broadcaster {
	s.mu.Lock()
	defer s.mu.Unlock()
	hub, ok := s.hubs[id]
	if !ok {
		hub = NewBroadcaster()
		s.hubs[id] = hub
	}
	return hub
}

func (s *Store) Publish(id string, event string) {
	hub := s.Broadcaster(id)
	hub.Publish(event)
}

func (s *Store) EnsureRoundLoop(id string, game *Game) {
	s.mu.Lock()
	if _, ok := s.loops[id]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.loops[id] = cancel
	wake := make(chan struct{}, 1)
	s.wakes[id] = wake
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.loops, id)
			delete(s.wakes, id)
			s.mu.Unlock()
		}()

		for {
			next, ok := game.NextTimer(time.Now().UTC())
			if !ok {
				return
			}
			wait := time.Until(next)
			if wait < 0 {
				wait = 0
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				if game.AdvanceIfNeeded(time.Now().UTC()) {
					s.Publish(id, "round")
					s.Publish(id, "scores")
					s.Publish(id, "players")
				}
			case <-wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				continue
			}
		}
	}()
}

func (s *Store) WakeRoundLoop(id string) {
	s.mu.RLock()
	wake, ok := s.wakes[id]
	s.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func NewGame(rounds int, duration time.Duration) *Game {
	roundData := BuildRounds(rounds)
	return &Game{
		ID:            newID(),
		CreatedAt:     time.Now().UTC(),
		Rounds:        rounds,
		RoundDuration: duration,
		RoundData:     roundData,
		Status:        StatusLobby,
		Players:       make(map[string]*Player),
	}
}

type Game struct {
	mu            sync.Mutex
	ID            string
	CreatedAt     time.Time
	Rounds        int
	RoundDuration time.Duration
	RoundData     []Round
	Status        string
	CurrentRound  int
	RoundStarted  time.Time
	RoundWinnerID string
	RoundSolvedAt time.Time
	RoundEndedAt  time.Time
	OwnerID       string
	Players       map[string]*Player
}

type Round struct {
	Word      string
	Scrambled string
}

type Player struct {
	ID       string
	Username string
	JoinedAt time.Time
	Points   int
	Progress int
}

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

func (g *Game) Start(now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusLobby {
		return errors.New("game already started")
	}
	g.Status = StatusInProgress
	g.CurrentRound = 1
	g.RoundStarted = now
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	g.RoundEndedAt = time.Time{}
	for _, player := range g.Players {
		player.Progress = 0
	}
	return nil
}

func (g *Game) Restart(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RoundData = BuildRounds(g.Rounds)
	g.Status = StatusInProgress
	g.CurrentRound = 1
	g.RoundStarted = now
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	g.RoundEndedAt = time.Time{}
	for _, player := range g.Players {
		player.Points = 0
		player.Progress = 0
	}
}

func (g *Game) AdvanceIfNeeded(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.advanceIfNeededLocked(now)
}

func (g *Game) advanceIfNeededLocked(now time.Time) bool {
	changed := false
	if g.Status != StatusInProgress || g.RoundStarted.IsZero() {
		return false
	}
	roundEnd := g.RoundStarted.Add(g.RoundDuration)
	if g.RoundEndedAt.IsZero() && now.After(roundEnd) {
		g.RoundEndedAt = roundEnd
		changed = true
	}
	if g.RoundEndedAt.IsZero() {
		return changed
	}
	if now.Before(g.RoundEndedAt.Add(5 * time.Second)) {
		return changed
	}
	if g.CurrentRound >= g.Rounds {
		g.Status = StatusFinished
		return true
	}
	g.CurrentRound++
	g.RoundStarted = now
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
	g.RoundEndedAt = time.Time{}
	for _, player := range g.Players {
		player.Progress = 0
	}
	changed = true
	return changed
}

func (g *Game) CurrentRoundData() Round {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.currentRoundDataLocked()
}

func (g *Game) currentRoundDataLocked() Round {
	if g.CurrentRound == 0 || g.CurrentRound > len(g.RoundData) {
		return Round{}
	}
	return g.RoundData[g.CurrentRound-1]
}

func (g *Game) SubmitGuess(playerID string, guess string, now time.Time) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return false, errors.New("game not in progress")
	}
	if g.RoundStarted.IsZero() {
		return false, errors.New("round not started")
	}
	_ = g.advanceIfNeededLocked(now)
	if g.Status != StatusInProgress {
		return false, nil
	}
	if !g.RoundEndedAt.IsZero() {
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
	halfTime := g.RoundStarted.Add(g.RoundDuration / 2)
	if now.Before(halfTime) {
		points = 2
	}
	player.Points += points
	player.Progress = len(round.Word)
	g.RoundWinnerID = playerID
	g.RoundSolvedAt = now
	g.RoundEndedAt = now
	return true, nil
}

func (g *Game) NextTimer(now time.Time) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress || g.RoundStarted.IsZero() {
		return time.Time{}, false
	}
	if g.RoundEndedAt.IsZero() {
		return g.RoundStarted.Add(g.RoundDuration), true
	}
	next := g.RoundEndedAt.Add(5 * time.Second)
	if now.After(next) {
		return now, true
	}
	return next, true
}

func (g *Game) UpdateProgress(playerID string, correct int, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return
	}
	g.advanceIfNeededLocked(now)
	if g.Status != StatusInProgress || !g.RoundEndedAt.IsZero() {
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

func (g *Game) PlayerName(playerID string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	player, ok := g.Players[playerID]
	if !ok {
		return "", false
	}
	return player.Username, true
}

func (g *Game) IsOwner(playerID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return playerID != "" && playerID == g.OwnerID
}

func (g *Game) PlayerNames() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	players := make([]string, 0, len(g.Players))
	for _, player := range g.Players {
		players = append(players, player.Username)
	}
	return players
}

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
	if !g.RoundEndedAt.IsZero() {
		nextRoundAt = g.RoundEndedAt.Add(5 * time.Second)
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
		CurrentRound:  g.CurrentRound,
		Rounds:        g.Rounds,
		RoundDuration: g.RoundDuration,
		RoundStarted:  g.RoundStarted,
		RoundData:     g.currentRoundDataLocked(),
		RoundWinner:   roundWinner,
		RoundEndedAt:  g.RoundEndedAt,
		NextRoundAt:   nextRoundAt,
		Players:       players,
		Progress:      progress,
		WordLength:    wordLength,
		Scores:        scores,
		WinnerName:    winnerName,
	}
}

type ScoreEntry struct {
	Name   string
	Points int
}

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
